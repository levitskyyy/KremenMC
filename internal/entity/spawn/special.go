package spawn

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// CustomSpawner is vanilla CustomSpawner: a per-level spawner ticked after
// natural spawning (ServerLevel.tickCustomSpawners(spawnEnemies)).
type CustomSpawner interface {
	Name() string
	// Tick runs once per level tick. Implementations draw from the level
	// random (or their own, as documented per type) and add mobs via sink.
	Tick(l Level, r rng.Source, spawnEnemies bool, sink Sink)
}

// The special spawners below are stubs: they document the vanilla
// behaviour (minecraft.wiki "Phantom", "Patrol", "Cat", "Wandering
// Trader", "Zombie siege" pages) and do nothing. None is verified against
// the reference; their Tick is a no-op until implemented.

// PhantomSpawner: overworld only, game rule spawn_phantoms. A countdown
// rolls 60-119 s (1200 + nextInt(1200) ticks); when it fires, for each
// non-spectator player with sky access at y >= sea level whose
// time-since-rest statistic exceeds 72000 ticks (with probability scaled
// by that statistic) and local difficulty above a random threshold, spawns
// 1 + nextInt(difficulty+1) phantoms 20-34 blocks above the player.
type PhantomSpawner struct{ NextTick int }

// PatrolSpawner: game rule spawn_patrols, gated by the
// gameplay/can_pillager_patrol_spawn environment attribute (early_game
// timeline enables it after day 5). A countdown of 12000 + nextInt(1200)
// ticks; then 1 in 5, a random player not in a village-ish area, a position
// 24-47 blocks away, not in a mushroom biome; spawns a captain plus
// difficulty-scaled members.
type PatrolSpawner struct{ NextTick int }

// CatSpawner: every 1200 ticks near a random player, 8-? blocks away; in a
// village with beds and few cats spawns a cat, in a swamp hut a black cat.
type CatSpawner struct{ NextTick int }

// WanderingTraderSpawner: game rule spawn_wandering_traders, saved
// counters (spawn delay 24000, chance 25%-75% increasing by 25%); spawns
// near a random player's meeting point with two trader llamas.
type WanderingTraderSpawner struct {
	TickDelay, SpawnDelay, SpawnChance int
}

// VillageSiege: zombie sieges at night in villages (biomes not in
// without_zombie_sieges), 10% chance per night, up to 20 zombies.
type VillageSiege struct{ State int }

func (*PhantomSpawner) Name() string         { return "phantom" }
func (*PatrolSpawner) Name() string          { return "patrol" }
func (*CatSpawner) Name() string             { return "cat" }
func (*WanderingTraderSpawner) Name() string { return "wandering_trader" }
func (*VillageSiege) Name() string           { return "village_siege" }

func (*PhantomSpawner) Tick(Level, rng.Source, bool, Sink)         {}
func (*PatrolSpawner) Tick(Level, rng.Source, bool, Sink)          {}
func (*CatSpawner) Tick(Level, rng.Source, bool, Sink)             {}
func (*WanderingTraderSpawner) Tick(Level, rng.Source, bool, Sink) {}
func (*VillageSiege) Tick(Level, rng.Source, bool, Sink)           {}

// TrialSpawnerConfig is the data-driven trial_spawner registry entry
// (spawn range, total/simultaneous mobs and their per-player additions,
// ticks between spawns, spawn potentials, loot tables). Trial spawners are
// a stub: the state machine (inactive, waiting_for_players, active,
// waiting_for_reward_ejection, ejecting_reward, cooldown), player detection
// and ominous variants are not implemented. Spawn attempts use reason
// TrialSpawner, which ignores light (Reason.IgnoresLight) and counts as a
// spawner (Reason.IsSpawner) in the shared predicates.
type TrialSpawnerConfig struct {
	SpawnRange                     int
	TotalMobs, SimultaneousMobs    float32
	TotalMobsAddedPerPlayer        float32
	SimultaneousMobsAddedPerPlayer float32
	TicksBetweenSpawn              int
}
