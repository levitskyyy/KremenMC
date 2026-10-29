package wgdata

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/net/regsync"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		path, reg, key string
		tag, ok        bool
	}{
		{"data/minecraft/worldgen/biome/plains.json", "worldgen/biome", "minecraft:plains", false, true},
		{"data/minecraft/worldgen/template_pool/village/plains/houses.json", "worldgen/template_pool", "minecraft:village/plains/houses", false, true},
		{"data/minecraft/loot_table/chests/simple_dungeon.json", "loot_table", "minecraft:chests/simple_dungeon", false, true},
		{"data/minecraft/structure/village/plains/houses/plains_small_house_1.nbt", "structure", "minecraft:village/plains/houses/plains_small_house_1", false, true},
		{"data/minecraft/tags/block/logs.json", "block", "minecraft:logs", true, true},
		{"data/minecraft/tags/worldgen/biome/is_overworld.json", "worldgen/biome", "minecraft:is_overworld", true, true},
		{"data/foo/dimension_type/bar.json", "dimension_type", "foo:bar", false, true},
		{"data/minecraft/datapacks/trade_rebalance/data/minecraft/loot_table/a.json", "", "", false, false},
		{"data/minecraft/worldgen/biome.json", "", "", false, false},
		{"reports/registries.json", "", "", false, false},
	}
	for _, tt := range tests {
		reg, key, tag, ok := classify(tt.path)
		if reg != tt.reg || key != tt.key || tag != tt.tag || ok != tt.ok {
			t.Errorf("classify(%q) = %q,%q,%v,%v want %q,%q,%v,%v", tt.path, reg, key, tag, ok, tt.reg, tt.key, tt.tag, tt.ok)
		}
	}
}

