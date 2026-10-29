package noise

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Reference BlendedNoise constants.
const (
	blendedBaseScale   = 684.412
	blendedLimitFactor = 0.9999847412109375
	blendedMainFactor  = 12.75
	blendedLimitFirst  = -15
	blendedMainFirst   = -7
)

// BlendedNoiseSeed is the positional hash name the reference uses to derive
// the blended noise random ("minecraft:terrain").
const BlendedNoiseSeed = "minecraft:terrain"

// BlendedParams are the "old_blended_noise" density function arguments.
type BlendedParams struct {
	XZScale              float64 `json:"xz_scale"`
	YScale               float64 `json:"y_scale"`
	XZFactor             float64 `json:"xz_factor"`
	YFactor              float64 `json:"y_factor"`
	SmearScaleMultiplier float64 `json:"smear_scale_multiplier"`
}

func (p BlendedParams) xzMultiplier() float64 { return float64(blendedBaseScale * p.XZScale) }
func (p BlendedParams) yMultiplier() float64  { return float64(blendedBaseScale * p.YScale) }

// CreateFbm is the reference BlendedNoise.createFbm: octaves firstOctave..0
// created by the legacy sequential initializer as smeared Perlin noise,
// highest frequency first. Octave i (frequency 2^-i) has fudge
// fudge*2^-i and amplitude factor*2^i/(2^n-1).
func CreateFbm(r rng.Source, firstOctave int, fudge, factor float64) *Stack {
	n := 1 - firstOctave
	oct := make([]*SmearedPerlin, n)
	for i := range n {
		oct[i] = NewSmearedPerlin(r, float64(fudge*math.Pow(0.5, float64(i))))
	}
	denom := math.Pow(2, float64(n)) - 1
	var b StackBuilder
	for i, p := range oct {
		b.Add(p, math.Pow(0.5, float64(i)), float32(float64(factor/denom)*math.Pow(2, float64(i))))
	}
	return b.Build()
}

// Blended is an instantiated blended noise: lerp(clamp(main+0.5, 0, 1),
// minLimit, maxLimit), each stack sampled at scaled block coordinates.
type Blended struct {
	MinLimit, MaxLimit, Main *Stack
	limitXZ, limitY          float64
	mainXZ, mainY            float64
	rng                      Interval
}

// NewBlended creates the three stacks from r in reference order
// (min limit, max limit, main).
func (p BlendedParams) NewBlended(r rng.Source) *Blended {
	xz, y := p.xzMultiplier(), p.yMultiplier()
	limitFudge := float64(y * p.SmearScaleMultiplier)
	b := &Blended{
		MinLimit: CreateFbm(r, blendedLimitFirst, limitFudge, blendedLimitFactor),
		MaxLimit: CreateFbm(r, blendedLimitFirst, limitFudge, blendedLimitFactor),
		Main:     CreateFbm(r, blendedMainFirst, limitFudge/p.YFactor, blendedMainFactor),
		limitXZ:  xz,
		limitY:   y,
		mainXZ:   xz / p.XZFactor,
		mainY:    y / p.YFactor,
	}
	lo, hi := b.MinLimit.Range(), b.MaxLimit.Range()
	b.rng = Interval{min(lo.Min, hi.Min), max(lo.Max, hi.Max)}
	return b
}

// Range is the reference BlendedNoise.range().
func (b *Blended) Range() Interval { return b.rng }

// Sample evaluates the blended noise at a block position.
func (b *Blended) Sample(x, y, z int32) float32 {
	fx, fy, fz := float64(x), float64(y), float64(z)
	a := mth.ClampF(b.Main.Get(float64(fx*b.mainXZ), float64(fy*b.mainY), float64(fz*b.mainXZ))+0.5, 0, 1)
	lx, ly, lz := float64(fx*b.limitXZ), float64(fy*b.limitY), float64(fz*b.limitXZ)
	switch {
	case a <= 0:
		return b.MinLimit.Get(lx, ly, lz)
	case a >= 1:
		return b.MaxLimit.Get(lx, ly, lz)
	}
	return mth.LerpF(a, b.MinLimit.Get(lx, ly, lz), b.MaxLimit.Get(lx, ly, lz))
}

// Scales returns the block-coordinate multipliers of the limit and main
// stacks.
func (b *Blended) Scales() (limitXZ, limitY, mainXZ, mainY float64) {
	return b.limitXZ, b.limitY, b.mainXZ, b.mainY
}
