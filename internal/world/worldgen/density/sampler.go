package density

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
)

// Sampler is a compiled density function.
type Sampler interface {
	// Value samples at a block position.
	Value(c *Ctx, x, y, z int32) float32
}

// Ctx is the per-goroutine sampling context: hooks for blending and
// structures (nil means the reference fallbacks) and cache storage.
type Ctx struct {
	BlendAlpha   func(x, y, z int32) float32
	BlendOffset  func(x, y, z int32) float32
	Beardifier   func(x, y, z int32) float32
	BlendDensity func(x, y, z int32, v float32) float32
	cache        []cacheCell
	corners      *cornerTable
}

// EnableCellCache makes interpolated functions memoize their cell corner
// values (for filling a region such as a chunk); ResetCellCache drops them.
func (c *Ctx) EnableCellCache() {
	if c.corners == nil {
		c.corners = newCornerTable()
	} else {
		c.corners.reset()
	}
}

func (c *Ctx) ResetCellCache() {
	if c.corners != nil {
		c.corners.reset()
	}
}

type cacheCell struct {
	x, y, z int32
	ok      bool
	v       float32
}

type constS struct{ v float32 }

func (s constS) Value(*Ctx, int32, int32, int32) float32 { return s.v }

type hookS struct {
	kind     Kind
	fallback float32
}

func (s hookS) Value(c *Ctx, x, y, z int32) float32 {
	if c != nil {
		var f func(x, y, z int32) float32
		switch s.kind {
		case KBlendAlpha:
			f = c.BlendAlpha
		case KBlendOffset:
			f = c.BlendOffset
		case KBeardifier:
			f = c.Beardifier
		}
		if f != nil {
			return f(x, y, z)
		}
	}
	return s.fallback
}

type noiseS struct {
	n     noise.Noise
	xz, y float64
}

func (s noiseS) Value(_ *Ctx, x, y, z int32) float32 {
	return s.n.Get(float64(float64(x)*s.xz), float64(float64(y)*s.y), float64(float64(z)*s.xz))
}

type shiftedS struct {
	sx, sy, sz Sampler
	n          noise.Noise
	xz, y      float64
}

func (s shiftedS) Value(c *Ctx, x, y, z int32) float32 {
	dx := float64(s.sx.Value(c, x, y, z))
	dy := float64(s.sy.Value(c, x, y, z))
	dz := float64(s.sz.Value(c, x, y, z))
	return s.n.Get(float64(float64(x)*s.xz)+dx, float64(float64(y)*s.y)+dy, float64(float64(z)*s.xz)+dz)
}

type shiftBS struct{ n noise.Noise }

func (s shiftBS) Value(_ *Ctx, x, _, z int32) float32 {
	return s.n.Get(float64(float64(z)*0.25), float64(float64(x)*0.25), 0) * 4
}

type gradientS struct {
	axis              Axis
	tiling            Tiling
	from, lo, hi, rng int32
	fromValue, factor float32
}

func (s gradientS) Value(_ *Ctx, x, y, z int32) float32 {
	c := y
	switch s.axis {
	case AxisX:
		c = x
	case AxisZ:
		c = z
	}
	return s.compute(c)
}

func (s gradientS) compute(c int32) float32 {
	switch s.tiling {
	case TileRepeat:
		c = s.from + mth.PositiveModulo(c-s.from, s.rng)
	case TileMirrored:
		m := mth.PositiveModulo(c-s.from, 2*s.rng)
		if m > s.rng {
			m = 2*s.rng - m
		}
		c = s.from + m
	default:
		c = min(max(c, s.lo), s.hi)
	}
	return s.fromValue + float32(float32(c-s.from)*s.factor)
}

type unaryS struct {
	kind Kind
	in   Sampler
	k    float32
}

func (s unaryS) Value(c *Ctx, x, y, z int32) float32 {
	return unary(s.kind, s.k, s.in.Value(c, x, y, z))
}

func unary(k Kind, f, v float32) float32 {
	switch k {
	case KAbs:
		return float32(math.Abs(float64(v)))
	case KSquare:
		return v * v
	case KCube:
		return float32(v*v) * v
	case KSqrt:
		return float32(math.Sqrt(float64(v)))
	case KHalfNegative, KQuarterNegative:
		if v > 0 {
			return v
		}
		return v * f
	case KReciprocal:
		return 1 / v
	case KNegate:
		return -v
	case KSqueeze:
		c := mth.ClampF(v, -1, 1)
		return c/2 - float32(float32(c*c)*c)/24
	case KLog:
		return float32(mth.StrictLog(float64(v)))
	case KSign:
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return v
	}
	panic("density: bad unary")
}

type binS struct {
	kind Kind
	l, r Sampler
	rb   float32 // right bound for min/max shortcuts
}

func (s binS) Value(c *Ctx, x, y, z int32) float32 {
	a := s.l.Value(c, x, y, z)
	switch s.kind {
	case KAdd:
		return a + s.r.Value(c, x, y, z)
	case KSub:
		return a - s.r.Value(c, x, y, z)
	case KMul:
		return a * s.r.Value(c, x, y, z)
	case KDiv:
		return a / s.r.Value(c, x, y, z)
	case KMin:
		if a < s.rb {
			return a
		}
		return float32(mth.JavaMin(float64(a), float64(s.r.Value(c, x, y, z))))
	case KMax:
		if a > s.rb {
			return a
		}
		return float32(mth.JavaMax(float64(a), float64(s.r.Value(c, x, y, z))))
	}
	panic("density: bad binary")
}

