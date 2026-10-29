package noise

// Noise is a sampled noise: the reference synth.Noise.
//
// AddTo is the per-point form of the reference bulk path
// (Noise.addToVolume): it returns acc + amp*noise at the block position
// (bx, by, bz) scaled by (xzScale, yScale, xzScale). For a stack the
// reference recurses into the layers, accumulating each one into acc with
// amplitude amp*layerAmplitude at scale xzScale*frequency, so a stack's
// bulk result differs in rounding from amp*Get.
type Noise interface {
	Get(x, y, z float64) float32
	Get2(x, z float64) float32
	Range() Interval
	AddTo(acc float32, bx, by, bz, xzScale, yScale float64, amp float32) float32
}

// Layer is one weighted, frequency-scaled noise in a Stack.
type Layer struct {
	Noise     Noise
	Frequency float64
	Amplitude float32
}

// Stack is the reference NoiseStack: the sum of amplitude-weighted layers
// sampled at frequency-scaled coordinates.
type Stack struct {
	layers  []Layer
	perlin  []perlinLayer  // set when every layer is a *Perlin
	smeared []smearedLayer // set when every layer is a *SmearedPerlin
	rng     Interval
}

type smearedLayer struct {
	p    *SmearedPerlin
	freq float64
	amp  float32
}

type perlinLayer struct {
	p    *Perlin
	freq float64
	amp  float32
}

// StackBuilder mirrors the reference NoiseStack.Builder: AddStack flattens
// a stack's layers, multiplying frequencies (float64) and amplitudes (float32).
type StackBuilder struct{ layers []Layer }

func (b *StackBuilder) Add(n Noise, frequency float64, amplitude float32) *StackBuilder {
	b.layers = append(b.layers, Layer{n, frequency, amplitude})
	return b
}

func (b *StackBuilder) AddStack(s *Stack, frequency float64, amplitude float32) *StackBuilder {
	for _, l := range s.layers {
		b.layers = append(b.layers, Layer{l.Noise, float64(l.Frequency * frequency), l.Amplitude * amplitude})
	}
	return b
}

func (b *StackBuilder) Build() *Stack { return NewStack(b.layers) }

// NewStack builds a stack from layers in summation order.
func NewStack(layers []Layer) *Stack {
	s := &Stack{layers: append([]Layer(nil), layers...)}
	all := true
	for _, l := range s.layers {
		p, ok := l.Noise.(*Perlin)
		if !ok {
			all = false
			break
		}
		s.perlin = append(s.perlin, perlinLayer{p, l.Frequency, l.Amplitude})
	}
	if !all {
		s.perlin = nil
		all = true
		for _, l := range s.layers {
			p, ok := l.Noise.(*SmearedPerlin)
			if !ok {
				all = false
				break
			}
			s.smeared = append(s.smeared, smearedLayer{p, l.Frequency, l.Amplitude})
		}
		if !all {
			s.smeared = nil
		}
	}
	for _, l := range s.layers {
		r := l.Noise.Range()
		a, b := r.Min*l.Amplitude, r.Max*l.Amplitude
		s.rng.Min += min(a, b)
		s.rng.Max += max(a, b)
	}
	return s
}

// Layers returns the layers (shared; do not modify).
func (s *Stack) Layers() []Layer { return s.layers }

func (s *Stack) Range() Interval { return s.rng }

func (s *Stack) Get(x, y, z float64) float32 {
	var v float32
	if s.perlin != nil {
		for i := range s.perlin {
			l := &s.perlin[i]
			v += float32(l.amp * l.p.Get(float64(x*l.freq), float64(y*l.freq), float64(z*l.freq)))
		}
		return v
	}
	if s.smeared != nil {
		for i := range s.smeared {
			l := &s.smeared[i]
			v += float32(l.amp * l.p.Get(float64(x*l.freq), float64(y*l.freq), float64(z*l.freq)))
		}
		return v
	}
	for _, l := range s.layers {
		v += float32(l.Amplitude * l.Noise.Get(float64(x*l.Frequency), float64(y*l.Frequency), float64(z*l.Frequency)))
	}
	return v
}

func (s *Stack) Get2(x, z float64) float32 {
	var v float32
	for _, l := range s.layers {
		v += float32(l.Amplitude * l.Noise.Get2(float64(x*l.Frequency), float64(z*l.Frequency)))
	}
	return v
}

func (s *Stack) AddTo(acc float32, bx, by, bz, xzScale, yScale float64, amp float32) float32 {
	for _, l := range s.layers {
		xz, y := float64(xzScale*l.Frequency), float64(yScale*l.Frequency)
		acc = l.Noise.AddTo(acc, bx, by, bz, xz, y, amp*l.Amplitude)
	}
	return acc
}
