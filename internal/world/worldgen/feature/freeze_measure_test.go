package feature_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
)

// TestEnvFeaturesMeasure prints, for the blocks the environment features
// write, reference count, kremenmc count and position matches over the border
// chunks of the bench reference worlds. Opt-in: KREMENMC_ENV_MEASURE=1.
func TestEnvFeaturesMeasure(t *testing.T) {
	if os.Getenv("KREMENMC_ENV_MEASURE") != "1" {
		t.Skip("KREMENMC_ENV_MEASURE=1 measures against the reference worlds")
	}
	names := []string{"snow", "ice", "water", "lava", "mossy_cobblestone", "cobblestone", "spawner", "chest", "magma_block", "cave_air", "snowy_grass_block", "snowy_podzol"}
	root, err := reftest.Root()
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []int64{8266389061079214669, 0, 12345} {
		if only := os.Getenv("KREMENMC_ENV_SEED"); only != "" && only != fmt.Sprint(seed) {
			continue
		}
		dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", fmt.Sprintf("%d-overworld", seed), "world/dimensions/minecraft/overworld/region")
		if _, err := os.Stat(dir); err != nil {
			t.Logf("seed %d: no reference world", seed)
			continue
		}
		st := anvil.NewStorage(dir, world.Overworld)
		tr, err := vanilla.NewTerrain("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		stages, err := tr.Stages("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages})
		if err != nil {
			t.Fatal(err)
		}
		ref, got, match := map[string]int{}, map[string]int{}, map[string]int{}
		name := func(s data.StateID) string {
			b := data.Canonical().BlockOf(s)
			if b == nil {
				return ""
			}
			n := strings.TrimPrefix(b.Name, "minecraft:")
			if v, ok := data.Canonical().Value(s, "snowy"); ok && v == "true" {
				n = "snowy_" + n
			}
			return n
		}
		spec := os.Getenv("KREMENMC_ENV_CHUNKS")
		if spec == "" {
			spec = "border"
		}
		ps, err := chunkspec.Parse(spec, nil)
		if err != nil {
			t.Fatal(err)
		}
		used := 0
		for _, q := range ps {
			cp := world.ChunkPos{X: q.X, Z: q.Z}
			rc, err := st.LoadChunk(cp)
			if err != nil || rc == nil {
				continue
			}
			used++
			pc, err := p.Get(context.Background(), cp, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			for y := world.Overworld.MinY; y < world.Overworld.MinY+world.Overworld.Height; y++ {
				for z := range 16 {
					for x := range 16 {
						a, b := name(rc.Block(x, y, z)), name(pc.Chunk.Block(x, y, z))
						ref[a]++
						got[b]++
						if a == b {
							match[a]++
						}
					}
				}
			}
		}
		p.Close()
		st.Close()
		var b strings.Builder
		for _, n := range names {
			fmt.Fprintf(&b, "  %-18s ref=%d got=%d match=%d\n", n, ref[n], got[n], match[n])
		}
		t.Logf("seed %d, %s, %d full reference chunks:\n%s", seed, spec, used, b.String())
	}
}

// TestEnvFindCold lists chunks whose biome at y=64 is cold, to pick a
// reference chunk set for snow and ice. Opt-in: KREMENMC_ENV_FIND=<seed>.
func TestEnvFindCold(t *testing.T) {
	v := os.Getenv("KREMENMC_ENV_FIND")
	if v == "" {
		t.Skip("KREMENMC_ENV_FIND=<seed>")
	}
	var seed int64
	fmt.Sscan(v, &seed)
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for cx := int32(-48); cx <= 48; cx += 8 {
		for cz := int32(-48); cz <= 48; cz += 8 {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusBiomes)
			if err != nil {
				t.Fatal(err)
			}
			n := data.Canonical().BiomeName(pc.Chunk.Biome(8, 64, 8))
			if strings.Contains(n, "snowy") || strings.Contains(n, "frozen") || strings.Contains(n, "ice") || n == "grove" {
				t.Logf("chunk %d,%d %s", cx, cz, n)
			}
		}
	}
}
