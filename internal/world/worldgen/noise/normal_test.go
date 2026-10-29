package noise

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func hexF64(t testing.TB, s string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return math.Float64frombits(v)
}

func hexF32(t testing.TB, s string) float32 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return math.Float32frombits(uint32(v))
}

func h64(v float64) string { return strconv.FormatUint(math.Float64bits(v), 16) }
func h32(v float32) string { return strconv.FormatUint(uint64(math.Float32bits(v)), 16) }

func javaBytesHash(p [256]uint8) int32 {
	h := int32(1)
	for _, b := range p {
		h = 31*h + int32(int8(b))
	}
	return h
}

type checker struct {
	t     *testing.T
	n     int
	fails int
}

func (c *checker) eq(ctx, got, want string) {
	c.n++
	if got != want {
		c.fails++
		if c.fails <= 25 {
			c.t.Errorf("%s: got %s want %s", ctx, got, want)
		}
	}
}

func (c *checker) done() {
	if c.fails > 0 {
		c.t.Errorf("%d/%d mismatches", c.fails, c.n)
	}
	c.t.Logf("%d checks", c.n)
}

func describeGradient(g *gradientBase, kind string) string {
	return kind + " " + h64(g.offX) + " " + h64(g.offY) + " " + h64(g.offZ) + " " + strconv.Itoa(int(javaBytesHash(g.perm)))
}

// describeStack renders a stack like the probe's Refl.describe.
func describeStack(s *Stack) []string {
	out := []string{"stack 0 Perlin " + h32(s.Range().Min) + " " + h32(s.Range().Max)}
	for _, l := range s.Layers() {
		p := l.Noise.(*Perlin)
		out = append(out, "layer 0 "+h64(l.Frequency)+" "+h32(l.Amplitude)+" "+describeGradient(&p.gradientBase, "PerlinNoise"))
	}
	return append(out, "end 0")
}

func TestNormalNoiseReference(t *testing.T) {
	data := reftest.Golden(t, "normal.txt", "kremenmc.probe.noise.NormalParams", "1", "110", "server-26.3.jar", "18")
	c := &checker{t: t}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	var nn *NormalNoise
	var stack *Stack
	var expect []string
	var ctx string
	flush := func() {
		for len(expect) > 0 {
			c.eq(ctx+" structure", "missing", expect[0])
			expect = expect[1:]
		}
	}
	for sc.Scan() {
		line := sc.Text()
		f := strings.Fields(line)
		if len(expect) > 0 && (f[0] == "stack" || f[0] == "layer" || f[0] == "end") {
			c.eq(ctx+" structure", expect[0], line)
			expect = expect[1:]
			continue
		}
		flush()
		switch f[0] {
		case "json":
			ctx = line
			var p NormalParams
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "json ")), &p); err != nil {
				t.Fatal(err)
			}
			var err error
			nn, err = NewNormalNoise(p)
			if err != nil {
				nn = nil
			}
		case "parity":
			ctx = line
			var amps []float64
			for _, s := range f[2:] {
				amps = append(amps, hexF64(t, s))
			}
			first, _ := strconv.Atoi(f[1])
			var err error
			nn, err = NewNormalNoiseParity(first, amps)
			if err != nil {
				nn = nil
			}
		case "error":
			c.eq(ctx, strconv.FormatBool(nn == nil), "true")
		case "params":
			if nn == nil {
				c.eq(ctx, "error", line)
				continue
			}
			p := nn.Params
			got := "params " + h64(p.BaseAmplitude) + " " + strconv.Itoa(p.BaseOctave) + " " + strconv.Itoa(p.OctaveCount) + " " + p.Normalize.String()
			for _, m := range p.AmplitudeModifiers {
				got += " " + h64(m)
			}
			c.eq(ctx, got, line)
			var want []string
			want = append(want, "nf "+h64(nn.NormalizationFactor())+" range "+h32(nn.Range().Min)+" "+h32(nn.Range().Max))
			for _, o := range nn.Octaves() {
				want = append(want, "octave "+strconv.Itoa(o.Index)+" "+h64(o.Frequency)+" "+h64(o.Amplitude)+" "+o.Seed())
			}
			for _, w := range want {
				if !sc.Scan() {
					t.Fatal("eof")
				}
				c.eq(ctx, w, sc.Text())
			}
		case "seed":
			sd, _ := strconv.ParseInt(f[1], 10, 64)
			stack = nn.Create(rng.NewXoroshiro(sd))
			expect = describeStack(stack)
			if s, err := nn.CreateForLegacyNetherBiome(rng.NewLegacy(sd)); err == nil {
				defer func(s *Stack) {}(s)
				legacyStacks[nn] = s
			} else {
				legacyStacks[nn] = nil
			}
		case "legacy":
			if len(f) > 1 {
				c.eq(ctx+" legacy", strconv.FormatBool(legacyStacks[nn] == nil), "true")
				continue
			}
			stack = legacyStacks[nn]
			if stack == nil {
				c.eq(ctx+" legacy", "error", "ok")
				continue
			}
			expect = describeStack(stack)
		case "s":
			x, y, z := hexF64(t, f[1]), hexF64(t, f[2]), hexF64(t, f[3])
			c.eq(ctx+" "+line, h32(stack.Get(x, y, z))+" "+h32(stack.Get2(x, z)), f[5]+" "+f[6])
		default:
			t.Fatalf("unknown line %q", line)
		}
	}
	flush()
	c.done()
}

var legacyStacks = map[*NormalNoise]*Stack{}
