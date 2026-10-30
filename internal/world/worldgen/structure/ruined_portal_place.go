package structure

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Ruined portal piece placement (minecraft:rupo), inferred from the black-box
// scene probe kremenmc.probe.rportal.RPScene (a saved start placed on a
// kremenmc terrain scene, every draw, read and write logged; inputs dumped by
// TestRuinedPortalSceneDump) and from the place settings dumped by
// kremenmc.probe.structlegacy.PortalSettings. See docs/vanilla-parity.md
// "Ruined portals".
//
//   - The whole portal is placed by the one chunk holding the box centre
//     (minX+sizeX/2, minZ+sizeZ/2); other chunks do nothing. Writes reach
//     the neighbouring chunks (3x3 write zone).
//   - Template: processors run over every block first (they read the world
//     before anything is written), then the blocks are written in template
//     order; a block entity with a LootTable gets LootTableSeed = nextLong.
//   - Netherrack spread over a manhattan diamond around the box centre,
//     drip columns under the portal's bottom layer, then vines and
//     overgrowth over the box.

type rpPiece struct {
	Template          string
	OX, OY, OZ        int32
	Rot               Rotation
	Mirror            Mirror
	Placement         string
	AirPocket, Cold   bool
	Overgrown, Vines  bool
	Blackstone        bool
	Mossiness         float32
	tmpl              *Template
	box               Box
	px, pz            int32
	traceHook         func(label string, v any)
	protected, leaves map[string]bool
}

func newRPPiece(st *rpStart) (*rpPiece, error) {
	p := &rpPiece{Template: st.Template, OX: st.OX, OY: st.OY, OZ: st.OZ, Rot: st.Rot, Mirror: st.Mirror,
		Placement: st.Placement, AirPocket: st.AirPocket, Cold: st.Cold, Overgrown: st.Overgrown,
		Vines: st.Vines, Blackstone: st.Blackstone, Mossiness: st.Mossiness}
	return p, p.init()
}

func (p *rpPiece) init() error {
	t, err := LoadTemplate(p.Template)
	if err != nil {
		return err
	}
	p.tmpl = t
	p.px, p.pz = t.SizeX/2, t.SizeZ/2
	p.box = TemplateBox(t, p.OX, p.OY, p.OZ, p.Mirror, p.Rot, p.px, p.pz)
	pk, err := wgdata.Load()
	if err != nil {
		return err
	}
	p.protected = blockTag(pk, "#minecraft:features_cannot_replace")
	p.leaves = blockTag(pk, "#minecraft:leaves")
	return nil
}

func (p *rpPiece) Type() string { return "minecraft:rupo" }
func (p *rpPiece) Box() Box     { return p.box }

func b2b(v bool) int8 {
	if v {
		return 1
	}
	return 0
}

func (p *rpPiece) Save() nbt.Compound {
	return nbt.Compound{
		"id": "minecraft:rupo", "GD": int32(0), "O": int32(2), "BB": p.box.IntArray(),
		"Template": qualify(p.Template), "TPX": p.OX, "TPY": p.OY, "TPZ": p.OZ,
		"Rotation": rotNames[p.Rot], "Mirror": mirrorNames[p.Mirror], "VerticalPlacement": p.Placement,
		"Properties": nbt.Compound{
			"air_pocket": b2b(p.AirPocket), "cold": b2b(p.Cold), "mossiness": p.Mossiness,
			"overgrown": b2b(p.Overgrown), "replace_with_blackstone": b2b(p.Blackstone), "vines": b2b(p.Vines),
		},
	}
}

func loadRPPiece(c nbt.Compound) (Piece, error) {
	p := &rpPiece{}
	p.Template, _ = c["Template"].(string)
	p.OX, _ = c["TPX"].(int32)
	p.OY, _ = c["TPY"].(int32)
	p.OZ, _ = c["TPZ"].(int32)
	rs, _ := c["Rotation"].(string)
	for i, n := range rotNames {
		if n == rs {
			p.Rot = Rotation(i)
		}
	}
	ms, _ := c["Mirror"].(string)
	for i, n := range mirrorNames {
		if n == ms {
			p.Mirror = Mirror(i)
		}
	}
	p.Placement, _ = c["VerticalPlacement"].(string)
	pr, _ := c["Properties"].(nbt.Compound)
	flag := func(k string) bool { v, _ := pr[k].(int8); return v != 0 }
	p.AirPocket, p.Cold, p.Overgrown = flag("air_pocket"), flag("cold"), flag("overgrown")
	p.Vines, p.Blackstone = flag("vines"), flag("replace_with_blackstone")
	p.Mossiness, _ = pr["mossiness"].(float32)
	return p, p.init()
}

