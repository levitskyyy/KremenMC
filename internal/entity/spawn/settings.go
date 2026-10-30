package spawn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// IntProvider is the subset of vanilla int providers used by spawn data.
type IntProvider struct {
	Kind     string // "constant", "uniform", "biased_to_bottom"
	Min, Max int32
}

// Sample draws like the reference IntProvider.sample. Constant providers
// consume nothing.
func (p IntProvider) Sample(r rng.Source) int32 {
	switch p.Kind {
	case "uniform":
		return r.NextIntn(p.Max-p.Min+1) + p.Min
	case "biased_to_bottom":
		return p.Min + r.NextIntn(r.NextIntn(p.Max-p.Min+1)+1)
	}
	return p.Min
}

func (p *IntProvider) UnmarshalJSON(b []byte) error {
	var n int32
	if json.Unmarshal(b, &n) == nil {
		*p = IntProvider{"constant", n, n}
		return nil
	}
	var o struct {
		Type  string `json:"type"`
		Value *int32 `json:"value"`
		Min   int32  `json:"min_inclusive"`
		Max   int32  `json:"max_inclusive"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return fmt.Errorf("int provider: %w", err)
	}
	switch strings.TrimPrefix(o.Type, "minecraft:") {
	case "constant":
		if o.Value == nil {
			return fmt.Errorf("int provider: constant without value")
		}
		*p = IntProvider{"constant", *o.Value, *o.Value}
	case "uniform":
		*p = IntProvider{"uniform", o.Min, o.Max}
	case "biased_to_bottom":
		*p = IntProvider{"biased_to_bottom", o.Min, o.Max}
	default:
		return fmt.Errorf("int provider: unsupported type %q", o.Type)
	}
	return nil
}

// SpawnerData is MobSpawnSettings.SpawnerData plus its list weight.
type SpawnerData struct {
	Type   *EntityType
	Count  IntProvider
	Weight int32
}

// WeightedList is vanilla WeightedList<SpawnerData>.
type WeightedList struct {
	Entries []*SpawnerData
	Total   int32
}

// Random is WeightedList.getRandom: one nextInt(total) draw, nothing when empty.
func (w *WeightedList) Random(r rng.Source) *SpawnerData {
	if w == nil || w.Total == 0 {
		return nil
	}
	n := r.NextIntn(w.Total)
	for _, e := range w.Entries {
		n -= e.Weight
		if n < 0 {
			return e
		}
	}
	return nil
}

// Contains reports list membership by identity, like the reference record
// equality on (type, count) within the same list.
func (w *WeightedList) Contains(d *SpawnerData) bool {
	if w == nil {
		return false
	}
	for _, e := range w.Entries {
		if e == d || (e.Type == d.Type && e.Count == d.Count) {
			return true
		}
	}
	return false
}

func (w *WeightedList) IsEmpty() bool { return w == nil || len(w.Entries) == 0 }

// MobSpawnCost is the soul-sand-valley style spawn potential cost.
type MobSpawnCost struct {
	EnergyBudget float64 `json:"energy_budget"`
	Charge       float64 `json:"charge"`
}

// SpawnSettings is MobSpawnSettings (the natural_mob_spawns attribute).
type SpawnSettings struct {
	ByCategory [NumCategories]*WeightedList
	Costs      map[int32]MobSpawnCost // by entity kind
}

func (s *SpawnSettings) Mobs(c Category) *WeightedList {
	if s == nil {
		return nil
	}
	return s.ByCategory[c]
}

func (s *SpawnSettings) Cost(t *EntityType) (MobSpawnCost, bool) {
	if s == nil {
		return MobSpawnCost{}, false
	}
	c, ok := s.Costs[t.Kind]
	return c, ok
}

type spawnEntryJSON struct {
	Type   string      `json:"type"`
	Count  IntProvider `json:"count"`
	Weight int32       `json:"weight"`
}

func parseList(entries []spawnEntryJSON) (*WeightedList, error) {
	w := &WeightedList{}
	for _, e := range entries {
		t, ok := EntityByName(e.Type)
		if !ok {
			return nil, fmt.Errorf("unknown entity %q", e.Type)
		}
		w.Entries = append(w.Entries, &SpawnerData{Type: t, Count: e.Count, Weight: e.Weight})
		w.Total += e.Weight
	}
	return w, nil
}

func parseSettings(raw json.RawMessage) (*SpawnSettings, error) {
	var a struct {
		Argument struct {
			SpawnCosts       map[string]MobSpawnCost     `json:"spawn_costs"`
			SpawnsByCategory map[string][]spawnEntryJSON `json:"spawns_by_category"`
		} `json:"argument"`
		Modifier string `json:"modifier"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	if a.Modifier != "" && a.Modifier != "overlay" {
		return nil, fmt.Errorf("natural_mob_spawns modifier %q", a.Modifier)
	}
	s := &SpawnSettings{Costs: map[int32]MobSpawnCost{}}
	for name, entries := range a.Argument.SpawnsByCategory {
		c, ok := CategoryByName(name)
		if !ok {
			return nil, fmt.Errorf("category %q", name)
		}
		l, err := parseList(entries)
		if err != nil {
			return nil, err
		}
		s.ByCategory[c] = l
	}
	for name, cost := range a.Argument.SpawnCosts {
		t, ok := EntityByName(name)
		if !ok {
			return nil, fmt.Errorf("spawn cost entity %q", name)
		}
		s.Costs[t.Kind] = cost
	}
	return s, nil
}

// DefaultCreatureWorldGenProbability is MobSpawnSettings.DEFAULT_CREATURE_WORLD_GEN_SPAWN_PROBABILITY.
const DefaultCreatureWorldGenProbability float32 = 0.1

// Biome is the spawn-relevant part of a biome definition.
type Biome struct {
	Name                string
	Spawns              *SpawnSettings
	WorldGenProbability float32 // gameplay/creature_world_gen_spawn_probability
}

// BoundingBoxType selects how a structure spawn override is matched.
type BoundingBoxType uint8

const (
	BoxPiece BoundingBoxType = iota // inside any structure piece
	BoxFull                         // inside the structure's overall bounding box
)

// StructureOverride is StructureSpawnOverride for one category.
type StructureOverride struct {
	Box    BoundingBoxType
	Spawns *WeightedList
}

// Structure lists a structure's per-category spawn overrides.
type Structure struct {
	Name      string
	Overrides [NumCategories]*StructureOverride
}

// Dimension is the spawn-relevant part of a dimension type.
type Dimension struct {
	Name                 string
	MinY, Height         int
	HasSkyLight          bool
	HasCeiling           bool
	AmbientLight         float32
	MonsterLight         IntProvider // monster_spawn_light_level
	MonsterBlockLightCap int         // monster_spawn_block_light_limit
}

// Data holds the parsed spawn registries and the tags the engine needs.
type Data struct {
	Biomes     map[string]*Biome
	Structures map[string]*Structure
	Dimensions map[string]*Dimension

	variants      map[string][]variantDef // "cow" -> cow_variant entries
	soundVariants map[string]int          // "cow" -> number of cow_sound_variant entries
	structTags    map[string]map[string]bool
	tagFunc       TagFunc

	blockTags  map[string][]bool // tag -> per-state membership
	biomeTags  map[string]map[string]bool
	entityTags map[string]map[int32]bool
}

// JSONFunc returns the raw JSON of registry entry key (e.g. "worldgen/biome",
// "minecraft:plains").
type JSONFunc func(registry, key string) ([]byte, bool)

// KeysFunc lists the entries of a registry.
type KeysFunc func(registry string) []string

// TagFunc returns the resolved members of a tag, e.g. ("block",
// "minecraft:animals_spawnable_on").
type TagFunc func(registry, key string) ([]string, bool)

// FromPack loads spawn data from the embedded vanilla data pack.
func FromPack(p *wgdata.Pack) (*Data, error) {
	return Load(p.JSON, p.Keys, p.Tag)
}

// Load parses biomes, structures and dimension types and resolves tags.
func Load(get JSONFunc, keys KeysFunc, tag TagFunc) (*Data, error) {
	d := &Data{
		Biomes: map[string]*Biome{}, Structures: map[string]*Structure{}, Dimensions: map[string]*Dimension{},
		blockTags: map[string][]bool{}, biomeTags: map[string]map[string]bool{}, entityTags: map[string]map[int32]bool{},
		variants: map[string][]variantDef{}, soundVariants: map[string]int{}, structTags: map[string]map[string]bool{}, tagFunc: tag,
	}
	for _, mob := range variantMobs {
		for _, k := range sortedKeys(keys(mob + "_variant")) {
			b, ok := get(mob+"_variant", k)
			if !ok {
				return nil, fmt.Errorf("spawn: %s_variant %s missing", mob, k)
			}
			v, err := parseVariant(qualify(k), b)
			if err != nil {
				return nil, fmt.Errorf("spawn: %s_variant %s: %w", mob, k, err)
			}
			d.variants[mob] = append(d.variants[mob], v)
		}
		d.soundVariants[mob] = len(keys(mob + "_sound_variant"))
		for _, v := range d.variants[mob] {
			for _, c := range v.conds {
				for _, b := range c.biomes {
					if t, ok := strings.CutPrefix(b, "#"); ok {
						d.biomeSet(t)
					}
				}
				for _, b := range c.structures {
					if t, ok := strings.CutPrefix(b, "#"); ok && tag != nil {
						set := map[string]bool{}
						members, _ := tag("worldgen/structure", t)
						for _, m := range members {
							set[qualify(m)] = true
						}
						d.structTags[qualify(t)] = set
					}
				}
			}
		}
	}
	for _, k := range sortedKeys(keys("worldgen/biome")) {
		b, ok := get("worldgen/biome", k)
		if !ok {
			return nil, fmt.Errorf("spawn: biome %s missing", k)
		}
		bio, err := parseBiome(qualify(k), b)
		if err != nil {
			return nil, fmt.Errorf("spawn: biome %s: %w", k, err)
		}
		d.Biomes[bio.Name] = bio
	}
	for _, k := range sortedKeys(keys("worldgen/structure")) {
		b, ok := get("worldgen/structure", k)
		if !ok {
			return nil, fmt.Errorf("spawn: structure %s missing", k)
		}
		s, err := parseStructure(qualify(k), b)
		if err != nil {
			return nil, fmt.Errorf("spawn: structure %s: %w", k, err)
		}
		d.Structures[s.Name] = s
	}
	for _, k := range sortedKeys(keys("dimension_type")) {
		b, ok := get("dimension_type", k)
		if !ok {
			return nil, fmt.Errorf("spawn: dimension type %s missing", k)
		}
		dim, err := parseDimension(qualify(k), b)
		if err != nil {
			return nil, fmt.Errorf("spawn: dimension type %s: %w", k, err)
		}
		d.Dimensions[dim.Name] = dim
	}
	for _, t := range blockTagsUsed {
		members, ok := tag("block", t)
		if !ok {
			return nil, fmt.Errorf("spawn: block tag %s missing", t)
		}
		set := make([]bool, len(blockTable))
		for _, m := range members {
			first, n, ok := StateRange(m)
			if !ok {
				continue
			}
			for i := range n {
				set[first+data.StateID(i)] = true
			}
		}
		d.blockTags[t] = set
	}
	for _, t := range biomeTagsUsed {
		members, ok := tag("worldgen/biome", t)
		if !ok {
			return nil, fmt.Errorf("spawn: biome tag %s missing", t)
		}
		set := map[string]bool{}
		for _, m := range members {
			set[qualify(m)] = true
		}
		d.biomeTags[t] = set
	}
	for _, t := range entityTagsUsed {
		members, ok := tag("entity_type", t)
		if !ok {
			return nil, fmt.Errorf("spawn: entity tag %s missing", t)
		}
		set := map[int32]bool{}
		for _, m := range members {
			if e, ok := EntityByName(m); ok {
				set[e.Kind] = true
			}
		}
		d.entityTags[t] = set
	}
	return d, nil
}

func sortedKeys(k []string) []string {
	k = append([]string(nil), k...)
	sort.Strings(k)
	return k
}

func parseBiome(name string, b []byte) (*Biome, error) {
	var j struct {
		Attributes map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	bio := &Biome{Name: name, WorldGenProbability: DefaultCreatureWorldGenProbability}
	if raw, ok := j.Attributes["minecraft:gameplay/natural_mob_spawns"]; ok {
		s, err := parseSettings(raw)
		if err != nil {
			return nil, err
		}
		bio.Spawns = s
	}
	if raw, ok := j.Attributes["minecraft:gameplay/creature_world_gen_spawn_probability"]; ok {
		if err := json.Unmarshal(raw, &bio.WorldGenProbability); err != nil {
			return nil, fmt.Errorf("creature_world_gen_spawn_probability: %w", err)
		}
	}
	return bio, nil
}

func parseStructure(name string, b []byte) (*Structure, error) {
	var j struct {
		SpawnOverrides map[string]struct {
			BoundingBox string           `json:"bounding_box"`
			Spawns      []spawnEntryJSON `json:"spawns"`
		} `json:"spawn_overrides"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	s := &Structure{Name: name}
	for cat, o := range j.SpawnOverrides {
		c, ok := CategoryByName(cat)
		if !ok {
			return nil, fmt.Errorf("category %q", cat)
		}
		l, err := parseList(o.Spawns)
		if err != nil {
			return nil, err
		}
		ov := &StructureOverride{Spawns: l}
		switch o.BoundingBox {
		case "piece":
			ov.Box = BoxPiece
		case "full":
			ov.Box = BoxFull
		default:
			return nil, fmt.Errorf("bounding_box %q", o.BoundingBox)
		}
		s.Overrides[c] = ov
	}
	return s, nil
}

func parseDimension(name string, b []byte) (*Dimension, error) {
	var j struct {
		MinY         int         `json:"min_y"`
		Height       int         `json:"height"`
		HasSkyLight  bool        `json:"has_skylight"`
		HasCeiling   bool        `json:"has_ceiling"`
		AmbientLight float32     `json:"ambient_light"`
		MonsterLight IntProvider `json:"monster_spawn_light_level"`
		BlockLimit   int         `json:"monster_spawn_block_light_limit"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	return &Dimension{Name: name, MinY: j.MinY, Height: j.Height, HasSkyLight: j.HasSkyLight, HasCeiling: j.HasCeiling, AmbientLight: j.AmbientLight, MonsterLight: j.MonsterLight, MonsterBlockLightCap: j.BlockLimit}, nil
}

// BlockTag reports whether a state's block is in a (preloaded) block tag.
func (d *Data) BlockTag(tag string, s data.StateID) bool {
	set := d.blockTags[tag]
	return int(s) < len(set) && set[s]
}

// biomeSet resolves a biome tag on first use (Load time only; not safe
// for concurrent first use).
func (d *Data) biomeSet(tag string) map[string]bool {
	tag = qualify(tag)
	if s, ok := d.biomeTags[tag]; ok {
		return s
	}
	set := map[string]bool{}
	if d.tagFunc != nil {
		members, _ := d.tagFunc("worldgen/biome", tag)
		for _, m := range members {
			set[qualify(m)] = true
		}
	}
	d.biomeTags[tag] = set
	return set
}

// BiomeTag reports whether a biome is in a (preloaded) biome tag.
func (d *Data) BiomeTag(tag, biome string) bool { return d.biomeTags[tag][biome] }

// EntityTag reports whether an entity type is in a (preloaded) entity tag.
func (d *Data) EntityTag(tag string, t *EntityType) bool { return d.entityTags[tag][t.Kind] }

// Settings returns the natural_mob_spawns value of a biome (nil when none).
func (d *Data) Settings(biome string) *SpawnSettings {
	if b := d.Biomes[biome]; b != nil {
		return b.Spawns
	}
	return nil
}
