package biome_test

import (
	"context"
	"hash/fnv"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
)

// The BIOMES stage run through the parallel pipeline must equal the
// sequential, oracle-verified FillWorldChunk for every worker count and
// request order.

var pipelineDims = []struct {
	name string
	dim  world.Dimension
}{
	{"overworld", world.Overworld},
	{"the_nether", world.Nether},
	{"the_end", world.End},
}

func pipelineChunks() []world.ChunkPos {
	var out []world.ChunkPos
	add := func(cx, cz, r int32) {
		for x := cx - r; x < cx+r; x++ {
			for z := cz - r; z < cz+r; z++ {
				out = append(out, world.ChunkPos{X: x, Z: z})
			}
		}
	}
	add(0, 0, 3)        // chunk and region corner at block 0,0
	add(32, -32, 3)     // region corner at block 512,-512
	add(-1875, 6250, 2) // around block -30000,100000
	add(62500, 62500, 2)
	return out
}

func biomeHash(c *world.Chunk) uint64 {
	h := fnv.New64a()
	var b [2]byte
	for i := range c.Sections {
		s := &c.Sections[i]
		for y := range 4 {
			for z := range 4 {
				for x := range 4 {
					id := s.Biome(x, y, z)
					b[0], b[1] = byte(id), byte(id>>8)
					h.Write(b[:])
				}
			}
		}
	}
	return h.Sum64()
}

func sequential(t testing.TB, src biome.Source, dim world.Dimension, ps []world.ChunkPos) map[world.ChunkPos]uint64 {
	plains, _ := data.Canonical().BiomeByName("plains")
	want := make(map[world.ChunkPos]uint64, len(ps))
	for _, p := range ps {
		c := world.NewChunk(p.X, p.Z, dim, plains)
		if err := biome.FillWorldChunk(src, c); err != nil {
			t.Fatal(err)
		}
		want[p] = biomeHash(c)
	}
	return want
}

func newPipeline(t testing.TB, src biome.Source, dim world.Dimension, workers int) *gen.Pipeline {
	p, err := gen.New(gen.Config{Seed: 1, Dim: dim, Workers: workers, Stages: []gen.Stage{
		gen.StageFunc{S: gen.StatusBiomes, F: func(sc *gen.StageContext) error {
			return biome.FillWorldChunk(src, sc.Chunk().Chunk)
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type order struct {
	name string
	perm func([]world.ChunkPos) []world.ChunkPos
	// concurrent issues every request at once from separate goroutines.
	concurrent bool
}

var orders = []order{
	{name: "listed", perm: slices.Clone[[]world.ChunkPos]},
	{name: "reverse", perm: func(ps []world.ChunkPos) []world.ChunkPos { o := slices.Clone(ps); slices.Reverse(o); return o }},
	{name: "random1", perm: shuffled(1)},
	{name: "random2", perm: shuffled(2)},
	{name: "all-at-once", perm: shuffled(3), concurrent: true},
}

func shuffled(seed uint64) func([]world.ChunkPos) []world.ChunkPos {
	return func(ps []world.ChunkPos) []world.ChunkPos {
		o := slices.Clone(ps)
		r := rand.New(rand.NewPCG(seed, seed))
		r.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
		return o
	}
}

func runPipeline(t testing.TB, p *gen.Pipeline, ps []world.ChunkPos, concurrent bool) map[world.ChunkPos]uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	got := make(map[world.ChunkPos]uint64, len(ps))
	var mu sync.Mutex
	get := func(pos world.ChunkPos) {
		pc, err := p.Get(ctx, pos, gen.StatusBiomes)
		if err != nil {
			t.Error(err)
			return
		}
		h := biomeHash(pc.Chunk)
		mu.Lock()
		got[pos] = h
		mu.Unlock()
	}
	if !concurrent {
		for _, pos := range ps {
			get(pos)
		}
		return got
	}
	var wg sync.WaitGroup
	for _, pos := range ps {
		wg.Go(func() { get(pos) })
	}
	wg.Wait()
	return got
}

func TestPipelineMatchesSequential(t *testing.T) {
	ps := pipelineChunks()
	workers := []int{1, 2, 4, 8, runtime.GOMAXPROCS(0)}
	if testing.Short() {
		workers = []int{1, 4}
	}
	for _, d := range pipelineDims {
		t.Run(d.name, func(t *testing.T) {
			src, err := biome.ForDimension(d.name, 8266389061079214669)
			if err != nil {
				t.Fatal(err)
			}
			want := sequential(t, src, d.dim, ps)
			for _, w := range workers {
				for _, o := range orders {
					p := newPipeline(t, src, d.dim, w)
					got := runPipeline(t, p, o.perm(ps), o.concurrent)
					if err := p.Close(); err != nil {
						t.Fatal(err)
					}
					bad := 0
					for _, pos := range ps {
						if got[pos] != want[pos] {
							bad++
						}
					}
					if bad > 0 {
						t.Errorf("workers=%d order=%s: %d/%d chunks differ from sequential", w, o.name, bad, len(ps))
					}
				}
			}
		})
	}
}

// BenchmarkPipelineBiomes reports chunks/s for the same 16x16 area at
// several worker counts.
func BenchmarkPipelineBiomes(b *testing.B) {
	src, err := biome.ForDimension("overworld", 8266389061079214669)
	if err != nil {
		b.Fatal(err)
	}
	var ps []world.ChunkPos
	for x := int32(0); x < 16; x++ {
		for z := int32(0); z < 16; z++ {
			ps = append(ps, world.ChunkPos{X: x, Z: z})
		}
	}
	for _, w := range []int{1, 2, 4, 8, runtime.GOMAXPROCS(0)} {
		b.Run("workers="+itoa(w), func(b *testing.B) {
			for b.Loop() {
				p := newPipeline(b, src, world.Overworld, w)
				runPipeline(b, p, ps, true)
				p.Close()
			}
			b.ReportMetric(float64(len(ps)*b.N)/b.Elapsed().Seconds(), "chunks/s")
		})
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
