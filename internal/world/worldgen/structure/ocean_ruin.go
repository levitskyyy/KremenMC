package structure

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// Ocean ruins (cold and warm), start layout inferred from the draw logs of
// kremenmc.probe.structlegacy.Starts (testdata/ocean_ruin, 193 cold starts)
// and the reference worlds' starts (see docs/vanilla-parity.md "Ocean
// ruins"):
//   - OCEAN_FLOOR_WG height h at chunk min + 8; biome at (min+8, h-1, min+8);
//   - nextInt(4) rotation; nextFloat < large_probability: large; template
//     nextInt(4) of big_{brick,..}_{1,2,3,8} (cold) / big_warm_{4..7}, else
//     nextInt(8)+1 of brick_N / warm_N; origin the chunk min corner at y 90,
//     rotated about the origin;
//   - large: nextFloat <= cluster_probability adds a cluster. Eight
//     candidates around the big box min (bx, bz), each x then z drawn:
//     (-15+i8, 17+i7) (-15+i8, 1+i7) (-15+i8, -12+i5) (1+i7, 17+i7)
//     (1+i7, -12+i3) (17+i7, 19+i6) (17+i7, 1+i7) (17+i7, -12+i5); then
//     4+nextInt(5) picks, each nextInt(remaining) removed from the list,
//     nextInt(4) rotation; a pick whose rotated 6x7 footprint intersects
//     the big ruin's box is skipped (other small ruins may overlap), else
//     nextInt(8)+1 small template;
//   - every ruin is three pieces brick/cracked/mossy (cold, integrity
//     0.8/0.7/0.5, large 0.9/0.7/0.5) or one warm piece (0.8, large 0.9).

var oceanRuinDebug func(string)

type oceanRuinType struct {
	warm      bool
	large     float32
	cluster   float32
	biomeTemp string
}

func (oceanRuinType) Step() int { return 4 }

var bigColdN = [4]int32{1, 2, 3, 8}