func (p *rpPiece) trace(label string, v any) {
	if p.traceHook != nil {
		p.traceHook(label, v)
	}
}

func (p *rpPiece) traceF(label string, f float32) float32 {
	p.trace(label, f)
	return f
}

func (p *rpPiece) Place(lv Level, r *rng.WorldgenRandom, _ Box, cx, cz int32) error {
	b := p.box
	mx, mz := b.MinX+(b.MaxX-b.MinX+1)/2, b.MinZ+(b.MaxZ-b.MinZ+1)/2
	if mx>>4 != cx || mz>>4 != cz {
		return nil
	}
	reg := data.Canonical()
	p.placeTemplate(reg, lv, r)
	p.spreadNetherrack(reg, lv, r, mx, mz)
	p.decorate(reg, lv, r)
	return nil
}

type rpBlock struct {
	x, y, z int32
	s       data.StateID
	be      nbt.Compound
}

func (p *rpPiece) placeTemplate(reg *data.Registry, lv Level, r *rng.WorldgenRandom) {
	if len(p.tmpl.Palettes) == 0 {
		return
	}
	var out []rpBlock
	for _, tb := range p.tmpl.Palettes[0] {
		x, z := TransformPos(tb.X, tb.Z, p.Mirror, p.Rot, p.px, p.pz)
		wx, wy, wz := p.OX+x, p.OY+tb.Y, p.OZ+z
		if wy < lv.MinY() || wy >= lv.MaxY() {
			continue
		}
		var ok bool
		st := tb.State
		if b := reg.BlockOf(st); b != nil && b.Name == "jigsaw" {
			fs, _ := tb.NBT["final_state"].(string)
			if st, ok = reg.ParseState(fs); !ok {
				continue
			}
		}
		s, ok := p.process(reg, lv, st, wx, wy, wz)
		be := tb.NBT
		if st != tb.State {
			be = nil
		}
		if !ok {
			continue
		}
		if reg.BlockOf(s) != reg.BlockOf(st) {
			be = nil // a chest kept as lava draws no loot seed
		}
		out = append(out, rpBlock{wx, wy, wz, TransformState(s, p.Mirror, p.Rot), be})
	}
	for _, b := range out {
		s := waterlog(reg, lv.Block(b.x, b.y, b.z), b.s)
		lv.SetBlock(b.x, b.y, b.z, s)
		if b.be == nil {
			continue
		}
		id, _ := b.be["id"].(string)
		if id == "" {
			continue
		}
		d := nbt.Compound{}
		for k, v := range b.be {
			switch k {
			case "id", "x", "y", "z":
			default:
				d[k] = v
			}
		}
		if _, ok := d["LootTable"]; ok {
			seed := r.NextLong()
			p.trace("loot", seed)
			d["LootTableSeed"] = seed
		}
		lv.SetBlockEntity(b.x, b.y, b.z, id, d)
	}
	templateWaterlog(reg, lv, out)
	templateShapeUpdates(lv, out)
}

// process runs the portal's processor list on template state s at the world
// position: block_ignore, rule, block_age, protected_blocks,
// lava_submerged_block and (nether) blackstone_replace.
func (p *rpPiece) process(reg *data.Registry, lv Level, s data.StateID, x, y, z int32) (data.StateID, bool) {
	bl := reg.BlockOf(s)
	if bl == nil {
		return s, false
	}
	switch bl.Name {
	case "structure_block":
		return s, false
	case "air":
		if !p.AirPocket {
			return s, false
		}
	}
	r := rng.NewLegacy(rng.GetSeed(x, y, z))
	switch bl.Name {
	case "gold_block":
		if r.NextFloat() < 0.3 {
			s = defaultState(reg, "air")
		}
	case "lava":
		switch {
		case p.Cold:
			s = defaultState(reg, "netherrack")
		case p.Placement == "on_ocean_floor" || r.NextFloat() < 0.2:
			s = defaultState(reg, "magma_block")
		}
	case "netherrack":
		if !p.Cold && r.NextFloat() < 0.07 {
			s = defaultState(reg, "magma_block")
		}
	}
	s = blockAge(reg, s, p.Mossiness, x, y, z)
	cur := lv.Block(x, y, z)
	if cb := reg.BlockOf(cur); cb != nil && p.protected[cb.Name] {
		return s, false
	}
	if b := reg.BlockOf(s); b != nil && b.Name == "lava" {
		p.trace("lava", [3]int32{x, y, z})
	}
	s = lavaSubmerged(reg, s, cur)
	if p.Blackstone {
		s = blackstoneReplace(reg, s)
	}
	return s, true
}

