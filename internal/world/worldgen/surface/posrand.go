package surface

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// posRandom is a positional random factory that derives sources by value
// for the two vanilla algorithms, so per-block draws do not allocate.
type posRandom struct {
	kind uint8 // 0 xoroshiro, 1 legacy, 2 other
	xo   rng.XoroshiroPositional
	lg   rng.LegacyPositional
	p    rng.Positional
}

func newPosRandom(p rng.Positional) posRandom {
	switch v := p.(type) {
	case rng.XoroshiroPositional:
		return posRandom{kind: 0, xo: v}
	case *rng.XoroshiroPositional:
		return posRandom{kind: 0, xo: *v}
	case rng.LegacyPositional:
		return posRandom{kind: 1, lg: v}
	case *rng.LegacyPositional:
		return posRandom{kind: 1, lg: *v}
	}
	return posRandom{kind: 2, p: p}
}

type localRandom struct {
	kind uint8
	xo   rng.Xoroshiro
	lg   rng.Legacy
	s    rng.Source
}

func (p *posRandom) at(x, y, z int32) localRandom {
	switch p.kind {
	case 0:
		return localRandom{kind: 0, xo: p.xo.AtValue(x, y, z)}
	case 1:
		return localRandom{kind: 1, lg: p.lg.AtValue(x, y, z)}
	}
	return localRandom{kind: 2, s: p.p.At(x, y, z)}
}

func (r *localRandom) nextFloat() float32 {
	switch r.kind {
	case 0:
		x := r.xo
		v := x.NextFloat()
		r.xo = x
		return v
	case 1:
		l := r.lg // copied: legacy methods leak their receiver
		v := l.NextFloat()
		r.lg = l
		return v
	}
	return r.s.NextFloat()
}

func (r *localRandom) nextDouble() float64 {
	switch r.kind {
	case 0:
		x := r.xo
		v := x.NextDouble()
		r.xo = x
		return v
	case 1:
		l := r.lg // copied: legacy methods leak their receiver
		v := l.NextDouble()
		r.lg = l
		return v
	}
	return r.s.NextDouble()
}

func (r *localRandom) nextIntn(n int32) int32 {
	switch r.kind {
	case 0:
		x := r.xo
		v := x.NextIntn(n)
		r.xo = x
		return v
	case 1:
		l := r.lg // copied: legacy methods leak their receiver
		v := l.NextIntn(n)
		r.lg = l
		return v
	}
	return r.s.NextIntn(n)
}
