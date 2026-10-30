package density

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
)

// splineS is a compiled cubic spline (constant when coord is nil).
type splineS struct {
	coord       Sampler
	value       float32
	locations   []float32
	values      []*splineS
	derivatives []float32
}

func (s *splineS) Value(c *Ctx, x, y, z int32) float32 {
	if s.coord == nil {
		return s.value
	}
	f := s.coord.Value(c, x, y, z)
	n := len(s.locations)
	i := 0
	for i < n && !(f < s.locations[i]) {
		i++
	}
	i--
	if i < 0 {
		return linearExtend(f, s.locations[0], s.values[0].Value(c, x, y, z), s.derivatives[0])
	}
	if i == n-1 {
		return linearExtend(f, s.locations[i], s.values[i].Value(c, x, y, z), s.derivatives[i])
	}
	g, h := s.locations[i], s.locations[i+1]
	k := (f - g) / (h - g)
	l, m := s.derivatives[i], s.derivatives[i+1]
	a := s.values[i].Value(c, x, y, z)
	b := s.values[i+1].Value(c, x, y, z)
	p := float32(l*(h-g)) - (b - a)
	q := float32(-m*(h-g)) + (b - a)
	return mth.LerpF(k, a, b) + float32(float32(k*(1-k))*mth.LerpF(k, p, q))
}

func linearExtend(f, loc, v, d float32) float32 {
	if d == 0 {
		return v
	}
	return v + float32(d*(f-loc))
}

type interpS struct {
	id          int
	in          Sampler
	cxz, cy     int32
	invXz, invY float32
	// mXz, mY are -size when both cell sizes are powers of two (cell
	// origin = coordinate & mask), 0 otherwise.
	mXz, mY int32
}

func newInterp(id int, in Sampler, cxz, cy int32) *interpS {
	s := &interpS{id: id, in: in, cxz: cxz, cy: cy, invXz: 1 / float32(cxz), invY: 1 / float32(cy)}
	if cxz > 0 && cy > 0 && cxz&(cxz-1) == 0 && cy&(cy-1) == 0 {
		s.mXz, s.mY = -cxz, -cy
	}
	return s
}

// Value interpolates trilinearly between the input at the corners of the
// cell containing the position (cells aligned to multiples of the size).
func (s *interpS) Value(c *Ctx, x, y, z int32) float32 {
	var x0, y0, z0 int32
	if s.mXz != 0 {
		x0, y0, z0 = x&s.mXz, y&s.mY, z&s.mXz
	} else {
		x0 = mth.FloorDiv(x, s.cxz) * s.cxz
		y0 = mth.FloorDiv(y, s.cy) * s.cy
		z0 = mth.FloorDiv(z, s.cxz) * s.cxz
	}
	if c != nil && c.corners != nil {
		t := c.corners
		if s.id >= len(t.last) {
			t.last = append(t.last, make([]lastCell, s.id+1-len(t.last))...)
		}
		l := &t.last[s.id]
		if l.gen != t.gen || l.x != x0 || l.y != y0 || l.z != z0 {
			x1, y1, z1 := x0+s.cxz, y0+s.cy, z0+s.cxz
			v := [8]float32{
				s.corner(c, x0, y0, z0), s.corner(c, x1, y0, z0), s.corner(c, x0, y1, z0), s.corner(c, x1, y1, z0),
				s.corner(c, x0, y0, z1), s.corner(c, x1, y0, z1), s.corner(c, x0, y1, z1), s.corner(c, x1, y1, z1)}
			l = &t.last[s.id]
			*l = lastCell{x: x0, y: y0, z: z0, gen: t.gen, v: v, dx: -1}
		}
		// Lerp3F blends x, then y, then z: the two xy-planes only change
		// with (x, y), so consecutive z steps reuse them.
		if dx, dy := x-x0, y-y0; dx != l.dx || dy != l.dy {
			tx := float32(dx) * s.invXz
			ty := float32(dy) * s.invY
			v := &l.v
			l.a = mth.Lerp2F(tx, ty, v[0], v[1], v[2], v[3])
			l.b = mth.Lerp2F(tx, ty, v[4], v[5], v[6], v[7])
			l.dx, l.dy = dx, dy
		}
		return mth.LerpF(float32(z-z0)*s.invXz, l.a, l.b)
	}
	x1, y1, z1 := x0+s.cxz, y0+s.cy, z0+s.cxz
	tx := float32(x-x0) * s.invXz
	ty := float32(y-y0) * s.invY
	tz := float32(z-z0) * s.invXz
	return mth.Lerp3F(tx, ty, tz,
		s.corner(c, x0, y0, z0), s.corner(c, x1, y0, z0), s.corner(c, x0, y1, z0), s.corner(c, x1, y1, z0),
		s.corner(c, x0, y0, z1), s.corner(c, x1, y0, z1), s.corner(c, x0, y1, z1), s.corner(c, x1, y1, z1))
}

func (s *interpS) corner(c *Ctx, x, y, z int32) float32 {
	if c == nil || c.corners == nil {
		return s.in.Value(c, x, y, z)
	}
	t := c.corners
	if i, ok := t.find(int32(s.id), x, y, z); ok {
		return t.slots[i].v
	}
	v := s.in.Value(c, x, y, z)
	t.insert(int32(s.id), x, y, z, v)
	return v
}

type topSurfaceS struct {
	density, upper Sampler
	lower, cell    int32
}

func (s topSurfaceS) Value(c *Ctx, x, y, z int32) float32 {
	top := mth.FloorF(s.upper.Value(c, x, y, z)/float32(s.cell)) * s.cell
	if top <= s.lower {
		return float32(s.lower)
	}
	for j := top; j >= s.lower; j -= s.cell {
		if s.density.Value(c, x, j, z) > 0 {
			return float32(j)
		}
	}
	return float32(s.lower)
}

type endIslandS struct{ n *noise.Simplex }

func (s endIslandS) Value(_ *Ctx, x, _, z int32) float32 {
	return (endHeight(s.n, x/8, z/8) - 8) / 128
}

func endHeight(n *noise.Simplex, x, z int32) float32 {
	i, j := x/2, z/2
	k, l := x%2, z%2
	f := float32(-100)
	for m := int32(-12); m <= 12; m++ {
		for o := int32(-12); o <= 12; o++ {
			p, q := int64(i+m), int64(j+o)
			if p*p+q*q > 4096 && n.Get2(float64(p), float64(q)) < -0.9 {
				sum := float32(float32(absI64(p))*3439) + float32(float32(absI64(q))*147)
				g := float32(math.Mod(float64(sum), 13)) + 9
				h := float32(k - m*2)
				r := float32(l - o*2)
				t := 100 - float32(float32(math.Sqrt(float64(float32(h*h)+float32(r*r))))*g)
				t = mth.ClampF(t, -100, 80)
				f = max(f, t)
			}
		}
	}
	return f
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

type roundS struct {
	kind     Kind
	in, mult Sampler
}

func roundInt(k Kind, v float32) float32 {
	switch k {
	case KFloor:
		return float32(math.Floor(float64(v)))
	case KCeil:
		return float32(math.Ceil(float64(v)))
	case KTruncate:
		return float32(math.Trunc(float64(v)))
	}
	return float32(math.Floor(float64(v) + 0.5))
}

func (s roundS) Value(c *Ctx, x, y, z int32) float32 {
	v := s.in.Value(c, x, y, z)
	if s.mult == nil {
		return roundInt(s.kind, v)
	}
	m := s.mult.Value(c, x, y, z)
	return roundInt(s.kind, v/m) * m
}
