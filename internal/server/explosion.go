package server

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Explosion mechanics (26.3), from the minecraft.wiki "Explosion" page and
// black-box probes of the reference (kremenmc.probe.explosion.Explode, goldens
// in testdata/explosion_*.txt).

// blockInteraction is what an explosion does to blocks.
type blockInteraction uint8

const (
	exKeep blockInteraction = iota
	exDestroy
	exDestroyWithDecay
	exTriggerBlock
)

// explosionKind is the level explosion interaction, which picks the block
// interaction from game rules.
type explosionKind uint8

const (
	explodeNone explosionKind = iota
	explodeBlock
	explodeMob
	explodeTNT
	explodeTrigger
)

type exBlocks interface {
	Block(x, y, z int) data.StateID
}

// exCalc is the damage calculator: per-block resistance and veto. The
// default (nil fields) uses block and fluid resistance and destroys anything.
type exCalc struct {
	// keep reports blocks the explosion must not destroy (e.g. portal
	// frames for TNT that went through a portal).
	keep func(s data.StateID) bool
}

// resistance is the block explosion resistance, or false for air without
// fluid. Any non-empty fluid state resists with 100 (water and lava).
func exResistance(s data.StateID) (float32, bool) {
	f := exFlags(s)
	if f&exAir != 0 && f&exFluid == 0 {
		return 0, false
	}
	r := exBlockOf(s).res
	if f&exFluid != 0 {
		r = max(r, 100)
	}
	return r, true
}

// explodedPositions casts the 1352 edge rays of a 16³ grid and returns the
// blocks to destroy, deduplicated, in Java HashSet<BlockPos> iteration order
// (the order the reference shuffles from). minY/maxY bound the world.
func explodedPositions(src exBlocks, minY, maxY int, c api.Vec3, radius float32, rnd rng.Source, calc *exCalc) []api.BlockPos {
	set := newPosSet()
	// A ray carries at most 1.3*radius and spends at least 0.225 per 0.3
	// step, so it stays within 1.734*radius of the centre.
	set.window(exFloor(c.X), exFloor(c.Y), exFloor(c.Z), int(float64(radius)*1.734)+2)
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			for k := 0; k < 16; k++ {
				if i != 0 && i != 15 && j != 0 && j != 15 && k != 0 && k != 15 {
					continue
				}
				dx := float64(float32(float32(i)/15*2) - 1)
				dy := float64(float32(float32(j)/15*2) - 1)
				dz := float64(float32(float32(k)/15*2) - 1)
				l := math.Sqrt(exDot(dx, dy, dz, dx, dy, dz))
				dx, dy, dz = dx/l, dy/l, dz/l
				h := radius * (0.7 + float32(rnd.NextFloat()*0.6))
				x, y, z := c.X, c.Y, c.Z
				px, py, pz, have := 0, 0, 0, false
				var s data.StateID
				var res float32
				var hasRes, keep bool
				for h > 0 {
					bx, by, bz := exFloor(x), exFloor(y), exFloor(z)
					if by < minY || by >= maxY {
						break
					}
					if !have || bx != px || by != py || bz != pz {
						s = src.Block(bx, by, bz)
						r, ok := exResistance(s)
						res, hasRes = float32((r+0.3)*0.3), ok
						keep = calc != nil && calc.keep != nil && calc.keep(s)
						px, py, pz, have = bx, by, bz, true
					}
					if hasRes {
						h -= res
					}
					if h > 0 && !keep {
						set.add(api.BlockPos{X: bx, Y: by, Z: bz})
					}
					x += float64(dx * 0.3)
					y += float64(dy * 0.3)
					z += float64(dz * 0.3)
					h -= 0.22500001
				}
			}
		}
	}
	return set.order()
}

func exFloor(v float64) int { return int(math.Floor(v)) }

// shuffle is the reference list shuffle: swap(i-1, nextInt(i)) from the end.
func shufflePositions(l []api.BlockPos, rnd rng.Source) {
	for i := len(l); i > 1; i-- {
		j := rnd.NextIntn(int32(i))
		l[i-1], l[j] = l[j], l[i-1]
	}
}

