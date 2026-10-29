package biome

import (
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world"
)

func TestConcurrentFill(t *testing.T) {
	src, err := ForDimension("overworld", 8266389061079214669)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int32]uint64{}
	for x := int32(-3); x < 3; x++ {
		for z := int32(-3); z < 3; z++ {
			want[[2]int32{x, z}] = fillHash(src, x, z, world.Overworld)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k, h := range want {
				if (k[0]+k[1]+int32(g))%2 == 0 && fillHash(src, k[0], k[1], world.Overworld) != h {
					errs <- "mismatch"
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

func BenchmarkFillChunkOverworld(b *testing.B) {
	src, err := ForDimension("overworld", 12345)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	n := 0
	i := int32(0)
	for b.Loop() {
		i++
		FillChunk(src, i, -i, -4, 24, func(qx, qy, qz int32, s string) { n += len(s) })
	}
}
