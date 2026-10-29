package mth

import "math"

const (
	ln2Hi = 6.93147180369123816490e-01
	ln2Lo = 1.90821492927058770002e-10
	two54 = 1.80143985094819840000e+16
	lg1   = 6.666666666666735130e-01
	lg2   = 3.999999999940941908e-01
	lg3   = 2.857142874366239149e-01
	lg4   = 2.222219843214978396e-01
	lg5   = 1.818357216161805012e-01
	lg6   = 1.531383769920937332e-01
	lg7   = 1.479819860511658591e-01
)

// StrictLog is fdlibm's __ieee754_log (Java StrictMath.log), evaluated
// without fused multiply-adds so it is bit-identical on every platform.
func StrictLog(x float64) float64 {
	b := math.Float64bits(x)
	hx := int32(b >> 32)
	lx := uint32(b)
	k := int32(0)
	if hx < 0x00100000 {
		if (hx&0x7fffffff)|int32(lx) == 0 {
			return math.Inf(-1)
		}
		if hx < 0 {
			return math.NaN()
		}
		k -= 54
		x *= two54
		hx = int32(math.Float64bits(x) >> 32)
	}
	if hx >= 0x7ff00000 {
		return x + x
	}
	k += hx>>20 - 1023
	hx &= 0x000fffff
	i := (hx + 0x95f64) & 0x100000
	x = math.Float64frombits(uint64(uint32(hx|(i^0x3ff00000)))<<32 | math.Float64bits(x)&0xffffffff)
	k += i >> 20
	f := x - 1.0
	dk := float64(k)
	if 0x000fffff&(2+hx) < 3 {
		if f == 0 {
			if k == 0 {
				return 0
			}
			return float64(dk*ln2Hi) + float64(dk*ln2Lo)
		}
		r := float64(float64(f*f) * float64(0.5-float64(0.33333333333333333*f)))
		if k == 0 {
			return f - r
		}
		return float64(dk*ln2Hi) - ((r - float64(dk*ln2Lo)) - f)
	}
	s := f / (2.0 + f)
	z := float64(s * s)
	i = hx - 0x6147a
	w := float64(z * z)
	j := 0x6b851 - hx
	t1 := float64(w * float64(lg2+float64(w*float64(lg4+float64(w*lg6)))))
	t2 := float64(z * float64(lg1+float64(w*float64(lg3+float64(w*float64(lg5+float64(w*lg7)))))))
	i |= j
	r := t2 + t1
	if i > 0 {
		hfsq := float64(float64(0.5*f) * f)
		if k == 0 {
			return f - (hfsq - float64(s*(hfsq+r)))
		}
		return float64(dk*ln2Hi) - ((hfsq - (float64(s*(hfsq+r)) + float64(dk*ln2Lo))) - f)
	}
	if k == 0 {
		return f - float64(s*(f-r))
	}
	return float64(dk*ln2Hi) - ((float64(s*(f-r)) - float64(dk*ln2Lo)) - f)
}
