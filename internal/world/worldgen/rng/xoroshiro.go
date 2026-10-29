package rng

// Xoroshiro128PlusPlus is Vigna's xoroshiro128++ generator state.
type Xoroshiro128PlusPlus struct{ Lo, Hi int64 }

// NewXoroshiro128PlusPlus replaces the all-zero state like the reference.
func NewXoroshiro128PlusPlus(lo, hi int64) Xoroshiro128PlusPlus {
	if lo == 0 && hi == 0 {
		return Xoroshiro128PlusPlus{GoldenRatio64, SilverRatio64}
	}
	return Xoroshiro128PlusPlus{lo, hi}
}

func (x *Xoroshiro128PlusPlus) NextLong() int64 {
	lo, hi := uint64(x.Lo), uint64(x.Hi)
	r := rotl(lo+hi, 17) + lo
	hi ^= lo
	x.Lo = int64(rotl(lo, 49) ^ hi ^ hi<<21)
	x.Hi = int64(rotl(hi, 28))
	return int64(r)
}

// Xoroshiro is the reference XoroshiroRandomSource. Not safe for concurrent use.
type Xoroshiro struct {
	s Xoroshiro128PlusPlus
	g gaussian
}

// NewXoroshiro seeds from a 64-bit seed via UpgradeSeedTo128bit.
func NewXoroshiro(seed int64) *Xoroshiro {
	s := UpgradeSeedTo128bit(seed)
	return NewXoroshiro128(s.Lo, s.Hi)
}

// NewXoroshiro128 uses the raw 128-bit state (no mixing).
func NewXoroshiro128(lo, hi int64) *Xoroshiro {
	return &Xoroshiro{s: NewXoroshiro128PlusPlus(lo, hi)}
}

// MakeXoroshiro is NewXoroshiro by value (no allocation).
func MakeXoroshiro(seed int64) Xoroshiro {
	s := UpgradeSeedTo128bit(seed)
	return MakeXoroshiro128(s.Lo, s.Hi)
}

// MakeXoroshiro128 is NewXoroshiro128 by value.
func MakeXoroshiro128(lo, hi int64) Xoroshiro { return Xoroshiro{s: NewXoroshiro128PlusPlus(lo, hi)} }

func NewXoroshiroSeed(s Seed128) *Xoroshiro { return NewXoroshiro128(s.Lo, s.Hi) }

func (x *Xoroshiro) SetSeed(seed int64) {
	s := UpgradeSeedTo128bit(seed)
	x.s = NewXoroshiro128PlusPlus(s.Lo, s.Hi)
	x.g.reset()
}

func (x *Xoroshiro) Fork() Source {
	lo := x.s.NextLong()
	return NewXoroshiro128(lo, x.s.NextLong())
}

func (x *Xoroshiro) ForkPositional() Positional {
	lo := x.s.NextLong()
	return XoroshiroPositional{lo, x.s.NextLong()}
}

// ForkPositionalX is ForkPositional returning the concrete factory.
func (x *Xoroshiro) ForkPositionalX() XoroshiroPositional {
	lo := x.s.NextLong()
	return XoroshiroPositional{lo, x.s.NextLong()}
}

func (x *Xoroshiro) nextBits(n uint) uint64 { return uint64(x.s.NextLong()) >> (64 - n) }

func (x *Xoroshiro) NextInt() int32 { return int32(x.s.NextLong()) }

func (x *Xoroshiro) NextIntn(bound int32) int32 {
	if bound <= 0 {
		panic("rng: bound must be positive")
	}
	l := uint64(uint32(x.NextInt()))
	m := l * uint64(bound)
	low := m & 0xffffffff
	if low < uint64(bound) {
		t := uint64(uint32(-bound) % uint32(bound))
		for low < t {
			l = uint64(uint32(x.NextInt()))
			m = l * uint64(bound)
			low = m & 0xffffffff
		}
	}
	return int32(m >> 32)
}

func (x *Xoroshiro) NextIntRange(lo, hi int32) int32 { return nextIntRange(x, lo, hi) }

func (x *Xoroshiro) NextIntBetweenInclusive(lo, hi int32) int32 {
	return nextIntBetweenInclusive(x, lo, hi)
}

func (x *Xoroshiro) NextLong() int64 { return x.s.NextLong() }

func (x *Xoroshiro) NextBool() bool { return x.s.NextLong()&1 != 0 }

func (x *Xoroshiro) NextFloat() float32 { return float32(x.nextBits(24)) * floatUnit }

func (x *Xoroshiro) NextDouble() float64 { return float64(x.nextBits(53)) * doubleUnit }

func (x *Xoroshiro) NextGaussian() float64 { return x.g.sample(x) }

func (x *Xoroshiro) Triangle(mode, dev float64) float64 { return triangle(x, mode, dev) }

func (x *Xoroshiro) TriangleF(mode, dev float32) float32 { return triangleF(x, mode, dev) }

func (x *Xoroshiro) Consume(n int) {
	for range n {
		x.s.NextLong()
	}
}

// XoroshiroPositional is the reference XoroshiroPositionalRandomFactory.
type XoroshiroPositional struct{ Lo, Hi int64 }

func (p XoroshiroPositional) At(x, y, z int32) Source { return p.AtX(x, y, z) }

func (p XoroshiroPositional) AtX(x, y, z int32) *Xoroshiro {
	return NewXoroshiro128(GetSeed(x, y, z)^p.Lo, p.Hi)
}

func (p XoroshiroPositional) FromHashOf(name string) Source { return p.FromHashOfX(name) }

func (p XoroshiroPositional) FromHashOfX(name string) *Xoroshiro {
	return NewXoroshiroSeed(SeedFromHashOf(name).Xor(p.Lo, p.Hi))
}

// AtValue is At returning the source by value.
func (p XoroshiroPositional) AtValue(x, y, z int32) Xoroshiro {
	return MakeXoroshiro128(GetSeed(x, y, z)^p.Lo, p.Hi)
}

// FromHashOfValue is FromHashOf returning the source by value.
func (p XoroshiroPositional) FromHashOfValue(name string) Xoroshiro {
	s := SeedFromHashOf(name).Xor(p.Lo, p.Hi)
	return MakeXoroshiro128(s.Lo, s.Hi)
}

func (p XoroshiroPositional) FromSeed(seed int64) Source {
	return NewXoroshiro128(seed^p.Lo, seed^p.Hi)
}
