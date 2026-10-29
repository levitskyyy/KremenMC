package bench

import (
	"context"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
)

// TestRunAgainstReference exercises the full compare path on whatever
// reference data exists (refbench gen/stages for seed 0, border preset).
func TestRunAgainstReference(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	stages := []string{"full"}
	if _, err := os.Stat(oracle.DumpPath(oracle.Dir(ref, 0, "overworld", true, false), "terrain")); err == nil {
		stages = append(stages, "biomes", "terrain")
	}
	if _, err := refgen.LoadManifest(refgen.WorldDir(ref, 0, "overworld", true)); err != nil {
		t.Skip("no seed 0 reference world; run go run ./tools/refbench gen -seed 0 -chunks border")
	}
	for _, st := range stages {
		r, err := Run(context.Background(), Options{Ref: ref, Seed: 0, Dim: "overworld", Spec: "border", Stage: st, Gen: "terrain", Structures: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Chunks) != 32 || r.Biomes.Total != 32*4*4*4*24 {
			t.Fatalf("%s: %d chunks, %d biome cells", st, len(r.Chunks), r.Biomes.Total)
		}
		t.Logf("%s: blocks %.2f%% biomes %.2f%%", st, r.Blocks.Pct(), r.Biomes.Pct())
	}
}
