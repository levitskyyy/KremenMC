package feature

// Copied from internal/world/worldgen/surface (verified there): the quart
// cell BiomeManager.getBiome reads for a block position.
func lcgNext(a, b int64) int64 { return a*(a*6364136223846793005+1442695040888963407) + b }

func fiddle(s int64) float64 {
	return (float64(((s>>24)%1024+1024)%1024)/1024 - 0.5) * 0.9
}

func fiddledDistance(seed int64, x, y, z int32, xf, yf, zf float64) float64 {
	m := lcgNext(seed, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	m = lcgNext(m, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	d := fiddle(m)
	m = lcgNext(m, seed)
	e := fiddle(m)
	m = lcgNext(m, seed)
	f := fiddle(m)
	zz, yy, xx := zf+f, yf+e, xf+d
	return zz*zz + yy*yy + xx*xx
}

func zoomQuart(seed int64, x, y, z int32) (int32, int32, int32) {
	i, j, k := x-2, y-2, z-2
	l, m, n := i>>2, j>>2, k>>2
	d, e, f := float64(i&3)/4, float64(j&3)/4, float64(k&3)/4
	best, bestD := 0, 0.0
	for o := range 8 {
		p, q, r := l, m, n
		g, h, s := d, e, f
		if o&4 != 0 {
			p, g = l+1, d-1
		}
		if o&2 != 0 {
			q, h = m+1, e-1
		}
		if o&1 != 0 {
			r, s = n+1, f-1
		}
		dd := fiddledDistance(seed, p, q, r, g, h, s)
		if o == 0 || bestD > dd {
			best, bestD = o, dd
		}
	}
	qx, qy, qz := l, m, n
	if best&4 != 0 {
		qx++
	}
	if best&2 != 0 {
		qy++
	}
	if best&1 != 0 {
		qz++
	}
	return qx, qy, qz
}
