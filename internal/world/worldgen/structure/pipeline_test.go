package structure_test

import (
	"context"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestShipwreckPlaced runs the vanilla pipeline to full on a chunk the
// reference shipwreck at 53,-35 covers and checks that wreck blocks appear.
func TestShipwreckPlaced(t *testing.T) {
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	st, err := tr.Stages("overworld", refSeed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: refSeed, Dim: world.Overworld, Stages: st, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	pc, err := p.Get(context.Background(), world.ChunkPos{X: 52, Z: -35}, gen.StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("refs", pc.Refs)
	n := map[string]int{}
	for y := -64; y < 320; y++ {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				if b := data.Canonical().BlockOf(pc.Chunk.Block(x, y, z)); b != nil && (len(b.Name) > 6 && (b.Name[len(b.Name)-6:] == "planks" || b.Name[len(b.Name)-6:] == "stairs")) {
					n[b.Name]++
				}
			}
		}
	}
	t.Log(n)
	if len(n) == 0 {
		t.Error("no shipwreck blocks placed")
	}
	loot := 0
	for _, be := range pc.Chunk.BlockEntities {
		if lt, _ := be.Data["LootTable"].(string); lt == "minecraft:chests/shipwreck_supply" {
			loot++
			t.Logf("chest at %d,%d,%d: %v", be.X, be.Y, be.Z, be.Data)
		}
	}
	if loot != 1 {
		t.Errorf("supply chests with loot = %d, want 1", loot)
	}
}
