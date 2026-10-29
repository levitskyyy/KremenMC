package noise

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Reference NormalNoise constants.
const (
	NormalInputFactor     = 1.0181268882175227
	NormalTargetDeviation = 0.3333333333333333
	normalSampleCount     = 2.0
	normalMaxOctaves      = 32
	normalMaxAmplitude    = 1000000.0
)

// Normalization selects how a normal noise scales its octaves.
type Normalization uint8

const (
	NormalizeEnabled Normalization = iota
	NormalizeDisabled
	NormalizeLegacy
)

func (n Normalization) String() string {
	switch n {
	case NormalizeDisabled:
		return "DISABLED"
	case NormalizeLegacy:
		return "LEGACY"
	}
	return "ENABLED"
}

// NormalParams are the 26.3 worldgen/noise JSON parameters.
type NormalParams struct {
	BaseAmplitude      float64       `json:"base_amplitude"`
	BaseOctave         int           `json:"base_octave"`
	OctaveCount        int           `json:"octave_count"`
	Normalize          Normalization `json:"normalize"`
	AmplitudeModifiers []float64     `json:"amplitude_modifiers"`
}

// UnmarshalJSON applies the reference defaults (base_amplitude 1,
// octave_count 1, normalize true) and accepts normalize as bool or "legacy".
func (p *NormalParams) UnmarshalJSON(b []byte) error {
	var raw struct {
		BaseAmplitude      *float64        `json:"base_amplitude"`
		BaseOctave         *int            `json:"base_octave"`
		OctaveCount        *int            `json:"octave_count"`
		Normalize          json.RawMessage `json:"normalize"`
		AmplitudeModifiers []float64       `json:"amplitude_modifiers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("normal noise: %w", err)
	}
	if raw.BaseOctave == nil {
		return errors.New("normal noise: missing base_octave")
	}
	*p = NormalParams{BaseAmplitude: 1, BaseOctave: *raw.BaseOctave, OctaveCount: 1, AmplitudeModifiers: raw.AmplitudeModifiers}
	if raw.BaseAmplitude != nil {
		p.BaseAmplitude = *raw.BaseAmplitude
	}
	if raw.OctaveCount != nil {
		p.OctaveCount = *raw.OctaveCount
	}
	switch string(raw.Normalize) {
	case "", "true":
	case "false":
		p.Normalize = NormalizeDisabled
	case `"legacy"`:
		p.Normalize = NormalizeLegacy
	default:
		return fmt.Errorf("normal noise: bad normalize %s", raw.Normalize)
	}
	return nil
}

// Validate mirrors the reference codec checks.
func (p NormalParams) Validate() error {
	if p.OctaveCount < 1 || p.OctaveCount > normalMaxOctaves {
		return fmt.Errorf("normal noise: octave_count %d out of range [1, %d]", p.OctaveCount, normalMaxOctaves)
	}
	if !(p.BaseAmplitude >= 0 && p.BaseAmplitude <= normalMaxAmplitude) {
		return fmt.Errorf("normal noise: base_amplitude %v out of range", p.BaseAmplitude)
	}
	if len(p.AmplitudeModifiers) != 0 && len(p.AmplitudeModifiers) != p.OctaveCount {
		return fmt.Errorf("normal noise: amplitude_modifiers had size %d, but octave_count was %d", len(p.AmplitudeModifiers), p.OctaveCount)
	}
	return nil
}

func (p NormalParams) modifier(i int) float64 {
	if len(p.AmplitudeModifiers) == 0 {
		return 1
	}
	return p.AmplitudeModifiers[i]
}

// Octave is one non-zero octave of a normal noise.
type Octave struct {
	Index     int
	Frequency float64
	Amplitude float64
}

// Seed is the positional hash name the modern initializer uses.
func (o Octave) Seed() string { return "octave_" + strconv.Itoa(o.Index) }

// Octaves lists the non-zero octaves in increasing frequency.
func (p NormalParams) Octaves() []Octave {
	n := p.OctaveCount
	norm := 1.0
	if p.Normalize != NormalizeDisabled {
		norm = math.Pow(2, float64(n-1)) / (math.Pow(2, float64(n)) - 1)
	}
	var out []Octave
	for i := range n {
		m := p.modifier(i)
		if m == 0 {
			continue
		}
		q := float64(norm * math.Pow(0.5, float64(i)))
		out = append(out, Octave{
			Index:     p.BaseOctave + i,
			Frequency: math.Pow(2, float64(p.BaseOctave+i)),
			Amplitude: float64(float64(p.BaseAmplitude*q) * m),
		})
	}
	return out
}

func estimateDeviation(oct []Octave) float64 {
	var s float64
	for _, o := range oct {
		d := float64(o.Amplitude * PerlinStandardDeviation)
		s += float64(d * d)
	}
	return math.Sqrt(s)
}

func computeNormalizationFactor(target float64, oct []Octave) float64 {
	e := estimateDeviation(oct)
	if e == 0 {
		return 0
	}
	return float64(target*NormalTargetDeviation) / float64(e*math.Sqrt(normalSampleCount))
}

// javaStreamSum is DoubleStream.sum: Kahan-compensated summation.
func javaStreamSum(v func(yield func(float64) bool)) float64 {
	var sum, comp, simple float64
	for d := range v {
		t := d - comp
		vv := sum + t
		comp = (vv - sum) - t
		sum = vv
		simple += d
	}
	r := sum - comp
	if math.IsNaN(r) && math.IsInf(simple, 0) {
		return simple
	}
	return r
}

func parityExpectedDeviation(span int) float64 {
	return float64(0.1 * (1 + 1/float64(span+1)))
}

// paritySpan is the index distance between the first and last non-zero
// modifier among the first count octaves.
func (p NormalParams) paritySpan() (int, bool) {
	lo, hi := -1, -1
	for i := range p.OctaveCount {
		if p.modifier(i) != 0 {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	return hi - lo, lo >= 0
}

func parityNormalizationFactor(base float64, p NormalParams) float64 {
	span, ok := p.paritySpan()
	if !ok {
		return 0
	}
	return float64(base*0.16666666666666666) / parityExpectedDeviation(span)
}

// NormalizationFactor is the scale applied to every octave amplitude.
func (p NormalParams) NormalizationFactor() float64 {
	oct := p.Octaves()
	if p.Normalize == NormalizeLegacy {
		return parityNormalizationFactor(p.BaseAmplitude, p)
	}
	sum := javaStreamSum(func(yield func(float64) bool) {
		for _, o := range oct {
			if !yield(math.Abs(o.Amplitude)) {
				return
			}
		}
	})
	return computeNormalizationFactor(sum, oct)
}

// NormalNoise is the reference NormalNoise description: parameters plus the
// derived octave list and normalization factor. Create instantiates it.
type NormalNoise struct {
	Params        NormalParams
	octaves       []Octave
	normalization float64
	rng           Interval
}

// NewNormalNoise validates params and derives octaves and normalization.
func NewNormalNoise(p NormalParams) (*NormalNoise, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.AmplitudeModifiers = append([]float64(nil), p.AmplitudeModifiers...)
	n := &NormalNoise{Params: p, octaves: p.Octaves(), normalization: p.NormalizationFactor()}
	e := float64(estimateDeviation(n.octaves) * math.Sqrt(normalSampleCount))
	n.rng = Symmetric(float32(float64(float64(n.normalization*e)/NormalTargetDeviation) * float64(PerlinRange.Max)))
	return n, nil
}

// ParityParams converts the pre-26.3 (firstOctave, amplitudes) form into
// parameters that reproduce the old normalization (reference createParity).
func ParityParams(firstOctave int, amplitudes []float64) NormalParams {
	p := NormalParams{BaseAmplitude: 1, BaseOctave: firstOctave, OctaveCount: len(amplitudes)}
	for _, a := range amplitudes {
		if a != 1 {
			p.AmplitudeModifiers = append([]float64(nil), amplitudes...)
			break
		}
	}
	nf := p.NormalizationFactor()
	if nf != 0 {
		p.BaseAmplitude = parityNormalizationFactor(1, p) / nf
	}
	return p
}

// NewNormalNoiseParity is NewNormalNoise(ParityParams(firstOctave, amplitudes)).
func NewNormalNoiseParity(firstOctave int, amplitudes []float64) (*NormalNoise, error) {
	return NewNormalNoise(ParityParams(firstOctave, amplitudes))
}

func (n *NormalNoise) Octaves() []Octave { return n.octaves }

func (n *NormalNoise) NormalizationFactor() float64 { return n.normalization }

// Range is the reference NormalNoise.range(): a 3-sigma-style bound.
func (n *NormalNoise) Range() Interval { return n.rng }

func (n *NormalNoise) layerAmp(o Octave) float32 { return float32(o.Amplitude * n.normalization) }

// Create instantiates the noise with the modern initializer: two positional
// forks of r, one Perlin per octave from FromHashOf("octave_<index>"),
// interleaved as (A_i, B_i) with B sampled at NormalInputFactor.
func (n *NormalNoise) Create(r rng.Source) *Stack {
	a := r.ForkPositional()
	b := r.ForkPositional()
	layers := make([]Layer, 0, 2*len(n.octaves))
	for _, o := range n.octaves {
		amp := n.layerAmp(o)
		layers = append(layers,
			Layer{NewPerlin(a.FromHashOf(o.Seed())), o.Frequency, amp},
			Layer{NewPerlin(b.FromHashOf(o.Seed())), float64(o.Frequency * NormalInputFactor), amp})
	}
	return NewStack(layers)
}
