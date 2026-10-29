package rng

// WorldgenRandom is the reference WorldgenRandom: a legacy-style bit source
// delegating raw draws to an inner source and counting them.
type WorldgenRandom struct {
	src   Source
	bits  BitSource
	count int
	g     gaussian
}

func NewWorldgenRandom(src Source) *WorldgenRandom {
	w := &WorldgenRandom{src: src}
	w.bits, _ = src.(BitSource)
	return w
}

// Algorithm is the reference WorldgenRandom.Algorithm.
type Algorithm int

const (
	AlgorithmLegacy Algorithm = iota
	AlgorithmXoroshiro
)

func (a Algorithm) NewInstance(seed int64) Source {
	if a == AlgorithmLegacy {
		return NewLegacy(seed)
	}
	return NewXoroshiro(seed)
}

func (w *WorldgenRandom) Count() int { return w.count }

func (w *WorldgenRandom) Next(bits int) int32 {
	w.count++
	if w.bits != nil {
		return w.bits.Next(bits)
	}
	return int32(uint64(w.src.NextLong()) >> (64 - bits))
}

func (w *WorldgenRandom) SetSeed(seed int64) { w.src.SetSeed(seed) }

func (w *WorldgenRandom) Fork() Source { return w.src.Fork() }

func (w *WorldgenRandom) ForkPositional() Positional { return w.src.ForkPositional() }

func (w *WorldgenRandom) SetDecorationSeed(levelSeed int64, x, z int32) int64 {
	w.SetSeed(levelSeed)
	a := w.NextLong() | 1
	b := w.NextLong() | 1
	s := int64(x)*a + int64(z)*b ^ levelSeed
	w.SetSeed(s)
	return s
}

func (w *WorldgenRandom) SetFeatureSeed(decorationSeed int64, index, step int32) {
	w.SetSeed(decorationSeed + int64(index) + int64(10000*step))
}

func (w *WorldgenRandom) SetLargeFeatureSeed(seed int64, cx, cz int32) {
	w.SetSeed(seed)
	a := w.NextLong()
	b := w.NextLong()
	w.SetSeed(int64(cx)*a ^ int64(cz)*b ^ seed)
}

func (w *WorldgenRandom) SetLargeFeatureWithSalt(levelSeed int64, x, z, salt int32) {
	w.SetSeed(int64(x)*341873128712 + int64(z)*132897987541 + levelSeed + int64(salt))
}

// SeedSlimeChunk is the reference slime-chunk random for chunk (x, z).
func SeedSlimeChunk(x, z int32, levelSeed, salt int64) Source {
	return NewLegacy(levelSeed + int64(x*x*4987142) + int64(x*5947611) + int64(z*z)*4392871 + int64(z*389711) ^ salt)
}

func (w *WorldgenRandom) NextInt() int32 { return w.Next(32) }

func (w *WorldgenRandom) NextIntn(bound int32) int32 { return nextIntBounded(w, bound) }

func (w *WorldgenRandom) NextIntRange(lo, hi int32) int32 { return nextIntRange(w, lo, hi) }

func (w *WorldgenRandom) NextIntBetweenInclusive(lo, hi int32) int32 {
	return nextIntBetweenInclusive(w, lo, hi)
}

func (w *WorldgenRandom) NextLong() int64 { return bitNextLong(w) }

func (w *WorldgenRandom) NextBool() bool { return w.Next(1) != 0 }

func (w *WorldgenRandom) NextFloat() float32 { return float32(w.Next(24)) * floatUnit }

func (w *WorldgenRandom) NextDouble() float64 { return bitNextDouble(w) }

func (w *WorldgenRandom) NextGaussian() float64 { return w.g.sample(w) }

func (w *WorldgenRandom) Triangle(mode, dev float64) float64 { return triangle(w, mode, dev) }

func (w *WorldgenRandom) TriangleF(mode, dev float32) float32 { return triangleF(w, mode, dev) }

func (w *WorldgenRandom) Consume(n int) {
	for range n {
		w.Next(32)
	}
}
