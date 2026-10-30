package structure

import (
	"encoding/json"
	"math"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Igloo starts, inferred from the reference (probe kremenmc.probe.legacy.Starts
// draw logs and piece lists at 21 chunks of seed 0, plus the reference
// start of seed 0 at chunk 62512,62500):
//   - draws: nextInt(4) rotation (NONE, CW90, CW180, CCW90), nextDouble()
//     < 0.5 for a basement, then nextInt(8)+3 ladder pieces;
//   - every piece origin is unrotated and relative to the chunk's min
//     corner at y 90: top (0,90,0) pivot 3,5; middle i (2,87-3i,4) pivot
//     1,1; bottom (0,90-3n-6,-2) pivot 3,7. All pivots are the same world
//     point, so the pieces rotate rigidly;
//   - piece order: bottom, middles top-down, top.

type iglooType struct{}

func (iglooType) Step() int { return 4 }

const iglooY = 90

func (iglooType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.Height == nil {
		return nil, nil
	}
	r := ctx.Random
	rot := Rotation(r.NextIntn(4))
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	var ps []Piece
	add := func(tmpl string, dx, y, dz, px, pz int32) error {
		p, err := NewTemplatePiece("minecraft:iglu", tmpl, x0+dx, y, z0+dz, rot, MirrorNone, px, pz)
		if err != nil {
			return err
		}
		ps = append(ps, &iglooPiece{TemplatePiece: p})
		return nil
	}
	if r.NextDouble() < 0.5 {
		n := r.NextIntn(8) + 3
		if err := add("minecraft:igloo/bottom", 0, iglooY-3*n-6, -2, 3, 7); err != nil {
			return nil, err
		}
		for i := int32(0); i < n; i++ {
			if err := add("minecraft:igloo/middle", 2, iglooY-3*(i+1), 4, 1, 1); err != nil {
				return nil, err
			}
		}
	}
	if err := add("minecraft:igloo/top", 0, iglooY, 0, 3, 5); err != nil {
		return nil, err
	}
	// Every piece moves by the same dy: the top's origin y becomes the
	// WORLD_SURFACE_WG height at the top's rotated origin (2/2 reference
	// igloos; the pivot, the box centre and the minimum of the corners
	// were ruled out). Vanilla moves the pieces when the igloo is first
	// placed; kremenmc uses the terrain height at start time, which is the
	// same unless an earlier step changed that column.
	tp := ps[len(ps)-1].(*iglooPiece)
	ox, oz := TransformPos(0, 0, MirrorNone, rot, 3, 5)
	dy := ctx.Height(x0+ox, z0+oz, "WORLD_SURFACE_WG") - iglooY
	for _, p := range ps {
		p.(*iglooPiece).DY = dy
	}
	top := tp.Box()
	cx, cz := (top.MinX+top.MaxX)/2, (top.MinZ+top.MaxZ)/2
	if ctx.BiomeOK != nil && !ctx.BiomeOK(cx, top.MinY, cz) {
		return nil, nil
	}
	return ps, nil
}

// iglooPiece is a TemplatePiece whose start is saved at y 90 and moved
// down onto the terrain when placed. The move is a pure function of the
// terrain at the shared pivot, so every chunk places the same igloo.
// The saved template position keeps y 90 and the saved box is the moved
// one, as in the reference starts.
type iglooPiece struct {
	*TemplatePiece
	DY int32
}

func loadIglooPiece(c nbt.Compound) (Piece, error) {
	p, err := loadTemplatePiece(c)
	if err != nil {
		return nil, err
	}
	tp := p.(*TemplatePiece)
	ip := &iglooPiece{TemplatePiece: tp}
	if bb := intList(c["BB"]); len(bb) == 6 {
		ip.DY = bb[1] - tp.box.MinY
	}
	return ip, nil
}

func (p *iglooPiece) Box() Box {
	b := p.TemplatePiece.Box()
	b.MinY += p.DY
	b.MaxY += p.DY
	return b
}

func (p *iglooPiece) Save() nbt.Compound {
	c := p.TemplatePiece.Save()
	c["BB"] = p.Box().IntArray()
	c["O"] = int32(2)
	return c
}

func (p *iglooPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	q := *p.TemplatePiece
	q.Move(0, p.DY, 0)
	if err := q.Place(lv, r, clip, cx, cz); err != nil {
		return err
	}
	for _, e := range q.tmpl.Entities {
		pos := doubles(e["pos"])
		src, _ := e["nbt"].(nbt.Compound)
		if len(pos) != 3 || src == nil {
			continue
		}
		x, z := transformVec(pos[0], pos[2], q.Rot, q.PX, q.PZ)
		wx, wy, wz := float64(q.OX)+x, float64(q.OY)+pos[1], float64(q.OZ)+z
		if !clip.Contains(int32(math.Floor(wx)), int32(math.Floor(wy)), int32(math.Floor(wz))) {
			continue
		}
		ent := nbt.Compound{}
		for k, v := range src {
			ent[k] = v
		}
		delete(ent, "UUID")
		ent["Pos"] = nbt.List{Type: nbt.TagDouble, Items: []any{wx, wy, wz}}
		if rl, ok := src["Rotation"].(nbt.List); ok && len(rl.Items) == 2 {
			yaw, _ := rl.Items[0].(float32)
			ent["Rotation"] = nbt.List{Type: nbt.TagFloat, Items: []any{yaw + 90*float32(q.Rot), rl.Items[1]}}
		}
		h := uint64(rng.GetSeed(int32(math.Floor(wx)), int32(math.Floor(wy)), int32(math.Floor(wz))))
		id, _ := src["id"].(string)
		for i := 0; i < len(id); i++ {
			h = h*31 + uint64(id[i])
		}
		ent["UUID"] = []int32{int32(h >> 32), int32(h), int32(h>>16) ^ 0x4000, int32(h>>48) ^ -0x80000000}
		lv.AddEntity(ent)
	}
	return nil
}

func doubles(v any) []float64 {
	l, ok := v.(nbt.List)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(l.Items))
	for _, it := range l.Items {
		if f, ok := it.(float64); ok {
			out = append(out, f)
		}
	}
	return out
}

// transformVec rotates an entity position (block-space doubles) about
// the pivot; Rot180 was checked against the reference igloo entities.
func transformVec(x, z float64, rot Rotation, px, pz int32) (float64, float64) {
	fx, fz := float64(px), float64(pz)
	switch rot {
	case Rot90:
		return fx + fz + 1 - z, fz - fx + x
	case Rot180:
		return 2*fx + 1 - x, 2*fz + 1 - z
	case Rot270:
		return fx - fz + z, fx + fz + 1 - x
	}
	return x, z
}

func init() {
	RegisterPiece("minecraft:iglu", loadIglooPiece)
	RegisterType("minecraft:igloo", func(string, json.RawMessage) (Type, error) { return iglooType{}, nil })
}