func synthetic(t *testing.T) *Pack {
	t.Helper()
	files := map[string][]byte{
		"data/minecraft/worldgen/biome/a.json":                {},
		"data/minecraft/worldgen/biome/b.json":                {},
		"data/minecraft/worldgen/biome/c.json":                {},
		"data/minecraft/tags/worldgen/biome/inner.json":       []byte(`{"values":["b","minecraft:a"]}`),
		"data/minecraft/tags/worldgen/biome/outer.json":       []byte(`{"values":["c","#inner","a",{"id":"zzz","required":false},{"id":"#nope","required":false},{"id":"b","required":true}]}`),
		"data/minecraft/tags/worldgen/biome/bad_ref.json":     []byte(`{"values":["#nope"]}`),
		"data/minecraft/tags/worldgen/biome/cycle_a.json":     []byte(`{"values":["#cycle_b"]}`),
		"data/minecraft/tags/worldgen/biome/cycle_b.json":     []byte(`{"values":["#cycle_a"]}`),
		"data/minecraft/tags/worldgen/biome/req_missing.json": []byte(`{"values":["zzz"]}`),
		"data/minecraft/tags/block/opt.json":                  []byte(`{"values":[{"id":"stone","required":false},{"id":"not_a_block","required":false},"also_unknown"]}`),
		"data/minecraft/tags/entity_type/opt.json":            []byte(`{"values":[{"id":"mod:thing","required":false}]}`),
		"reports/registries.json":                             []byte(`{"minecraft:block":{"entries":{"minecraft:air":{"protocol_id":0},"minecraft:stone":{"protocol_id":1}}}}`),
	}
	b, err := Encode(files)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTagResolution(t *testing.T) {
	p := synthetic(t)
	tests := []struct {
		reg, key string
		want     []string
		ok       bool
	}{
		{"worldgen/biome", "#inner", []string{"minecraft:b", "minecraft:a"}, true},
		{"worldgen/biome", "minecraft:outer", []string{"minecraft:c", "minecraft:b", "minecraft:a"}, true},
		{"minecraft:worldgen/biome", "outer", []string{"minecraft:c", "minecraft:b", "minecraft:a"}, true},
		// Required plain entries are kept even if unknown; vanilla would fail
		// to load such a tag, which cannot happen with vanilla data.
		{"worldgen/biome", "req_missing", []string{"minecraft:zzz"}, true},
		{"worldgen/biome", "bad_ref", nil, false},
		{"worldgen/biome", "cycle_a", nil, false},
		{"worldgen/biome", "missing", nil, false},
		{"block", "#opt", []string{"minecraft:stone", "minecraft:also_unknown"}, true},
		{"entity_type", "opt", []string{"mod:thing"}, true},
	}
	for _, tt := range tests {
		got, ok := p.Tag(tt.reg, tt.key)
		if ok != tt.ok || !slices.Equal(got, tt.want) {
			t.Errorf("Tag(%q,%q) = %v,%v want %v,%v", tt.reg, tt.key, got, ok, tt.want, tt.ok)
		}
	}
	got, _ := p.Tag("worldgen/biome", "inner")
	got[0] = "mutated"
	if again, _ := p.Tag("worldgen/biome", "inner"); again[0] != "minecraft:b" {
		t.Error("Tag result aliases the cache")
	}
}

func embedded(t *testing.T) *Pack {
	t.Helper()
	p, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p
}

func TestEmbeddedCounts(t *testing.T) {
	p := embedded(t)
	tests := []struct {
		reg  string
		want int
	}{
		{"worldgen/biome", 67},
		{"worldgen/noise_settings", 7},
		{"worldgen/noise", 64},
		{"worldgen/density_function", 55},
		{"worldgen/feature", 240},
		{"worldgen/configured_feature", 240},
		{"worldgen/placed_feature", 273},
		{"worldgen/carver", 4},
		{"worldgen/structure", 52},
		{"worldgen/structure_set", 21},
		{"worldgen/template_pool", 245},
		{"worldgen/processor_list", 40},
		{"worldgen/world_preset", 7},
		{"worldgen/multi_noise_biome_source_parameter_list", 2},
		{"structure", 1511},
		{"loot_table", 1447},
		{"dimension_type", 4},
		{"recipe", 2042},
		{"enchantment", 43},
	}
	for _, tt := range tests {
		if got := len(p.Keys(tt.reg)); got != tt.want {
			t.Errorf("len(Keys(%q)) = %d want %d", tt.reg, got, tt.want)
		}
		if !slices.IsSorted(p.Keys(tt.reg)) {
			t.Errorf("Keys(%q) not sorted", tt.reg)
		}
	}
}

func TestEmbeddedLookups(t *testing.T) {
	p := embedded(t)
	jsonTests := []struct{ reg, key, field string }{
		{"worldgen/noise_settings", "minecraft:overworld", "noise_router"},
		{"worldgen/noise_settings", "overworld", "material_rule"},
		{"worldgen/biome", "plains", "attributes"},
		{"worldgen/configured_feature", "oak", "type"},
		{"worldgen/placed_feature", "minecraft:trees_plains", "placement"},
		{"worldgen/structure_set", "villages", "placement"},
		{"worldgen/template_pool", "village/plains/houses", "elements"},
		{"loot_table", "chests/simple_dungeon", "pools"},
		{"dimension_type", "the_nether", "has_ceiling"},
		{"recipe", "crafting_table", "type"},
	}
	for _, tt := range jsonTests {
		b, ok := p.JSON(tt.reg, tt.key)
		if !ok {
			t.Errorf("JSON(%q,%q) missing", tt.reg, tt.key)
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Errorf("JSON(%q,%q): %v", tt.reg, tt.key, err)
		} else if _, ok := m[tt.field]; !ok {
			t.Errorf("JSON(%q,%q) lacks %q", tt.reg, tt.key, tt.field)
		}
	}
	if _, ok := p.JSON("worldgen/biome", "nope"); ok {
		t.Error("JSON(nope) ok")
	}

	nbt, ok := p.Template("minecraft:village/plains/houses/plains_small_house_1")
	if !ok || len(nbt) < 3 || nbt[0] != 10 {
		t.Errorf("Template: %d bytes %v, want an uncompressed compound", len(nbt), ok)
	}

	for _, r := range []string{"registries.json", "blocks.json", "datapack.json", "biome_parameters/minecraft/overworld.json", "biome_parameters/minecraft/nether.json", "minecraft/components/item/stone.json"} {
		b, ok := p.Report(r)
		if !ok || !json.Valid(b) {
			t.Errorf("Report(%q) = %d bytes,%v", r, len(b), ok)
		}
	}
	types, ok := p.Builtin("worldgen/feature_type")
	if !ok || len(types) < 50 || !slices.Contains(types, "minecraft:tree") {
		t.Errorf("Builtin(feature_type) = %d entries,%v", len(types), ok)
	}
	if bs, _ := p.Builtin("minecraft:block"); len(bs) == 0 || bs[0] != "minecraft:air" {
		t.Error("Builtin(block)[0] != air")
	}
}

func TestEmbeddedTags(t *testing.T) {
	p := embedded(t)
	tests := []struct {
		reg, key string
		has      []string
	}{
		{"worldgen/biome", "#minecraft:is_overworld", []string{"minecraft:plains", "minecraft:deep_dark", "minecraft:ocean"}},
		{"worldgen/biome", "has_structure/village_plains", []string{"minecraft:plains", "minecraft:meadow"}},
		{"block", "#logs", []string{"minecraft:oak_log", "minecraft:crimson_stem", "minecraft:stripped_birch_wood"}},
		{"block", "minecraft:base_stone_overworld", []string{"minecraft:stone", "minecraft:deepslate"}},
		{"item", "logs_that_burn", []string{"minecraft:oak_log"}},
		{"entity_type", "skeletons", []string{"minecraft:skeleton", "minecraft:stray"}},
		{"worldgen/structure", "village", []string{"minecraft:village_plains"}},
	}
	for _, tt := range tests {
		got, ok := p.Tag(tt.reg, tt.key)
		if !ok {
			t.Errorf("Tag(%q,%q) missing", tt.reg, tt.key)
			continue
		}
		for _, h := range tt.has {
			if !slices.Contains(got, h) {
				t.Errorf("Tag(%q,%q) lacks %s", tt.reg, tt.key, h)
			}
		}
		for _, e := range got {
			if e[0] == '#' {
				t.Errorf("Tag(%q,%q) has unresolved %s", tt.reg, tt.key, e)
			}
		}
	}
	for _, reg := range []string{"block", "item", "entity_type", "worldgen/biome", "worldgen/structure", "fluid", "game_event"} {
		for _, k := range p.TagKeys(reg) {
			if _, ok := p.Tag(reg, k); !ok {
				t.Errorf("Tag(%q,%q) does not resolve", reg, k)
			}
		}
	}
}

// Chunk biome palettes and registry sync must agree on biome IDs.
func TestBiomeOrderMatchesRegsync(t *testing.T) {
	p := embedded(t)
	for _, r := range regsync.Registries() {
		if r.Name != "minecraft:worldgen/biome" {
			continue
		}
		if !slices.Equal(r.Entries, p.Keys("worldgen/biome")) {
			t.Errorf("biome order differs:\nregsync %v\nwgdata  %v", r.Entries, p.Keys("worldgen/biome"))
		}
		return
	}
	t.Fatal("regsync has no biome registry")
}

func TestConcurrentAccess(t *testing.T) {
	p := embedded(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			keys := p.Keys("structure")
			for j := i; j < len(keys); j += 37 {
				if _, ok := p.Template(keys[j]); !ok {
					t.Errorf("Template(%s) missing", keys[j])
				}
			}
			for _, k := range p.TagKeys("worldgen/biome") {
				p.Tag("worldgen/biome", k)
			}
			p.Builtin("worldgen/feature_type")
		})
	}
	wg.Wait()
}
