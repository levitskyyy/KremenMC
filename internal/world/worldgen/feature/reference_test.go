package feature_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
)

// Reference vegetation is extracted from the bench's reference worlds
// (`refbench gen -chunks border`, sequential order) into testdata with
// KREMENMC_REF=1; plain `go test` compares against the extracted golden and the
// pinned per-block statistics, without Java.

var refSeeds = []int64{8266389061079214669, 0}

// tracked reports blocks produced by vegetation features.
func tracked(name string) bool {
	switch name {
	case "short_grass", "fern", "tall_grass", "large_fern", "bush", "firefly_bush", "dead_bush", "sugar_cane",
		"pumpkin", "cactus", "cactus_flower", "sweet_berry_bush", "lily_pad", "brown_mushroom", "red_mushroom",
		"leaf_litter", "wildflowers", "pink_petals", "bee_nest", "vine", "short_dry_grass", "tall_dry_grass",
		"melon", "lilac", "rose_bush", "peony", "sunflower", "dandelion", "poppy", "blue_orchid", "allium",
		"azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy", "cornflower",
		"lily_of_the_valley", "open_eyeblossom", "closed_eyeblossom", "seagrass", "tall_seagrass", "kelp", "kelp_plant":
		return true
	}
	return strings.HasSuffix(name, "_log") || strings.HasSuffix(name, "_leaves")
}

type entry struct {
	x, y, z int
	name    string
}

func shortName(s data.StateID) string {
	b := data.Canonical().BlockOf(s)
	if b == nil {
		return ""
	}
	return strings.TrimPrefix(b.Name, "minecraft:")
}

func extract(c *world.Chunk) []entry {
	var out []entry
	for y := c.Dim.MinY; y < c.Dim.MinY+c.Dim.Height; y++ {
		for z := range 16 {
			for x := range 16 {
				if n := shortName(c.Block(x, y, z)); tracked(n) {
					out = append(out, entry{int(c.X)<<4 | x, y, int(c.Z)<<4 | z, n})
				}
			}
		}
	}
	return out
}

func goldenPath(seed int64) string {
	return filepath.Join("testdata", fmt.Sprintf("ref-%d.txt.gz", seed))
}

func borderChunks(t *testing.T) []world.ChunkPos {
	ps, err := chunkspec.Parse("border", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]world.ChunkPos, len(ps))
	for i, p := range ps {
		out[i] = world.ChunkPos{X: p.X, Z: p.Z}
	}
	return out
}

func writeGolden(t *testing.T, seed int64, chunks []world.ChunkPos) {
	root, err := reftest.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".refcache", reftest.Version, "worlds", fmt.Sprintf("%d-overworld", seed), "world/dimensions/minecraft/overworld/region")
	st := anvil.NewStorage(dir, world.Overworld)
	defer st.Close()
	f, err := os.Create(goldenPath(seed))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	w := bufio.NewWriter(zw)
	for _, p := range chunks {
		c, err := st.LoadChunk(p)
		if err != nil || c == nil {
			t.Fatalf("reference chunk %v: %v", p, err)
		}
		fmt.Fprintf(w, "c %d %d\n", p.X, p.Z)
		for _, e := range extract(c) {
			fmt.Fprintf(w, "%d %d %d %s\n", e.x&15, e.y, e.z&15, e.name)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func readGolden(t *testing.T, seed int64) map[world.ChunkPos][]entry {
	f, err := os.Open(goldenPath(seed))
	if err != nil {
		t.Skipf("no golden (run with KREMENMC_REF=1 after refbench gen): %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	out := map[world.ChunkPos][]entry{}
	var cur world.ChunkPos
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		var a, b, c int
		var n string
		if strings.HasPrefix(sc.Text(), "c ") {
			fmt.Sscanf(sc.Text(), "c %d %d", &a, &b)
			cur = world.ChunkPos{X: int32(a), Z: int32(b)}
			out[cur] = nil
			continue
		}
		fmt.Sscanf(sc.Text(), "%d %d %d %s", &a, &b, &c, &n)
		out[cur] = append(out[cur], entry{int(cur.X)<<4 | a, b, int(cur.Z)<<4 | c, n})
	}
	return out
}

type stat struct{ ref, got, match, edgeMiss int }

func TestReferenceVegetation(t *testing.T) {
	if testing.Short() {
		t.Skip("generates terrain")
	}
	chunks := borderChunks(t)
	seeds := refSeeds
	if raceEnabled {
		// The race detector makes terrain ~25× slower; one seed still covers
		// the concurrent pipeline.
		seeds = seeds[:1]
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			if os.Getenv("KREMENMC_REF") == "1" {
				writeGolden(t, seed, chunks)
			}
			ref := readGolden(t, seed)
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
			stats := map[string]*stat{}
			get := func(n string) *stat {
				if stats[n] == nil {
					stats[n] = &stat{}
				}
				return stats[n]
			}
			for _, cp := range chunks {
				pc, err := p.Get(context.Background(), cp, gen.StatusFull)
				if err != nil {
					t.Fatal(err)
				}
				got := map[[3]int]string{}
				for _, e := range extract(pc.Chunk) {
					got[[3]int{e.x, e.y, e.z}] = e.name
					get(e.name).got++
				}
				for _, e := range ref[cp] {
					s := get(e.name)
					s.ref++
					if got[[3]int{e.x, e.y, e.z}] == e.name {
						s.match++
					} else if lx, lz := e.x&15, e.z&15; min(lx, lz, 15-lx, 15-lz) < 2 {
						s.edgeMiss++
					}
				}
			}
			names := slices.Sorted(func(yield func(string) bool) {
				for n := range stats {
					if !yield(n) {
						return
					}
				}
			})
			var b strings.Builder
			for _, n := range names {
				s := stats[n]
				fmt.Fprintf(&b, "%s ref=%d got=%d match=%d edge_miss=%d\n", n, s.ref, s.got, s.match, s.edgeMiss)
			}
			t.Logf("seed %d, %d chunks:\n%s", seed, len(chunks), b.String())
			pin := filepath.Join("testdata", fmt.Sprintf("pinned-%d.txt", seed))
			if os.Getenv("KREMENMC_UPDATE") == "1" {
				if err := os.WriteFile(pin, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(pin)
			if err != nil {
				t.Skipf("no pinned stats (KREMENMC_UPDATE=1): %v", err)
			}
			if string(want) != b.String() {
				t.Errorf("vegetation stats changed; rerun with KREMENMC_UPDATE=1 if intended.\nwant:\n%s", want)
			}
			t.Logf("explicit no-ops: %v", feature.Unsupported())
		})
	}
}
