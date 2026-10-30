package structure

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Jungle temple (jungle_pyramid), inferred black-box like the desert
// pyramid (kremenmc.probe.temple.TStarts, TScene):
//   - start: WORLD_SURFACE_WG at the chunk min corner, +12 on x and +15 on
//     z (4 corners, unrotated); none when the lowest is below 64 (64
//     passes); biome at (min+8, height-1, min+8); one nextInt(4)
//     orientation; box 12 (x) x 10 x 15 (z) at the chunk min corner, y 64,
//     x and z swapped for east/west; the whole temple lies in one chunk;
//   - placement: box min y = integer mean of the height over the box's
//     180 columns (vanilla MOTION_BLOCKING_NO_LEAVES on placement;
//     kremenmc the frozen WORLD_SURFACE_WG), then the recorded ops: moss
//     stone (placement random nextFloat < 0.4 cobblestone, else mossy),
//     air, traps, two dispensers (chests/jungle_temple_dispenser) and two
//     chests (chests/jungle_temple), each drawing its LootTableSeed.

//go:embed legacy_jungle_pyramid.txt.gz
var junglePyramidGz []byte

var junglePyramidOps = sync.OnceValues(func() ([]legacyOp, error) {
	return loadLegacyOps("jungle pyramid", junglePyramidGz)
})

type junglePyramidType struct{}

func (junglePyramidType) Step() int { return 4 }

func (junglePyramidType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	low := int32(1 << 30)
	for _, c := range [][2]int32{{0, 0}, {0, 15}, {12, 0}, {12, 15}} {
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
	b := Box{x0, 64, z0, x0 + 11, 73, z0 + 14}
	if d == 1 || d == 3 {
		b = Box{x0, 64, z0, x0 + 14, 73, z0 + 11}
	}
	return []Piece{&junglePyramidPiece{box: b, o: hut2D[d], hpos: -1}}, nil
}

type junglePyramidPiece struct {
	box  Box
	o    int32
	hpos int32
}

func (p *junglePyramidPiece) Type() string { return "minecraft:tejp" }
func (p *junglePyramidPiece) Box() Box     { return p.box }

func (p *junglePyramidPiece) Save() nbt.Compound {
	return nbt.Compound{"id": "minecraft:tejp", "GD": int32(0), "O": p.o, "BB": p.box.IntArray(),
		"Width": int32(12), "Height": int32(10), "Depth": int32(15), "HPos": p.hpos,
		"placedHiddenChest": int8(0), "placedMainChest": int8(0), "placedTrap1": int8(0), "placedTrap2": int8(0)}
}

func loadJunglePyramid(c nbt.Compound) (Piece, error) {
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return nil, fmt.Errorf("jungle pyramid: bad BB")
	}
	o, _ := c["O"].(int32)
	hp, ok := c["HPos"].(int32)
	if !ok {
		hp = -1
	}
	return &junglePyramidPiece{box: Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}, o: o, hpos: hp}, nil
}

func (p *junglePyramidPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	ops, err := junglePyramidOps()
	if err != nil {
		return err
	}
	b := p.box
	if p.hpos < 0 {
		var sum, n int32
		for z := b.MinZ; z <= b.MaxZ; z++ {
			for x := b.MinX; x <= b.MaxX; x++ {
				sum += wgSurface(lv, x, z)
				n++
			}
		}
		dy := sum/n - b.MinY
		b.MinY += dy
		b.MaxY += dy
	}
	run := legacyRun{lv: lv, r: r, clip: clip, f: newLegacyFrame(b, p.o), reg: data.Canonical(),
		chestLoot: "minecraft:chests/jungle_temple", dispLoot: "minecraft:chests/jungle_temple_dispenser"}
	run.ops(ops)
	return nil
}

func init() {
	RegisterPiece("minecraft:tejp", loadJunglePyramid)
	RegisterType("minecraft:jungle_temple", func(key string, _ json.RawMessage) (Type, error) {
		if !legacyEnabled[key] {
			return nil, nil
		}
		return junglePyramidType{}, nil
	})
}
