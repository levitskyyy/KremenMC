package surface_test

import (
	"fmt"
	"hash/fnv"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/terrain"
)

// Nether and end chunks are dimension height (256) over 128-block noise
// settings. Our noise fill plus the pass on such chunks matched the
// reference surface dumps 100% (refbench compare -gen surface -stage
// surface, seed 8266389061079214669, border); these pins keep it.
func TestDimensionHeightChunks(t *testing.T) {
	const seed = 8266389061079214669
	pins := map[string]uint64{}
	want := map[string]uint64{}
	for k, v := range tallPins {
		want[k] = v
	}
	for _, dim := range []string{"the_nether", "the_end"} {
		st, err := terrain.NewStaged(dim, seed)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := surface.SettingsFor(dim)
		sys, err := surface.Vanilla(key, seed)
		if err != nil {
			t.Fatal(err)
		}
		d := world.Nether
		if dim == "the_end" {
			d = world.End
		}
		if d.Height != 256 {
			t.Fatalf("%s height %d", dim, d.Height)
		}
		for _, p := range [][2]int32{{-1, -1}, {0, 0}} {
			plains, _ := data.Canonical().BiomeByName("plains")
			c := world.NewChunk(p[0], p[1], d, plains)
			if err := st.GenerateStageCtx(&density.Ctx{}, c, "noise"); err != nil {
				t.Fatal(err)
			}
			if err := sys.Build(surface.NewCtx(), c, surface.NewSourceBiomes(st.Biomes, int32(d.MinY), int32(d.Height))); err != nil {
				t.Fatal(err)
			}
			h := fnv.New64a()
			for i := range c.Sections {
				for j := range 4096 {
					fmt.Fprint(h, data.StateString(data.StateID(c.Sections[i].Blocks.Get(j))), ";")
				}
			}
			k := fmt.Sprintf("%s %d %d", dim, p[0], p[1])
			pins[k] = h.Sum64()
			if os.Getenv("SURFACE_PRINT_PINS") == "" && h.Sum64() != want[k] {
				t.Errorf("%s: hash %#x, want %#x", k, h.Sum64(), want[k])
			}
		}
	}
	if os.Getenv("SURFACE_PRINT_PINS") != "" {
		t.Logf("%#v", pins)
	}
}

var tallPins = map[string]uint64{
	"the_end -1 -1": 0x256601cc8ed4f0, "the_end 0 0": 0x64909ed8d894ce44,
	"the_nether -1 -1": 0x2da3d9d098f3435c, "the_nether 0 0": 0xcae3d2bc550bf05a,
}
