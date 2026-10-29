package noise

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// SimplexRange is the reference SimplexNoise.RANGE bound.
var SimplexRange = Interval{-2, 2}

// SimplexStandardDeviation is the reference deviation estimate.
const SimplexStandardDeviation = 0.42544

// Evaluated at run time like the reference's static initializers.
var (
	sqrt3 = math.Sqrt(3)
	f2    = float64(0.5 * (sqrt3 - 1))
	g2    = (3 - sqrt3) / 6
	g2x2  = float64(2 * g2)
	f3    = 0.3333333333333333
	g3    = 0.16666666666666666
	g3x2  = float64(2 * g3)
	g3x3  = float64(3 * g3)
)

// Simplex is the reference SimplexNoise (2D and 3D simplex noise computed
// in float64 and rounded to float32 at the end).
type Simplex struct{ gradientBase }

// NewSimplex consumes 3 doubles and 256 bounded ints from r; samples are
// shifted by the random offsets.
func NewSimplex(r rng.Source) *Simplex { return NewSimplexOpt(r, false) }

// NewSimplexOpt mirrors the reference SimplexNoise(random, boolean): with
// zeroOffsets the random offsets are drawn but then zeroed (end islands).
func NewSimplexOpt(r rng.Source, zeroOffsets bool) *Simplex {
	s := &Simplex{}
	s.init(r)
	if zeroOffsets {
		s.offX, s.offY, s.offZ = 0, 0, 0
	}
	return s
}

func (s *Simplex) Range() Interval { return SimplexRange }

func corner(h int32, x, y, z, base float64) float64 {
	d := base - float64(x*x) - float64(y*y) - float64(z*z)
	if d < 0 {
		return 0
	}
	d = float64(d * d)
	g := &gradients[h]
	dot := float64(float64(float64(g[0])*x)+float64(float64(g[1])*y)) + float64(float64(g[2])*z)
	return float64(float64(d*d) * dot)
}

// Get2 is the 2D simplex value at (x, y); with offsets enabled the X and
// Y offsets are added.
func (s *Simplex) Get2(x, y float64) float32 {
	return float32(s.raw2(x+s.offX, y+s.offY))
}

func (s *Simplex) raw2(x, y float64) float64 {
	d0 := float64((x + y) * f2)
	i := mth.Floor(x + d0)
	j := mth.Floor(y + d0)
	d1 := float64(float64(i+j) * g2)
	x0 := x - (float64(i) - d1)
	y0 := y - (float64(j) - d1)
	var k, l int32
	if x0 > y0 {
		k = 1
	} else {
		l = 1
	}
	x1 := x0 - float64(k) + g2
	y1 := y0 - float64(l) + g2
	x2 := x0 - 1 + g2x2
	y2 := y0 - 1 + g2x2
	ii := i & 255
	jj := j & 255
	h0 := s.p(ii+s.p(jj)) % 12
	h1 := s.p(ii+k+s.p(jj+l)) % 12
	h2 := s.p(ii+1+s.p(jj+1)) % 12
	return float64(70 * (corner(h0, x0, y0, 0, 0.5) + corner(h1, x1, y1, 0, 0.5) + corner(h2, x2, y2, 0, 0.5)))
}

// Get is the 3D simplex value at (x, y, z).
func (s *Simplex) Get(x, y, z float64) float32 {
	x += s.offX
	y += s.offY
	z += s.offZ
	d0 := float64((x + y + z) * f3)
	i := mth.Floor(x + d0)
	j := mth.Floor(y + d0)
	k := mth.Floor(z + d0)
	d1 := float64(float64(i+j+k) * g3)
	x0 := x - (float64(i) - d1)
	y0 := y - (float64(j) - d1)
	z0 := z - (float64(k) - d1)
	var i1, j1, k1, i2, j2, k2 int32
	switch {
	case x0 >= y0 && y0 >= z0:
		i1, j1, k1, i2, j2, k2 = 1, 0, 0, 1, 1, 0
	case x0 >= y0 && x0 >= z0:
		i1, j1, k1, i2, j2, k2 = 1, 0, 0, 1, 0, 1
	case x0 >= y0:
		i1, j1, k1, i2, j2, k2 = 0, 0, 1, 1, 0, 1
	case y0 < z0:
		i1, j1, k1, i2, j2, k2 = 0, 0, 1, 0, 1, 1
	case x0 < z0:
		i1, j1, k1, i2, j2, k2 = 0, 1, 0, 0, 1, 1
	default:
		i1, j1, k1, i2, j2, k2 = 0, 1, 0, 1, 1, 0
	}
	x1 := x0 - float64(i1) + g3
	y1 := y0 - float64(j1) + g3
	z1 := z0 - float64(k1) + g3
	x2 := x0 - float64(i2) + g3x2
	y2 := y0 - float64(j2) + g3x2
	z2 := z0 - float64(k2) + g3x2
	x3 := x0 - 1 + g3x3
	y3 := y0 - 1 + g3x3
	z3 := z0 - 1 + g3x3
	ii, jj, kk := i&255, j&255, k&255
	h0 := s.p(ii+s.p(jj+s.p(kk))) % 12
	h1 := s.p(ii+i1+s.p(jj+j1+s.p(kk+k1))) % 12
	h2 := s.p(ii+i2+s.p(jj+j2+s.p(kk+k2))) % 12
	h3 := s.p(ii+1+s.p(jj+1+s.p(kk+1))) % 12
	return float32(float64(32 * (corner(h0, x0, y0, z0, 0.6) + corner(h1, x1, y1, z1, 0.6) + corner(h2, x2, y2, z2, 0.6) + corner(h3, x3, y3, z3, 0.6))))
}

func (p *Simplex) AddTo(acc float32, bx, by, bz, xzScale, yScale float64, amp float32) float32 {
	return acc + float32(amp*p.Get(float64(bx*xzScale), float64(by*yScale), float64(bz*xzScale)))
}
