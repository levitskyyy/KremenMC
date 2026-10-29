package structure

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Jigsaw assembly from template pools. The assembly rules are inferred from
// reference piece lists and draw logs (see Assemble); terrain adaptation is
// in beard.go, placement in jigsaw_place.go.

type poolElement struct {
	kind       string // single, empty, list, feature
	raw        json.RawMessage
	location   string
	feature    string
	processors *ProcessorList
	procName   string
	procJSON   string
	rigid      bool
	legacy     bool
	list       []*poolElement
}

type pool struct {
	fallback string
	elements []*poolElement // expanded by weight
}

func loadPool(pk *wgdata.Pack, name string) (*pool, error) {
	b, ok := pk.JSON("worldgen/template_pool", qualify(name))
	if !ok {
		return nil, fmt.Errorf("structure: template pool %s missing", name)
	}
	var j struct {
		Fallback string `json:"fallback"`
		Elements []struct {
			Element json.RawMessage `json:"element"`
			Weight  int             `json:"weight"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("structure: pool %s: %w", name, err)
	}
	p := &pool{fallback: j.Fallback}
	for _, e := range j.Elements {
		el, err := parseElement(pk, e.Element)
		if err != nil {
			return nil, fmt.Errorf("structure: pool %s: %w", name, err)
		}
		for range e.Weight {
			p.elements = append(p.elements, el)
		}
	}
	return p, nil
}

func parseElement(pk *wgdata.Pack, b []byte) (*poolElement, error) {
	var j struct {
		Type       string            `json:"element_type"`
		Location   string            `json:"location"`
		Processors json.RawMessage   `json:"processors"`
		Projection string            `json:"projection"`
		Elements   []json.RawMessage `json:"elements"`
		Feature    string            `json:"feature"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	e := &poolElement{raw: append(json.RawMessage(nil), b...), location: j.Location, feature: j.Feature, rigid: j.Projection != "terrain_matching"}
	switch trimNS(j.Type) {
	case "single_pool_element", "legacy_single_pool_element":
		e.kind = "single"
		e.legacy = trimNS(j.Type) == "legacy_single_pool_element"
		if len(j.Processors) > 0 {
			var name string
			if json.Unmarshal(j.Processors, &name) == nil {
				pl, err := LoadProcessorList(name)
				if err != nil {
					return nil, err
				}
				e.processors, e.procName = pl, name
			} else {
				pl, err := ParseProcessorList(pk, j.Processors)
				if err != nil {
					return nil, err
				}
				if len(pl.procs) > 0 {
					e.processors, e.procJSON = pl, string(j.Processors)
				}
			}
		}
	case "list_pool_element":
		e.kind = "list"
		for _, s := range j.Elements {
			c, err := parseElement(pk, s)
			if err != nil {
				return nil, err
			}
			e.list = append(e.list, c)
		}
	case "empty_pool_element":
		e.kind = "empty"
	case "feature_pool_element":
		e.kind = "feature"
	default:
		return nil, fmt.Errorf("unknown pool element %s", j.Type)
	}
	return e, nil
}

// primary is the template that defines the element's box and jigsaws.
func (e *poolElement) primary() string {
	if e.kind == "list" && len(e.list) > 0 {
		return e.list[0].primary()
	}
	if e.kind == "single" {
		return e.location
	}
	return ""
}

type jigsawBlock struct {
	x, y, z      int32 // template-relative
	front, top   string
	aligned      bool
	name, target string
	pool         string
}

var dirVec = map[string][3]int32{
	"north": {0, 0, -1}, "south": {0, 0, 1}, "east": {1, 0, 0}, "west": {-1, 0, 0}, "up": {0, 1, 0}, "down": {0, -1, 0},
}
var opposite = map[string]string{"north": "south", "south": "north", "east": "west", "west": "east", "up": "down", "down": "up"}

func templateJigsaws(t *Template) []jigsawBlock {
	reg := data.Canonical()
	var out []jigsawBlock
	if len(t.Palettes) == 0 {
		return nil
	}
	for _, b := range t.Palettes[0] {
		bl := reg.BlockOf(b.State)
		if bl == nil || bl.Name != "jigsaw" {
			continue
		}
		orient := ""
		for i := range bl.Props {
			if bl.Props[i].Name == "orientation" {
				orient = bl.Props[i].Values[bl.ValueIndex(b.State, i)]
			}
		}
		front, top, _ := strings.Cut(orient, "_")
		j := jigsawBlock{x: b.X, y: b.Y, z: b.Z, front: front, top: top}
		joint, _ := b.NBT["joint"].(string)
		j.aligned = joint == "aligned"
		j.name, _ = b.NBT["name"].(string)
		j.target, _ = b.NBT["target"].(string)
		j.pool, _ = b.NBT["pool"].(string)
		out = append(out, j)
	}
	return out
}

type placedJigsaw struct {
	jp    *JigsawPiece
	tmpl  *Template // primary template; nil for a feature element
	depth int
	rigid bool
	outer *space // the space the piece was placed in
	inner *space // the piece's own box, for children connecting inside it
}

// Assembler builds jigsaw structures from the embedded pools.
type Assembler struct {
	pk      *wgdata.Pack
	pools   map[string]*pool
	heights map[string]int32
	// Height, if set, is the WORLD_SURFACE_WG height for terrain_matching
	// pieces.
	Height func(x, z int32) int32
	// StartY, if set, gives the start piece's y from the column at the
	// centre of its box.
	StartY func(x, z int32) int32
}

func NewAssembler() (*Assembler, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	return &Assembler{pk: pk, pools: map[string]*pool{}, heights: map[string]int32{}}, nil
}

func (a *Assembler) pool(name string) (*pool, error) {
	name = qualify(name)
	if p, ok := a.pools[name]; ok {
		return p, nil
	}
	p, err := loadPool(a.pk, name)
	if err != nil {
		return nil, err
	}
	a.pools[name] = p
	return p, nil
}

// space is a region pieces may be placed in: inside bound, not touching
// any occupied box.
type space struct {
	bound    Box
	occupied []Box
	raw      []rawBox // trace only: plain boxes and projection
}

type rawBox struct {
	B     Box
	Rigid bool
}

func (s *space) fits(b Box) bool {
	if !(b.MinX >= s.bound.MinX && b.MaxX <= s.bound.MaxX && b.MinY >= s.bound.MinY && b.MaxY <= s.bound.MaxY &&
		b.MinZ >= s.bound.MinZ && b.MaxZ <= s.bound.MaxZ) {
		return false
	}
	for _, o := range s.occupied {
		if o.Intersects(b) {
			return false
		}
	}
	return true
}

// Assemble builds the pieces of a jigsaw structure whose start piece origin
// is x,y,z, up to maxDepth jigsaw levels and maxDist blocks from the start.
//
// Inferred from reference draw logs and piece lists (seed
// 8266389061079214669, plains village at chunk -13,-28): rotation
// nextInt(4), start element nextInt(total weight); each piece's jigsaws are
// shuffled when the piece is processed (breadth first); per jigsaw the
// target pool's weighted list is shuffled, then the fallback pool's list is
// shuffled and appended; an empty element ends the attempt; per candidate the
// four rotations are shuffled, and per rotation the candidate's jigsaws are
// shuffled. A child whose connection point lies inside its parent's box must
// fit inside the parent box beside the parent's other inner children;
// otherwise it must fit the shared outer space (the max-distance box minus
// every piece placed there).
func (a *Assembler) Assemble(r *rng.WorldgenRandom, startPool string, x, y, z int32, maxDepth int, maxDist int32) ([]Piece, error) {
	if jigDrawLog != nil {
		jigDrawLog, jigTrace = jigDrawLog[:0], jigTrace[:0]
	}
	sp, err := a.pool(startPool)
	if err != nil {
		return nil, err
	}
	if len(sp.elements) == 0 {
		return nil, nil
	}
	rot := Rotation(jn(r, 4))
	el := sp.elements[jn(r, int32(len(sp.elements)))]
	first, err := newJigsawPiece(el, x, y, z, rot)
	if err != nil || first == nil {
		return nil, err
	}
	fb := first.box
	if a.StartY != nil {
		// Observed: the projection samples the start piece's box centre
		// after the rotation and element draws; the centre is (min+max)/2
		// truncated toward zero (outpost probe stubs at 727 and -1384 for
		// boxes 720..735 and -1392..-1377).
		first.move(0, a.StartY((fb.MinX+fb.MaxX)/2, (fb.MinZ+fb.MaxZ)/2)-y, 0)
		y = first.Y
		fb = first.box
	}
	cx, cz := (fb.MinX+fb.MaxX)/2, (fb.MinZ+fb.MaxZ)/2
	outer := &space{bound: Box{cx - maxDist, y - maxDist, cz - maxDist, cx + maxDist + 1, y + maxDist + 1, cz + maxDist + 1},
		occupied: []Box{fb}, raw: []rawBox{{fb, el.rigid}}}
	placed := []*placedJigsaw{{jp: first, tmpl: first.primary(), rigid: el.rigid, outer: outer, inner: &space{bound: fb}}}
	for qi := 0; qi < len(placed); qi++ {
		cur := placed[qi]
		if cur.depth > maxDepth || cur.tmpl == nil {
			continue
		}
		js := templateJigsaws(cur.tmpl)
		for i := len(js) - 1; i > 0; i-- {
			k := jn(r, int32(i+1))
			js[i], js[k] = js[k], js[i]
		}
		for _, j := range js {
			if j.pool == "" || j.pool == "minecraft:empty" {
				continue
			}
			sx, sz := TransformPos(j.x, j.z, MirrorNone, cur.jp.Rot, 0, 0)
			sx, sy, sz := cur.jp.X+sx, cur.jp.Y+j.y, cur.jp.Z+sz
			base := cur.jp.Y // the parent's ground reference at this jigsaw
			if !cur.rigid && a.Height != nil {
				// Observed: a terrain_matching piece's jigsaws sit on the
				// surface of their own column (WORLD_SURFACE_WG - 1 + y).
				base = a.Height(sx, sz) - 1
				sy = base + j.y
			}
			front := j.front
			if dirVec[front][1] == 0 {
				front = rotDir(front, cur.jp.Rot)
			}
			d := dirVec[front]
			tx, ty, tz := sx+d[0], sy+d[1], sz+d[2]
			sp := cur.outer
			// Observed: for a terrain_matching parent only x and z decide
			// whether the connection point is inside it.
			ib := cur.inner.bound
			inside := ib.Contains(tx, ty, tz)
			if !cur.rigid {
				inside = tx >= ib.MinX && tx <= ib.MaxX && tz >= ib.MinZ && tz <= ib.MaxZ
			}
			if inside {
				sp = cur.inner
			}
			// Joint (minecraft.wiki; desert camel_spawn): an aligned vertical
			// jigsaw only connects to a jigsaw whose top faces the same way.
			top := ""
			if j.aligned && dirVec[j.front][1] != 0 {
				top = rotDir(j.top, cur.jp.Rot)
			}
			child, err := a.tryAttach(r, j.pool, j.target, front, top, tx, ty, tz, cur, sp, cur.depth >= maxDepth)
			if err != nil {
				return nil, err
			}
			if child == nil {
				continue
			}
			// Junctions and ground level deltas, fitted to the reference
			// starts of all five villages (447/447 connections): ground is
			// the parent's base plus its delta; a rigid child's delta keeps
			// that ground, a terrain_matching child keeps its element's.
			ground := base + cur.jp.GLD
			var delta int32
			if child.rigid {
				delta = child.jp.Y - base
				child.jp.GLD = ground - child.jp.Y
			}
			py, cy := ground, ground
			if cur.rigid && child.rigid {
				cy = ty - delta - 1 + cur.jp.GLD
			} else {
				py = ground + d[1]
			}
			cur.jp.Junctions = append(cur.jp.Junctions, Junction{tx, py, tz, delta, child.rigid})
			child.jp.Junctions = append([]Junction{{sx, cy, sz, -delta, cur.rigid}}, child.jp.Junctions...)
			placed = append(placed, child)
		}
	}
	out := make([]Piece, len(placed))
	for i, p := range placed {
		out[i] = p.jp
	}
	return out, nil
}

// raise is how far a terrain_matching piece's box reaches above its
// template box: the tallest rigid template of the pools its jigsaws point
// to. Observed with JDI on the reference free-space test (village_plains,
// 2,649 tests) and in fresh reference starts of the desert and savanna
// villages (decor 4 and 2, houses 18 and 14, streets 0).
func (a *Assembler) raise(t *Template) (int32, error) {
	var r int32
	for _, j := range templateJigsaws(t) {
		if j.pool == "" || j.pool == "minecraft:empty" {
			continue
		}
		h, err := a.poolHeight(j.pool)
		if err != nil {
			return 0, err
		}
		r = max(r, h)
	}
	return r, nil
}

func (a *Assembler) poolHeight(name string) (int32, error) {
	if h, ok := a.heights[name]; ok {
		return h, nil
	}
	p, err := a.pool(name)
	if err != nil {
		return 0, err
	}
	var m int32
	for _, e := range p.elements {
		if e.primary() == "" || !e.rigid {
			continue
		}
		t, err := LoadTemplate(e.primary())
		if err != nil {
			return 0, err
		}
		m = max(m, t.SizeY)
	}
	a.heights[name] = m
	return m, nil
}

func shuffleElements(r *rng.WorldgenRandom, l []*poolElement) []*poolElement {
	c := append([]*poolElement(nil), l...)
	for i := len(c) - 1; i > 0; i-- {
		k := jn(r, int32(i+1))
		c[i], c[k] = c[k], c[i]
	}
	return c
}

func (a *Assembler) tryAttach(r *rng.WorldgenRandom, poolName, target, front, top string, tx, ty, tz int32,
	parent *placedJigsaw, sp *space, fallbackOnly bool) (*placedJigsaw, error) {
	pl, err := a.pool(poolName)
	if err != nil {
		return nil, err
	}
	// Observed: pieces at the size limit still try their jigsaws, with
	// the fallback pool only.
	var cands []*poolElement
	if !fallbackOnly {
		cands = shuffleElements(r, pl.elements)
	}
	if pl.fallback != "" {
		fb, err := a.pool(pl.fallback)
		if err != nil {
			return nil, err
		}
		cands = append(cands, shuffleElements(r, fb.elements)...)
	}
	want := opposite[front]
	for _, c := range cands {
		if c.kind == "empty" {
			return nil, nil
		}
		var cj []jigsawBlock
		var rise int32
		switch {
		case c.kind == "feature":
			// Observed: a 1x1x1 piece at the connection point with one
			// downward "minecraft:bottom" jigsaw.
			cj = []jigsawBlock{{front: "down", name: "minecraft:bottom"}}
		case c.primary() == "":
			continue
		default:
			t, err := LoadTemplate(c.primary())
			if err != nil {
				return nil, err
			}
			cj = templateJigsaws(t)
			if !c.rigid {
				if rise, err = a.raise(t); err != nil {
					return nil, err
				}
			}
		}
		rots := []Rotation{RotNone, Rot90, Rot180, Rot270}
		for i := 3; i > 0; i-- {
			k := jn(r, int32(i+1))
			rots[i], rots[k] = rots[k], rots[i]
		}
		for _, rot := range rots {
			// Each rotation shuffles a fresh copy of the template's jigsaw
			// list (outpost 209,-501: free-space test order, JigJdi).
			sj := append([]jigsawBlock(nil), cj...)
			for i := len(sj) - 1; i > 0; i-- {
				k := jn(r, int32(i+1))
				sj[i], sj[k] = sj[k], sj[i]
			}
			for _, j := range sj {
				if j.name != target {
					continue
				}
				f := j.front
				if dirVec[f][1] == 0 {
					f = rotDir(f, rot)
				}
				if f != want || top != "" && rotDir(j.top, rot) != top {
					continue
				}
				jx, jz := TransformPos(j.x, j.z, MirrorNone, rot, 0, 0)
				ox, oy, oz := tx-jx, ty-j.y, tz-jz
				if parent.rigid && !c.rigid && c.kind != "feature" && a.Height != nil {
					// Observed (desert, savanna, snowy): a terrain_matching
					// child of a rigid parent sits on the surface of the
					// parent jigsaw's column; under a terrain_matching parent
					// it follows the parent's jigsaw (plains).
					// The child's jigsaw lands at WORLD_SURFACE_WG of that
					// column (villages: jigsaw y 1, outposts: y 0).
					d := dirVec[front]
					oy = a.Height(tx-d[0], tz-d[2]) - j.y
				}
				jp, err := newJigsawPiece(c, ox, oy, oz, rot)
				if err != nil {
					return nil, err
				}
				if jp == nil {
					continue
				}
				b := jp.box
				// Observed (fresh starts: desert streets 2+4 and 2+18,
				// outpost feature plates 4 tall with 4-tall features -> 6):
				// the box reaches minY + 1 + the tallest rigid target.
				cb := b
				if rise > 0 {
					cb.MaxY = max(cb.MaxY, cb.MinY+1+rise)
				}
				if jigDrawLog != nil {
					tj, _ := json.Marshal(map[string]any{"d": len(jigDrawLog), "c": c.primary(), "rot": rot, "rigid": c.rigid, "box": b,
						"inner": sp == parent.inner, "bound": sp.bound, "occ": sp.raw, "fit": sp.fits(cb)})
					jigTrace = append(jigTrace, string(tj))
				}
				if !sp.fits(cb) {
					continue
				}
				sp.occupied = append(sp.occupied, cb)
				sp.raw = append(sp.raw, rawBox{b, c.rigid})
				// A fresh terrain_matching piece keeps the raised box; a
				// reloaded one gets its template box back (reference
				// PoolElementStructurePiece save/load round trip).
				jp.box = cb
				return &placedJigsaw{jp: jp, tmpl: jp.primary(), depth: parent.depth + 1, rigid: c.rigid,
					outer: sp, inner: &space{bound: cb}}, nil
			}
		}
	}
	return nil, nil
}

// jigsawType is a worldgen/structure of type jigsaw. Only the structures in
// jigsawEnabled are registered.
type jigsawType struct {
	step      int
	startPool string
	size      int
	maxDist   int32
	startY    int32
	heightmap string
	adapt     string
}

func (t *jigsawType) terrainAdaptation() string { return t.adapt }

// jigsawEnabled lists the jigsaw structures wired into generation: those
// whose starts, terrain adaptation and placed blocks match the reference
// (docs/vanilla-parity.md, "Villages"). Read-only after init.
var jigsawEnabled = map[string]bool{
	"minecraft:village_plains":  true,
	"minecraft:village_desert":  true,
	"minecraft:village_savanna": true,
	"minecraft:village_snowy":   true,
	"minecraft:village_taiga":   true,
}

func (t *jigsawType) Step() int { return t.step }

func (t *jigsawType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	x, z := ctx.ChunkX<<4, ctx.ChunkZ<<4
	a, err := NewAssembler()
	if err != nil {
		return nil, err
	}
	a.Height = func(x, z int32) int32 { return ctx.Height(x, z, "WORLD_SURFACE_WG") }
	if t.heightmap != "" {
		// Observed (flat ground only): y = surface height - 1.
		a.StartY = func(x, z int32) int32 { return t.startY + ctx.Height(x, z, t.heightmap) - 1 }
	}
	ps, err := a.Assemble(ctx.Random, t.startPool, x, t.startY, z, t.size, t.maxDist)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	// Black box (probe kremenmc.probe.outpost.Starts, findValidGenerationPoint
	// with a logging biome resolver): the start is rejected unless some
	// quart of the column at the start piece's box centre has a valid
	// biome (bottom up, first match ends the scan) and the biome at the
	// stub, (centre x, start y + 1, centre z), is valid.
	if ctx.BiomeOK != nil {
		b := ps[0].Box()
		cx, cz := (b.MinX+b.MaxX)/2, (b.MinZ+b.MaxZ)/2
		minY, maxY := ctx.MinY, ctx.MaxY
		if minY == maxY {
			minY, maxY = -64, 320
		}
		ok := false
		for qy := minY >> 2; qy < maxY>>2 && !ok; qy++ {
			ok = ctx.BiomeOK(cx, qy<<2, cz)
		}
		sy := ps[0].(*JigsawPiece).Y
		if t.heightmap != "" {
			sy++
		}
		if !ok || !ctx.BiomeOK(cx, sy, cz) {
			return nil, nil
		}
	}
	return ps, nil
}

func init() {
	RegisterPiece("minecraft:jigsaw", loadJigsawPiece)
	RegisterType("minecraft:jigsaw", func(key string, raw json.RawMessage) (Type, error) {
		if !jigsawEnabled[key] {
			return nil, nil
		}
		var j struct {
			StartPool   string `json:"start_pool"`
			Size        int    `json:"size"`
			MaxDist     int32  `json:"max_distance_from_center"`
			Heightmap   string `json:"project_start_to_heightmap"`
			Step        string `json:"step"`
			Adapt       string `json:"terrain_adaptation"`
			StartHeight struct {
				Absolute int32 `json:"absolute"`
			} `json:"start_height"`
		}
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, err
		}
		if j.MaxDist == 0 {
			j.MaxDist = 80
		}
		return &jigsawType{step: stepIndex(j.Step), startPool: j.StartPool, size: j.Size, maxDist: j.MaxDist,
			startY: j.StartHeight.Absolute, heightmap: j.Heightmap, adapt: j.Adapt}, nil
	})
}

var jigDrawLog []string
var jigTrace []string

func jn(r *rng.WorldgenRandom, n int32) int32 {
	v := r.NextIntn(n)
	if jigDrawLog != nil {
		jigDrawLog = append(jigDrawLog, fmt.Sprintf("i%d=%d", n, v))
	}
	return v
}
