package noise

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

const smearEpsilon = float64(float32(1e-7))

// SmearedPerlin is the reference SmearedPerlinNoise: Perlin noise whose
// gradient y fraction is snapped to multiples of FudgeYScale (the legacy
// "yScale/yMax" sampling used by blended terrain noise).
type SmearedPerlin struct {
	Perlin
	fudgeYScale float64
}

func NewSmearedPerlin(r rng.Source, fudgeYScale float64) *SmearedPerlin {
	s := &SmearedPerlin{fudgeYScale: fudgeYScale}
	s.init(r)
	return s
}

// SmearedRange is the reference SmearedPerlinNoise.range(fudge).
func SmearedRange(fudgeYScale float64) Interval {
	m := float32(float64(PerlinRange.Max) + fudgeYScale)
	return Interval{-m, m}
}

func (s *SmearedPerlin) FudgeYScale() float64 { return s.fudgeYScale }

func (s *SmearedPerlin) Range() Interval { return SmearedRange(s.fudgeYScale) }

func (s *SmearedPerlin) Get(x, y, z float64) float32 {
	d := Wrap(x) + s.offX
	e := Wrap(y) + s.offY
	f := Wrap(z) + s.offZ
	i, j, k := mth.Floor(d), mth.Floor(e), mth.Floor(f)
	h := e - float64(j)
	shift := 0.0
	if s.fudgeYScale != 0 {
		m := h
		if y >= 0 && y < h {
			m = y
		}
		shift = float64(float64(mth.Floor(m/s.fudgeYScale+smearEpsilon)) * s.fudgeYScale)
	}
	return s.SampleAndLerp(i, j, k, float32(d-float64(i)), float32(h-shift), float32(f-float64(k)), float32(h))
}

func (s *SmearedPerlin) Get2(x, z float64) float32 { return s.Get(x, 0, z) }

func (p *SmearedPerlin) AddTo(acc float32, bx, by, bz, xzScale, yScale float64, amp float32) float32 {
	return acc + float32(amp*p.Get(float64(bx*xzScale), float64(by*yScale), float64(bz*xzScale)))
}
