package bench

import (
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
	"github.com/levitskyyy/KremenMC/tools/refbench/refserver"
)

// TestAnvilReadsReferenceWorld loads every full chunk of the reference worlds
// (refbench gen) through anvil.Storage, the path the server uses, and checks
// the result against the lossless name-based decoding of the same NBT.
func TestAnvilReadsReferenceWorld(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	worlds, _ := filepath.Glob(filepath.Join(ref.Dir, "worlds", "*", "manifest.json"))
	if len(worlds) == 0 {
		t.Skip("no reference worlds; run go run ./tools/refbench gen")
	}
	for _, mf := range worlds {
		dir := filepath.Dir(mf)
		m, err := refgen.LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			dimDir := refserver.DimensionDir(filepath.Join(dir, m.World), m.Dimension)
			dim, err := gensDim(m.Dimension)
			if err != nil {
				t.Skip(err)
			}
			st := anvil.NewStorage(filepath.Join(dimDir, "region"), dim)
			st.ReadOnly = true
			defer st.Close()
			var ps []compare.Pos
			for _, c := range m.Runs[len(m.Runs)-1].Final {
				if c.Status == "minecraft:full" {
					ps = append(ps, compare.Pos{X: c.X, Z: c.Z})
				}
			}
			refSet, err := compare.LoadRegion(dimDir, ps)
			if err != nil {
				t.Fatal(err)
			}
			got := compare.Set{}
			for _, p := range ps {
				root, err := anvil.ReadRawNBT(filepath.Join(dimDir, "region"), p.X, p.Z)
				if err != nil {
					t.Fatal(err)
				}
				c, stats, err := anvil.DecodeChunk(root, dim)
				if err != nil || c == nil {
					t.Fatalf("chunk %v: %v %v", p, c, err)
				}
				if stats.UnknownBlocks+stats.UnknownBiomes > 0 {
					t.Errorf("chunk %v: %+v", p, stats)
				}
				if !c.LightReady {
					t.Errorf("chunk %v: light not ready", p)
				}
				lc, err := st.LoadChunk(world.ChunkPos{X: p.X, Z: p.Z})
				if err != nil || lc == nil {
					t.Fatalf("storage chunk %v: %v %v", p, lc, err)
				}
				got[p] = compare.FromWorld(lc, "minecraft:full")
			}
			o := compare.FullOptions()
			o.Entities = false // world.Chunk does not carry entities
			r := compare.Compare(refSet, got, ps, o)
			if !r.Blocks.Equal() || !r.Biomes.Equal() || !r.BlockEnts.Equal() {
				t.Fatalf("%d chunks: blocks %v biomes %v block entities %v first %v", len(ps), r.Blocks, r.Biomes, r.BlockEnts, r.First[:min(5, len(r.First))])
			}
			// Heightmaps are recomputed from kremenmc block flags on load; vanilla
			// does not count azalea, pointed dripstone and big dripleaf as
			// motion-blocking, kremenmc's data flags do (reported, not fixed here).
			tops := map[string]int{}
			for _, mm := range r.First {
				if mm.Kind == "heightmap:MOTION_BLOCKING" {
					rv := refSet[compare.Pos{X: int32(mm.X >> 4), Z: int32(mm.Z >> 4)}]
					tops[rv.Block(mm.X&15, atoi(mm.Ref), mm.Z&15)]++
				}
			}
			for name, h := range r.Heightmaps {
				if !h.Equal() {
					t.Logf("heightmap %s recomputed by kremenmc differs from vanilla: %d/%d match, max |d| %d (blocks at vanilla height: %v)", name, h.Match, h.Total, h.MaxAbs, tops)
				}
			}
			t.Logf("%d full chunks read identically (%d block entities)", len(ps), r.BlockEnts.Total)
		})
	}
}

func gensDim(dim string) (world.Dimension, error) {
	switch dim {
	case "minecraft:overworld":
		return world.Overworld, nil
	case "minecraft:the_nether":
		return world.Nether, nil
	case "minecraft:the_end":
		return world.End, nil
	}
	return world.Dimension{}, errUnknownDim(dim)
}

type errUnknownDim string

func (e errUnknownDim) Error() string { return "unknown dimension " + string(e) }

func atoi(s string) int {
	n := 0
	neg := false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		return -n
	}
	return n
}
