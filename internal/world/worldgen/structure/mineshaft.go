package structure

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Mineshaft layout, inferred from black-box probes (tools/refprobe
// kremenmc.probe.legacy.MineLog: start draws interleaved with every piece added
// and every collision query; kremenmc.probe.legacy.MineParts: the piece finders
// and addChildren of each piece kind in isolation, 4 directions x 16
// seeds). See docs/vanilla-parity.md "Legacy structures".

type msDir int32 // 0 north, 1 east, 2 south, 3 west

const (
	msNorth msDir = iota
	msEast
	msSouth
	msWest
)

// msDir2D is the saved "O" (2D data value: south 0, west 1, north 2, east 3).
var msDir2D = [4]int32{2, 3, 0, 1}

const (
	msRoom = iota
	msCorridor
	msCrossing
	msStairs
)

var msIDs = [4]string{"minecraft:msroom", "minecraft:mscorridor", "minecraft:mscrossing", "minecraft:msstairs"}

type msPiece struct {
	kind  int
	box   Box
	dir   msDir // -1 for rooms and crossings' orientation
	depth int32
	mesa  bool

	entrances []Box // room
	rails     bool  // corridor
	spider    bool  // corridor
	sections  int32 // corridor
	// placedSpider is vanilla's hasPlacedSpider (hps): set when the cave
	// spider spawner is placed.
	placedSpider bool
	crossDir     msDir // crossing
	twoFloored   bool  // crossing
}

func (p *msPiece) Type() string { return msIDs[p.kind] }
func (p *msPiece) Box() Box     { return p.box }

func (p *msPiece) Save() nbt.Compound {
	o := int32(-1)
	if p.kind == msCorridor || p.kind == msStairs {
		o = msDir2D[p.dir]
	}
	mst := int32(0)
	if p.mesa {
		mst = 1
	}
	c := nbt.Compound{"id": msIDs[p.kind], "GD": p.depth, "O": o, "BB": p.box.IntArray(), "MST": mst}
	b := func(v bool) int8 {
		if v {
			return 1
		}
		return 0
	}
	switch p.kind {
	case msRoom:
		items := make([]any, len(p.entrances))
		for i, e := range p.entrances {
			items[i] = e.IntArray()
		}
		c["Entrances"] = nbt.List{Type: nbt.TagIntArray, Items: items}
	case msCorridor:
		c["hr"], c["sc"], c["hps"], c["Num"] = b(p.rails), b(p.spider), b(p.placedSpider), p.sections
	case msCrossing:
		c["D"], c["tf"] = msDir2D[p.crossDir], b(p.twoFloored)
	}
	return c
}

func loadMsPiece(c nbt.Compound) (Piece, error) {
	id, _ := c["id"].(string)
	p := &msPiece{dir: -1}
	switch qualify(id) {
	case msIDs[msRoom]:
		p.kind = msRoom
	case msIDs[msCorridor]:
		p.kind = msCorridor
	case msIDs[msCrossing]:
		p.kind = msCrossing
	case msIDs[msStairs]:
		p.kind = msStairs
	default:
		return nil, fmt.Errorf("mineshaft: unknown piece %s", id)
	}
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return nil, fmt.Errorf("mineshaft: bad BB")
	}
	p.box = Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}
	p.depth, _ = c["GD"].(int32)
	mst, _ := c["MST"].(int32)
	p.mesa = mst == 1
	from2D := func(v int32) msDir {
		for i, d := range msDir2D {
			if d == v {
				return msDir(i)
			}
		}
		return -1
	}
	o, _ := c["O"].(int32)
	if o >= 0 {
		p.dir = from2D(o)
	}
	flag := func(k string) bool { v, _ := c[k].(int8); return v != 0 }
	switch p.kind {
	case msRoom:
		for _, it := range listItems(c["Entrances"]) {
			if e := intList(it); len(e) == 6 {
				p.entrances = append(p.entrances, Box{e[0], e[1], e[2], e[3], e[4], e[5]})
			}
		}
	case msCorridor:
		p.rails, p.spider, p.placedSpider = flag("hr"), flag("sc"), flag("hps")
		p.sections, _ = c["Num"].(int32)
	case msCrossing:
		d, _ := c["D"].(int32)
		p.crossDir = from2D(d)
		p.twoFloored = flag("tf")
	}
	return p, nil
}