// lavaSubmerged is minecraft:lava_submerged_block. Observed: template lava
// over air, water, stone and grass is written unchanged (57/57 cases).
// Over lava (nether scenes) a template block that is not a full cube on
// every side keeps the lava.
func lavaSubmerged(reg *data.Registry, s, cur data.StateID) data.StateID {
	if b := reg.BlockOf(cur); b == nil || b.Name != "lava" {
		return s
	}
	for _, d := range [...]string{"down", "up", "north", "south", "west", "east"} {
		if !collisionFaceFull(s, d) {
			return cur
		}
	}
	return s
}

// rpSpreadProb is the spread probability by R - manhattan distance.
var rpSpreadProb = [7]float64{0.2, 0.4, 0.6, 0.7, 0.8, 0.9, 0.9}

func (p *rpPiece) spreadNetherrack(reg *data.Registry, lv Level, r *rng.WorldgenRandom, mx, mz int32) {
	b := p.box
	sum := (b.MaxX - b.MinX + 1) + (b.MaxZ - b.MinZ + 1)
	rmin := 6 + sum/4
	// Radius 13 - nextInt(max(1, 14 - rmin)): bounds 5..1 observed for box
	// x+z sizes 12..32 (16x16 giant portals draw nextInt(1)).
	rad := 13 - r.NextIntn(max(1, 14-rmin))
	p.trace("radius", rad)
	for x := mx - rad; x <= mx+rad; x++ {
		w := rad - abs32(x-mx)
		for z := mz - w; z <= mz+w; z++ {
			k := rad - abs32(x-mx) - abs32(z-mz)
			pr := 1.0
			if int(k) < len(rpSpreadProb) {
				pr = rpSpreadProb[k]
			}
			d := r.NextDouble()
			p.trace("D", d)
			if d >= pr {
				continue
			}
			y, ok := p.surfaceY(lv, x, z)
			p.trace("spread", [4]int32{x, z, y, map[bool]int32{false: 0, true: 1}[ok]})
			if !ok || !p.canReplace(reg, lv.Block(x, y, z)) {
				continue
			}
			p.placeNetherrack(reg, lv, r, x, y, z)
			if p.Overgrown {
				p.maybeLeaves(reg, lv, r, x, y, z)
			}
			p.dripColumn(reg, lv, r, x, y-1, z)
		}
	}
	// Columns below the portal: every netherrack of the bottom layer's
	// interior (box shrunk by one horizontally) drips down.
	b = p.box
	nr := defaultState(reg, "netherrack")
	for x := b.MinX + 1; x < b.MaxX; x++ {
		for z := b.MinZ + 1; z < b.MaxZ; z++ {
			if lv.Block(x, b.MinY, z) == nr {
				p.dripColumn(reg, lv, r, x, b.MinY-1, z)
			}
		}
	}
}

// dripColumn places netherrack at y and below, up to 9 blocks, continuing
// while nextFloat < 0.5.
func (p *rpPiece) dripColumn(reg *data.Registry, lv Level, r *rng.WorldgenRandom, x, y, z int32) {
	for k := int32(0); k < 9; k++ {
		p.placeNetherrack(reg, lv, r, x, y-k, z)
		if k == 8 || p.traceF("drip", r.NextFloat()) >= 0.5 {
			break
		}
	}
}

// surfaceY is the column's spread height: heightmap - 1 (OCEAN_FLOOR_WG on
// the ocean floor, WORLD_SURFACE_WG otherwise), at most the box bottom for
// the buried placements (underground, partly_buried, in_mountain,
// in_nether), accepted within 3 blocks of the box bottom.
func (p *rpPiece) surfaceY(lv Level, x, z int32) (int32, bool) {
	hm := "WORLD_SURFACE_WG"
	if p.Placement == "on_ocean_floor" {
		hm = "OCEAN_FLOOR_WG"
	}
	y := lv.Height(hm, x, z) - 1
	if p.Placement != "on_land_surface" && p.Placement != "on_ocean_floor" {
		y = min(y, p.box.MinY)
	}
	return y, y >= p.box.MinY-3 && y <= p.box.MinY+3
}

func (p *rpPiece) canReplace(reg *data.Registry, s data.StateID) bool {
	b := reg.BlockOf(s)
	if b == nil || reg.IsAir(s) {
		return false
	}
	if b.Name == "lava" && p.Placement != "in_nether" {
		return false
	}
	return b.Name != "obsidian" && !p.protected[b.Name]
}

