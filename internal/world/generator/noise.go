// Package generator provides chunk generators: a configurable flat world and
// an original noise-based terrain generator.
package generator

import "math"

// Perlin is seeded improved Perlin noise. Immutable after creation.
type Perlin struct {
	p          [512]uint8
	ox, oy, oz float64
}

func splitmix(s *uint64) uint64 {
	*s += 0x9E3779B97F4A7C15
	z := *s
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

func NewPerlin(seed uint64) *Perlin {
	n := &Perlin{}
	s := seed
	var perm [256]uint8
	for i := range perm {
		perm[i] = uint8(i)
	}
	for i := 255; i > 0; i-- {
		j := splitmix(&s) % uint64(i+1)
		perm[i], perm[j] = perm[j], perm[i]
	}
	for i := range n.p {
		n.p[i] = perm[i&255]
	}
	unit := func() float64 { return float64(splitmix(&s)>>11) / (1 << 53) * 256 }
	n.ox, n.oy, n.oz = unit(), unit(), unit()
	return n
}

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(t, a, b float64) float64 { return a + t*(b-a) }

func grad3(h uint8, x, y, z float64) float64 {
	switch h & 15 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x + z
	case 5:
		return -x + z
	case 6:
		return x - z
	case 7:
		return -x - z
	case 8:
		return y + z
	case 9:
		return -y + z
	case 10:
		return y - z
	case 11:
		return -y - z
	case 12:
		return x + y
	case 13:
		return -y + z
	case 14:
		return -x + y
	default:
		return -y - z
	}
}

func grad2(h uint8, x, y float64) float64 {
	switch h & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

// Noise3 returns noise in roughly [-1, 1].
func (n *Perlin) Noise3(x, y, z float64) float64 {
	x, y, z = x+n.ox, y+n.oy, z+n.oz
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	X, Y, Z := int(fx)&255, int(fy)&255, int(fz)&255
	x, y, z = x-fx, y-fy, z-fz
	u, v, w := fade(x), fade(y), fade(z)
	p := &n.p
	A := int(p[X]) + Y
	AA, AB := int(p[A])+Z, int(p[A+1])+Z
	B := int(p[X+1]) + Y
	BA, BB := int(p[B])+Z, int(p[B+1])+Z
	return lerp(w,
		lerp(v,
			lerp(u, grad3(p[AA], x, y, z), grad3(p[BA], x-1, y, z)),
			lerp(u, grad3(p[AB], x, y-1, z), grad3(p[BB], x-1, y-1, z))),
		lerp(v,
			lerp(u, grad3(p[AA+1], x, y, z-1), grad3(p[BA+1], x-1, y, z-1)),
			lerp(u, grad3(p[AB+1], x, y-1, z-1), grad3(p[BB+1], x-1, y-1, z-1))))
}

// Noise2 returns noise in roughly [-1, 1].
func (n *Perlin) Noise2(x, y float64) float64 {
	x, y = x+n.ox, y+n.oy
	fx, fy := math.Floor(x), math.Floor(y)
	X, Y := int(fx)&255, int(fy)&255
	x, y = x-fx, y-fy
	u, v := fade(x), fade(y)
	p := &n.p
	A, B := int(p[X])+Y, int(p[X+1])+Y
	return lerp(v,
		lerp(u, grad2(p[A], x, y), grad2(p[B], x-1, y)),
		lerp(u, grad2(p[A+1], x, y-1), grad2(p[B+1], x-1, y-1)))
}

// Octaves sums octaves of Perlin noise with halving amplitude, normalised to ~[-1, 1].
type Octaves struct {
	o    []*Perlin
	norm float64
}

func NewOctaves(seed uint64, n int) *Octaves {
	o := &Octaves{}
	amp := 1.0
	for i := range n {
		s := seed + uint64(i)*0x632BE59BD9B4E019
		o.o = append(o.o, NewPerlin(splitmix(&s)))
		o.norm += amp
		amp /= 2
	}
	o.norm = 1 / o.norm
	return o
}

func (o *Octaves) At2(x, z float64) float64 {
	sum, amp, f := 0.0, 1.0, 1.0
	for _, p := range o.o {
		sum += p.Noise2(x*f, z*f) * amp
		amp /= 2
		f *= 2
	}
	return sum * o.norm
}

func (o *Octaves) At3(x, y, z float64) float64 {
	sum, amp, f := 0.0, 1.0, 1.0
	for _, p := range o.o {
		sum += p.Noise3(x*f, y*f, z*f) * amp
		amp /= 2
		f *= 2
	}
	return sum * o.norm
}

// hash returns a deterministic 64-bit value for the inputs.
func hash(seed uint64, vals ...int64) uint64 {
	h := seed
	for _, v := range vals {
		h ^= uint64(v) + 0x9E3779B97F4A7C15 + h<<6 + h>>2
		h = splitmix(&h)
	}
	return h
}

// rng is a small deterministic generator for per-chunk features.
type rng struct{ s uint64 }

func (r *rng) next() uint64   { return splitmix(&r.s) }
func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }
func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }
func (r *rng) between(a, b int) int {
	if b <= a {
		return a
	}
	return a + r.intn(b-a+1)
}
