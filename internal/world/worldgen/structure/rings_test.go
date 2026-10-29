package structure_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestRingPositions compares the stronghold ring chunks with the
// black-box probe's getRingPositionsFor (testdata/rings).
func TestRingPositions(t *testing.T) {
	b, err := os.ReadFile("testdata/rings/rings_8266389061079214669.txt")
	if err != nil {
		t.Skip()
	}
	want := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])[1:]
	sets, err := structure.LoadSets()
	if err != nil {
		t.Fatal(err)
	}
	var p *structure.Placement
	for _, s := range sets {
		if strings.HasSuffix(s.Key, "strongholds") {
			p = &s.Placement
		}
	}
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	got := structure.RingPositions(p, refSeed, ss.TerrainProbe().Biome)
	// Known misses: biome-source differences in the snap area (see
	// docs/vanilla-parity.md, concentric rings).
	known := map[int]bool{96: true, 124: true}
	ok := 0
	for i, w := range want {
		g := fmt.Sprintf("%d,%d", got[i].X, got[i].Z)
		switch {
		case g == w:
			ok++
		case known[i]:
			t.Logf("ring %d: want %s got %s (known)", i, w, g)
		default:
			t.Errorf("ring %d: want %s got %s", i, w, g)
		}
	}
	t.Logf("%d/%d ring positions match", ok, len(want))
}