// seenPercent is the fraction of grid points in box with an unobstructed
// collider ray to c (sample grid per the wiki: steps of 1/(2·size+1),
// centred horizontally).
func seenPercent(src exBlocks, c api.Vec3, b entity.AABB) float32 {
	sx := 1 / (float64((b.MaxX-b.MinX)*2) + 1)
	sy := 1 / (float64((b.MaxY-b.MinY)*2) + 1)
	sz := 1 / (float64((b.MaxZ-b.MinZ)*2) + 1)
	ox := (1 - float64(math.Floor(1/sx)*sx)) / 2
	oz := (1 - float64(math.Floor(1/sz)*sz)) / 2
	if sx < 0 || sy < 0 || sz < 0 {
		return 0
	}
	hits, total := 0, 0
	for fx := 0.0; fx <= 1; fx += sx {
		for fy := 0.0; fy <= 1; fy += sy {
			for fz := 0.0; fz <= 1; fz += sz {
				p := api.Vec3{
					X: exLerp(fx, b.MinX, b.MaxX) + ox,
					Y: exLerp(fy, b.MinY, b.MaxY),
					Z: exLerp(fz, b.MinZ, b.MaxZ) + oz,
				}
				if !clipCollider(src, p, c) {
					hits++
				}
				total++
			}
		}
	}
	return float32(hits) / float32(total)
}

// exLerp is Mth.lerp. Explicit conversions keep Go from fusing
// multiply-adds, which would differ from the reference in the last bit.
func exLerp(t, a, b float64) float64 { return a + float64(t*(b-a)) }

func exDot(ax, ay, az, bx, by, bz float64) float64 {
	return float64(float64(ax*bx)+float64(ay*by)) + float64(az*bz)
}

func exDistSq(a, b api.Vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return exDot(dx, dy, dz, dx, dy, dz)
}

// clipCollider reports whether the segment from→to hits a block collision
// shape, walking the blocks the segment passes through.
func clipCollider(src exBlocks, from, to api.Vec3) bool {
	if from == to {
		return false
	}
	// Like the reference traversal, nudge both ends inwards by 1e-7.
	ex := exLerp(-1e-7, to.X, from.X)
	ey := exLerp(-1e-7, to.Y, from.Y)
	ez := exLerp(-1e-7, to.Z, from.Z)
	sx := exLerp(-1e-7, from.X, to.X)
	sy := exLerp(-1e-7, from.Y, to.Y)
	sz := exLerp(-1e-7, from.Z, to.Z)
	x, y, z := exFloor(sx), exFloor(sy), exFloor(sz)
	if hitShapes(src, x, y, z, from, to) {
		return true
	}
	dx, dy, dz := ex-sx, ey-sy, ez-sz
	stepX, stepY, stepZ := exSign(dx), exSign(dy), exSign(dz)
	tdx, tdy, tdz := exInv(stepX, dx), exInv(stepY, dy), exInv(stepZ, dz)
	tx := tdx * exFrac(stepX, sx)
	ty := tdy * exFrac(stepY, sy)
	tz := tdz * exFrac(stepZ, sz)
	for tx <= 1 || ty <= 1 || tz <= 1 {
		switch {
		case tx < ty && tx < tz:
			x += stepX
			tx += tdx
		case ty < tz:
			y += stepY
			ty += tdy
		default:
			z += stepZ
			tz += tdz
		}
		if hitShapes(src, x, y, z, from, to) {
			return true
		}
	}
	return false
}

func exSign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func exInv(step int, d float64) float64 {
	if step == 0 {
		return math.MaxFloat64
	}
	return float64(step) / d
}

func exFrac(step int, v float64) float64 {
	f := v - math.Floor(v)
	if step > 0 {
		return 1 - f
	}
	return f
}

// hitShapes reports whether the segment intersects a collision box of the
// block at (x, y, z).
func hitShapes(src exBlocks, x, y, z int, from, to api.Vec3) bool {
	shapes := data.Collision(src.Block(x, y, z))
	if len(shapes) == 0 {
		return false
	}
	fx, fy, fz := float64(x), float64(y), float64(z)
	for i := range shapes {
		s := &shapes[i]
		if segmentHitsBox(from, to, s.MinX+fx, s.MinY+fy, s.MinZ+fz, s.MaxX+fx, s.MaxY+fy, s.MaxZ+fz) {
			return true
		}
	}
	return false
}

