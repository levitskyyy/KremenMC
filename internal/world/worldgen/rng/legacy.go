package rng

const (
	lcgMul  = 0x5DEECE66D
	lcgAdd  = 0xB
	lcgMask = 1<<48 - 1
)

// Legacy is the reference LegacyRandomSource / SingleThreadedRandomSource:
// the java.util.Random 48-bit LCG. Not safe for concurrent use.
type Legacy struct {
	seed int64
	g    gaussian
}

func NewLegacy(seed int64) *Legacy {
	l := MakeLegacy(seed)
	return &l
}

// MakeLegacy returns a seeded source by value (no allocation); use it via
// its address.
func MakeLegacy(seed int64) Legacy {
	var l Legacy
	l.SetSeed(seed)
	return l
}

func (l *Legacy) SetSeed(seed int64) {
	l.seed = (seed ^ lcgMul) & lcgMask
	l.g.reset()
}

func (l *Legacy) Next(bits int) int32 {
	l.seed = (l.seed*lcgMul + lcgAdd) & lcgMask
	return int32(l.seed >> (48 - bits))
}

func (l *Legacy) Fork() Source { return NewLegacy(l.NextLong()) }

func (l *Legacy) ForkPositional() Positional { return LegacyPositional{l.NextLong()} }

func (l *Legacy) NextInt() int32 { return l.Next(32) }

func (l *Legacy) NextIntn(bound int32) int32 { return nextIntBounded(l, bound) }

func (l *Legacy) NextIntRange(lo, hi int32) int32 { return nextIntRange(l, lo, hi) }

func (l *Legacy) NextIntBetweenInclusive(lo, hi int32) int32 {
	return nextIntBetweenInclusive(l, lo, hi)
}

func (l *Legacy) NextLong() int64 { return bitNextLong(l) }

func (l *Legacy) NextBool() bool { return l.Next(1) != 0 }

func (l *Legacy) NextFloat() float32 { return float32(l.Next(24)) * floatUnit }

func (l *Legacy) NextDouble() float64 { return bitNextDouble(l) }

func (l *Legacy) NextGaussian() float64 { return l.g.sample(l) }

func (l *Legacy) Triangle(mode, dev float64) float64 { return triangle(l, mode, dev) }

func (l *Legacy) TriangleF(mode, dev float32) float32 { return triangleF(l, mode, dev) }

func (l *Legacy) Consume(n int) {
	for range n {
		l.Next(32)
	}
}

// LegacyPositional is the reference LegacyPositionalRandomFactory.
type LegacyPositional struct{ Seed int64 }

func (p LegacyPositional) At(x, y, z int32) Source { return NewLegacy(GetSeed(x, y, z) ^ p.Seed) }

func (p LegacyPositional) FromHashOf(name string) Source {
	return NewLegacy(int64(JavaStringHash(name)) ^ p.Seed)
}

func (p LegacyPositional) FromSeed(seed int64) Source { return NewLegacy(seed) }

// AtValue is At returning the source by value.
func (p LegacyPositional) AtValue(x, y, z int32) Legacy { return MakeLegacy(GetSeed(x, y, z) ^ p.Seed) }
