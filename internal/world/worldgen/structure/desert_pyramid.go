package structure

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Desert pyramid, inferred black-box (kremenmc.probe.temple.TStarts,
// TScene, RegionRand; see docs/vanilla-parity.md "Desert pyramid"):
//   - start: WORLD_SURFACE_WG at the chunk min corner and +21 on x, z, both
//     (4 corners); none when the lowest is below 64; biome at (min+8,
//     height-1, min+8); one nextInt(4) orientation (saved O as the swamp
//     hut); box 21x15x21 at the chunk min corner, y 64;
//   - placement (every chunk): placement random nextInt(3) first; box min
//     y = lowest height over the box's 441 columns minus the first-placing
//     chunk's draw (kremenmc: the start chunk's draw, see "first chunk");
//     then the recorded ops (legacy_ops.go); then the suspicious sand.

//go:embed legacy_desert_pyramid.txt.gz
var desertPyramidGz []byte

var desertPyramidOps = sync.OnceValues(func() ([]legacyOp, error) {
	return loadLegacyOps("desert pyramid", desertPyramidGz)
})

const desertPyramidKey = "minecraft:desert_pyramid"

type desertPyramidType struct{}

func (desertPyramidType) Step() int { return 4 }

func (desertPyramidType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	low := int32(1 << 30)
	for _, c := range [][2]int32{{0, 0}, {0, 21}, {21, 0}, {21, 21}} {
		low = min(low, ctx.Height(x0+c[0], z0+c[1], "WORLD_SURFACE_WG"))
	}
	if low < 64 {
		return nil, nil
	}
	h := ctx.Height(x0+8, z0+8, "WORLD_SURFACE_WG")
	if ctx.BiomeOK != nil && !ctx.BiomeOK(x0+8, h-1, z0+8) {
		return nil, nil
	}
	d := ctx.Random.NextIntn(4)
	return []Piece{&desertPyramidPiece{box: Box{x0, 64, z0, x0 + 20, 78, z0 + 20}, o: hut2D[d], hpos: -1}}, nil
}

type desertPyramidPiece struct {
	box  Box
	o    int32
	hpos int32
}

func (p *desertPyramidPiece) Type() string { return "minecraft:tedp" }
func (p *desertPyramidPiece) Box() Box     { return p.box }

func (p *desertPyramidPiece) Save() nbt.Compound {
	c := nbt.Compound{"id": "minecraft:tedp", "GD": int32(0), "O": p.o, "BB": p.box.IntArray(),
		"Width": int32(21), "Height": int32(15), "Depth": int32(21), "HPos": p.hpos}
	for i := 0; i < 4; i++ {
		c[fmt.Sprintf("hasPlacedChest%d", i)] = int8(0)
	}
	return c
}

func loadDesertPyramid(c nbt.Compound) (Piece, error) {
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return nil, fmt.Errorf("desert pyramid: bad BB")
	}
	o, _ := c["O"].(int32)
	hp, ok := c["HPos"].(int32)
	if !ok {
		hp = -1
	}
	return &desertPyramidPiece{box: Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}, o: o, hpos: hp}, nil
}

// placedBox is the box at its placement height. Vanilla moves the box on
// the first placing chunk (lowest MOTION_BLOCKING_NO_LEAVES of the box's
// columns minus that chunk's first nextInt(3)); kremenmc takes the lowest
// frozen WORLD_SURFACE_WG and the start chunk's draw, a pure function of
// the world, so every chunk agrees whatever the decoration order.
func scatteredPlacedBox(lv Level, b Box, hpos int32, key string) Box {
	if hpos >= 0 {
		return b
	}
	low := int32(1 << 30)
	for z := b.MinZ; z <= b.MaxZ; z++ {
		for x := b.MinX; x <= b.MaxX; x++ {
			low = min(low, wgSurface(lv, x, z))
		}
	}
	off := placementRandom(levelSeed(lv), key, b.MinX>>4, b.MinZ>>4).NextIntn(3)
	dy := low - off - b.MinY
	b.MinY += dy
	b.MaxY += dy
	return b
}