// segmentHitsBox reports whether the segment a→b hits the box like a
// voxel shape clip: either the point 0.001 of the way along lies inside
// the box, or the segment enters it through a face at 0 < t < 1 (grazing
// a face or edge, or leaving from the boundary, is not a hit).
func segmentHitsBox(a, b api.Vec3, x0, y0, z0, x1, y1, z1 float64) bool {
	in := func(p api.Vec3) bool {
		return p.X > x0 && p.X < x1 && p.Y > y0 && p.Y < y1 && p.Z > z0 && p.Z < z1
	}
	if in(api.Vec3{X: a.X + float64((b.X-a.X)*0.001), Y: a.Y + float64((b.Y-a.Y)*0.001), Z: a.Z + float64((b.Z-a.Z)*0.001)}) {
		return true
	}
	if in(a) {
		return false
	}
	enter, exit := math.Inf(-1), math.Inf(1)
	for _, ax := range [3][4]float64{{a.X, b.X - a.X, x0, x1}, {a.Y, b.Y - a.Y, y0, y1}, {a.Z, b.Z - a.Z, z0, z1}} {
		p, d, lo, hi := ax[0], ax[1], ax[2], ax[3]
		if d == 0 {
			if p <= lo || p >= hi {
				return false
			}
			continue
		}
		u, v := (lo-p)/d, (hi-p)/d
		if u > v {
			u, v = v, u
		}
		enter, exit = max(enter, u), min(exit, v)
	}
	return enter > 0 && enter < 1 && enter < exit
}

// explosionDamage is the default calculator's entity damage.
func explosionDamage(c api.Vec3, radius float32, pos api.Vec3, seen float32) float32 {
	q := float64(radius * 2)
	dist := math.Sqrt(exDistSq(pos, c)) / q
	impact := (1 - dist) * float64(seen)
	return float32(float64(float64(float64(impact*impact)+impact)/2*7*q) + 1)
}

// posSet emulates the iteration order of java.util.HashSet<BlockPos>
// (HashMap with default capacity 16, load factor 0.75, BlockPos hashCode
// (y + z*31)*31 + x, spread h ^ h>>>16, insertion order within a bucket).
type posSet struct {
	seen map[api.BlockPos]struct{}
	keys []api.BlockPos

	// bits covers the cube of half-size r around (ox, oy, oz) when set.
	bits       []uint64
	ox, oy, oz int
	r, side    int
}

func newPosSet() *posSet { return &posSet{seen: map[api.BlockPos]struct{}{}} }

// window switches to a bitmap for positions within r of the origin; others
// still go through the map.
func (s *posSet) window(x, y, z, r int) {
	side := 2*r + 1
	if side > 128 {
		return
	}
	s.ox, s.oy, s.oz, s.r, s.side = x, y, z, r, side
	s.bits = make([]uint64, (side*side*side+63)/64)
}

func (s *posSet) add(p api.BlockPos) {
	if s.bits != nil {
		dx, dy, dz := p.X-s.ox+s.r, p.Y-s.oy+s.r, p.Z-s.oz+s.r
		if uint(dx) < uint(s.side) && uint(dy) < uint(s.side) && uint(dz) < uint(s.side) {
			i := (dx*s.side+dy)*s.side + dz
			if s.bits[i>>6]&(1<<(i&63)) != 0 {
				return
			}
			s.bits[i>>6] |= 1 << (i & 63)
			s.keys = append(s.keys, p)
			return
		}
	}
	if _, ok := s.seen[p]; ok {
		return
	}
	s.seen[p] = struct{}{}
	s.keys = append(s.keys, p)
}

func javaPosHash(p api.BlockPos) uint32 {
	h := (int32(p.Y)+int32(p.Z)*31)*31 + int32(p.X)
	u := uint32(h)
	return u ^ u>>16
}

func (s *posSet) order() []api.BlockPos {
	n := len(s.keys)
	cap := 16
	for float64(n) > float64(cap)*0.75 {
		cap *= 2
	}
	buckets := make([][]api.BlockPos, cap)
	for _, k := range s.keys {
		b := javaPosHash(k) & uint32(cap-1)
		buckets[b] = append(buckets[b], k)
	}
	out := make([]api.BlockPos, 0, n)
	for _, b := range buckets {
		out = append(out, b...)
	}
	return out
}