func (p *rpPiece) placeNetherrack(reg *data.Registry, lv Level, r *rng.WorldgenRandom, x, y, z int32) {
	if !p.canReplace(reg, lv.Block(x, y, z)) {
		return
	}
	s := defaultState(reg, "netherrack")
	if !p.Cold && p.traceF("place", r.NextFloat()) < 0.07 {
		s = defaultState(reg, "magma_block")
	}
	lv.SetBlock(x, y, z, s)
}

var rpHoriz = [4][3]int32{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
var rpOpp = [4]string{"south", "west", "north", "east"}
var rpDirName = [4]string{"north", "east", "south", "west"}

// decorate runs over the box (x inner, then y, then z): vines, then
// overgrowth.
//   - Vines: every block but air and vines draws a horizontal direction
//     (N, E, S, W); an air neighbour there gets a vine attached back when
//     the block's collision face is full (leaves count).
//   - Overgrown: maybeLeaves at every position.
func (p *rpPiece) decorate(reg *data.Registry, lv Level, r *rng.WorldgenRandom) {
	if !p.Vines && !p.Overgrown {
		return
	}
	b := p.box
	for z := b.MinZ; z <= b.MaxZ; z++ {
		for y := b.MinY; y <= b.MaxY; y++ {
			for x := b.MinX; x <= b.MaxX; x++ {
				if p.Vines {
					p.maybeVine(reg, lv, r, x, y, z)
				}
				if p.Overgrown {
					p.maybeLeaves(reg, lv, r, x, y, z)
				}
			}
		}
	}
}

func (p *rpPiece) maybeVine(reg *data.Registry, lv Level, r *rng.WorldgenRandom, x, y, z int32) {
	s := lv.Block(x, y, z)
	if bl := reg.BlockOf(s); reg.IsAir(s) || bl == nil || bl.Name == "vine" {
		return
	}
	d := r.NextIntn(4)
	p.trace("vine", d)
	o := rpHoriz[d]
	nx, ny, nz := x+o[0], y+o[1], z+o[2]
	if !reg.IsAir(lv.Block(nx, ny, nz)) || !collisionFaceFull(s, rpDirName[d]) {
		return
	}
	v, _ := reg.StateFrom("vine", func(k string) (string, bool) {
		if k == rpOpp[d] {
			return "true", true
		}
		return "", false
	})
	lv.SetBlock(nx, ny, nz, v)
}

// maybeLeaves: nextFloat < 0.5 puts persistent jungle leaves (distance 7)
// on a netherrack block with air above.
func (p *rpPiece) maybeLeaves(reg *data.Registry, lv Level, r *rng.WorldgenRandom, x, y, z int32) {
	if p.traceF("leaves", r.NextFloat()) >= 0.5 {
		return
	}
	if b := reg.BlockOf(lv.Block(x, y, z)); b == nil || b.Name != "netherrack" || !reg.IsAir(lv.Block(x, y+1, z)) {
		return
	}
	s, _ := reg.ParseState("minecraft:jungle_leaves[distance=7,persistent=true,waterlogged=false]")
	lv.SetBlock(x, y+1, z, s)
}

// RuinedPortalTrace sets a trace hook on a ruined portal piece (tests).
func RuinedPortalTrace(p Piece, f func(label string, v any)) {
	if rp, ok := p.(*rpPiece); ok {
		rp.traceHook = f
	}
}

// Generate is the start: findGenerationPoint (see ruined_portal.go), then
// the biome test at the template origin.
func (t *ruinedPortalType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Column == nil || ctx.Height == nil {
		return nil, nil
	}
	minY := ctx.MinY
	if minY == 0 && ctx.MaxY == 0 {
		minY = -64
	}
	biome := func(x, y, z int32) data.BiomeID { return ctx.Biome(x>>2, y>>2, z>>2) }
	st, err := t.start(ctx.Random, ctx.ChunkX, ctx.ChunkZ, ctx.Height, ctx.Column, biome, minY)
	if err != nil {
		return nil, err
	}
	if ctx.BiomeOK != nil && !ctx.BiomeOK(st.OX, st.OY, st.OZ) {
		return nil, nil
	}
	p, err := newRPPiece(st)
	if err != nil {
		return nil, err
	}
	return []Piece{p}, nil
}

// TryStructure generates structure key's start at chunk cx,cz, ignoring
// the structure set's placement (tests, tools).
func (s *Stages) TryStructure(key string, cx, cz int32) (*Start, error) {
	return s.tryStructure(WeightedStructure{Structure: key}, cx, cz)
}

func init() {
	RegisterPiece("minecraft:rupo", loadRPPiece)
	RegisterType("minecraft:ruined_portal", func(_ string, raw json.RawMessage) (Type, error) { return newRuinedPortalType(raw) })
}
