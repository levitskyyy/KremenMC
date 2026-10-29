// Package mth holds the scalar math helpers the reference world generator
// uses, with the reference's exact floating-point evaluation order. Products
// are wrapped in explicit conversions so the Go compiler cannot fuse them
// into FMA instructions, which the reference (strict IEEE) never does.
package mth

import "math"

// GetSeed is the reference positional seed hash of a block position.
func GetSeed(x, y, z int32) int64 {
	l := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	l = l*l*42317861 + l*11
	return l >> 16
}

// Floor is the reference Mth.floor: floor saturated to the int32 range.
func Floor(v float64) int32 { return javaD2I(math.Floor(v)) }

func FloorF(v float32) int32 { return javaD2I(math.Floor(float64(v))) }

// LFloor is floor saturated to the int64 range.
func LFloor(v float64) int64 { return javaD2L(math.Floor(v)) }

func Ceil(v float64) int32 { return javaD2I(math.Ceil(v)) }

// javaD2I is Java's (int) cast: NaN -> 0, saturating at the int32 range.
func javaD2I(v float64) int32 {
	switch {
	case v != v:
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	}
	return int32(v)
}

// javaD2L is Java's (long) cast: NaN -> 0, saturating at the int64 range.
func javaD2L(v float64) int64 {
	switch {
	case v != v:
		return 0
	case v >= 0x1p63:
		return math.MaxInt64
	case v <= -0x1p63:
		return math.MinInt64
	}
	return int64(v)
}

// D2I is exported Java (int) double cast semantics.
func D2I(v float64) int32 { return javaD2I(v) }

// D2L is exported Java (long) double cast semantics.
func D2L(v float64) int64 { return javaD2L(v) }

func Lerp(t, a, b float64) float64 { return a + float64(t*(b-a)) }

func Lerp2(tx, ty, v00, v10, v01, v11 float64) float64 {
	return Lerp(ty, Lerp(tx, v00, v10), Lerp(tx, v01, v11))
}

func Lerp3(tx, ty, tz, v000, v100, v010, v110, v001, v101, v011, v111 float64) float64 {
	return Lerp(tz, Lerp2(tx, ty, v000, v100, v010, v110), Lerp2(tx, ty, v001, v101, v011, v111))
}

func LerpF(t, a, b float32) float32 { return a + float32(t*(b-a)) }

func Lerp2F(tx, ty, v00, v10, v01, v11 float32) float32 {
	return LerpF(ty, LerpF(tx, v00, v10), LerpF(tx, v01, v11))
}

func Lerp3F(tx, ty, tz, v000, v100, v010, v110, v001, v101, v011, v111 float32) float32 {
	return LerpF(tz, Lerp2F(tx, ty, v000, v100, v010, v110), Lerp2F(tx, ty, v001, v101, v011, v111))
}

// SmoothStep is the quintic fade 6t^5 - 15t^4 + 10t^3 in float32.
func SmoothStep(t float32) float32 {
	return float32(float32(float32(t*t)*t) * (float32(t*float32(float32(t*6)-15)) + 10))
}

func SmoothStepDerivative(t float32) float32 {
	return float32(float32(float32(float32(30*t)*t)*(t-1)) * (t - 1))
}

func ClampedLerp(t, a, b float64) float64 {
	if t < 0 {
		return a
	}
	if t > 1 {
		return b
	}
	return Lerp(t, a, b)
}

func ClampedLerpF(t, a, b float32) float32 {
	if t < 0 {
		return a
	}
	if t > 1 {
		return b
	}
	return LerpF(t, a, b)
}

func InverseLerp(v, a, b float64) float64 { return (v - a) / (b - a) }

func InverseLerpF(v, a, b float32) float32 { return (v - a) / (b - a) }

func Map(v, a, b, c, d float64) float64 { return Lerp(InverseLerp(v, a, b), c, d) }

func MapF(v, a, b, c, d float32) float32 { return LerpF(InverseLerpF(v, a, b), c, d) }

func ClampedMap(v, a, b, c, d float64) float64 { return ClampedLerp(InverseLerp(v, a, b), c, d) }

func ClampedMapF(v, a, b, c, d float32) float32 { return ClampedLerpF(InverseLerpF(v, a, b), c, d) }

// Clamp follows the reference: values below lo give lo, otherwise min(v, hi).
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	return JavaMin(v, hi)
}

func ClampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	return float32(JavaMin(float64(v), float64(hi)))
}

func ClampI(v, lo, hi int32) int32 { return min(max(v, lo), hi) }

func FloorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func PositiveModulo(a, b int32) int32 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

func Square(v float64) float64 { return v * v }

func Frac(v float64) float64 { return v - float64(LFloor(v)) }

// JavaMin is java.lang.Math.min: NaN if either is NaN, -0 < +0.
func JavaMin(a, b float64) float64 {
	if a != a || b != b {
		return math.NaN()
	}
	return math.Min(a, b)
}

// JavaMax is java.lang.Math.max.
func JavaMax(a, b float64) float64 {
	if a != a || b != b {
		return math.NaN()
	}
	return math.Max(a, b)
}
