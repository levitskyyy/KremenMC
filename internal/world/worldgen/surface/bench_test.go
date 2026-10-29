package surface

import (
	"path/filepath"
	"testing"
)

func BenchmarkBuildOverworld(b *testing.B) {
	rc := refCases(b)[1]
	sys, err := Vanilla(rc.settings, rc.seed)
	if err != nil {
		b.Fatal(err)
	}
	in := readInputs(b, filepath.Join("testdata", rc.name+".in.gz"), dimOf(sys))
	bs := NewSourceBiomes(biomeSource(b, rc.settings, rc.dim, rc.seed), sys.MinY, sys.Height)
	ctx := NewCtx()
	for _, c := range in {
		_ = sys.Build(ctx, cloneChunk(c), bs) // warm biome cache
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		c := cloneChunk(in[i%len(in)])
		i++
		if err := sys.Build(ctx, c, bs); err != nil {
			b.Fatal(err)
		}
	}
}
