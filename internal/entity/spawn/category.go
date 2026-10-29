// Package spawn is a deterministic mob spawning engine matching the 26.3
// reference: natural spawning, chunk-generation spawning, despawning, mob
// spawner block entities, finalizeSpawn hooks and special spawner stubs.
//
// The engine owns no world state. It reads the world through the Level
// interface, draws from caller-supplied rng.Source values in the reference
// order, and reports spawns through callbacks so the server can add the
// entities (and make them visible to later checks in the same pass).
package spawn

// Category is vanilla MobCategory, in reference ordinal order.
type Category uint8

const (
	Monster Category = iota
	Creature
	Ambient
	Axolotls
	UndergroundWaterCreature
	WaterCreature
	WaterAmbient
	Misc
	NumCategories
)

// CategoryInfo mirrors the reference MobCategory constants.
type CategoryInfo struct {
	Name       string
	Max        int // getMaxInstancesPerChunk
	Friendly   bool
	Persistent bool
	Despawn    int // getDespawnDistance
	NoDespawn  int // getNoDespawnDistance
}

var categories = [NumCategories]CategoryInfo{
	{"monster", 70, false, false, 128, 32},
	{"creature", 10, true, true, 128, 32},
	{"ambient", 15, true, false, 128, 32},
	{"axolotls", 5, true, false, 128, 32},
	{"underground_water_creature", 5, true, false, 128, 32},
	{"water_creature", 5, true, false, 128, 32},
	{"water_ambient", 20, true, false, 64, 32},
	{"misc", -1, true, true, 128, 32},
}

// SpawningCategories is NaturalSpawner.SPAWNING_CATEGORIES (every category
// except misc), in iteration order.
var SpawningCategories = [...]Category{Monster, Creature, Ambient, Axolotls, UndergroundWaterCreature, WaterCreature, WaterAmbient}

// NaturalSpawner constants.
const (
	MinSpawnDistance                  = 24
	SpawnDistanceChunk                = 8
	SpawnDistanceBlock                = 128
	InscribedSquareSpawnDistanceChunk = 5
	MagicNumber                       = 289 // (2*8+1)^2 chunks per player for global caps
)

func (c Category) Info() CategoryInfo { return categories[c] }
func (c Category) String() string     { return categories[c].Name }

// CategoryByName parses a serialized category name.
func CategoryByName(n string) (Category, bool) {
	for i, c := range categories {
		if c.Name == n {
			return Category(i), true
		}
	}
	return 0, false
}
