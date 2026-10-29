package terrain

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

var dims = map[string]struct {
	settings string
	wd       world.Dimension
}{
	"overworld":  {"minecraft:overworld", world.Overworld},
	"the_nether": {"minecraft:nether", world.Nether},
	"the_end":    {"minecraft:end", world.End},
}

func oracleNoise(t *testing.T, seed int64, dim string) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	p := oracle.DumpPath(filepath.Join(ref.Dir, "stages", fmt.Sprintf("%d-%s-nostructures", seed, dim)), "noise")
	if _, err := os.Stat(p); err != nil {
		p = oracle.DumpPath(oracle.Dir(ref, seed, dim, false, false), "noise")
		if _, err := os.Stat(p); err != nil {
			t.Skipf("no noise dump: %v", err)
		}
	}
	raws, err := oracle.ReadDump(p)
	if err != nil {
		t.Fatal(err)
	}
	d := dims[dim]
	g, err := New(d.settings, seed)
	if err != nil {
		t.Fatal(err)
	}
	src, err := biome.ForDimension(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	refSet := compare.FromRawList(raws)
	got := compare.Set{}
	var order []compare.Pos
	for _, rc := range raws {
		pos := compare.Pos{X: rc.X, Z: rc.Z}
		order = append(order, pos)
		c := world.NewChunk(rc.X, rc.Z, d.wd, 0)
		if err := biome.FillWorldChunk(src, c); err != nil {
			t.Fatal(err)
		}
		g.Fill(nil, c)
		got[pos] = compare.FromWorld(c, "noise")
	}
	r := compare.Compare(refSet, got, order, compare.StageOptions("noise"))
	t.Logf("seed %d %s: %d chunks, blocks %.4f%% (%d/%d), biomes %.4f%%", seed, dim, len(raws), r.Blocks.Pct(), r.Blocks.Match, r.Blocks.Total, r.Biomes.Pct())
	// The reference noise variant still places frozen-ocean icebergs
	// (packed ice, snow blocks), which are not part of the noise fill.
	other := 0
	for _, p := range r.Confusion {
		if p.Ref != "minecraft:packed_ice" && p.Ref != "minecraft:snow_block" {
			other += p.Count
		}
	}
	if other > 0 {
		t.Errorf("block mismatches: %d; top: %v", r.Blocks.Total-r.Blocks.Match, r.Confusion[:min(12, len(r.Confusion))])
	} else if !r.Blocks.Equal() {
		var bad [][2]int32
		for _, c := range r.Chunks {
			if !c.Blocks.Equal() {
				bad = append(bad, [2]int32{c.X, c.Z})
			}
		}
		t.Logf("iceberg blocks only: %v in chunks %v", r.Confusion, bad)
	}
}

func TestOracleNoise(t *testing.T) {
	for _, seed := range []int64{0, 12345, 8266389061079214669} {
		for _, dim := range []string{"the_end", "the_nether", "overworld"} {
			t.Run(fmt.Sprintf("%d-%s", seed, dim), func(t *testing.T) { oracleNoise(t, seed, dim) })
		}
	}
}