func (p *desertPyramidPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	ops, err := desertPyramidOps()
	if err != nil {
		return err
	}
	r.NextIntn(3)
	f := newLegacyFrame(scatteredPlacedBox(lv, p.box, p.hpos, desertPyramidKey), p.o)
	run := legacyRun{lv: lv, r: r, rr: regionRandom(lv, cx, cz), clip: clip, f: f, reg: data.Canonical(),
		chestLoot: "minecraft:chests/desert_pyramid"}
	run.ops(ops)
	p.suspiciousSand(&run)
	return nil
}

// suspiciousSand is the structure's afterPlace (every placing chunk): the
// collapsed roof's random position (legacy positional random of the world
// seed at the roof's local origin; local x then z nextInt(5)) becomes
// suspicious sand; the potential positions, sorted by y, z, x, are
// shuffled with the legacy positional random at the box centre, the first
// 5+nextInt(3) become suspicious sand, the rest sand. Suspicious sand gets
// LootTable archaeology/desert_pyramid and LootTableSeed = the packed
// position (BlockPos.asLong), whatever the world seed.
func (p *desertPyramidPiece) suspiciousSand(run *legacyRun) {
	reg := run.reg
	seed := levelSeed(run.lv)
	sus, _ := reg.ParseState("minecraft:suspicious_sand[dusted=0]")
	sand, _ := reg.ParseState("minecraft:sand")
	place := func(x, y, z int32, s data.StateID) {
		if !run.clip.Contains(x, y, z) {
			return
		}
		run.lv.SetBlock(x, y, z, s)
		if s == sus {
			run.lv.SetBlockEntity(x, y, z, "minecraft:brushable_block", nbt.Compound{
				"LootTable": "minecraft:archaeology/desert_pyramid", "LootTableSeed": packedPos(x, y, z)})
		}
	}
	if run.roofSet {
		ox, oy, oz := run.f.world(run.roof[0], run.roof[1], run.roof[2])
		rr := rng.NewLegacy(seed).ForkPositional().At(ox, oy, oz)
		lx, lz := rr.NextIntn(5), rr.NextIntn(5)
		x, y, z := run.f.world(run.roof[0]+lx, run.roof[1], run.roof[2]+lz)
		place(x, y, z, sus)
	}
	pos := append([][3]int32(nil), run.pot...)
	sort.Slice(pos, func(i, j int) bool {
		a, b := pos[i], pos[j]
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		if a[2] != b[2] {
			return a[2] < b[2]
		}
		return a[0] < b[0]
	})
	b := run.f.box
	sr := rng.NewLegacy(seed).ForkPositional().At(b.MinX+(b.MaxX-b.MinX+1)/2, b.MinY+(b.MaxY-b.MinY+1)/2, b.MinZ+(b.MaxZ-b.MinZ+1)/2)
	for i := int32(len(pos)); i > 1; i-- {
		j := sr.NextIntn(i)
		pos[i-1], pos[j] = pos[j], pos[i-1]
	}
	n := int(5 + sr.NextIntn(3))
	for i, q := range pos {
		if i < n {
			place(q[0], q[1], q[2], sus)
		} else {
			place(q[0], q[1], q[2], sand)
		}
	}
}

// packedPos is BlockPos.asLong.
func packedPos(x, y, z int32) int64 {
	return (int64(x)&0x3FFFFFF)<<38 | (int64(z)&0x3FFFFFF)<<12 | (int64(y) & 0xFFF)
}

// legacyRun executes recorded ops for one chunk.
type legacyRun struct {
	lv        Level
	r         *rng.WorldgenRandom
	rr        rng.Source
	clip      Box
	f         legacyFrame
	reg       *data.Registry
	chestLoot string
	dispLoot  string
	flag      bool
	pot       [][3]int32 // world positions
	roof      [3]int32   // local origin of the collapsed roof
	roofSet   bool
}

func (u *legacyRun) set(x, y, z int32, s data.StateID) {
	wx, wy, wz := u.f.world(x, y, z)
	if u.clip.Contains(wx, wy, wz) {
		u.lv.SetBlock(wx, wy, wz, u.f.state(s))
	}
}

