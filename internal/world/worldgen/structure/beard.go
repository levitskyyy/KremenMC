package structure

import (
	"math"
	"sort"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// Terrain adaptation (the reference Beardifier), derived black box from
// kremenmc.probe.beard.BeardProbe, which samples the reference Beardifier for
// chosen rigid boxes and junctions:
//   - kernel(dx,dy,dz) = float(exp(-(dx²+(dy+0.5)²+dz²)/16)) for
//     dx,dy,dz in [-12,12) (the private 24³ table, read by reflection,
//     13,824/13,824 bit-exact), 0 outside;
//   - term(dx,dy,dz) = float(float(-y'·float(fisr(r²/2))/2) · kernel) with
//     y' = dy+0.5, r² = dx²+y'²+dz² in double and fisr the double fast
//     inverse square root with one Newton step (bit-exact on 13,824
//     junction and 50,505 beard_thin samples);
//   - beard_thin rigid piece: 0.8f·term(distance to the box in x and z,
//     y - (minY + ground_level_delta), same z distance);
//   - junction: 0.4f·term(x-sourceX, y-sourceGroundY, z-sourceZ); delta_y
//     and the destination projection do not change it;
//   - the sum is accumulated in float, rigid pieces first, then junctions
//     (50,653/50,653 on a mix of two pieces and two junctions), and is 0
//     outside the affected box (inclusive).
// beard_box, bury and encapsulate (BeardProbe, 46,512 + 59,040 samples
// each and a 97,196-sample mix, bit-exact), with ground = minY +
// ground_level_delta and dx, dz the distances to the box:
//   - beard_box: beardTerm's factor at dy = y - ground, kernel at the
//     distance to [ground, maxY];
//   - bury: float 1 - |(dx, (y-ground)/2, dz)|/6, at least 0;
//   - encapsulate: float 0.8·max(0, 1 - |d|/12), d the distance to the box.

var beardKernel = func() (k [24 * 24 * 24]float32) {
	for x := range 24 {
		for z := range 24 {
			for y := range 24 {
				dx, dy, dz := float64(x-12), float64(y-12)+0.5, float64(z-12)
				k[(x*24+z)*24+y] = float32(math.Exp(-(dx*dx + dy*dy + dz*dz) / 16))
			}
		}
	}
	return
}()

func fastInvSqrt(x float64) float64 {
	h := 0.5 * x
	y := math.Float64frombits(0x5fe6eb50c7b537a9 - math.Float64bits(x)>>1)
	// Explicit conversions keep each product rounded (no fused multiply-add).
	return y * (1.5 - float64(float64(h*y)*y))
}

func beardTerm(dx, dy, dz int32) float32 {
	if dx < -12 || dx >= 12 || dy < -12 || dy >= 12 || dz < -12 || dz >= 12 {
		return 0
	}
	yp := float64(dy) + 0.5
	r2 := float64(dx*dx+dz*dz) + yp*yp
	e := float32(-yp * float64(float32(fastInvSqrt(r2/2))) * 0.5)
	return float32(e * beardKernel[((dx+12)*24+dz+12)*24+dy+12])
}

type beardRigid struct {
	box   Box
	gld   int32
	adapt string // beard_thin, beard_box, bury, encapsulate
}

// beardBoxTerm is beardTerm with the kernel taken at a separate y distance
// dk (beard_box: the distance to the box's span above its ground).
func beardBoxTerm(dx, dy, dz, dk int32) float32 {
	if dx < -12 || dx >= 12 || dk < -12 || dk >= 12 || dz < -12 || dz >= 12 {
		return 0
	}
	yp := float64(dy) + 0.5
	r2 := float64(dx*dx+dz*dz) + yp*yp
	e := float32(-yp * float64(float32(fastInvSqrt(r2/2))) * 0.5)
	return float32(e * beardKernel[((dx+12)*24+dz+12)*24+dk+12])
}

func (r *beardRigid) value(x, y, z int32) float32 {
	b := r.box
	dx := max(b.MinX-x, 0, x-b.MaxX)
	dz := max(b.MinZ-z, 0, z-b.MaxZ)
	ground := b.MinY + r.gld
	switch r.adapt {
	case "beard_box":
		dk := max(ground-y, 0, y-b.MaxY)
		return float32(beardBoxTerm(dx, y-ground, dz, dk) * 0.8)
	case "bury":
		h := float32(y-ground) / 2
		d := float32(math.Sqrt(float64(float32(dx*dx+dz*dz) + h*h)))
		return max(0, 1-d/6)
	case "encapsulate":
		dy := max(b.MinY-y, 0, y-b.MaxY)
		d := float32(math.Sqrt(float64(float32(dx*dx + dy*dy + dz*dz))))
		return 0.8 * max(0, 1-d/12)
	}
	if t := beardTerm(dx, y-ground, dz); t != 0 {
		return float32(t * 0.8)
	}
	return 0
}

// Beard is the terrain adaptation density of one chunk. The zero value
// (and nil) contributes nothing.
type Beard struct {
	rigid     []beardRigid
	junctions []Junction
	aff       Box
}

// Value is the density added to final_density at x,y,z.
func (b *Beard) Value(x, y, z int32) float32 {
	if b == nil || !b.aff.Contains(x, y, z) {
		return 0
	}
	var f float32
	for i := range b.rigid {
		if v := b.rigid[i].value(x, y, z); v != 0 {
			f += v
		}
	}
	for _, j := range b.junctions {
		if t := beardTerm(x-j.X, y-j.GroundY, z-j.Z); t != 0 {
			f += float32(t * 0.4)
		}
	}
	return f
}

// terrainAdapter is implemented by structure types with a
// terrain_adaptation other than none.
type terrainAdapter interface{ terrainAdaptation() string }

// BeardFor collects the terrain adaptation of the chunk being generated
// from the starts it references (status structure_references), or nil.
// Starts are read from the neighbours' structure_starts data, so the
// result depends only on the seed and the chunk.
//
// Inferred, checked against the stage oracle's terrain (refbench stages
// -structures=true): every rigid jigsaw piece and every junction of a
// terrain_matching piece within 12 blocks of the chunk.
func (s *Stages) BeardFor(sc *gen.StageContext) *Beard {
	pc := sc.Chunk()
	if len(pc.Refs) == 0 {
		return nil
	}
	pos := sc.Pos()
	near := Box{pos.X<<4 - 12, math.MinInt32, pos.Z<<4 - 12, pos.X<<4 + 27, math.MaxInt32, pos.Z<<4 + 27}
	keys := make([]string, 0, len(pc.Refs))
	for k := range pc.Refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := &Beard{}
	first := true
	grow := func(o Box) {
		if first {
			b.aff, first = o, false
		} else {
			b.aff = b.aff.Encapsulate(o)
		}
	}
	for _, key := range keys {
		d := s.defs[key]
		if d == nil {
			continue
		}
		ta, ok := d.typ.(terrainAdapter)
		if !ok {
			continue
		}
		adapt := trimNS(ta.terrainAdaptation())
		switch adapt {
		case "beard_thin", "beard_box", "bury", "encapsulate":
		default:
			continue
		}
		for _, k := range pc.Refs[key] {
			sp := world.PosFromKey(uint64(k))
			n, _, ok := sc.Neighbour(sp.X, sp.Z)
			if !ok {
				continue
			}
			st := s.startOf(n, key)
			if st == nil {
				continue
			}
			for _, p := range st.Pieces {
				jp, ok := p.(*JigsawPiece)
				if !ok {
					// Other pieces adapt by their whole box, ground at its
					// bottom (stronghold bury).
					if p.Box().Intersects(near) {
						b.rigid = append(b.rigid, beardRigid{p.Box(), 0, adapt})
						grow(p.Box())
					}
					continue
				}
				if !jp.box.Intersects(near) {
					continue
				}
				if jp.el.rigid {
					b.rigid = append(b.rigid, beardRigid{jp.box, jp.GLD, adapt})
					grow(jp.box)
				}
				if beardNoJunctions && adapt != "beard_thin" {
					continue
				}
				for _, j := range jp.Junctions {
					if near.Contains(j.X, j.GroundY, j.Z) {
						b.junctions = append(b.junctions, j)
						grow(Box{j.X, j.GroundY, j.Z, j.X, j.GroundY, j.Z})
					}
				}
			}
		}
	}
	if first {
		return nil
	}
	b.aff = Box{b.aff.MinX - 12, b.aff.MinY - 12, b.aff.MinZ - 12, b.aff.MaxX + 12, b.aff.MaxY + 12, b.aff.MaxZ + 12}
	return b
}

var beardNoJunctions = false
