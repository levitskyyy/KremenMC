package rng

import "fmt"

// BitSource is a source whose primitive draw is next(bits), like the
// reference BitRandomSource; derived draws are computed from it.
type BitSource interface {
	Source
	Next(bits int) int32
}

const (
	floatUnit  = float32(1.0 / (1 << 24))
	doubleUnit = 1.0 / (1 << 53)
)

func nextIntBounded(s BitSource, bound int32) int32 {
	if bound <= 0 {
		panic(fmt.Sprintf("rng: bound must be positive, got %d", bound))
	}
	if bound&-bound == bound {
		return int32(int64(bound) * int64(s.Next(31)) >> 31)
	}
	for {
		b := s.Next(31)
		v := b % bound
		if b-v+(bound-1) >= 0 {
			return v
		}
	}
}

func bitNextLong(s BitSource) int64 {
	hi := int64(s.Next(32))
	return hi<<32 + int64(s.Next(32))
}

func bitNextDouble(s BitSource) float64 {
	hi := int64(s.Next(26))
	return float64(hi<<27+int64(s.Next(27))) * doubleUnit
}

func nextIntRange(s Source, lo, hi int32) int32 {
	if lo >= hi {
		panic(fmt.Sprintf("rng: bound - origin is non positive (%d, %d)", lo, hi))
	}
	return lo + s.NextIntn(hi-lo)
}

func nextIntBetweenInclusive(s Source, lo, hi int32) int32 {
	return s.NextIntn(hi-lo+1) + lo
}

func triangle(s Source, mode, dev float64) float64 {
	a := s.NextDouble()
	return mode + float64(dev*(a-s.NextDouble()))
}

func triangleF(s Source, mode, dev float32) float32 {
	a := s.NextFloat()
	return mode + float32(dev*(a-s.NextFloat()))
}
