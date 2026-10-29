package server

import "math"

// fdlibmSin and fdlibmCos are fdlibm's sin/cos (the reference JVM's
// Math.sin/cos results), for arguments below 2^19·π/2. Go's math.Sin
// differs in the last bit for some inputs, which shows in primed TNT
// velocities. Every product is rounded explicitly so nothing is fused.
func fdlibmSin(x float64) float64 {
	ix := fdHi(x) & 0x7fffffff
	if ix <= 0x3fe921fb {
		return fdKSin(x, 0, false)
	}
	if ix >= 0x7ff00000 || ix > 0x413921fb {
		return math.Sin(x)
	}
	n, y0, y1 := fdRemPio2(x)
	switch n & 3 {
	case 0:
		return fdKSin(y0, y1, true)
	case 1:
		return fdKCos(y0, y1)
	case 2:
		return -fdKSin(y0, y1, true)
	}
	return -fdKCos(y0, y1)
}

func fdlibmCos(x float64) float64 {
	ix := fdHi(x) & 0x7fffffff
	if ix <= 0x3fe921fb {
		return fdKCos(x, 0)
	}
	if ix >= 0x7ff00000 || ix > 0x413921fb {
		return math.Cos(x)
	}
	n, y0, y1 := fdRemPio2(x)
	switch n & 3 {
	case 0:
		return fdKCos(y0, y1)
	case 1:
		return -fdKSin(y0, y1, true)
	case 2:
		return -fdKCos(y0, y1)
	}
	return fdKSin(y0, y1, true)
}

func fdHi(x float64) int32 { return int32(math.Float64bits(x) >> 32) }

func fdMul(a, b float64) float64 { return float64(a * b) }

const (
	fdS1 = -1.66666666666666324348e-01
	fdS2 = 8.33333333332248946124e-03
	fdS3 = -1.98412698298579493134e-04
	fdS4 = 2.75573137070700676789e-06
	fdS5 = -2.50507602534068634195e-08
	fdS6 = 1.58969099521155010221e-10

	fdC1 = 4.16666666666666019037e-02
	fdC2 = -1.38888888888741095749e-03
	fdC3 = 2.48015872894767294178e-05
	fdC4 = -2.75573143513906633035e-07
	fdC5 = 2.08757232129817482790e-09
	fdC6 = -1.13596475577881948265e-11
)

func fdKSin(x, y float64, iy bool) float64 {
	if fdHi(x)&0x7fffffff < 0x3e400000 && int(x) == 0 {
		return x
	}
	z := fdMul(x, x)
	v := fdMul(z, x)
	r := fdS2 + fdMul(z, fdS3+fdMul(z, fdS4+fdMul(z, fdS5+fdMul(z, fdS6))))
	if !iy {
		return x + fdMul(v, fdS1+fdMul(z, r))
	}
	return x - ((fdMul(z, fdMul(0.5, y)-fdMul(v, r)) - y) - fdMul(v, fdS1))
}

func fdKCos(x, y float64) float64 {
	ix := fdHi(x) & 0x7fffffff
	if ix < 0x3e400000 && int(x) == 0 {
		return 1
	}
	z := fdMul(x, x)
	r := fdMul(z, fdC1+fdMul(z, fdC2+fdMul(z, fdC3+fdMul(z, fdC4+fdMul(z, fdC5+fdMul(z, fdC6))))))
	if ix < 0x3fd33333 {
		return 1 - (fdMul(0.5, z) - (fdMul(z, r) - fdMul(x, y)))
	}
	qx := 0.28125
	if ix <= 0x3fe90000 {
		qx = math.Float64frombits(uint64(uint32(ix-0x00200000)) << 32)
	}
	hz := fdMul(0.5, z) - qx
	a := 1 - qx
	return a - (hz - (fdMul(z, r) - fdMul(x, y)))
}

const (
	fdInvPio2 = 6.36619772367581382433e-01
	fdPio2_1  = 1.57079632673412561417e+00
	fdPio2_1t = 6.07710050650619224932e-11
	fdPio2_2  = 6.07710050630396597660e-11
	fdPio2_2t = 2.02226624879595063154e-21
	fdPio2_3  = 2.02226624871116645580e-21
	fdPio2_3t = 8.47842766036889956997e-32
)

// fdRemPio2 is __ieee754_rem_pio2 for |x| <= 2^19·π/2: x = n·π/2 + y0 + y1.
func fdRemPio2(x float64) (int32, float64, float64) {
	hx := fdHi(x)
	ix := hx & 0x7fffffff
	if ix < 0x4002d97c {
		if hx > 0 {
			z := x - fdPio2_1
			if ix != 0x3ff921fb {
				y0 := z - fdPio2_1t
				return 1, y0, (z - y0) - fdPio2_1t
			}
			z -= fdPio2_2
			y0 := z - fdPio2_2t
			return 1, y0, (z - y0) - fdPio2_2t
		}
		z := x + fdPio2_1
		if ix != 0x3ff921fb {
			y0 := z + fdPio2_1t
			return -1, y0, (z - y0) + fdPio2_1t
		}
		z += fdPio2_2
		y0 := z + fdPio2_2t
		return -1, y0, (z - y0) + fdPio2_2t
	}
	t := math.Abs(x)
	n := int32(fdMul(t, fdInvPio2) + 0.5)
	fn := float64(n)
	r := t - fdMul(fn, fdPio2_1)
	w := fdMul(fn, fdPio2_1t)
	j := ix >> 20
	y0 := r - w
	if i := j - (fdHi(y0)>>20)&0x7ff; i > 16 {
		t = r
		w = fdMul(fn, fdPio2_2)
		r = t - w
		w = fdMul(fn, fdPio2_2t) - ((t - r) - w)
		y0 = r - w
		if i = j - (fdHi(y0)>>20)&0x7ff; i > 49 {
			t = r
			w = fdMul(fn, fdPio2_3)
			r = t - w
			w = fdMul(fn, fdPio2_3t) - ((t - r) - w)
			y0 = r - w
		}
	}
	y1 := (r - y0) - w
	if hx < 0 {
		return -n, -y0, -y1
	}
	return n, y0, y1
}
