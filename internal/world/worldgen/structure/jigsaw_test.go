package structure

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestJigsawAssembles only checks that the pools assemble without errors;
// the layouts are not compared with the reference.
func TestJigsawAssembles(t *testing.T) {
	a, err := NewAssembler()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pool  string
		depth int
	}{{"minecraft:pillager_outpost/base_plates", 7}, {"minecraft:village/plains/town_centers", 6},
		{"minecraft:village/desert/town_centers", 6}, {"minecraft:village/savanna/town_centers", 6},
		{"minecraft:village/snowy/town_centers", 6}, {"minecraft:village/taiga/town_centers", 6}} {
		r := rng.NewWorldgenRandom(rng.NewLegacy(0))
		r.SetLargeFeatureSeed(8266389061079214669, 10, 10)
		ps, err := a.Assemble(r, c.pool, 160, 70, 160, c.depth, 80)
		if err != nil {
			t.Fatalf("%s: %v", c.pool, err)
		}
		t.Logf("%s: %d pieces", c.pool, len(ps))
		if len(ps) < 2 {
			t.Errorf("%s: only %d pieces", c.pool, len(ps))
		}
	}
}
