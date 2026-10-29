package carver

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

var oracleRuns = []struct {
	seed int64
	dim  string
}{
	{0, "overworld"}, {12345, "overworld"}, {8266389061079214669, "overworld"},
	{0, "the_nether"}, {12345, "the_nether"}, {8266389061079214669, "the_nether"},
}

func dimContext(dim string) Context {
	if dim == "the_nether" {
		return dimCtx["nether"]
	}
	return dimCtx["overworld"]
}

func chunkMask(t testing.TB, set *Set, src biome.Source, cx, cz int32) *Mask {
	ctx := &density.Ctx{}
	m := NewMask(set.Ctx.MinY, set.Ctx.Height)
	set.CarveChunk(cx, cz, func(qx, qz int32) string { return src.Biome(ctx, nil, qx, 0, qz) }, m)
	return m
}

func maskDigest(m *Mask) string {
	b := make([]byte, 8*len(m.bits))
	for i, w := range m.bits {
		for k := range 8 {
			b[8*i+k] = byte(w >> (8 * k))
		}
	}
	n := 0
	for x := range 16 {
		for z := range 16 {
			for y := m.minY; y < m.minY+m.height; y++ {
				if m.Get(x, y, z) {
					n++
				}
			}
		}
	}
	return fmt.Sprintf("%d %08x", n, crc32.ChecksumIEEE(b))
}

func isCarvedState(s string) bool {
	return s == "minecraft:air" || s == "minecraft:cave_air" || strings.HasPrefix(s, "minecraft:water") || strings.HasPrefix(s, "minecraft:lava")
}

// TestOracleShape compares carving masks with the reference terrain status:
// every block the reference turned into air or fluid relative to the
// carver-less surface stage must be in the mask (shape), and mask positions
// split into air / water / lava / kept (aquifer barrier or already open).
// With KREMENMC_REF=1 it rewrites the pinned mask digests checked by
// TestPinnedMasks.
func TestOracleShape(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	var pins []string
	for _, run := range oracleRuns {
		dir := filepath.Join(ref.Dir, "stages", fmt.Sprintf("%d-%s-nostructures", run.seed, run.dim))
		surf, err1 := oracle.ReadDump(oracle.DumpPath(dir, "surface"))
		terr, err2 := oracle.ReadDump(oracle.DumpPath(dir, "terrain"))
		if err1 != nil || err2 != nil {
			t.Logf("%d %s: no dumps", run.seed, run.dim)
			continue
		}
		ctx := dimContext(run.dim)
		set, err := NewSet(ctx, run.seed)
		if err != nil {
			t.Fatal(err)
		}
		src, err := biome.ForDimension(run.dim, run.seed)
		if err != nil {
			t.Fatal(err)
		}
		bySurf := map[[2]int32]*anvil.RawChunk{}
		for _, c := range surf {
			bySurf[[2]int32{c.X, c.Z}] = c
		}
		var refCarved, missing, toAir, toWater, toLava, kept, open, otherChanged int
		for _, tc := range terr {
			sc := bySurf[[2]int32{tc.X, tc.Z}]
			if sc == nil {
				continue
			}
			m := chunkMask(t, set, src, tc.X, tc.Z)
			pins = append(pins, fmt.Sprintf("%d %s %d %d %s", run.seed, run.dim, tc.X, tc.Z, maskDigest(m)))
			minY := tc.MinSection * 16
			for y := minY; y < minY+len(tc.Sections)*16; y++ {
				for z := range 16 {
					for x := range 16 {
						a, b := sc.Block(x, y, z), tc.Block(x, y, z)
						in := m.Get(x, y, z)
						carved := a != b && isCarvedState(b) && !isCarvedState(a)
						if carved {
							refCarved++
							if !in {
								missing++
							}
						} else if a != b && !in {
							otherChanged++
						}
						if !in {
							continue
						}
						switch {
						case isCarvedState(a):
							open++
						case !carved:
							kept++
						case strings.HasPrefix(b, "minecraft:water"):
							toWater++
						case strings.HasPrefix(b, "minecraft:lava"):
							toLava++
						default:
							toAir++
						}
					}
				}
			}
		}
		t.Logf("seed %d %s: %d chunks; reference carved %d, outside mask %d (shape %.4f%%); mask: air %d water %d lava %d kept-solid %d already-open %d; other changes outside mask %d",
			run.seed, run.dim, len(terr), refCarved, missing, 100*float64(refCarved-missing)/float64(max(refCarved, 1)), toAir, toWater, toLava, kept, open, otherChanged)
		if missing > 0 {
			t.Errorf("seed %d %s: %d carved positions outside the mask", run.seed, run.dim, missing)
		}
	}
	if os.Getenv("KREMENMC_REF") == "1" && len(pins) > 0 {
		if err := os.WriteFile(filepath.Join("testdata", "masks.txt"), []byte(strings.Join(pins, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
