package noise

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func refSource(t testing.TB, alg, seed string) rng.Source {
	sd, err := strconv.ParseInt(seed, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if alg == "xoro" {
		return rng.NewXoroshiro(sd)
	}
	return rng.NewLegacy(sd)
}

func TestPrimitivesReference(t *testing.T) {
	data := reftest.Golden(t, "prim.txt", "kremenmc.probe.noise.PrimProbe", "11", "160", "60")
	c := &checker{t: t}
	sc := bufio.NewScanner(bytes.NewReader(data))
	var (
		src     rng.Source
		perlin  *Perlin
		simplex *Simplex
		smeared *SmearedPerlin
		ctx     string
	)
	for sc.Scan() {
		line := sc.Text()
		lhs, rhs, _ := strings.Cut(line, " = ")
		f := strings.Fields(lhs)
		w := strings.Fields(rhs)
		switch f[0] {
		case "perlin":
			ctx = line
			src = refSource(t, f[1], f[2])
			perlin, simplex, smeared = NewPerlin(src), nil, nil
		case "state":
			c.eq(ctx, "state "+describeGradient(&perlin.gradientBase, "PerlinNoise")+" next "+strconv.FormatInt(src.NextLong(), 10), line)
		case "simplex":
			ctx = line
			src = refSource(t, f[1], f[2])
			simplex, perlin, smeared = NewSimplexOpt(src, f[3] == "true"), nil, nil
			c.eq(ctx, describeGradient(&simplex.gradientBase, "SimplexNoise"), strings.Join(f[4:], " "))
		case "smeared":
			ctx = line
			src = refSource(t, f[1], f[2])
			smeared, perlin, simplex = NewSmearedPerlin(src, hexF64(t, f[3])), nil, nil
			c.eq(ctx+" range", h32(smeared.Range().Max), f[4])
			c.eq(ctx, describeGradient(&smeared.gradientBase, "SmearedPerlinNoise")+" "+h64(smeared.FudgeYScale()), strings.Join(f[5:], " "))
		case "g":
			x, y, z := hexF64(t, f[1]), hexF64(t, f[2]), hexF64(t, f[3])
			d := [3]float32{0.25, -1, 3}
			v := perlin.NoiseWithDerivative(x, y, z, &d)
			c.eq(ctx+" "+lhs, strings.Join([]string{h32(perlin.Get(x, y, z)), h32(perlin.Get2(x, z)), h32(v), h32(d[0]), h32(d[1]), h32(d[2])}, " "), rhs)
		case "sl":
			var gi [3]int32
			for i := range gi {
				v, _ := strconv.ParseInt(f[1+i], 10, 32)
				gi[i] = int32(v)
			}
			got := perlin.SampleAndLerp(gi[0], gi[1], gi[2], hexF32(t, f[4]), hexF32(t, f[5]), hexF32(t, f[6]), hexF32(t, f[7]))
			c.eq(ctx+" "+lhs, h32(got), w[0])
		case "s":
			x, y, z := hexF64(t, f[1]), hexF64(t, f[2]), hexF64(t, f[3])
			var n Noise = simplex
			if smeared != nil {
				n = smeared
			}
			c.eq(ctx+" "+lhs, h32(n.Get(x, y, z))+" "+h32(n.Get2(x, z)), rhs)
		default:
			t.Fatalf("unknown line %q", line)
		}
	}
	c.done()
}
