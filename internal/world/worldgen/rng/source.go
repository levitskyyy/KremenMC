// Package rng implements the random sources vanilla 26.3 uses for world
// generation and gameplay, bit-compatible with the reference: the legacy
// 48-bit LCG, Xoroshiro128++, their positional factories and the worldgen
// seeding helpers (decoration, feature, large-feature seeds).
package rng

// Source is the common random source interface. Method semantics, including
// how many raw draws each call consumes, match the reference exactly.
type Source interface {
	Fork() Source
	ForkPositional() Positional
	SetSeed(seed int64)
	NextInt() int32
	// NextIntn returns [0, bound); bound must be > 0.
	NextIntn(bound int32) int32
	// NextIntRange returns [lo, hi) like the reference nextInt(int, int).
	NextIntRange(lo, hi int32) int32
	// NextIntBetweenInclusive returns [lo, hi].
	NextIntBetweenInclusive(lo, hi int32) int32
	NextLong() int64
	NextBool() bool
	NextFloat() float32
	NextDouble() float64
	NextGaussian() float64
	Triangle(mode, deviation float64) float64
	TriangleF(mode, deviation float32) float32
	Consume(n int)
}

// Positional derives independent sources from coordinates or names.
type Positional interface {
	At(x, y, z int32) Source
	FromHashOf(name string) Source
	FromSeed(seed int64) Source
}