// constBinS is l <op> k (or k <op> r for sub/div with constant left).
type constBinS struct {
	kind Kind
	in   Sampler
	k    float32
}

func (s constBinS) Value(c *Ctx, x, y, z int32) float32 {
	v := s.in.Value(c, x, y, z)
	switch s.kind {
	case KAdd:
		return v + s.k
	case KMul:
		return v * s.k
	case KSub:
		return s.k - v
	case KDiv:
		return s.k / v
	case KMin:
		return float32(mth.JavaMin(float64(v), float64(s.k)))
	case KMax:
		return float32(mth.JavaMax(float64(v), float64(s.k)))
	}
	panic("density: bad const binary")
}

type powS struct {
	base, exp   Sampler
	cBase, cExp float64
	mode        int // 0 general, 1 const base, 2 const exponent
}

func (s powS) Value(c *Ctx, x, y, z int32) float32 {
	switch s.mode {
	case 1:
		return float32(math.Pow(s.cBase, float64(s.exp.Value(c, x, y, z))))
	case 2:
		return float32(math.Pow(float64(s.base.Value(c, x, y, z)), s.cExp))
	}
	b := s.base.Value(c, x, y, z)
	return float32(math.Pow(float64(b), float64(s.exp.Value(c, x, y, z))))
}

type lerpS struct {
	a, f, s Sampler
	cf, cs  float32
	mode    int // 0 general, 1 const first, 2 const second
}

func (s lerpS) Value(c *Ctx, x, y, z int32) float32 {
	a := s.a.Value(c, x, y, z)
	first := func() float32 {
		if s.mode == 1 {
			return s.cf
		}
		return s.f.Value(c, x, y, z)
	}
	second := func() float32 {
		if s.mode == 2 {
			return s.cs
		}
		return s.s.Value(c, x, y, z)
	}
	if a <= 0 {
		return first()
	}
	if a >= 1 {
		return second()
	}
	return mth.LerpF(a, first(), second())
}

type clampS struct {
	in     Sampler
	lo, hi float32
}

func (s clampS) Value(c *Ctx, x, y, z int32) float32 {
	return mth.ClampF(s.in.Value(c, x, y, z), s.lo, s.hi)
}

type rangeS struct {
	in      Sampler
	lo, hi  float32
	yes, no Sampler
}

func (s rangeS) Value(c *Ctx, x, y, z int32) float32 {
	v := s.in.Value(c, x, y, z)
	if v >= s.lo && v < s.hi {
		return s.yes.Value(c, x, y, z)
	}
	return s.no.Value(c, x, y, z)
}

type intervalS struct {
	in  Sampler
	th  []float32
	fns []Sampler
}

func (s intervalS) Value(c *Ctx, x, y, z int32) float32 {
	v := s.in.Value(c, x, y, z)
	i := 0
	for i < len(s.th) && v >= s.th[i] {
		i++
	}
	return s.fns[i].Value(c, x, y, z)
}

type cacheS struct {
	id int
	in Sampler
}

func (s cacheS) Value(c *Ctx, x, y, z int32) float32 {
	if c == nil {
		return s.in.Value(c, x, y, z)
	}
	if s.id >= len(c.cache) {
		c.cache = append(c.cache, make([]cacheCell, s.id+1-len(c.cache))...)
	}
	e := &c.cache[s.id]
	if e.ok && e.x == x && e.y == y && e.z == z {
		return e.v
	}
	v := s.in.Value(c, x, y, z)
	c.cache[s.id] = cacheCell{x, y, z, true, v}
	return v
}

type blendDensityS struct{ in Sampler }

func (s blendDensityS) Value(c *Ctx, x, y, z int32) float32 {
	v := s.in.Value(c, x, y, z)
	if c != nil && c.BlendDensity != nil {
		return c.BlendDensity(x, y, z, v)
	}
	return v
}

type sliceS struct {
	in   Sampler
	axis Axis
	at   int32
}

func (s sliceS) Value(c *Ctx, x, y, z int32) float32 {
	switch s.axis {
	case AxisX:
		x = s.at
	case AxisY:
		y = s.at
	default:
		z = s.at
	}
	return s.in.Value(c, x, y, z)
}

type distanceS struct {
	px, py, pz int32
	metric     Metric
}

func (s distanceS) Value(_ *Ctx, x, y, z int32) float32 {
	dx, dy, dz := float32(x-s.px), float32(y-s.py), float32(z-s.pz)
	switch s.metric {
	case MetricEuclideanSquared:
		return float32(dx*dx+float32(dy*dy)) + float32(dz*dz)
	case MetricManhattan:
		return float32(math.Abs(float64(dx))+math.Abs(float64(dy))) + float32(math.Abs(float64(dz)))
	case MetricChebyshev:
		return float32(math.Max(math.Max(math.Abs(float64(dx)), math.Abs(float64(dy))), math.Abs(float64(dz))))
	}
	return float32(math.Sqrt(float64(float32(float32(dx*dx)+float32(dy*dy)) + float32(dz*dz))))
}
