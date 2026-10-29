package noise

import (
	"errors"
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// legacySkip is the number of raw ints one Perlin construction consumes.
const legacySkip = 262

// LegacyOctaves is the reference LegacyFbmInitializer: the pre-1.18
// sequential octave initialization. It always constructs the frequency-1
// octave first, then walks down to the lowest frequency, skipping (but
// consuming) octaves outside amplitudes or with zero amplitude. The result
// is indexed like amplitudes; zero-amplitude entries are nil.
func LegacyOctaves(r rng.Source, firstOctave int, amplitudes []float64) ([]*Perlin, error) {
	k := -firstOctave
	j := len(amplitudes)
	if k < j-1 {
		return nil, errors.New("noise: positive octaves are not supported by the legacy initializer")
	}
	out := make([]*Perlin, j)
	first := NewPerlin(r)
	if k >= 0 && k < j && amplitudes[k] != 0 {
		out[k] = first
	}
	for l := k - 1; l >= 0; l-- {
		if l < j && amplitudes[l] != 0 {
			out[l] = NewPerlin(r)
		} else {
			r.Consume(legacySkip)
		}
	}
	return out, nil
}

// LegacyStack is the reference LegacyFbmInitializer.createForLegacyNetherBiome:
// the legacy octaves as a stack whose amplitudes carry the old per-octave
// weighting 2^(n-1-i)/(2^n-1) times the amplitude.
func LegacyStack(r rng.Source, firstOctave int, amplitudes []float64) (*Stack, error) {
	oct, err := LegacyOctaves(r, firstOctave, amplitudes)
	if err != nil {
		return nil, err
	}
	n := len(amplitudes)
	norm := math.Pow(2, float64(n-1)) / (math.Pow(2, float64(n)) - 1)
	var b StackBuilder
	for i, p := range oct {
		if p == nil {
			continue
		}
		q := float64(norm * math.Pow(0.5, float64(i)))
		b.Add(p, math.Pow(2, float64(firstOctave+i)), float32(q*amplitudes[i]))
	}
	return b.Build(), nil
}

// CreateForLegacyNetherBiome instantiates the noise with two sequential
// legacy initializations from r (reference createForLegacyNetherBiome).
func (n *NormalNoise) CreateForLegacyNetherBiome(r rng.Source) (*Stack, error) {
	p := n.Params
	amps := make([]float64, p.OctaveCount)
	for i := range amps {
		amps[i] = p.modifier(i)
	}
	a, err := LegacyStack(r, p.BaseOctave, amps)
	if err != nil {
		return nil, err
	}
	b, err := LegacyStack(r, p.BaseOctave, amps)
	if err != nil {
		return nil, err
	}
	amp := float32(p.BaseAmplitude * n.normalization)
	var sb StackBuilder
	return sb.AddStack(a, 1, amp).AddStack(b, NormalInputFactor, amp).Build(), nil
}
