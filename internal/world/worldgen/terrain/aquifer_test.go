package terrain

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// pinned are noise-stage chunk hashes for chunks that matched the reference
// dumps block for block (border+far presets); regenerate with KREMENMC_PIN=1.
var pinned = map[int64][][3]int64{
	0:                   {{-2, -2, -7335779700608015083}, {1, 1, 6350214763140229354}, {30, -34, -8269096448512239765}, {33, -31, 1805266346771127172}, {1873, 6248, -5581932645976635383}, {-1876, -6252, -3086397476182807500}, {62498, 62498, 5948281003621787932}},
	12345:               {{-2, -2, 5893794393555835936}, {1, 1, -3288756965454797710}, {30, -34, 5432319759088963218}, {33, -31, -1923166322360305313}, {1873, 6248, -9194878600848327859}, {-1876, -6252, 2853499083781479505}, {62498, 62498, 6396501915296963206}},
	8266389061079214669: {{-2, -2, -6782748500999582300}, {1, 1, 1050252277296192194}, {30, -34, 4239817506300834929}, {33, -31, 3028567581449167801}, {1873, 6248, -6538417316077469879}, {-1876, -6252, 4371151325371618880}, {62498, 62498, -6465731347345635027}},
}

var pinChunks = [][2]int32{{-2, -2}, {1, 1}, {30, -34}, {33, -31}, {1873, 6248}, {-1876, -6252}, {62498, 62498}}

func noiseHash(t testing.TB, s *Staged, ctx *density.Ctx, x, z int32) uint64 {
	c := world.NewChunk(x, z, world.Overworld, 0)
	if err := s.GenerateStageCtx(ctx, c, "noise"); err != nil {
		t.Fatal(err)
	}
	return chunkHash(c)
}

func TestPinnedOverworldNoise(t *testing.T) {
	regen := os.Getenv("KREMENMC_PIN") == "1"
	for seed, want := range pinned {
		s, err := NewStaged("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		ctx := &density.Ctx{}
		var got [][3]int64
		for _, p := range pinChunks {
			got = append(got, [3]int64{int64(p[0]), int64(p[1]), int64(noiseHash(t, s, ctx, p[0], p[1]))})
		}
		if regen {
			fmt.Printf("\t%d: %#v,\n", seed, got)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("seed %d: got %v want %v", seed, got, want)
		}
	}
}

func TestParallelOverworldNoise(t *testing.T) {
	s, err := NewStaged("overworld", 12345)
	if err != nil {
		t.Fatal(err)
	}
	var pos [][2]int32
	for x := int32(-3); x < 3; x++ {
		for z := int32(-2); z < 2; z++ {
			pos = append(pos, [2]int32{x, z})
		}
	}
	run := func(workers int, reverse bool) (map[[2]int32]uint64, time.Duration) {
		out := make(map[[2]int32]uint64, len(pos))
		var mu sync.Mutex
		var wg sync.WaitGroup
		next := make(chan [2]int32)
		t0 := time.Now()
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := &density.Ctx{}
				for p := range next {
					h := noiseHash(t, s, ctx, p[0], p[1])
					mu.Lock()
					out[p] = h
					mu.Unlock()
				}
			}()
		}
		for i := range pos {
			if reverse {
				i = len(pos) - 1 - i
			}
			next <- pos[i]
		}
		close(next)
		wg.Wait()
		return out, time.Since(t0)
	}
	want, d := run(1, false)
	t.Logf("workers 1: %.1f chunks/s", float64(len(pos))/d.Seconds())
	got, d := run(runtime.GOMAXPROCS(0), true)
	t.Logf("workers %d: %.1f chunks/s", runtime.GOMAXPROCS(0), float64(len(pos))/d.Seconds())
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatal("parallel output differs from sequential")
	}
}