func (u *legacyRun) ops(ops []legacyOp) {
	reg := u.reg
	sandstone, _ := reg.ParseState("minecraft:sandstone")
	sand, _ := reg.ParseState("minecraft:sand")
	cobble, _ := reg.ParseState("minecraft:cobblestone")
	mossy, _ := reg.ParseState("minecraft:mossy_cobblestone")
	for _, o := range ops {
		switch o.kind {
		case 'S':
			u.set(o.x, o.y, o.z, o.s0)
		case 'C':
			wx, wy, wz := u.f.world(o.x, o.y, o.z)
			if u.clip.Contains(wx, wy, wz) && !reg.IsAir(u.lv.Block(wx, wy, wz)) {
				u.lv.SetBlock(wx, wy, wz, u.f.state(o.s0))
			}
		case 'F':
			wx, wy, wz := u.f.world(o.x, o.y, o.z)
			if !u.clip.Contains(wx, wy, wz) {
				continue
			}
			s := u.f.state(o.s0)
			for y := wy; y > u.lv.MinY(); y-- {
				b := u.lv.Block(wx, y, wz)
				if !fillsDown(reg, b) {
					break
				}
				u.lv.SetBlock(wx, y, wz, s)
			}
		case 'b':
			u.flag = u.rr.NextBool()
		case 'B':
			if u.flag {
				u.set(o.x, o.y, o.z, o.s1)
			} else {
				u.set(o.x, o.y, o.z, o.s0)
			}
		case 'R':
			if !u.roofSet || o.x < u.roof[0] || (o.x == u.roof[0] && o.z < u.roof[2]) {
				u.roof, u.roofSet = [3]int32{o.x, o.y, o.z}, true
			}
			if u.rr.NextFloat() < 0.33 {
				u.set(o.x, o.y, o.z, sandstone)
			} else {
				u.set(o.x, o.y, o.z, sand)
			}
		case 'K':
			wx, wy, wz := u.f.world(o.x, o.y, o.z)
			if !u.clip.Contains(wx, wy, wz) {
				continue
			}
			if b := reg.BlockOf(u.lv.Block(wx, wy, wz)); b != nil && b.Name == "chest" {
				continue
			}
			f := reorientChest(u.lv, reg, wx, wy, wz, "north")
			cs, _ := reg.ParseState("minecraft:chest[facing=" + f + ",type=single,waterlogged=false]")
			u.lv.SetBlock(wx, wy, wz, cs)
			u.lv.SetBlockEntity(wx, wy, wz, "minecraft:chest", nbt.Compound{"LootTable": u.chestLoot, "LootTableSeed": u.r.NextLong()})
		case 'M':
			// MossStoneSelector: placement random nextFloat per block,
			// < 0.4 cobblestone, else mossy cobblestone (1522 draws).
			if u.r.NextFloat() < 0.4 {
				u.set(o.x, o.y, o.z, cobble)
			} else {
				u.set(o.x, o.y, o.z, mossy)
			}
		case 'D':
			wx, wy, wz := u.f.world(o.x, o.y, o.z)
			if !u.clip.Contains(wx, wy, wz) {
				continue
			}
			if b := reg.BlockOf(u.lv.Block(wx, wy, wz)); b != nil && b.Name == "dispenser" {
				continue
			}
			u.lv.SetBlock(wx, wy, wz, u.f.state(o.s0))
			u.lv.SetBlockEntity(wx, wy, wz, "minecraft:dispenser", nbt.Compound{"LootTable": u.dispLoot, "LootTableSeed": u.r.NextLong()})
		case 'P':
			wx, wy, wz := u.f.world(o.x, o.y, o.z)
			u.pot = append(u.pot, [3]int32{wx, wy, wz})
		}
	}
}

func init() {
	RegisterPiece("minecraft:tedp", loadDesertPyramid)
	RegisterType("minecraft:desert_pyramid", func(key string, _ json.RawMessage) (Type, error) {
		if !legacyEnabled[key] {
			return nil, nil
		}
		return desertPyramidType{}, nil
	})
}
