package structure

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Swamp hut, inferred from probe kremenmc.probe.legacy.Starts (seed 0, 6
// chunks) and 4 reference huts of seed 0 (one per orientation):
//   - one nextInt(4) picks the orientation (0 north, 1 east, 2 south,
//     3 west; saved O 2, 3, 0, 1); the box is 7x7x9 at the chunk's min
//     corner, the 9 along the orientation axis; the biome is tested at the
//     chunk centre on the surface;
//   - the box min y (HPos) is the integer mean of WORLD_SURFACE_WG over
//     the box's 63 columns (4/4);
//   - local x,z map to world N (minX+x, maxZ-z), S (minX+x, minZ+z),
//     E (minX+z, minZ+x), W (maxX-z, minZ+x); states are the north hut's,
//     mirrored left-right for S and W and rotated 90 for E and W
//     (swamp_hut_layout.txt, all 4 reference huts agree on every block);
//   - the 4 corner logs continue down through air and water;
//   - a witch and an all_black cat with a red collar stand at local (2,2,5).

//go:embed swamp_hut_layout.txt
var hutLayoutTxt string

type hutCell struct {
	x, y, z int32
	s       data.StateID
}

var hutLayout = sync.OnceValues(func() ([]hutCell, error) {
	reg := data.Canonical()
	var out []hutCell
	sc := bufio.NewScanner(strings.NewReader(hutLayoutTxt))
	for sc.Scan() {
		var c hutCell
		var st string
		if _, err := fmt.Sscan(sc.Text(), &c.x, &c.y, &c.z, &st); err != nil {
			return nil, fmt.Errorf("swamp hut layout: %w", err)
		}
		s, ok := reg.ParseState(st)
		if !ok {
			return nil, fmt.Errorf("swamp hut layout: bad state %s", st)
		}
		c.s = s
		out = append(out, c)
	}
	return out, nil
})

// hut2D is the saved "O" (2D data value) for the drawn orientation.
var hut2D = [4]int32{2, 3, 0, 1}

type swampHutType struct{}

// SwampHutTypeForTest is the swamp hut start generator, for tests.
func SwampHutTypeForTest() Type { return swampHutType{} }

func (swampHutType) Step() int { return 4 }

func (swampHutType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	d := ctx.Random.NextIntn(4)
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	b := Box{x0, 64, z0, x0 + 6, 70, z0 + 8}
	if d == 1 || d == 3 {
		b = Box{x0, 64, z0, x0 + 8, 70, z0 + 6}
	}
	cx, cz := x0+8, z0+8
	if ctx.BiomeOK != nil && !ctx.BiomeOK(cx, ctx.Height(cx, cz, "WORLD_SURFACE_WG"), cz) {
		return nil, nil
	}
	// Vanilla moves the box on first placement; kremenmc uses the terrain
	// height at start time (the same unless an earlier step changed it).
	var sum, n int32
	for z := b.MinZ; z <= b.MaxZ; z++ {
		for x := b.MinX; x <= b.MaxX; x++ {
			sum += ctx.Height(x, z, "WORLD_SURFACE_WG")
			n++
		}
	}
	h := sum / n
	b.MinY, b.MaxY = h, h+6
	return []Piece{&swampHutPiece{box: b, dir: hut2D[d]}}, nil
}

type swampHutPiece struct {
	box Box
	dir int32 // saved 2D orientation
}

func (p *swampHutPiece) Type() string { return "minecraft:tesh" }
func (p *swampHutPiece) Box() Box     { return p.box }
func (p *swampHutPiece) Save() nbt.Compound {
	return nbt.Compound{"id": "minecraft:tesh", "GD": int32(0), "O": p.dir, "BB": p.box.IntArray(),
		"Width": int32(7), "Height": int32(7), "Depth": int32(9), "HPos": p.box.MinY, "Witch": int8(1), "Cat": int8(1)}
}

func loadSwampHut(c nbt.Compound) (Piece, error) {
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return nil, fmt.Errorf("swamp hut: bad BB")
	}
	o, _ := c["O"].(int32)
	return &swampHutPiece{box: Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}, dir: o}, nil
}

func (p *swampHutPiece) world(x, z int32) (int32, int32) {
	b := p.box
	switch p.dir {
	case 2:
		return b.MinX + x, b.MaxZ - z
	case 0:
		return b.MinX + x, b.MinZ + z
	case 3:
		return b.MinX + z, b.MinZ + x
	default:
		return b.MaxX - z, b.MinZ + x
	}
}

func (p *swampHutPiece) orient() (Mirror, Rotation) {
	switch p.dir {
	case 0:
		return MirrorLeftRight, RotNone
	case 3:
		return MirrorNone, Rot90
	case 1:
		return MirrorLeftRight, Rot90
	}
	return MirrorNone, RotNone
}

func (p *swampHutPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	cells, err := hutLayout()
	if err != nil {
		return err
	}
	reg := data.Canonical()
	m, rot := p.orient()
	y0 := p.box.MinY
	for _, c := range cells {
		x, z := p.world(c.x, c.z)
		y := y0 + c.y
		if !clip.Contains(x, y, z) {
			continue
		}
		lv.SetBlock(x, y, z, TransformState(c.s, m, rot))
	}
	log, _ := reg.ParseState("minecraft:oak_log[axis=y]")
	for _, c := range [][2]int32{{1, 2}, {5, 2}, {1, 7}, {5, 7}} {
		x, z := p.world(c[0], c[1])
		if !clip.Contains(x, y0, z) {
			continue
		}
		for y := y0 - 1; y >= lv.MinY(); y-- {
			b := reg.BlockOf(lv.Block(x, y, z))
			if !reg.IsAir(lv.Block(x, y, z)) && (b == nil || (b.Name != "water" && b.Name != "lava")) {
				break
			}
			lv.SetBlock(x, y, z, log)
		}
	}
	ex, ez := p.world(2, 5)
	if clip.Contains(ex, y0+2, ez) {
		pos := func() nbt.List {
			return nbt.List{Type: nbt.TagDouble, Items: []any{float64(ex) + 0.5, float64(y0 + 2), float64(ez) + 0.5}}
		}
		rotl := func() nbt.List { return nbt.List{Type: nbt.TagFloat, Items: []any{float32(0), float32(0)}} }
		lv.AddEntity(nbt.Compound{"id": "minecraft:witch", "Pos": pos(), "Rotation": rotl(), "PersistenceRequired": int8(1),
			"UUID": hutUUID(ex, y0+2, ez, 1)})
		lv.AddEntity(nbt.Compound{"id": "minecraft:cat", "Pos": pos(), "Rotation": rotl(), "PersistenceRequired": int8(1),
			"variant": "minecraft:all_black", "CollarColor": int8(14), "UUID": hutUUID(ex, y0+2, ez, 2)})
	}
	return nil
}

// hutUUID is a deterministic kremenmc UUID (vanilla's are random).
func hutUUID(x, y, z int32, k uint64) []int32 {
	h := uint64(rng.GetSeed(x, y, z))*31 + k
	return []int32{int32(h >> 32), int32(h), int32(h>>16)&^0xf000 | 0x4000, int32(h>>40) | -0x80000000}
}

func init() {
	RegisterPiece("minecraft:tesh", loadSwampHut)
	RegisterType("minecraft:swamp_hut", func(string, json.RawMessage) (Type, error) { return swampHutType{}, nil })
}
