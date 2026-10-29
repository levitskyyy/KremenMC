package noise

import (
	"encoding/json"
	"math"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func TestNormalParamsJSON(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    NormalParams
		wantErr bool
	}{
		{`{"base_octave":-3}`, NormalParams{BaseAmplitude: 1, BaseOctave: -3, OctaveCount: 1}, false},
		{`{"base_octave":-7,"octave_count":2,"normalize":false}`, NormalParams{1, -7, 2, NormalizeDisabled, nil}, false},
		{`{"base_octave":-7,"normalize":"legacy","base_amplitude":0.5}`, NormalParams{0.5, -7, 1, NormalizeLegacy, nil}, false},
		{`{"base_octave":-7,"normalize":true}`, NormalParams{1, -7, 1, NormalizeEnabled, nil}, false},
		{`{"octave_count":2}`, NormalParams{}, true},
		{`{"base_octave":1,"normalize":"maybe"}`, NormalParams{}, true},
	} {
		var p NormalParams
		err := json.Unmarshal([]byte(c.in), &p)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err %v", c.in, err)
			continue
		}
		if err == nil && (p.BaseAmplitude != c.want.BaseAmplitude || p.BaseOctave != c.want.BaseOctave || p.OctaveCount != c.want.OctaveCount || p.Normalize != c.want.Normalize) {
			t.Errorf("%s: got %+v want %+v", c.in, p, c.want)
		}
	}
}

func TestNormalParamsValidate(t *testing.T) {
	for _, c := range []struct {
		p  NormalParams
		ok bool
	}{
		{NormalParams{1, -7, 1, NormalizeEnabled, nil}, true},
		{NormalParams{1, -7, 0, NormalizeEnabled, nil}, false},
		{NormalParams{1, -7, 33, NormalizeEnabled, nil}, false},
		{NormalParams{1, -7, 2, NormalizeEnabled, []float64{1}}, false},
		{NormalParams{-1, -7, 1, NormalizeEnabled, nil}, false},
		{NormalParams{math.NaN(), -7, 1, NormalizeEnabled, nil}, false},
	} {
		if err := c.p.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err %v", c.p, err)
		}
	}
}

func TestLegacyOctavesRejectsPositive(t *testing.T) {
	for _, c := range []struct {
		first int
		n     int
		ok    bool
	}{{-3, 4, true}, {-3, 5, false}, {0, 1, true}, {1, 1, false}, {-20, 2, true}} {
		_, err := LegacyOctaves(rng.NewLegacy(1), c.first, make([]float64, c.n))
		if (err == nil) != c.ok {
			t.Errorf("first %d n %d: err %v", c.first, c.n, err)
		}
	}
}

func TestWrap(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{0, 0}, {1.5, 1.5}, {16777216, -16777216}, {33554432, 0}, {-16777216.5, 16777215.5}, {1e9 + 0.25, -6632959.75},
	} {
		if got := Wrap(c.in); got != c.want {
			t.Errorf("Wrap(%v) = %v want %v", c.in, got, c.want)
		}
	}
}

func vanillaNoises(t testing.TB) (Noise, Noise, *Blended) {
	p := NormalParams{BaseAmplitude: 1.2453007926713473, BaseOctave: -10, OctaveCount: 6, AmplitudeModifiers: []float64{1.5, 0, 1, 0, 0, 0}}
	nn, err := NewNormalNoise(p)
	if err != nil {
		t.Fatal(err)
	}
	x := rng.NewXoroshiro(12345)
	pos := x.ForkPositionalX()
	temp := nn.Create(pos.FromHashOf("minecraft:temperature"))
	end := NewSimplexOpt(rng.NewLegacy(12345), true)
	bl := BlendedParams{0.25, 0.125, 80, 160, 8}.NewBlended(pos.FromHashOf(BlendedNoiseSeed))
	return temp, end, bl
}

// TestConcurrentSampling checks that noise values sampled from many
// goroutines are bit-identical to sequential sampling (run with -race).
func TestConcurrentSampling(t *testing.T) {
	temp, end, bl := vanillaNoises(t)
	const n = 4096
	want := make([]uint32, 3*n)
	sample := func(i int) (uint32, uint32, uint32) {
		x := float64(i*37-70000) + 0.25
		z := float64(i*-53+90000) - 0.75
		return math.Float32bits(temp.Get(x/4, 0, z/4)),
			math.Float32bits(end.Get2(x/8, z/8)),
			math.Float32bits(bl.Sample(int32(i*29-60000), int32(i%384-64), int32(70000-i*31)))
	}
	for i := range n {
		want[3*i], want[3*i+1], want[3*i+2] = sample(i)
	}
	var wg sync.WaitGroup
	errs := make(chan int, 64)
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range n {
				i := (k*7 + g*131) % n
				a, b, c := sample(i)
				if a != want[3*i] || b != want[3*i+1] || c != want[3*i+2] {
					select {
					case errs <- i:
					default:
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for i := range errs {
		t.Errorf("concurrent sample %d differs from sequential", i)
	}
}

var sink32 float32

func BenchmarkPerlinGet(b *testing.B) {
	p := NewPerlin(rng.NewXoroshiro(1))
	b.ReportAllocs()
	x := 0.0
	for b.Loop() {
		x += 0.37
		sink32 += p.Get(x, 64.5, -x)
	}
}

func BenchmarkNormalNoiseGet(b *testing.B) {
	temp, _, _ := vanillaNoises(b)
	b.ReportAllocs()
	x := 0.0
	for b.Loop() {
		x += 1.37
		sink32 += temp.Get(x, 0, -x)
	}
}

func BenchmarkNormalNoiseAddTo(b *testing.B) {
	temp, _, _ := vanillaNoises(b)
	b.ReportAllocs()
	x := 0.0
	for b.Loop() {
		x += 4
		sink32 = temp.AddTo(sink32, x, 0, -x, 0.25, 0, 1)
	}
}

func BenchmarkSimplexGet2(b *testing.B) {
	_, end, _ := vanillaNoises(b)
	b.ReportAllocs()
	x := 0.0
	for b.Loop() {
		x += 0.37
		sink32 += end.Get2(x, -x)
	}
}

func BenchmarkSimplexGet3(b *testing.B) {
	s := NewSimplex(rng.NewXoroshiro(1))
	b.ReportAllocs()
	x := 0.0
	for b.Loop() {
		x += 0.37
		sink32 += s.Get(x, 3.5, -x)
	}
}

func BenchmarkBlendedSample(b *testing.B) {
	_, _, bl := vanillaNoises(b)
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		sink32 += bl.Sample(i*4, i%384-64, -i*4)
	}
}