func listItems(v any) []any {
	if l, ok := v.(nbt.List); ok {
		return l.Items
	}
	return nil
}

type msBuilder struct {
	pieces []*msPiece
	start  *msPiece
	r      *rng.WorldgenRandom
	mesa   bool
}

func (b *msBuilder) collides(bx Box) bool {
	for _, p := range b.pieces {
		if p.box.Intersects(bx) {
			return true
		}
	}
	return false
}

// msCorridorBox: 2..4 sections of 5, shrunk by one section per collision.
func (b *msBuilder) corridorBox(x, y, z int32, d msDir) (Box, bool) {
	for n := b.r.NextIntn(3) + 2; n > 0; n-- {
		l := n * 5
		var bx Box
		switch d {
		case msNorth:
			bx = Box{x, y, z - l + 1, x + 2, y + 2, z}
		case msSouth:
			bx = Box{x, y, z, x + 2, y + 2, z + l - 1}
		case msWest:
			bx = Box{x - l + 1, y, z, x, y + 2, z + 2}
		default:
			bx = Box{x, y, z, x + l - 1, y + 2, z + 2}
		}
		if !b.collides(bx) {
			return bx, true
		}
	}
	return Box{}, false
}

func (b *msBuilder) crossingBox(x, y, z int32, d msDir) (Box, bool, bool) {
	h := int32(2)
	two := b.r.NextIntn(4) == 0
	if two {
		h = 6
	}
	var bx Box
	switch d {
	case msNorth:
		bx = Box{x - 1, y, z - 4, x + 3, y + h, z}
	case msSouth:
		bx = Box{x - 1, y, z, x + 3, y + h, z + 4}
	case msWest:
		bx = Box{x - 4, y, z - 1, x, y + h, z + 3}
	default:
		bx = Box{x, y, z - 1, x + 4, y + h, z + 3}
	}
	if b.collides(bx) {
		return Box{}, false, false
	}
	return bx, two, true
}

func (b *msBuilder) stairsBox(x, y, z int32, d msDir) (Box, bool) {
	var bx Box
	switch d {
	case msNorth:
		bx = Box{x, y - 5, z - 8, x + 2, y + 2, z}
	case msSouth:
		bx = Box{x, y - 5, z, x + 2, y + 2, z + 8}
	case msWest:
		bx = Box{x - 8, y - 5, z, x, y + 2, z + 2}
	default:
		bx = Box{x, y - 5, z, x + 8, y + 2, z + 2}
	}
	if b.collides(bx) {
		return Box{}, false
	}
	return bx, true
}

func (b *msBuilder) randomPiece(x, y, z int32, d msDir, depth int32) *msPiece {
	v := b.r.NextIntn(100)
	switch {
	case v >= 80:
		bx, two, ok := b.crossingBox(x, y, z, d)
		if !ok {
			return nil
		}
		return &msPiece{kind: msCrossing, box: bx, dir: -1, depth: depth, mesa: b.mesa, crossDir: d, twoFloored: two}
	case v >= 70:
		bx, ok := b.stairsBox(x, y, z, d)
		if !ok {
			return nil
		}
		return &msPiece{kind: msStairs, box: bx, dir: d, depth: depth, mesa: b.mesa}
	default:
		bx, ok := b.corridorBox(x, y, z, d)
		if !ok {
			return nil
		}
		p := &msPiece{kind: msCorridor, box: bx, dir: d, depth: depth, mesa: b.mesa}
		p.rails = b.r.NextIntn(3) == 0
		p.spider = !p.rails && b.r.NextIntn(23) == 0
		if d == msNorth || d == msSouth {
			p.sections = (bx.MaxZ - bx.MinZ + 1) / 5
		} else {
			p.sections = (bx.MaxX - bx.MinX + 1) / 5
		}
		return p
	}
}

