package structure

import (
	"encoding/json"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// buriedType is minecraft:buried_treasure. Its start is one piece at block
// 9,90,9 of the start chunk (reference starts); the height is chosen when
// the piece is placed. Behaviour inferred from the black-box probe
// tools/refprobe/java/kremenmc/probe/structlegacy/Buried.java (testdata
// buried_probe.txt, TestBuriedProbe).
type buriedType struct{}

func (buriedType) Step() int { return 3 }

func (buriedType) Generate(ctx *GenContext) ([]Piece, error) {
	x, z := ctx.ChunkX<<4+9, ctx.ChunkZ<<4+9
	if ctx.BiomeOK != nil && !ctx.BiomeOK(x, buriedBiomeY, z) {
		return nil, nil
	}
	return []Piece{newBuriedPiece(x, 90, z)}, nil
}

// buriedBiomeY is the y of the start's biome check.
const buriedBiomeY = 90

// BuriedPiece is minecraft:btp. Its box is the chest block; Place moves it
// to the chosen height (vanilla saves the moved box).
type BuriedPiece struct {
	x, z int32
	y    atomic.Int32
}

func newBuriedPiece(x, y, z int32) *BuriedPiece {
	p := &BuriedPiece{x: x, z: z}
	p.y.Store(y)
	return p
}

func (p *BuriedPiece) Type() string { return "minecraft:btp" }

func (p *BuriedPiece) Box() Box {
	y := p.y.Load()
	return Box{p.x, y, p.z, p.x, y, p.z}
}

func (p *BuriedPiece) Save() nbt.Compound {
	return nbt.Compound{"id": "minecraft:btp", "GD": int32(0), "O": int32(-1), "BB": p.Box().IntArray()}
}

var buriedBase = map[string]bool{"stone": true, "granite": true, "andesite": true, "diorite": true, "sandstone": true}

// Place puts the chest above the highest stone, granite, andesite, diorite or sandstone of the column
// and seals its air and liquid neighbours (probe-inferred, see
// TestBuriedProbe for the cases).
func (p *BuriedPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	if !clip.Contains(p.x, clip.MinY, p.z) {
		return nil
	}
	reg := data.Canonical()
	name := func(s data.StateID) string {
		if b := reg.BlockOf(s); b != nil {
			return b.Name
		}
		return ""
	}
	open := func(s data.StateID) bool { return reg.IsAir(s) || reg.HasFluid(s) }
	for y := lv.MaxY() - 1; y > lv.MinY(); y-- {
		below := lv.Block(p.x, y-1, p.z)
		if !buriedBase[name(below)] {
			continue
		}
		orig := lv.Block(p.x, y, p.z)
		sand, _ := reg.DefaultState("sand")
		fill := sand
		if !open(orig) {
			fill = orig
		}
		if open(lv.Block(p.x, y+1, p.z)) {
			lv.SetBlock(p.x, y+1, p.z, fill)
		}
		for _, d := range [4][2]int32{{-1, 0}, {0, -1}, {0, 1}, {1, 0}} {
			nx, nz := p.x+d[0], p.z+d[1]
			if !clip.Contains(nx, y, nz) {
				continue
			}
			if !open(lv.Block(nx, y, nz)) {
				continue
			}
			if open(lv.Block(nx, y-1, nz)) {
				lv.SetBlock(nx, y, nz, below)
			} else {
				lv.SetBlock(nx, y, nz, fill)
			}
		}
		chest, _ := reg.ParseState("minecraft:chest[facing=east,type=single,waterlogged=false]")
		lv.SetBlock(p.x, y, p.z, chest)
		lv.SetBlockEntity(p.x, y, p.z, "minecraft:chest", nbt.Compound{
			"LootTable": "minecraft:chests/buried_treasure", "LootTableSeed": r.NextLong(),
		})
		p.y.Store(y)
		return nil
	}
	return nil
}

func loadBuriedPiece(c nbt.Compound) (Piece, error) {
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return newBuriedPiece(0, 90, 0), nil
	}
	return newBuriedPiece(bb[0], bb[1], bb[2]), nil
}

func init() {
	RegisterPiece("minecraft:btp", loadBuriedPiece)
}

func init() {
	RegisterType("minecraft:buried_treasure", func(string, json.RawMessage) (Type, error) { return buriedType{}, nil })
}
