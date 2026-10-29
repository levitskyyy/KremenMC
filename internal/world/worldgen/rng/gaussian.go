package rng

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
)

// gaussian is the reference Marsaglia polar method over a source's
// NextDouble, caching the second deviate.
type gaussian struct {
	next float64
	have bool
}

func (g *gaussian) reset() { g.have = false }

func (g *gaussian) sample(src interface{ NextDouble() float64 }) float64 {
	if g.have {
		g.have = false
		return g.next
	}
	for {
		v1 := float64(2*src.NextDouble()) - 1
		v2 := float64(2*src.NextDouble()) - 1
		s := float64(v1*v1) + float64(v2*v2)
		if s < 1 && s != 0 {
			m := math.Sqrt(float64(-2*mth.StrictLog(s)) / s)
			g.next = v2 * m
			g.have = true
			return v1 * m
		}
	}
}
