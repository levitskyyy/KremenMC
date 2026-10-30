package structure_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
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
	got := structure.RingPositions(p, refSeed, ss.TerrainProbe().(interface {
		SearchBiome() func(x, y, z int32) data.BiomeID
	}).SearchBiome())
	// The snap search keeps one climate cursor (vanilla's per-thread last
	// search result breaks ties); without it rings 96 and 124 differ.
	ok := 0
	for i, w := range want {
		g := fmt.Sprintf("%d,%d", got[i].X, got[i].Z)
		switch {
		case g == w:
			ok++
		default:
			t.Errorf("ring %d: want %s got %s", i, w, g)
		}
	}
	t.Logf("%d/%d ring positions match", ok, len(want))
}
