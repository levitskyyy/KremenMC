package regsync

import (
	"slices"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

func tagIDs(registry, name string) []int32 {
	for _, g := range Tags() {
		if g.Registry != registry {
			continue
		}
		for _, t := range g.Tags {
			if t.Name == name {
				return t.IDs
			}
		}
	}
	return nil
}

func TestRegistries(t *testing.T) {
	regs := Registries()
	if len(regs) != 32 {
		t.Fatalf("got %d registries, want 32", len(regs))
	}
	if regs[0].Name != "minecraft:worldgen/biome" || regs[31].Name != "minecraft:worldgen/block_state_provider" {
		t.Errorf("order: first %s, last %s", regs[0].Name, regs[31].Name)
	}
	biomes := data.Canonical().Biomes
	if len(regs[0].Entries) != len(biomes) {
		t.Fatalf("got %d biomes, internal/data has %d", len(regs[0].Entries), len(biomes))
	}
	for i, b := range biomes {
		if regs[0].Entries[i] != "minecraft:"+b.Name {
			t.Fatalf("biome %d: got %s, want %s", i, regs[0].Entries[i], b.Name)
		}
	}
	tests := []struct {
		registry, entry string
		ok              bool
	}{
		{"minecraft:dimension_type", "minecraft:overworld", true},
		{"dimension_type", "the_nether", true},
		{"minecraft:worldgen/biome", "minecraft:plains", true},
		{"minecraft:damage_type", "minecraft:generic", true},
		{"minecraft:dimension_type", "minecraft:nope", false},
		{"minecraft:block", "minecraft:stone", false},
	}
	for _, tc := range tests {
		if _, ok := Index(tc.registry, tc.entry); ok != tc.ok {
			t.Errorf("Index(%s, %s) ok=%v, want %v", tc.registry, tc.entry, ok, tc.ok)
		}
	}
	if id, _ := Index("minecraft:worldgen/biome", "minecraft:plains"); id != int32(mustBiome(t, "plains")) {
		t.Errorf("plains index %d disagrees with internal/data", id)
	}
}

func mustBiome(t *testing.T, name string) data.BiomeID {
	id, ok := data.Canonical().BiomeByName(name)
	if !ok {
		t.Fatalf("biome %s missing", name)
	}
	return id
}

func TestTags(t *testing.T) {
	stone, ok := data.Canonical().BlockByName("stone")
	if !ok {
		t.Fatal("no stone")
	}
	tests := []struct {
		registry, tag string
		check         func([]int32) bool
	}{
		{"minecraft:block", "minecraft:mineable/pickaxe", func(ids []int32) bool { return slices.Contains(ids, int32(stone.ID)) }},
		{"minecraft:fluid", "minecraft:water", func(ids []int32) bool { return slices.Equal(ids, []int32{2, 1}) }},
		{"minecraft:worldgen/biome", "minecraft:is_overworld", func(ids []int32) bool {
			return slices.Contains(ids, int32(mustBiome(t, "plains")))
		}},
	}
	for _, tc := range tests {
		ids := tagIDs(tc.registry, tc.tag)
		if ids == nil || !tc.check(ids) {
			t.Errorf("%s #%s = %v", tc.registry, tc.tag, ids)
		}
	}
	for _, g := range Tags() {
		if g.Registry == "minecraft:worldgen/structure" || g.Registry == "minecraft:villager_trade" {
			t.Errorf("tags of non-synchronized registry %s", g.Registry)
		}
	}
}
