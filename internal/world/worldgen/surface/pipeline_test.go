package surface_test

import (
	"context"
	"hash/fnv"
	"runtime"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/terrain"
)

// The material pass inside the parallel pipeline (biomes of neighbours read
// through the stage view) must equal the sequential pass with biomes from
// the source, for 1 and GOMAXPROCS workers and reversed request order.

func blockHash(c *world.Chunk) uint64 {
	h := fnv.New64a()
	var b [4]byte
	for i := range c.Sections {
		for j := range 4096 {
			v := c.Sections[i].Blocks.Get(j)
			b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
			h.Write(b[:])
		}
	}
	return h.Sum64()
}

func TestPipelineEquality(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	const seed = 8266389061079214669
	src, err := biome.ForDimension("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	tg, err := terrain.New("minecraft:overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	sys, err := surface.Vanilla("minecraft:overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	var ps []world.ChunkPos
	for _, c := range [][2]int32{{0, 0}, {32, -32}, {-1875, -6250}} {
		for x := c[0] - 2; x < c[0]+2; x++ {
			for z := c[1] - 1; z < c[1]+1; z++ {
				ps = append(ps, world.ChunkPos{X: x, Z: z})
			}
		}
	}
	plains, _ := data.Canonical().BiomeByName("plains")
	want := map[world.ChunkPos]uint64{}
	ctx := surface.NewCtx()
	for _, p := range ps {
		c := world.NewChunk(p.X, p.Z, world.Overworld, plains)
		if err := biome.FillWorldChunk(src, c); err != nil {
			t.Fatal(err)
		}
		tg.Fill(&density.Ctx{}, c)
		if err := sys.Build(ctx, c, surface.NewSourceBiomes(src, -64, 384)); err != nil {
			t.Fatal(err)
		}
		want[p] = blockHash(c)
	}
	for _, workers := range []int{1, runtime.GOMAXPROCS(0)} {
		p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Workers: workers, Stages: []gen.Stage{
			gen.StageFunc{S: gen.StatusBiomes, F: func(sc *gen.StageContext) error { return biome.FillWorldChunk(src, sc.Chunk().Chunk) }},
			gen.StageFunc{S: gen.StatusTerrain, F: func(sc *gen.StageContext) error {
				c := sc.Chunk().Chunk
				tg.Fill(&density.Ctx{}, c)
				return sys.Build(surface.NewCtx(), c, surface.RegionBiomes(sc))
			}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		for i := len(ps) - 1; i >= 0; i-- {
			pc, err := p.Get(cctx, ps[i], gen.StatusTerrain)
			if err != nil {
				t.Fatal(err)
			}
			if h := blockHash(pc.Chunk); h != want[ps[i]] {
				t.Errorf("workers %d chunk %v: pipeline result differs from sequential", workers, ps[i])
			}
		}
		cancel()
		p.Close()
	}
}
