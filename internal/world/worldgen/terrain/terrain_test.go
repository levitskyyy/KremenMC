package terrain

import (
	"fmt"
	"hash/fnv"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world"
)

func chunkHash(c *world.Chunk) uint64 {
	h := fnv.New64a()
	for y := c.Dim.MinY; y < c.Dim.MinY+c.Dim.Height; y++ {
		for z := range 16 {
			for x := range 16 {
				fmt.Fprint(h, c.Block(x, y, z), ",")
			}
		}
	}
	return h.Sum64()
}

func TestConcurrentStages(t *testing.T) {
	s, err := NewStaged("the_nether", 12345)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(x, z int32) uint64 {
		c := world.NewChunk(x, z, world.Nether, 0)
		if err := s.GenerateStage(c, "noise"); err != nil {
			t.Error(err)
		}
		return chunkHash(c)
	}
	var pos [][2]int32
	want := map[[2]int32]uint64{}
	for x := int32(-2); x < 2; x++ {
		for z := int32(-2); z < 2; z++ {
			pos = append(pos, [2]int32{x, z})
			want[[2]int32{x, z}] = gen(x, z)
		}
	}
	for _, workers := range []int{2, 8} {
		var wg sync.WaitGroup
		var mu sync.Mutex
		bad := 0
		for w := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := len(pos) - 1 - w; i >= 0; i -= workers {
					if gen(pos[i][0], pos[i][1]) != want[pos[i]] {
						mu.Lock()
						bad++
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		if bad > 0 {
			t.Fatalf("%d workers: %d chunks differ from sequential", workers, bad)
		}
	}
}

func BenchmarkFillNether(b *testing.B) {
	s, err := NewStaged("the_nether", 12345)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		c := world.NewChunk(i, -i, world.Nether, 0)
		_ = s.GenerateStage(c, "noise")
	}
}