// add places a child of a piece of depth parentDepth; nil past depth 8 or
// 80 blocks from the room.
func (b *msBuilder) add(x, y, z int32, d msDir, parentDepth int32) *msPiece {
	if parentDepth > 8 {
		return nil
	}
	if abs32(x-b.start.box.MinX) > 80 || abs32(z-b.start.box.MinZ) > 80 {
		return nil
	}
	p := b.randomPiece(x, y, z, d, parentDepth+1)
	if p == nil {
		return nil
	}
	b.pieces = append(b.pieces, p)
	b.children(p)
	return p
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (b *msBuilder) children(p *msPiece) {
	r := b.r
	bx := p.box
	switch p.kind {
	case msRoom:
		yr := bx.MaxY - bx.MinY + 1 - 4
		if yr <= 0 {
			yr = 1
		}
		side := func(span int32, at func(k, y int32) (int32, int32, int32, msDir)) {
			for k := int32(0); k < span; k += 4 {
				k += r.NextIntn(span)
				if k+3 > span {
					break
				}
				y := bx.MinY + r.NextIntn(yr) + 1
				x, yy, z, d := at(k, y)
				if c := b.add(x, yy, z, d, p.depth); c != nil {
					cb := c.box
					var e Box
					switch d {
					case msNorth:
						e = Box{cb.MinX, cb.MinY, bx.MinZ, cb.MaxX, cb.MaxY, bx.MinZ + 1}
					case msSouth:
						e = Box{cb.MinX, cb.MinY, bx.MaxZ - 1, cb.MaxX, cb.MaxY, bx.MaxZ}
					case msWest:
						e = Box{bx.MinX, cb.MinY, cb.MinZ, bx.MinX + 1, cb.MaxY, cb.MaxZ}
					default:
						e = Box{bx.MaxX - 1, cb.MinY, cb.MinZ, bx.MaxX, cb.MaxY, cb.MaxZ}
					}
					p.entrances = append(p.entrances, e)
				}
			}
		}
		xs, zs := bx.MaxX-bx.MinX+1, bx.MaxZ-bx.MinZ+1
		side(xs, func(k, y int32) (int32, int32, int32, msDir) { return bx.MinX + k, y, bx.MinZ - 1, msNorth })
		side(xs, func(k, y int32) (int32, int32, int32, msDir) { return bx.MinX + k, y, bx.MaxZ + 1, msSouth })
		side(zs, func(k, y int32) (int32, int32, int32, msDir) { return bx.MinX - 1, y, bx.MinZ + k, msWest })
		side(zs, func(k, y int32) (int32, int32, int32, msDir) { return bx.MaxX + 1, y, bx.MinZ + k, msEast })
	case msCorridor:
		c := r.NextIntn(4)
		y := bx.MinY + r.NextIntn(3) - 1
		switch p.dir {
		case msNorth:
			switch c {
			case 2:
				b.add(bx.MinX-1, y, bx.MinZ, msWest, p.depth)
			case 3:
				b.add(bx.MaxX+1, y, bx.MinZ, msEast, p.depth)
			default:
				b.add(bx.MinX, y, bx.MinZ-1, msNorth, p.depth)
			}
		case msSouth:
			switch c {
			case 2:
				b.add(bx.MinX-1, y, bx.MaxZ-3, msWest, p.depth)
			case 3:
				b.add(bx.MaxX+1, y, bx.MaxZ-3, msEast, p.depth)
			default:
				b.add(bx.MinX, y, bx.MaxZ+1, msSouth, p.depth)
			}
		case msWest:
			switch c {
			case 2:
				b.add(bx.MinX, y, bx.MinZ-1, msNorth, p.depth)
			case 3:
				b.add(bx.MinX, y, bx.MaxZ+1, msSouth, p.depth)
			default:
				b.add(bx.MinX-1, y, bx.MinZ, msWest, p.depth)
			}
		default:
			switch c {
			case 2:
				b.add(bx.MaxX-3, y, bx.MinZ-1, msNorth, p.depth)
			case 3:
				b.add(bx.MaxX-3, y, bx.MaxZ+1, msSouth, p.depth)
			default:
				b.add(bx.MaxX+1, y, bx.MinZ, msEast, p.depth)
			}
		}
		if p.depth < 8 {
			if p.dir == msNorth || p.dir == msSouth {
				for k := bx.MinZ + 3; k+3 <= bx.MaxZ; k += 5 {
					switch r.NextIntn(5) {
					case 0:
						b.add(bx.MinX-1, bx.MinY, k, msWest, p.depth+1)
					case 1:
						b.add(bx.MaxX+1, bx.MinY, k, msEast, p.depth+1)
					}
				}
			} else {
				for k := bx.MinX + 3; k+3 <= bx.MaxX; k += 5 {
					switch r.NextIntn(5) {
					case 0:
						b.add(k, bx.MinY, bx.MinZ-1, msNorth, p.depth+1)
					case 1:
						b.add(k, bx.MinY, bx.MaxZ+1, msSouth, p.depth+1)
					}
				}
			}
		}
	case msCrossing:
		y := bx.MinY
		north := func(y int32) { b.add(bx.MinX+1, y, bx.MinZ-1, msNorth, p.depth) }
		south := func(y int32) { b.add(bx.MinX+1, y, bx.MaxZ+1, msSouth, p.depth) }
		west := func(y int32) { b.add(bx.MinX-1, y, bx.MinZ+1, msWest, p.depth) }
		east := func(y int32) { b.add(bx.MaxX+1, y, bx.MinZ+1, msEast, p.depth) }
		switch p.crossDir {
		case msNorth:
			north(y)
			west(y)
			east(y)
		case msSouth:
			south(y)
			west(y)
			east(y)
		case msWest:
			north(y)
			south(y)
			west(y)
		default:
			north(y)
			south(y)
			east(y)
		}
		if p.twoFloored {
			if r.NextBool() {
				north(y + 4)
			}
			if r.NextBool() {
				west(y + 4)
			}
			if r.NextBool() {
				east(y + 4)
			}
			if r.NextBool() {
				south(y + 4)
			}
		}
	case msStairs:
		switch p.dir {
		case msNorth:
			b.add(bx.MinX, bx.MinY, bx.MinZ-1, msNorth, p.depth)
		case msSouth:
			b.add(bx.MinX, bx.MinY, bx.MaxZ+1, msSouth, p.depth)
		case msWest:
			b.add(bx.MinX-1, bx.MinY, bx.MinZ, msWest, p.depth)
		default:
			b.add(bx.MaxX+1, bx.MinY, bx.MinZ, msEast, p.depth)
		}
	}
}

// msLayout builds the pieces of a mineshaft start at y 50 (before the
// vertical move); the random is the start random after the probability
// draw.
func msLayout(r *rng.WorldgenRandom, cx, cz int32, mesa bool) []*msPiece {
	x, z := cx<<4+2, cz<<4+2
	room := &msPiece{kind: msRoom, dir: -1, mesa: mesa}
	room.box = Box{x, 50, z, x + 7 + r.NextIntn(6), 54 + r.NextIntn(6), z + 7 + r.NextIntn(6)}
	b := &msBuilder{r: r, start: room, mesa: mesa, pieces: []*msPiece{room}}
	b.children(room)
	return b.pieces
}

func (p *msPiece) move(dy int32) {
	p.box.MinY += dy
	p.box.MaxY += dy
	for i := range p.entrances {
		p.entrances[i].MinY += dy
		p.entrances[i].MaxY += dy
	}
}

type mineshaftType struct {
	prob float64
	mesa bool
}

func (t *mineshaftType) Step() int { return 3 }

// Generate: the probability draw, the layout at y 50, then the vertical
// move, then the biome test at the chunk's middle x, min z and the room's
// moved min y (probe stub position).
func (t *mineshaftType) Generate(ctx *GenContext) ([]Piece, error) {
	var height func(x, z int32) int32
	if t.mesa && ctx.Height != nil {
		height = func(x, z int32) int32 { return ctx.Height(x, z, "WORLD_SURFACE_WG") }
	}
	ps := msStart(ctx.Random, ctx.ChunkX, ctx.ChunkZ, t.prob, t.mesa, height)
	if ps == nil {
		return nil, nil
	}
	if ctx.BiomeOK != nil && !ctx.BiomeOK(ctx.ChunkX<<4+8, ps[0].Box().MinY, ctx.ChunkZ<<4) {
		return nil, nil
	}
	return ps, nil
}

// msStart: normal shafts move so the box's min y is
// -62 + nextInt(116 - height) (6/6 probe starts; sea level 63, min y -64).
// Mesa shafts move the box's centre y ((min+max)/2) to
// randomBetweenInclusive(63, h) with h the WORLD_SURFACE_WG height at the
// box's x/z centre, or to 63 without a draw when h <= 63 (5/5 probe
// starts, kremenmc.probe.legacy.Starts minecraft:mineshaft_mesa; OCEAN_FLOOR_WG
// gives the same h in every sample).
func msStart(r *rng.WorldgenRandom, cx, cz int32, prob float64, mesa bool, height func(x, z int32) int32) []Piece {
	if r.NextDouble() >= prob {
		return nil
	}
	ps := msLayout(r, cx, cz, mesa)
	bb := ps[0].box
	for _, p := range ps[1:] {
		bb = bb.Encapsulate(p.box)
	}
	var dy int32
	if mesa {
		target := int32(63)
		if height != nil {
			if h := height((bb.MinX+bb.MaxX)/2, (bb.MinZ+bb.MaxZ)/2); h > target {
				target += r.NextIntn(h - target + 1)
			}
		}
		dy = target - (bb.MinY+bb.MaxY)/2
	} else {
		dy = -62 + r.NextIntn(116-(bb.MaxY-bb.MinY+1)) - bb.MinY
	}
	out := make([]Piece, len(ps))
	for i, p := range ps {
		p.move(dy)
		out[i] = p
	}
	return out
}

// MineshaftStartForTest is the normal mineshaft start at a chunk without
// the biome test.
func MineshaftStartForTest(seed int64, cx, cz int32) []Piece {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	return msStart(r, cx, cz, 0.004, false, nil)
}

// MesaMineshaftStartForTest is the mesa mineshaft start at a chunk without
// the biome test.
func MesaMineshaftStartForTest(seed int64, cx, cz int32, height func(x, z int32) int32) []Piece {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	return msStart(r, cx, cz, 0.004, true, height)
}

func (p *msPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	newMsPlacer(p, lv, r, clip).run()
	return nil
}

func newMineshaftType(raw json.RawMessage) (*mineshaftType, error) {
	var j struct {
		Prob float64 `json:"probability"`
		Type string  `json:"mineshaft_type"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	// 26.3 has no probability field: the start's first nextDouble is the
	// same draw the set's legacy_type_3 frequency reducer (0.004) tests.
	if j.Prob == 0 {
		j.Prob = 0.004
	}
	return &mineshaftType{prob: j.Prob, mesa: j.Type == "mesa"}, nil
}

// RegisterMineshaftForTest registers the mineshaft type (normal and mesa);
// init does it too, tests may call it again.
func RegisterMineshaftForTest() {
	RegisterType("minecraft:mineshaft", func(_ string, raw json.RawMessage) (Type, error) { return newMineshaftType(raw) })
}

func init() {
	RegisterMineshaftForTest()
	for _, id := range msIDs {
		RegisterPiece(id, loadMsPiece)
	}
}
