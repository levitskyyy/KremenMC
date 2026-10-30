package feature_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
)

// TestDimMeasure prints per block name reference count, kremenmc count and
// position matches over reference chunks of the nether or the end.
// Opt-in: KREMENMC_DIM_MEASURE=the_nether|the_end [KREMENMC_DIM_SEED=…]
// [KREMENMC_DIM_CHUNKS=border] [KREMENMC_DIM_BIOME=1: split by biome]
// [KREMENMC_DIM_DIFF=name: print the first differing positions of a block].
func TestDimMeasure(t *testing.T) {
	dim := os.Getenv("KREMENMC_DIM_MEASURE")
	if dim == "" {
		t.Skip("KREMENMC_DIM_MEASURE=the_nether|the_end")
	}
	seed := int64(8266389061079214669)
	if v := os.Getenv("KREMENMC_DIM_SEED"); v != "" {
		fmt.Sscan(v, &seed)
	}
	wd := world.Nether
	sub := "the_nether"
	if dim == "the_end" {
		wd, sub = world.End, "the_end"
	}
	root, err := reftest.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", fmt.Sprintf("%d-%s", seed, sub), "world/dimensions/minecraft/"+sub+"/region")
	st := anvil.NewStorage(dir, wd)
	defer st.Close()
	tr, err := vanilla.NewTerrain(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: wd, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	spec := os.Getenv("KREMENMC_DIM_CHUNKS")
	if spec == "" {
		spec = "border"
	}
	ps, err := chunkspec.Parse(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	byBiome := os.Getenv("KREMENMC_DIM_BIOME") != ""
	diff := os.Getenv("KREMENMC_DIM_DIFF")
	name := func(s data.StateID) string {
		b := data.Canonical().BlockOf(s)
		if b == nil {
			return ""
		}
		return strings.TrimPrefix(b.Name, "minecraft:")
	}
	ref, got, match := map[string]int{}, map[string]int{}, map[string]int{}
	var diffs []string
	used, ticksRef, ticksGot, ticksMatch := 0, 0, 0, 0
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
		refTicks := map[string]bool{}
		if l, ok := rc.Extra["fluid_ticks"].(nbt.List); ok {
			for _, it := range l.Items {
				m := it.(nbt.Compound)
				refTicks[fmt.Sprintf("%v %d %d %d t=%v", m["i"], m["x"], m["y"], m["z"], m["t"])] = true
			}
		}
		for _, tk := range pc.FluidTicks {
			k := fmt.Sprintf("%s %d %d %d t=%d", tk.Type, tk.X, tk.Y, tk.Z, tk.Delay)
			ticksGot++
			if refTicks[k] {
				ticksMatch++
			}
		}
		ticksRef += len(refTicks)
		for y := wd.MinY; y < wd.MinY+wd.Height; y++ {
			for z := range 16 {
				for x := range 16 {
					a, b := name(rc.Block(x, y, z)), name(pc.Chunk.Block(x, y, z))
					if a == b && (a == "air" || a == "netherrack" || a == "end_stone" || a == "bedrock") {
						continue
					}
					sfx := ""
					if byBiome {
						sfx = "@" + strings.TrimPrefix(data.Canonical().BiomeName(rc.Biome(x, y, z)), "minecraft:")
					}
					ref[a+sfx]++
					got[b+sfx]++
					if a == b {
						match[a+sfx]++
					} else if diff != "" && (a == diff || b == diff) && len(diffs) < 60 {
						diffs = append(diffs, fmt.Sprintf("%d %d %d ref=%s got=%s", int(cp.X)*16+x, y, int(cp.Z)*16+z, a, b))
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(ref))
	for k := range ref {
		keys = append(keys, k)
	}
	for k := range got {
		if _, ok := ref[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		if ref[k] == match[k] && got[k] == match[k] {
			fmt.Fprintf(&b, "  %-40s ref=%d ok\n", k, ref[k])
			continue
		}
		fmt.Fprintf(&b, "  %-40s ref=%d got=%d match=%d\n", k, ref[k], got[k], match[k])
	}
	t.Logf("seed %d %s %s, %d reference chunks (air/netherrack/end_stone/bedrock matches skipped):\n%sfluid ticks ref=%d got=%d match=%d\n%s", seed, dim, spec, used, b.String(), ticksRef, ticksGot, ticksMatch, strings.Join(diffs, "\n"))
}

// TestDimFindBiomes lists, per biome, grid chunks whose biome at y 40 (and
// 64) is that biome, to pick reference chunk sets.
// Opt-in: KREMENMC_DIM_FIND=the_nether|the_end [KREMENMC_DIM_SEED=…].
func TestDimFindBiomes(t *testing.T) {
	dim := os.Getenv("KREMENMC_DIM_FIND")
	if dim == "" {
		t.Skip("KREMENMC_DIM_FIND=the_nether|the_end")
	}
	seed := int64(8266389061079214669)
	if v := os.Getenv("KREMENMC_DIM_SEED"); v != "" {
		fmt.Sscan(v, &seed)
	}
	wd := world.Nether
	if dim == "the_end" {
		wd = world.End
	}
	tr, err := vanilla.NewTerrain(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: wd, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	found := map[string][]string{}
	for cx := int32(-96); cx <= 96; cx += 6 {
		for cz := int32(-96); cz <= 96; cz += 6 {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusBiomes)
			if err != nil {
				t.Fatal(err)
			}
			n := strings.TrimPrefix(data.Canonical().BiomeName(pc.Chunk.Biome(8, 40, 8)), "minecraft:")
			if len(found[n]) < 12 {
				found[n] = append(found[n], fmt.Sprintf("%d,%d", cx, cz))
			}
		}
	}
	for k, v := range found {
		t.Logf("%s: %s", k, strings.Join(v, " "))
	}
}

// TestNetherStemBorders splits reference huge-fungus stem bases (stem over
// nylium) into found / missing in ours by distance to the chunk border.
// Opt-in: KREMENMC_STEM_BORDERS=1.
func TestNetherStemBorders(t *testing.T) {
	if os.Getenv("KREMENMC_STEM_BORDERS") == "" {
		t.Skip()
	}
	seed := int64(8266389061079214669)
	root, _ := reftest.Root()
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", fmt.Sprintf("%d-the_nether", seed), "world/dimensions/minecraft/the_nether/region")
	st := anvil.NewStorage(dir, world.Nether)
	defer st.Close()
	tr, _ := vanilla.NewTerrain("the_nether", seed)
	stages, _ := tr.Stages("the_nether", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Nether, Stages: stages})
	defer p.Close()
	ps, _ := chunkspec.Parse("border", nil)
	isStem := func(s data.StateID) bool { return strings.HasSuffix(data.StateString(s), "_stem[axis=y]") }
	isNyl := func(s data.StateID) bool { return strings.Contains(data.StateString(s), "nylium") }
	found, miss := map[int]int{}, map[int]int{}
	for _, q := range ps {
		cp := world.ChunkPos{X: q.X, Z: q.Z}
		rc, err := st.LoadChunk(cp)
		if err != nil || rc == nil {
			continue
		}
		pc, _ := p.Get(context.Background(), cp, gen.StatusFull)
		for y := 1; y < 128; y++ {
			for z := range 16 {
				for x := range 16 {
					if isStem(rc.Block(x, y, z)) && isNyl(rc.Block(x, y-1, z)) {
						d := min(x, z, 15-x, 15-z)
						if isStem(pc.Chunk.Block(x, y, z)) {
							found[d]++
						} else {
							miss[d]++
						}
					}
				}
			}
		}
	}
	t.Logf("by distance to chunk border: found %v missing %v", found, miss)
}