func (t oceanRuinType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	h := ctx.Height(x0+8, z0+8, "OCEAN_FLOOR_WG")
	if ctx.BiomeOK != nil && !ctx.BiomeOK(x0+8, h-1, z0+8) {
		return nil, nil
	}
	r := ctx.Random
	rot := Rotation(r.NextIntn(4))
	large := r.NextFloat() < t.large
	var out []Piece
	var boxes []Box
	add := func(x, z int32, rot Rotation, big bool, n int32) error {
		ps, err := t.ruin(x, z, rot, big, n)
		if err != nil {
			return err
		}
		out = append(out, ps...)
		boxes = append(boxes, ps[0].Box())
		return nil
	}
	if !large {
		return out, add(x0, z0, rot, false, r.NextIntn(8)+1)
	}
	n := r.NextIntn(4)
	if t.warm {
		n += 4
	} else {
		n = bigColdN[n]
	}
	if err := add(x0, z0, rot, true, n); err != nil {
		return nil, err
	}
	if r.NextFloat() > t.cluster {
		return out, nil
	}
	bx, bz := boxes[0].MinX, boxes[0].MinZ
	type cand struct{ x, z int32 }
	spec := [8][4]int32{{-15, 8, 17, 7}, {-15, 8, 1, 7}, {-15, 8, -12, 5}, {1, 7, 17, 7}, {1, 7, -12, 3}, {17, 7, 19, 6}, {17, 7, 1, 7}, {17, 7, -12, 5}}
	cands := make([]cand, 0, 8)
	for _, s := range spec {
		x := bx + s[0] + r.NextIntn(s[1])
		z := bz + s[2] + r.NextIntn(s[3])
		cands = append(cands, cand{x, z})
	}
	count := 4 + r.NextIntn(5)
	for i := int32(0); i < count && len(cands) > 0; i++ {
		k := r.NextIntn(int32(len(cands)))
		c := cands[k]
		cands = append(cands[:k], cands[k+1:]...)
		crot := Rotation(r.NextIntn(4))
		fx0, fz0 := TransformPos(0, 0, MirrorNone, crot, 0, 0)
		fx1, fz1 := TransformPos(5, 6, MirrorNone, crot, 0, 0)
		fb := Box{c.x + min(fx0, fx1), 90, c.z + min(fz0, fz1), c.x + max(fx0, fx1), 96, c.z + max(fz0, fz1)}
		hit := boxes[0].Intersects(fb)
		if oceanRuinDebug != nil {
			oceanRuinDebug(fmt.Sprintf("cand %d,%d rot %d fp %v hit %v boxes %v", c.x, c.z, crot, fb, hit, boxes))
		}
		if hit {
			continue
		}
		if err := add(c.x, c.z, crot, false, r.NextIntn(8)+1); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ruin returns the pieces of one ruin at origin x, 90, z.
func (t oceanRuinType) ruin(x, z int32, rot Rotation, big bool, n int32) ([]Piece, error) {
	type part struct {
		name string
		in   float32
	}
	var parts []part
	pre := ""
	if big {
		pre = "big_"
	}
	if t.warm {
		in := float32(0.8)
		if big {
			in = 0.9
		}
		parts = []part{{fmt.Sprintf("%swarm_%d", pre, n), in}}
	} else {
		in := float32(0.8)
		if big {
			in = 0.9
		}
		parts = []part{{fmt.Sprintf("%sbrick_%d", pre, n), in}, {fmt.Sprintf("%scracked_%d", pre, n), 0.7}, {fmt.Sprintf("%smossy_%d", pre, n), 0.5}}
	}
	var out []Piece
	for _, p := range parts {
		tp, err := NewTemplatePiece("minecraft:orp", "minecraft:underwater_ruin/"+p.name, x, 90, z, rot, MirrorNone, 0, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, &oceanRuinPiece{TemplatePiece: tp, integrity: p.in, large: big, biomeType: t.biomeTemp})
	}
	return out, nil
}

// oceanRuinPiece is vanilla's "orp" piece (placement: ocean_ruin_place.go).
type oceanRuinPiece struct {
	*TemplatePiece
	integrity float32
	large     bool
	biomeType string
}

func (p *oceanRuinPiece) Save() nbt.Compound {
	lg := int8(0)
	if p.large {
		lg = 1
	}
	return nbt.Compound{"id": "minecraft:orp", "GD": int32(0), "O": int32(2), "BB": p.Box().IntArray(),
		"Template": p.Template, "TPX": p.OX, "TPY": p.OY, "TPZ": p.OZ, "Rot": rotNames[p.Rot],
		"Integrity": p.integrity, "IsLarge": lg, "BiomeType": p.biomeType}
}

func loadOceanRuin(c nbt.Compound) (Piece, error) {
	tmpl, _ := c["Template"].(string)
	ox, _ := c["TPX"].(int32)
	oy, _ := c["TPY"].(int32)
	oz, _ := c["TPZ"].(int32)
	var rot Rotation
	rs, _ := c["Rot"].(string)
	for i, n := range rotNames {
		if n == rs {
			rot = Rotation(i)
		}
	}
	tp, err := NewTemplatePiece("minecraft:orp", tmpl, ox, oy, oz, rot, MirrorNone, 0, 0)
	if err != nil {
		return nil, err
	}
	p := &oceanRuinPiece{TemplatePiece: tp}
	p.integrity, _ = c["Integrity"].(float32)
	lg, _ := c["IsLarge"].(int8)
	p.large = lg != 0
	p.biomeType, _ = c["BiomeType"].(string)
	if bb := intList(c["BB"]); len(bb) == 6 {
		p.box = Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}
	}
	return p, nil
}

// OceanRuinTypeForTest builds the start generator of an ocean_ruin
// structure JSON.
func OceanRuinTypeForTest(raw json.RawMessage) (Type, error) { return newOceanRuinType(raw) }

func newOceanRuinType(raw json.RawMessage) (Type, error) {
	var j struct {
		BiomeTemp string  `json:"biome_temp"`
		Large     float32 `json:"large_probability"`
		Cluster   float32 `json:"cluster_probability"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	bt := "COLD"
	if j.BiomeTemp == "warm" {
		bt = "WARM"
	}
	return oceanRuinType{warm: j.BiomeTemp == "warm", large: j.Large, cluster: j.Cluster, biomeTemp: bt}, nil
}

func init() {
	RegisterPiece("minecraft:orp", loadOceanRuin)
	RegisterType("minecraft:ocean_ruin", func(key string, raw json.RawMessage) (Type, error) {
		if !legacyEnabled[key] {
			return nil, nil
		}
		return newOceanRuinType(raw)
	})
}
