package feature_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
)

// TestTicksObserve prints the fluid_ticks/block_ticks of reference full
// chunks. Opt-in: KREMENMC_TICKS_OBSERVE=<seed>-<dim>.
func TestTicksObserve(t *testing.T) {
	w := os.Getenv("KREMENMC_TICKS_OBSERVE")
	if w == "" {
		t.Skip()
	}
	root, _ := reftest.Root()
	var seed int64
	var dim string
	fmt.Sscanf(w, "%d-%s", &seed, &dim)
	wd := map[string]world.Dimension{"overworld": world.Overworld, "the_nether": world.Nether, "the_end": world.End}[dim]
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", w, "world/dimensions/minecraft/"+dim+"/region")
	st := anvil.NewStorage(dir, wd)
	defer st.Close()
	spec := os.Getenv("KREMENMC_TICKS_CHUNKS")
	if spec == "" {
		spec = "border"
	}
	ps, _ := chunkspec.Parse(spec, nil)
	stats := map[string]int{}
	name := func(c *world.Chunk, x, y, z int32) string {
		if x>>4 != c.X || z>>4 != c.Z || int(y) < wd.MinY || int(y) >= wd.MinY+wd.Height {
			return "?"
		}
		return strings.TrimPrefix(data.StateString(c.Block(int(x&15), int(y), int(z&15))), "minecraft:")
	}
	for _, q := range ps {
		c, err := st.LoadChunk(world.ChunkPos{X: q.X, Z: q.Z})
		if err != nil || c == nil {
			continue
		}
		for _, k := range []string{"fluid_ticks", "block_ticks"} {
			l, _ := c.Extra[k].(nbt.List)
			items := l.Items
			for _, it := range items {
				m := it.(nbt.Compound)
				x, y, z := m["x"].(int32), m["y"].(int32), m["z"].(int32)
				key := fmt.Sprintf("%s %v t=%v p=%v at=%s below=%s", k, m["i"], m["t"], m["p"], name(c, x, y, z), name(c, x, y-1, z))
				if k == "fluid_ticks" {
					var sides []string
					for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						sides = append(sides, name(c, x+d[0], y, z+d[1]))
					}
					key += " sides=" + strings.Join(sides, ",")
					if os.Getenv("KREMENMC_TICKS_POS") != "" {
						fmt.Println(x, y, z, key)
					}
				}
				stats[key]++
			}
		}
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		if strings.HasPrefix(k, "fluid") || os.Getenv("KREMENMC_TICKS_BLOCK") != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println(stats[k], k)
	}
}

// TestPostProcessObserve summarises PostProcessing positions and ticks of
// reference proto-chunks by status and block. Opt-in:
// KREMENMC_PP_OBSERVE=<seed>-<dim> [KREMENMC_PP_REGION=rx,rz].
func TestPostProcessObserve(t *testing.T) {
	w := os.Getenv("KREMENMC_PP_OBSERVE")
	if w == "" {
		t.Skip()
	}
	root, _ := reftest.Root()
	var seed int64
	var dim string
	fmt.Sscanf(w, "%d-%s", &seed, &dim)
	wd := map[string]world.Dimension{"overworld": world.Overworld, "the_nether": world.Nether, "the_end": world.End}[dim]
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", w, "world/dimensions/minecraft/"+dim+"/region")
	rx, rz := int32(0), int32(0)
	if v := os.Getenv("KREMENMC_PP_REGION"); v != "" {
		fmt.Sscanf(v, "%d,%d", &rx, &rz)
	}
	r, err := anvil.OpenRegionReadOnly(dir, rx, rz)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	stats := map[string]int{}
	for lz := range 32 {
		for lx := range 32 {
			raw, err := r.Read(lx, lz)
			if err != nil || raw == nil {
				continue
			}
			_, rootc, _, err := nbt.Unmarshal(raw, false)
			if err != nil {
				continue
			}
			p, _, err := anvil.DecodeProto(rootc, wd)
			if err != nil {
				continue
			}
			stats[p.Status+" chunks"]++
			for si, l := range p.PostProcessing {
				for _, v := range l {
					x, y, z := int(v&15), int(v>>4&15), int(v>>8&15)
					y += (wd.MinY>>4 + si) * 16
					stats[fmt.Sprintf("%s PP %s", p.Status, strings.TrimPrefix(data.StateString(p.Chunk.Block(x, y, z)), "minecraft:"))]++
				}
			}
			for _, tk := range p.FluidTicks {
				stats[fmt.Sprintf("%s FT %s t=%d", p.Status, tk.Type, tk.Delay)]++
			}
			for _, tk := range p.BlockTicks {
				stats[fmt.Sprintf("%s BT %s t=%d", p.Status, tk.Type, tk.Delay)]++
			}
		}
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println(stats[k], k)
	}
}

