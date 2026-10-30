package structure

import (
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Ocean ruin placement, inferred from kremenmc.probe.oruin.ORScene logs
// (synthetic sea floors and kremenmc terrain scenes) and
// kremenmc.probe.oruin.DrownedSpawn (3000 finalizeSpawn calls); see
// docs/vanilla-parity.md "Ocean ruins".
//
// Every chunk the piece box touches recomputes the piece height:
// h = OCEAN_FLOOR_WG at the template origin; each footprint column is
// scanned down from h-1 while the block is air, holds water or is in #ice
// (live blocks); when at least (box x width - 2) columns end 3 or more
// below h-1, y = lowest column floor + 1, else y = h.
//
// Template: block_rot(integrity) on a positional legacy random (first
// nextFloat > integrity drops the block), block_ignore air and structure
// blocks, then capped(5) gravel/sand -> suspicious with archaeology loot on
// the whole list. Blocks inside the chunk are written in template order; a
// waterloggable block written into a water source becomes waterlogged and
// schedules a water tick. Then every written block runs its neighbour shape
// update (six directions), then the faces of the written set update both
// sides (DiscreteVoxelShape.forAllFaces order: Z faces x,y,z; Y faces
// z,x,y; X faces y,z,x; negative face first). Shape updates schedule
// falling-block ticks (2) and water ticks (5), turn a kelp_plant whose top
// is no longer kelp into kelp with age region-random nextInt(25) and
// re-derive stairs/walls/fences.
//
// DATA markers inside the chunk, template order: "chest" places
// chest[facing=north] (waterlogged in water) with
// chests/underwater_ruin_{big,small} and the placement random's nextLong;
// "drowned" spawns a persistent drowned (finalized on the region random,
// see drownedSpawn) and leaves water at or below sea level, air above.

const oceanRuinSeaLevel = 63

// oceanRuinYHook sees the height of every placement (tests).
var oceanRuinYHook func(y int32)

func (p *oceanRuinPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	reg := data.Canonical()
	t := p.tmpl
	if len(t.Palettes) == 0 {
		return nil
	}
	y := p.floorY(reg, lv, lv.Height("OCEAN_FLOOR_WG", p.OX, p.OZ))
	if oceanRuinYHook != nil {
		oceanRuinYHook(y)
	}
	pl, err := oceanRuinCapped(p.biomeType)
	if err != nil {
		return err
	}
	var list []cappedEntry
	var markers []marker
	for _, b := range t.Palettes[0] {
		x, z := TransformPos(b.X, b.Z, MirrorNone, p.Rot, 0, 0)
		wx, wy, wz := p.OX+x, y+b.Y, p.OZ+z
		bl := reg.BlockOf(b.State)
		if bl == nil {
			continue
		}
		if bl.Name == "structure_block" {
			if mode, _ := b.NBT["mode"].(string); mode == "DATA" {
				md, _ := b.NBT["metadata"].(string)
				markers = append(markers, marker{wx, wy, wz, md})
			}
			continue
		}
		if rng.NewLegacy(rng.GetSeed(wx, wy, wz)).NextFloat() > p.integrity {
			continue
		}
		if bl.Name == "air" {
			continue
		}
		list = append(list, cappedEntry{wx, wy, wz, TransformState(b.State, MirrorNone, p.Rot), b.NBT})
	}
	pl.finalizeCapped(lv, reg, p.OX, y, p.OZ, list)

	rr := regionRandom(lv, cx, cz)
	var placed []rpBlock
	for _, e := range list {
		if !clip.Contains(e.x, e.y, e.z) || e.y < lv.MinY() || e.y >= lv.MaxY() {
			continue
		}
		s := e.state
		if waterFluid(reg, lv.Block(e.x, e.y, e.z)) && stateProp(s, "waterlogged") == "false" {
			s = withProp(s, "waterlogged", "true")
			scheduleTick(lv, e.x, e.y, e.z, "minecraft:water", 5, true)
		}
		lv.SetBlock(e.x, e.y, e.z, s)
		if e.be != nil {
			if id, _ := e.be["id"].(string); id != "" {
				d := nbt.Compound{}
				for k, v := range e.be {
					switch k {
					case "id", "x", "y", "z":
					default:
						d[k] = v
					}
				}
				if lootContainers[trimNS(id)] {
					seed := r.NextLong()
					if _, ok := d["LootTable"]; ok {
						d["LootTableSeed"] = seed
					}
				}
				lv.SetBlockEntity(e.x, e.y, e.z, id, d)
			}
		}
		// A real WorldGenRegion.setBlock reports success even when the
		// state is unchanged, so every written block is in the shape
		// (probe RegionSet).
		placed = append(placed, rpBlock{x: e.x, y: e.y, z: e.z, s: s})
	}
	orWaterlogSpread(reg, lv, placed)
	orEdgeUpdates(reg, lv, rr, placed)
	for _, b := range placed {
		s := lv.Block(b.x, b.y, b.z)
		n := s
		for _, d := range dirOrder {
			n = orUpdateShape(reg, lv, rr, b.x, b.y, b.z, n, d)
		}
		if n != s {
			lv.SetBlock(b.x, b.y, b.z, n)
		}
	}

	for _, m := range markers {
		if !clip.Contains(m.x, m.y, m.z) {
			continue
		}
		switch m.meta {
		case "chest":
			s := defaultState(reg, "chest")
			s = withProp(s, "facing", "north")
			if waterFluid(reg, lv.Block(m.x, m.y, m.z)) {
				s = withProp(s, "waterlogged", "true")
			}
			lv.SetBlock(m.x, m.y, m.z, s)
			lt := "minecraft:chests/underwater_ruin_small"
			if p.large {
				lt = "minecraft:chests/underwater_ruin_big"
			}
			lv.SetBlockEntity(m.x, m.y, m.z, "minecraft:chest", nbt.Compound{"LootTable": lt, "LootTableSeed": r.NextLong()})
		case "drowned":
			for _, e := range drownedSpawn(reg, lv, rr, m.x, m.y, m.z) {
				lv.AddEntity(e)
			}
			sea := int32(oceanRuinSeaLevel)
			if sl, ok := lv.(interface{ SeaLevel() int32 }); ok {
				sea = sl.SeaLevel()
			}
			if m.y > sea {
				lv.SetBlock(m.x, m.y, m.z, defaultState(reg, "air"))
			} else {
				lv.SetBlock(m.x, m.y, m.z, defaultState(reg, "water"))
			}
		}
	}
	return nil
}

// floorY is OceanRuinPiece.getHeight (see the file comment).
func (p *oceanRuinPiece) floorY(reg *data.Registry, lv Level, h int32) int32 {
	b := p.Box()
	top := h - 1
	minFloor := int32(1 << 30)
	deep := int32(0)
	ice := iceBlocks()
	for z := b.MinZ; z <= b.MaxZ; z++ {
		for x := b.MinX; x <= b.MaxX; x++ {
			y := top
			for y > lv.MinY() {
				s := lv.Block(x, y, z)
				bl := reg.BlockOf(s)
				if !(reg.IsAir(s) || waterFluid(reg, s) || (bl != nil && ice[bl.Name])) {
					break
				}
				y--
			}
			minFloor = min(minFloor, y)
			if y < top-2 {
				deep++
			}
		}
	}
	if top-minFloor > 2 && deep >= b.MaxX-b.MinX+1-2 {
		return minFloor + 1
	}
	return h
}

var iceBlocks = sync.OnceValue(func() map[string]bool {
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	return blockTag(pk, "#minecraft:ice")
})

// orWaterlogSpread: a written dry waterloggable block next to a water
// source above or on a horizontal side becomes waterlogged (and schedules
// a water tick); passes in write order until nothing changes.
func orWaterlogSpread(reg *data.Registry, lv Level, placed []rpBlock) {
	var wl []rpBlock
	for _, b := range placed {
		if stateProp(lv.Block(b.x, b.y, b.z), "waterlogged") == "false" {
			wl = append(wl, b)
		}
	}
	nb := [5][3]int32{{0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
	for changed := true; changed; {
		changed = false
		for _, b := range wl {
			s := lv.Block(b.x, b.y, b.z)
			if stateProp(s, "waterlogged") != "false" {
				continue
			}
			for _, d := range nb {
				if waterSourceFluid(reg, lv.Block(b.x+d[0], b.y+d[1], b.z+d[2])) {
					lv.SetBlock(b.x, b.y, b.z, withProp(s, "waterlogged", "true"))
					scheduleTick(lv, b.x, b.y, b.z, "minecraft:water", 5, true)
					changed = true
					break
				}
			}
		}
	}
}

// waterSourceFluid is waterFluid restricted to source water.
func waterSourceFluid(reg *data.Registry, s data.StateID) bool {
	if b := reg.BlockOf(s); b != nil && b.Name == "water" {
		return s == b.DefaultState
	}
	return waterFluid(reg, s)
}

// waterFluid reports a block whose fluid is water: water (any level),
// waterlogged blocks and the always-water plants.
func waterFluid(reg *data.Registry, s data.StateID) bool {
	b := reg.BlockOf(s)
	if b == nil {
		return false
	}
	switch b.Name {
	case "water", "kelp", "kelp_plant", "seagrass", "tall_seagrass", "bubble_column":
		return true
	}
	return stateProp(s, "waterlogged") == "true"
}

var oceanRuinCappedLists sync.Map

// oceanRuinCapped is the capped(5) archaeology processor of a cold or warm
// ruin (OrSettings: gravel/sand rule with append_loot).
func oceanRuinCapped(bt string) (*ProcessorList, error) {
	if v, ok := oceanRuinCappedLists.Load(bt); ok {
		return v.(*ProcessorList), nil
	}
	in, out, lt := "minecraft:gravel", "minecraft:suspicious_gravel", "minecraft:archaeology/ocean_ruin_cold"
	if bt == "WARM" {
		in, out, lt = "minecraft:sand", "minecraft:suspicious_sand", "minecraft:archaeology/ocean_ruin_warm"
	}
	js := fmt.Sprintf(`[{"processor_type":"minecraft:capped","limit":5,"delegate":{"processor_type":"minecraft:rule","rules":[{"input_predicate":{"predicate_type":"minecraft:block_match","block":%q},"location_predicate":{"predicate_type":"minecraft:always_true"},"output_state":{"Name":%q},"block_entity_modifier":{"type":"minecraft:append_loot","loot_table":%q}}]}}]`, in, out, lt)
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	pl, err := ParseProcessorList(pk, []byte(js))
	if err != nil {
		return nil, err
	}
	oceanRuinCappedLists.Store(bt, pl)
	return pl, nil
}

func scheduleTick(lv Level, x, y, z int32, typ string, delay int32, fluid bool) {
	if t, ok := lv.(interface {
		ScheduleBlockTick(x, y, z int32, typ string, delay int32, fluid bool)
	}); ok {
		t.ScheduleBlockTick(x, y, z, typ, delay, fluid)
	}
}

type dir3 struct {
	dx, dy, dz int32
	name       string
}

// dirOrder is Direction order: down, up, north, south, west, east.
var dirOrder = [6]dir3{{0, -1, 0, "down"}, {0, 1, 0, "up"}, {0, 0, -1, "north"}, {0, 0, 1, "south"}, {-1, 0, 0, "west"}, {1, 0, 0, "east"}}

var fallingBlocks = map[string]bool{
	"gravel": true, "sand": true, "red_sand": true, "suspicious_gravel": true, "suspicious_sand": true,
	"anvil": true, "chipped_anvil": true, "damaged_anvil": true, "dragon_egg": true,
	"white_concrete_powder": true, "orange_concrete_powder": true, "magenta_concrete_powder": true, "light_blue_concrete_powder": true,
	"yellow_concrete_powder": true, "lime_concrete_powder": true, "pink_concrete_powder": true, "gray_concrete_powder": true,
	"light_gray_concrete_powder": true, "cyan_concrete_powder": true, "purple_concrete_powder": true, "blue_concrete_powder": true,
	"brown_concrete_powder": true, "green_concrete_powder": true, "red_concrete_powder": true, "black_concrete_powder": true,
}

// orUpdateShape is BlockState.updateShape of the block s at x,y,z after its
// neighbour in direction d changed (proto-chunk semantics: ticks recorded,
// no neighbour notifications).
func orUpdateShape(reg *data.Registry, lv Level, rr rng.Source, x, y, z int32, s data.StateID, d dir3) data.StateID {
	bl := reg.BlockOf(s)
	if bl == nil {
		return s
	}
	if fallingBlocks[bl.Name] {
		scheduleTick(lv, x, y, z, "minecraft:"+bl.Name, 2, false)
	}
	air := defaultState(reg, "air")
	switch bl.Name {
	case "seagrass":
		if !plantSurvives(reg, lv.Block(x, y-1, z)) {
			return air
		}
	case "tall_seagrass":
		other := lv.Block(x, y+1, z)
		want := "upper"
		if stateProp(s, "half") == "upper" {
			other, want = lv.Block(x, y-1, z), "lower"
		}
		if ob := reg.BlockOf(other); ob == nil || ob.Name != "tall_seagrass" || stateProp(other, "half") != want {
			return air
		}
		if want == "upper" && !plantSurvives(reg, lv.Block(x, y-1, z)) {
			return air
		}
		return s
	case "sea_pickle":
		if !collisionFaceFull(lv.Block(x, y-1, z), "up") {
			return air
		}
	case "kelp_plant":
		if d.dy == -1 && !kelpSurvives(reg, lv.Block(x, y-1, z)) {
			scheduleTick(lv, x, y, z, "minecraft:kelp_plant", 1, false)
		}
		if d.dy == 1 {
			if ab := reg.BlockOf(lv.Block(x, y+1, z)); ab == nil || (ab.Name != "kelp" && ab.Name != "kelp_plant") {
				k, _ := reg.ParseState(fmt.Sprintf("minecraft:kelp[age=%d]", rr.NextIntn(25)))
				return k
			}
		}
	case "kelp":
		if d.dy == -1 && !kelpSurvives(reg, lv.Block(x, y-1, z)) {
			scheduleTick(lv, x, y, z, "minecraft:kelp", 1, false)
		}
	}
	if waterFluid(reg, s) {
		scheduleTick(lv, x, y, z, "minecraft:water", 5, true)
	}
	switch bl.Name {
	case "water":
		if b := reg.BlockOf(lv.Block(x, y-1, z)); d.dy == -1 && b != nil && (b.Name == "magma_block" || b.Name == "soul_sand") {
			scheduleTick(lv, x, y, z, "minecraft:water", 20, false)
		}
	}
	if famOf(s).fam != famNone {
		return reshapeAt(lv, x, y, z, s)
	}
	return s
}

// plantSurvives: seagrass stands on a sturdy top face other than magma.
func plantSurvives(reg *data.Registry, below data.StateID) bool {
	if b := reg.BlockOf(below); b == nil || b.Name == "magma_block" {
		return false
	}
	return collisionFaceFull(below, "up")
}

// kelpSurvives: kelp stands on kelp or a sturdy top face other than magma.
func kelpSurvives(reg *data.Registry, below data.StateID) bool {
	b := reg.BlockOf(below)
	if b == nil {
		return false
	}
	switch b.Name {
	case "kelp", "kelp_plant":
		return true
	case "magma_block":
		return false
	}
	return collisionFaceFull(below, "up")
}

// orEdgeUpdates runs updateShapeAtEdge over the written set.
func orEdgeUpdates(reg *data.Registry, lv Level, rr rng.Source, placed []rpBlock) {
	if len(placed) == 0 {
		return
	}
	in := make(map[[3]int32]bool, len(placed))
	b := Box{MinX: placed[0].x, MinY: placed[0].y, MinZ: placed[0].z, MaxX: placed[0].x, MaxY: placed[0].y, MaxZ: placed[0].z}
	for _, p := range placed {
		in[[3]int32{p.x, p.y, p.z}] = true
		b = b.Encapsulate(Box{p.x, p.y, p.z, p.x, p.y, p.z})
	}
	face := func(x, y, z int32, neg, pos dir3) {
		if !in[[3]int32{x, y, z}] {
			return
		}
		for _, d := range [2]dir3{neg, pos} {
			nx, ny, nz := x+d.dx, y+d.dy, z+d.dz
			if in[[3]int32{nx, ny, nz}] {
				continue
			}
			s := lv.Block(x, y, z)
			if n := orUpdateShape(reg, lv, rr, x, y, z, s, d); n != s {
				lv.SetBlock(x, y, z, n)
			}
			if ny < lv.MinY() || ny >= lv.MaxY() {
				continue
			}
			o := lv.Block(nx, ny, nz)
			if n := orUpdateShape(reg, lv, rr, nx, ny, nz, o, dir3{-d.dx, -d.dy, -d.dz, ""}); n != o {
				lv.SetBlock(nx, ny, nz, n)
			}
		}
	}
	north, south := dirOrder[2], dirOrder[3]
	down, up := dirOrder[0], dirOrder[1]
	west, east := dirOrder[4], dirOrder[5]
	for x := b.MinX; x <= b.MaxX; x++ {
		for y := b.MinY; y <= b.MaxY; y++ {
			for z := b.MinZ; z <= b.MaxZ; z++ {
				face(x, y, z, north, south)
			}
		}
	}
	for z := b.MinZ; z <= b.MaxZ; z++ {
		for x := b.MinX; x <= b.MaxX; x++ {
			for y := b.MinY; y <= b.MaxY; y++ {
				face(x, y, z, down, up)
			}
		}
	}
	for y := b.MinY; y <= b.MaxY; y++ {
		for z := b.MinZ; z <= b.MaxZ; z++ {
			for x := b.MinX; x <= b.MaxX; x++ {
				face(x, y, z, west, east)
			}
		}
	}
}
