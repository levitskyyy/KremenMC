package anvil

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// TestGenEntitiesAndSavedTicks round-trips generated entities not yet in
// the live world and the live scheduler snapshot through a full chunk.
func TestGenEntitiesAndSavedTicks(t *testing.T) {
	c := world.NewChunk(2, -1, world.Overworld, 0)
	c.Finish()
	c.GenEntities = []nbt.Compound{{"id": "minecraft:end_crystal", "ShowBottom": int8(1)}}
	c.PendingTicks = []world.PendingTick{{X: 33, Y: 64, Z: -9, Type: "minecraft:sand", Delay: 2}}
	c.SavedTicks = []world.PendingTick{{X: 34, Y: 64, Z: -9, Type: "minecraft:water", Delay: 5, Fluid: true}}
	buf, err := nbt.Marshal(nil, "", EncodeChunk(c, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	_, root, _, err := nbt.Unmarshal(buf, false)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := DecodeChunk(root, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.GenEntities) != 1 || d.GenEntities[0]["id"] != "minecraft:end_crystal" {
		t.Fatalf("gen entities %v", d.GenEntities)
	}
	if _, ok := d.Extra[genEntitiesKey]; ok {
		t.Fatal("gen entities left in Extra")
	}
	if len(d.PendingTicks) != 2 || len(d.SavedTicks) != 0 {
		t.Fatalf("ticks: pending %v saved %v", d.PendingTicks, d.SavedTicks)
	}
}