// TestEndSpikesObserve prints obsidian column tops, iron bars, bedrock and
// fire of the reference end around 0,0. Opt-in: KREMENMC_END_OBSERVE=1.
func TestEndSpikesObserve(t *testing.T) {
	if os.Getenv("KREMENMC_END_OBSERVE") == "" {
		t.Skip()
	}
	root, _ := reftest.Root()
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", "8266389061079214669-the_end", "world/dimensions/minecraft/the_end/region")
	st := anvil.NewStorage(dir, world.End)
	defer st.Close()
	type col struct{ lo, hi, n int }
	cols := map[[2]int]*col{}
	for cx := int32(-4); cx <= 3; cx++ {
		for cz := int32(-4); cz <= 3; cz++ {
			c, err := st.LoadChunk(world.ChunkPos{X: cx, Z: cz})
			if err != nil || c == nil {
				fmt.Println("missing", cx, cz)
				continue
			}
			for y := 0; y < 256; y++ {
				for z := range 16 {
					for x := range 16 {
						n := strings.TrimPrefix(data.StateString(c.Block(x, y, z)), "minecraft:")
						X, Z := int(cx)*16+x, int(cz)*16+z
						switch {
						case n == "obsidian":
							k := [2]int{X, Z}
							if cols[k] == nil {
								cols[k] = &col{lo: y, hi: y}
							}
							cols[k].hi = y
							cols[k].n++
						case n == "end_stone" || n == "air":
						default:
							fmt.Println("block", X, y, Z, n)
						}
					}
				}
			}
		}
	}
	for k, v := range cols {
		fmt.Println("obs", k[0], k[1], v.lo, v.hi, v.n)
	}
}

// TestAquiferPPCompare compares the water post-processing positions of
// reference terrain-status proto-chunks (region 0,0) with ours. Opt-in:
// KREMENMC_PP_COMPARE=<seed>.
func TestAquiferPPCompare(t *testing.T) {
	v := os.Getenv("KREMENMC_PP_COMPARE")
	if v == "" {
		t.Skip()
	}
	var seed int64
	fmt.Sscan(v, &seed)
	root, _ := reftest.Root()
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", fmt.Sprintf("%d-overworld", seed), "world/dimensions/minecraft/overworld/region")
	r, err := anvil.OpenRegionReadOnly(dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages})
	defer p.Close()
	ref, got, match, chunks := 0, 0, 0, 0
	for lz := range 32 {
		for lx := range 32 {
			raw, err := r.Read(lx, lz)
			if err != nil || raw == nil {
				continue
			}
			_, rootc, _, _ := nbt.Unmarshal(raw, false)
			rp, _, err := anvil.DecodeProto(rootc, world.Overworld)
			if err != nil || rp.Status != "minecraft:terrain" {
				continue
			}
			chunks++
			pos := func(pp [][]int16, keep func(x, y, z int) bool) map[[3]int]bool {
				m := map[[3]int]bool{}
				for si, l := range pp {
					for _, v := range l {
						x, y, z := int(v&15), world.Overworld.MinY+si*16+int(v>>4&15), int(v>>8&15)
						if keep(x, y, z) {
							m[[3]int{x, y, z}] = true
						}
					}
				}
				return m
			}
			isWater := func(c *world.Chunk) func(x, y, z int) bool {
				return func(x, y, z int) bool { return strings.Contains(data.StateString(c.Block(x, y, z)), "water") }
			}
			pc, err := p.Get(context.Background(), world.ChunkPos{X: rp.Chunk.X, Z: rp.Chunk.Z}, gen.StatusTerrain)
			if err != nil {
				t.Fatal(err)
			}
			a := pos(rp.PostProcessing, isWater(rp.Chunk))
			b := pos(pc.PostProcessing, isWater(pc.Chunk))
			ref += len(a)
			got += len(b)
			for k := range a {
				if b[k] {
					match++
				}
			}
		}
	}
	t.Logf("%d terrain chunks: water PP ref=%d got=%d match=%d", chunks, ref, got, match)
}

// TestEndEntitiesObserve dumps the reference end's entity NBT around 0,0.
// Opt-in: KREMENMC_END_ENT=1.
func TestEndEntitiesObserve(t *testing.T) {
	if os.Getenv("KREMENMC_END_ENT") == "" {
		t.Skip()
	}
	root, _ := reftest.Root()
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", "8266389061079214669-the_end", "world/dimensions/minecraft/the_end/entities")
	for _, rr := range [][2]int32{{0, 0}, {-1, -1}, {0, -1}, {-1, 0}} {
		r, err := anvil.OpenRegionReadOnly(dir, rr[0], rr[1])
		if err != nil {
			continue
		}
		for lz := range 32 {
			for lx := range 32 {
				raw, err := r.Read(lx, lz)
				if err != nil || raw == nil {
					continue
				}
				_, c, _, _ := nbt.Unmarshal(raw, false)
				if l, ok := c["Entities"].(nbt.List); ok && len(l.Items) > 0 {
					for _, e := range l.Items {
						fmt.Printf("%v\n", e)
					}
				}
			}
		}
		r.Close()
	}
}
