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

func describeSmearedStack(s *Stack) []string {
	out := []string{"stack 0 SmearedPerlin " + h32(s.Range().Min) + " " + h32(s.Range().Max)}
	for _, l := range s.Layers() {
		p := l.Noise.(*SmearedPerlin)
		out = append(out, "layer 0 "+h64(l.Frequency)+" "+h32(l.Amplitude)+" "+describeGradient(&p.gradientBase, "SmearedPerlinNoise")+" "+h64(p.FudgeYScale()))
	}
	return append(out, "end 0")
}

func TestBlendedReference(t *testing.T) {
	data := reftest.Golden(t, "blended.txt", "kremenmc.probe.noise.BlendedProbe", "5", "40", "150")
	c := &checker{t: t}
	sc := bufio.NewScanner(bytes.NewReader(data))
	var expect []string
	var b *Blended
	var ctx string
	nBlended := 0
	for sc.Scan() {
		line := sc.Text()
		f := strings.Fields(line)
		switch f[0] {
		case "stack", "layer", "end":
			if len(expect) == 0 {
				c.eq(ctx, "nothing", line)
				continue
			}
			c.eq(ctx, expect[0], line)
			expect = expect[1:]
		case "fbm":
			ctx = line
			sd, _ := strconv.ParseInt(f[1], 10, 64)
			first, _ := strconv.Atoi(f[2])
			expect = describeSmearedStack(CreateFbm(rng.NewXoroshiro(sd), first, hexF64(t, f[3]), hexF64(t, f[4])))
		case "blended":
			ctx = line
			p := BlendedParams{hexF64(t, f[3]), hexF64(t, f[4]), hexF64(t, f[5]), hexF64(t, f[6]), hexF64(t, f[7])}
			b = p.NewBlended(refSource(t, f[1], f[2]))
			c.eq(ctx+" range", h32(b.Range().Min)+" "+h32(b.Range().Max), f[9]+" "+f[10])
			expect = nil
			nBlended++
			if nBlended > 2 {
				continue
			}
			for _, s := range []*Stack{b.MinLimit, b.MaxLimit, b.Main} {
				expect = append(expect, describeSmearedStack(s)...)
			}
		case "v":
			expect = nil
			var p [3]int32
			for i := range p {
				v, _ := strconv.Atoi(f[1+i])
				p[i] = int32(v)
			}
			c.eq(ctx+" "+line, h32(b.Sample(p[0], p[1], p[2])), f[5])
		default:
			t.Fatalf("unknown line %q", line)
		}
	}
	c.done()
}
