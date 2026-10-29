package carver

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// BenchmarkCarveChunk: mask of one overworld chunk (17x17 sources), with
// biomes precomputed so only carving is measured.
func BenchmarkCarveChunk(b *testing.B) {
	set, err := NewSet(dimCtx["overworld"], 12345)
	if err != nil {
		b.Fatal(err)
	}
	src, _ := biome.ForDimension("overworld", 12345)
	ctx := &density.Ctx{}
	biomes := map[[2]int32]string{}
	at := func(qx, qz int32) string {
		k := [2]int32{qx, qz}
		if v, ok := biomes[k]; ok {
			return v
		}
		v := src.Biome(ctx, nil, qx, 0, qz)
		biomes[k] = v
		return v
	}
	m := NewMask(-64, 384)
	b.ReportAllocs()
	for i := range b.N {
		m.Reset()
		set.CarveChunk(int32(i%8), int32(i/8%8), at, m)
	}
}

func BenchmarkCarveEllipsoid(b *testing.B) {
	m := NewMask(-64, 384)
	skip := func(dx, dy, dz float64, _ int) bool { return caveSkip(dx, dy, dz, -0.7) }
	b.ReportAllocs()
	for range b.N {
		CarveEllipsoid(0, 0, 8.3, 20.1, 7.7, 6.5, 4.2, m, skip)
	}
}
