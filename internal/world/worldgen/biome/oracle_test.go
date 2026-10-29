package biome

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

// GenerateChunk fills a world chunk's biomes like the BIOMES status.
func generateChunk(t testing.TB, src Source, x, z int32, dim world.Dimension) *world.Chunk {
	c := world.NewChunk(x, z, dim, 0)
	reg := data.Canonical()
	FillChunk(src, x, z, int32(dim.MinSection()), int32(dim.Sections()), func(qx, qy, qz int32, b string) {
		id, ok := reg.BiomeByName(b)
		if !ok {
			t.Fatalf("unknown biome %s", b)
		}
		c.SetBiome(int(qx)*4, dim.MinY+int(qy)*4, int(qz)*4, id)
	})
	return c
}

func oracleCompare(t *testing.T, seed int64, dim string, wd world.Dimension) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	dir := filepath.Join(ref.Dir, "stages", fmt.Sprintf("%d-%s", seed, dim))
	p := oracle.DumpPath(dir, "biomes")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no biome stage dump at %s", p)
	}
	raws, err := oracle.ReadDump(p)
	if err != nil {
		t.Fatal(err)
	}
	src, err := ForDimension(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	refSet := compare.FromRawList(raws)
	got := compare.Set{}
	var order []compare.Pos
	for _, rc := range raws {
		pos := compare.Pos{X: rc.X, Z: rc.Z}
		order = append(order, pos)
		got[pos] = compare.FromWorld(generateChunk(t, src, rc.X, rc.Z, wd), "biomes")
	}
	r := compare.Compare(refSet, got, order, compare.StageOptions("biomes"))
	// Evidence for the quart order: quarts where another nesting or no
	// cursor memory would give a different biome than ours.
	sens := map[string]int{}
	for _, p := range order {
		minSec, secs := int32(wd.MinSection()), int32(wd.Sections())
		var c0 climate.Cursor
		base := fillOrder(src, p.X, p.Z, minSec, secs, [3]int{0, 1, 2}, &c0, false)
		for _, v := range []struct {
			name  string
			order [3]int
			nilc  bool
		}{{"zyx", [3]int{2, 1, 0}, false}, {"yzx", [3]int{1, 2, 0}, false}, {"nocursor", [3]int{0, 1, 2}, true}} {
			var c climate.Cursor
			cp := &c
			if v.nilc {
				cp = nil
			}
			alt := fillOrder(src, p.X, p.Z, minSec, secs, v.order, cp, false)
			for j := range alt {
				if alt[j] != base[j] {
					sens[v.name]++
				}
			}
		}
	}
	t.Logf("order-sensitive quarts in these chunks (our result matches vanilla there): %v", sens)
	t.Logf("seed %d %s: %d chunks, biomes %.4f%% (%d/%d)", seed, dim, len(raws), r.Biomes.Pct(), r.Biomes.Match, r.Biomes.Total)
	if !r.Biomes.Equal() {
		t.Errorf("biome mismatches: %d", r.Biomes.Total-r.Biomes.Match)
		return
	}
	if os.Getenv("KREMENMC_PIN") == "1" {
		f, err := os.OpenFile(filepath.Join("testdata", "pinned.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, p := range order {
			fmt.Fprintf(f, "%d %s %d %d %016x\n", seed, dim, p.X, p.Z, fillHash(src, p.X, p.Z, wd))
		}
	}
}

func TestOracleBiomes(t *testing.T) {
	for _, c := range []struct {
		seed int64
		dim  string
		wd   world.Dimension
	}{
		{0, "overworld", world.Overworld}, {12345, "overworld", world.Overworld}, {8266389061079214669, "overworld", world.Overworld},
		{0, "the_nether", world.Nether}, {12345, "the_nether", world.Nether}, {8266389061079214669, "the_nether", world.Nether},
		{0, "the_end", world.End}, {12345, "the_end", world.End}, {8266389061079214669, "the_end", world.End},
	} {
		t.Run(fmt.Sprintf("%d-%s", c.seed, c.dim), func(t *testing.T) { oracleCompare(t, c.seed, c.dim, c.wd) })
	}
}
