package rng

import (
	"testing"
)

func TestJavaStringHash(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int32
	}{
		{"", 0},
		{"a", 97},
		{"Aa", 2112},
		{"BB", 2112},
		{"\U0001F600", 1772899},
	} {
		if got := JavaStringHash(c.s); got != c.want {
			t.Errorf("JavaStringHash(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestValueConstructorsMatch(t *testing.T) {
	for _, seed := range []int64{0, -1, 42, 1 << 62} {
		a, b := NewXoroshiro(seed), MakeXoroshiro(seed)
		l1, l2 := NewLegacy(seed), MakeLegacy(seed)
		p := XoroshiroPositional{seed, ^seed}
		at, atv := p.At(3, -64, 9), p.AtValue(3, -64, 9)
		h, hv := p.FromHashOf("octave_-7"), p.FromHashOfValue("octave_-7")
		lp := LegacyPositional{seed}
		la, lav := lp.At(-5, 70, 1<<20), lp.AtValue(-5, 70, 1<<20)
		for range 50 {
			if a.NextLong() != b.NextLong() || l1.NextLong() != l2.NextLong() || at.NextLong() != atv.NextLong() ||
				h.NextLong() != hv.NextLong() || la.NextLong() != lav.NextLong() {
				t.Fatalf("seed %d: value constructor diverges", seed)
			}
		}
	}
}

func TestZeroStateXoroshiro(t *testing.T) {
	z := MakeXoroshiro128(0, 0)
	g := MakeXoroshiro128(GoldenRatio64, SilverRatio64)
	if z.NextLong() != g.NextLong() {
		t.Fatal("all-zero state must be replaced by the golden/silver constants")
	}
}

var sink int64

func BenchmarkXoroshiroNextLong(b *testing.B) {
	x := MakeXoroshiro(1)
	b.ReportAllocs()
	for b.Loop() {
		sink += x.NextLong()
	}
}

func BenchmarkXoroshiroNextIntn(b *testing.B) {
	x := MakeXoroshiro(1)
	b.ReportAllocs()
	for b.Loop() {
		sink += int64(x.NextIntn(1000))
	}
}

func BenchmarkLegacyNextIntn(b *testing.B) {
	l := MakeLegacy(1)
	b.ReportAllocs()
	for b.Loop() {
		sink += int64(l.NextIntn(1000))
	}
}

func BenchmarkLegacyNextDouble(b *testing.B) {
	l := MakeLegacy(1)
	b.ReportAllocs()
	for b.Loop() {
		sink += int64(l.NextDouble() * 1000)
	}
}

func BenchmarkPositionalAtValue(b *testing.B) {
	x := MakeXoroshiro(7)
	p := x.ForkPositionalX()
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		r := p.AtValue(i, 64, -i)
		sink += r.NextLong()
	}
}

func BenchmarkFromHashOfValue(b *testing.B) {
	x := MakeXoroshiro(7)
	p := x.ForkPositionalX()
	b.ReportAllocs()
	for b.Loop() {
		r := p.FromHashOfValue("minecraft:terrain")
		sink += r.NextLong()
	}
}

func BenchmarkWorldgenDecorationSeed(b *testing.B) {
	w := NewWorldgenRandom(NewXoroshiro(0))
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		sink += w.SetDecorationSeed(12345, i*16, -i*16)
	}
}
