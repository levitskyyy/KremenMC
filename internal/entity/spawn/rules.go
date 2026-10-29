package spawn

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Reason is EntitySpawnReason.
type Reason uint8

const (
	ReasonNatural Reason = iota
	ReasonChunkGeneration
	ReasonSpawner
	ReasonStructure
	ReasonBreeding
	ReasonMobSummoned
	ReasonJockey
	ReasonEvent
	ReasonConversion
	ReasonReinforcement
	ReasonTriggered
	ReasonBucket
	ReasonSpawnItemUse
	ReasonCommand
	ReasonDispenser
	ReasonPatrol
	ReasonTrialSpawner
	ReasonLoad
	ReasonDimensionTravel
)

// IsSpawner is EntitySpawnReason.isSpawner.
func (r Reason) IsSpawner() bool { return r == ReasonSpawner || r == ReasonTrialSpawner }

// IgnoresLight is EntitySpawnReason.ignoresLightRequirements.
func (r Reason) IgnoresLight() bool { return r == ReasonTrialSpawner }

var blockTagsUsed = []string{
	"minecraft:prevent_mob_spawning_inside", "minecraft:animals_spawnable_on", "minecraft:axolotls_spawnable_on",
	"minecraft:goats_spawnable_on", "minecraft:mooshrooms_spawnable_on", "minecraft:parrots_spawnable_on",
	"minecraft:polar_bears_spawnable_on_alternate", "minecraft:rabbits_spawnable_on", "minecraft:foxes_spawnable_on",
	"minecraft:wolves_spawnable_on", "minecraft:frogs_spawnable_on", "minecraft:armadillo_spawnable_on",
	"minecraft:bats_spawnable_on", "minecraft:camels_spawnable_on", "minecraft:sand", "minecraft:leaves",
	"minecraft:badlands_terracotta",
}

var biomeTagsUsed = []string{
	"minecraft:allows_surface_slime_spawns", "minecraft:allows_tropical_fish_spawns_at_any_height",
	"minecraft:more_frequent_drowned_spawns", "minecraft:polar_bears_spawn_on_alternate_blocks",
	"minecraft:reduce_water_ambient_spawns",
}

var entityTagsUsed = []string{}

func (e *Engine) is(s data.StateID, block string) bool { return BlockName(s) == block }

func (e *Engine) tag(s data.StateID, tag string) bool { return e.Data.BlockTag("minecraft:"+tag, s) }

func (e *Engine) biomeTag(l Level, x, y, z int, tag string) bool {
	return e.Data.BiomeTag("minecraft:"+tag, l.Biome(x, y, z))
}

func isWater(l Level, x, y, z int) bool { return props(l.BlockState(x, y, z)).isWater() }
func isLava(l Level, x, y, z int) bool  { return props(l.BlockState(x, y, z)).isLava() }

// isValidEmptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock.
func (e *Engine) isValidEmptySpawnBlock(s data.StateID, t *EntityType) bool {
	p := props(s)
	if p&bpFullCollision != 0 || p&bpSignal != 0 || p.fluid() != FluidNone {
		return false
	}
	if e.tag(s, "prevent_mob_spawning_inside") {
		return false
	}
	return !isDangerous(t, s)
}

// PlacementOK is SpawnPlacements.isSpawnPositionOk.
func (e *Engine) PlacementOK(l Level, t *EntityType, x, y, z int) bool {
	switch t.Placement {
	case PlacementInWater:
		return isWater(l, x, y, z) && props(l.BlockState(x, y+1, z))&bpConductor == 0
	case PlacementInLava:
		return isLava(l, x, y, z)
	case PlacementOnGround:
		below := l.BlockState(x, y-1, z)
		if !validSpawnOn(below, t) {
			return false
		}
		return e.isValidEmptySpawnBlock(l.BlockState(x, y, z), t) && e.isValidEmptySpawnBlock(l.BlockState(x, y+1, z), t)
	}
	return true
}

// checkMobSpawnRules is Mob.checkMobSpawnRules.
func (e *Engine) checkMobSpawnRules(l Level, t *EntityType, reason Reason, x, y, z int) bool {
	return reason.IsSpawner() || validSpawnOn(l.BlockState(x, y-1, z), t)
}

// IsDarkEnoughToSpawn is Monster.isDarkEnoughToSpawn.
func (e *Engine) IsDarkEnoughToSpawn(l Level, x, y, z int, r rng.Source) bool {
	if l.SkyLight(x, y, z) > int(r.NextIntn(32)) {
		return false
	}
	dim := l.Dimension()
	if dim.MonsterBlockLightCap < 15 && l.BlockLight(x, y, z) > dim.MonsterBlockLightCap {
		return false
	}
	var light int
	if l.Thundering() {
		light = maxLocalRawBrightness(l, x, y, z, 10)
	} else {
		light = MaxLocalRawBrightness(l, x, y, z)
	}
	return light <= int(dim.MonsterLight.Sample(r))
}

func (e *Engine) checkMonsterSpawnRules(l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return l.Difficulty() != Peaceful && (reason.IgnoresLight() || e.IsDarkEnoughToSpawn(l, x, y, z, r)) && e.checkMobSpawnRules(l, t, reason, x, y, z)
}

func (e *Engine) checkAnyLightMonsterSpawnRules(l Level, t *EntityType, reason Reason, x, y, z int) bool {
	return l.Difficulty() != Peaceful && e.checkMobSpawnRules(l, t, reason, x, y, z)
}

// isBrightEnoughToSpawn is Animal.isBrightEnoughToSpawn.
func isBrightEnoughToSpawn(l Level, x, y, z int) bool { return rawBrightness(l, x, y, z, 0) > 8 }

func (e *Engine) checkAnimalSpawnRules(l Level, reason Reason, x, y, z int, floorTag string) bool {
	bright := reason.IgnoresLight() || isBrightEnoughToSpawn(l, x, y, z)
	return e.tag(l.BlockState(x, y-1, z), floorTag) && bright
}

// canSeeSky is LevelReader.canSeeSky: observed to follow the WORLD_SURFACE
// heightmap (a carpet or rail at the position hides the sky even though its
// sky light is 15).
func canSeeSky(l Level, x, y, z int) bool { return y >= l.Height(HeightmapWorldSurface, x, z) }

func (e *Engine) checkSurfaceWaterAnimal(l Level, x, y, z int) bool {
	sea := l.SeaLevel()
	return y >= sea-13 && y <= sea && isWater(l, x, y-1, z) && e.is(l.BlockState(x, y+1, z), "minecraft:water")
}

// CheckSpawnRules is SpawnPlacements.checkSpawnRules: the per-type spawn
// predicate registered with the placement table. Types without a
// registration pass.
func (e *Engine) CheckSpawnRules(l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if !t.Registered {
		return true
	}
	// Observed gate: in peaceful, types not allowed in peaceful fail before
	// their predicate draws anything (drowned, guardian).
	if l.Difficulty() == Peaceful && !t.Peaceful {
		return false
	}
	if f := spawnRules[t.Name]; f != nil {
		return f(e, l, t, reason, x, y, z, r)
	}
	return e.checkMobSpawnRules(l, t, reason, x, y, z)
}

type ruleFunc func(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool

func monsterRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return e.checkMonsterSpawnRules(l, t, reason, x, y, z, r)
}

func anyLightMonsterRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return e.checkAnyLightMonsterSpawnRules(l, t, reason, x, y, z)
}

func animalOn(tag string) ruleFunc {
	return func(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
		return e.checkAnimalSpawnRules(l, reason, x, y, z, tag)
	}
}

func surfaceWaterRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return e.checkSurfaceWaterAnimal(l, x, y, z)
}

func notOnWartRule(peacefulBlocks bool) ruleFunc {
	return func(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
		if peacefulBlocks && l.Difficulty() == Peaceful {
			return false
		}
		return !e.is(l.BlockState(x, y-1, z), "minecraft:nether_wart_block")
	}
}

func seeSkyMonsterRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return e.checkMonsterSpawnRules(l, t, reason, x, y, z, r) && (reason.IsSpawner() || canSeeSky(l, x, y, z))
}

var spawnRules map[string]ruleFunc

func init() {
	spawnRules = map[string]ruleFunc{
		"minecraft:zombie": monsterRule, "minecraft:skeleton": monsterRule, "minecraft:creeper": monsterRule,
		"minecraft:spider": monsterRule, "minecraft:cave_spider": monsterRule, "minecraft:witch": monsterRule,
		"minecraft:enderman": monsterRule, "minecraft:zombie_villager": monsterRule, "minecraft:wither_skeleton": monsterRule,
		"minecraft:bogged": monsterRule, "minecraft:vindicator": monsterRule, "minecraft:evoker": monsterRule,
		"minecraft:illusioner": monsterRule, "minecraft:ravager": monsterRule, "minecraft:vex": monsterRule,
		"minecraft:giant": monsterRule, "minecraft:zombie_horse": monsterRule, "minecraft:silverfish": monsterRule,
		"minecraft:endermite": monsterRule, "minecraft:creaking": monsterRule, "minecraft:breeze": monsterRule,
		"minecraft:blaze": anyLightMonsterRule, "minecraft:shulker": anyLightMonsterRule, "minecraft:warden": anyLightMonsterRule,

		"minecraft:husk": seeSkyMonsterRule, "minecraft:parched": seeSkyMonsterRule,
		"minecraft:stray": strayRule,

		"minecraft:cow": animalOn("animals_spawnable_on"), "minecraft:pig": animalOn("animals_spawnable_on"),
		"minecraft:sheep": animalOn("animals_spawnable_on"), "minecraft:chicken": animalOn("animals_spawnable_on"),
		"minecraft:horse": animalOn("animals_spawnable_on"), "minecraft:donkey": animalOn("animals_spawnable_on"),
		"minecraft:mule": animalOn("animals_spawnable_on"), "minecraft:llama": animalOn("animals_spawnable_on"),
		"minecraft:trader_llama": animalOn("animals_spawnable_on"), "minecraft:panda": animalOn("animals_spawnable_on"),
		"minecraft:cat": animalOn("animals_spawnable_on"), "minecraft:sniffer": animalOn("animals_spawnable_on"),
		"minecraft:skeleton_horse": animalOn("animals_spawnable_on"), "minecraft:camel": animalOn("camels_spawnable_on"),
		"minecraft:rabbit": animalOn("rabbits_spawnable_on"), "minecraft:fox": animalOn("foxes_spawnable_on"),
		"minecraft:wolf": animalOn("wolves_spawnable_on"), "minecraft:goat": animalOn("goats_spawnable_on"),
		"minecraft:frog": animalOn("frogs_spawnable_on"), "minecraft:mooshroom": animalOn("mooshrooms_spawnable_on"),
		"minecraft:parrot": animalOn("parrots_spawnable_on"), "minecraft:armadillo": animalOn("armadillo_spawnable_on"),
		"minecraft:polar_bear": polarBearRule, "minecraft:turtle": turtleRule, "minecraft:ocelot": ocelotRule,

		"minecraft:cod": surfaceWaterRule, "minecraft:salmon": surfaceWaterRule, "minecraft:pufferfish": surfaceWaterRule,
		"minecraft:squid": surfaceWaterRule, "minecraft:dolphin": surfaceWaterRule,
		"minecraft:tropical_fish": tropicalFishRule, "minecraft:glow_squid": glowSquidRule,
		"minecraft:axolotl": axolotlRule, "minecraft:drowned": drownedRule, "minecraft:guardian": guardianRule,
		"minecraft:elder_guardian": guardianRule,

		"minecraft:bat": batRule, "minecraft:ghast": ghastRule, "minecraft:slime": slimeRule,
		"minecraft:magma_cube": magmaCubeRule, "minecraft:zombified_piglin": notOnWartRule(true),
		"minecraft:piglin": notOnWartRule(false), "minecraft:hoglin": notOnWartRule(false),
		"minecraft:strider": striderRule, "minecraft:nautilus": nautilusRule, "minecraft:sulfur_cube": alwaysRule, "minecraft:pillager": patrolRule,
	}
}

func strayRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	py := y
	for {
		py++
		if !e.is(l.BlockState(x, py, z), "minecraft:powder_snow") {
			break
		}
	}
	return e.checkMonsterSpawnRules(l, t, reason, x, y, z, r) && (reason.IsSpawner() || canSeeSky(l, x, py-1, z))
}

func polarBearRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if !e.biomeTag(l, x, y, z, "polar_bears_spawn_on_alternate_blocks") {
		return e.checkAnimalSpawnRules(l, reason, x, y, z, "animals_spawnable_on")
	}
	return isBrightEnoughToSpawn(l, x, y, z) && e.tag(l.BlockState(x, y-1, z), "polar_bears_spawnable_on_alternate")
}

func turtleRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return y < l.SeaLevel()+4 && e.tag(l.BlockState(x, y-1, z), "sand") && isBrightEnoughToSpawn(l, x, y, z)
}

func ocelotRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return r.NextIntn(3) != 0
}

func tropicalFishRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return isWater(l, x, y-1, z) && e.is(l.BlockState(x, y+1, z), "minecraft:water") &&
		(e.biomeTag(l, x, y, z, "allows_tropical_fish_spawns_at_any_height") || e.checkSurfaceWaterAnimal(l, x, y, z))
}

func glowSquidRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return y <= l.SeaLevel()-33 && rawBrightness(l, x, y, z, 0) == 0 && e.is(l.BlockState(x, y, z), "minecraft:water")
}

func axolotlRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return e.tag(l.BlockState(x, y-1, z), "axolotls_spawnable_on")
}

func drownedRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if !isWater(l, x, y-1, z) && !reason.IsSpawner() {
		return false
	}
	ok := l.Difficulty() != Peaceful && (reason.IgnoresLight() || e.IsDarkEnoughToSpawn(l, x, y, z, r)) && (reason.IsSpawner() || isWater(l, x, y, z))
	if reason.IsSpawner() {
		return ok
	}
	if e.biomeTag(l, x, y, z, "more_frequent_drowned_spawns") {
		return r.NextIntn(15) == 0 && ok
	}
	return r.NextIntn(40) == 0 && y < l.SeaLevel()-5 && ok
}

func guardianRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return (r.NextIntn(20) == 0 || !canSeeSkyFromBelowWater(l, x, y, z)) && l.Difficulty() != Peaceful &&
		(reason.IsSpawner() || isWater(l, x, y, z)) && isWater(l, x, y-1, z)
}

func canSeeSkyFromBelowWater(l Level, x, y, z int) bool {
	sea := l.SeaLevel()
	if y >= sea {
		return canSeeSky(l, x, y, z)
	}
	if !canSeeSky(l, x, sea, z) {
		return false
	}
	for py := sea - 1; py > y; py-- {
		p := props(l.BlockState(x, py, z))
		if p.lightBlock() > 0 && !p.liquid() {
			return false
		}
	}
	return true
}

// batRule: outside Halloween a coin flip rejects first; then the local
// light must not exceed nextInt(4) (7 on Halloween) and the floor must be in
// bats_spawnable_on. Positions that see the sky are rejected before any
// draw; the pre-26 sea-level cap is gone (reference accepts y above sea
// level).
func batRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if canSeeSky(l, x, y, z) {
		return false
	}
	limit := int32(4)
	if e.halloween() {
		limit = 7
	} else if r.NextBool() {
		return false
	}
	return MaxLocalRawBrightness(l, x, y, z) <= int(r.NextIntn(limit)) && e.tag(l.BlockState(x, y-1, z), "bats_spawnable_on")
}

func ghastRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return l.Difficulty() != Peaceful && r.NextIntn(20) == 0 && e.checkMobSpawnRules(l, t, reason, x, y, z)
}

func magmaCubeRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return l.Difficulty() != Peaceful
}

func striderRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	py := y
	for {
		py++
		if !isLava(l, x, py, z) {
			break
		}
	}
	return props(l.BlockState(x, py, z))&bpAir != 0
}

func patrolRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if l.BlockLight(x, y, z) > 8 {
		return false
	}
	return e.checkAnyLightMonsterSpawnRules(l, t, reason, x, y, z)
}

func slimeRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	if l.Difficulty() == Peaceful {
		return false
	}
	if reason.IsSpawner() {
		return e.checkMobSpawnRules(l, t, reason, x, y, z)
	}
	if e.biomeTag(l, x, y, z, "allows_surface_slime_spawns") && y > 50 && y < 70 &&
		r.NextFloat() < l.SurfaceSlimeChance(x, y, z) && MaxLocalRawBrightness(l, x, y, z) <= int(r.NextIntn(8)) {
		return e.checkMobSpawnRules(l, t, reason, x, y, z)
	}
	if reason != ReasonNatural && reason != ReasonChunkGeneration {
		return false
	}
	slimeChunk := IsSlimeChunk(l.Seed(), x>>4, z>>4)
	if r.NextIntn(10) == 0 && slimeChunk && y < 40 {
		return e.checkMobSpawnRules(l, t, reason, x, y, z)
	}
	return false
}

// IsSlimeChunk is the slime chunk test (salt 987234911).
func IsSlimeChunk(seed int64, cx, cz int) bool {
	return rng.SeedSlimeChunk(int32(cx), int32(cz), seed, 987234911).NextIntn(10) == 0
}

// walkTargetValue is PathfinderMob.getWalkTargetValue(pos, level) for a
// fresh mob, dispatched on the reference class that declares the override.
func (e *Engine) walkTargetValue(l Level, t *EntityType, x, y, z int) float32 {
	switch t.Declarer["getWalkTargetValue"] {
	case "Monster":
		return -pathfindingCostFromLight(l, x, y, z)
	case "Animal":
		if e.is(l.BlockState(x, y-1, z), "minecraft:grass_block") {
			return 10
		}
		return pathfindingCostFromLight(l, x, y, z)
	case "MushroomCow":
		if e.is(l.BlockState(x, y-1, z), "minecraft:mycelium") {
			return 10
		}
		return pathfindingCostFromLight(l, x, y, z)
	case "Strider":
		if isLava(l, x, y, z) {
			return 10
		}
		return 0
	case "Turtle":
		if isWater(l, x, y, z) || e.tag(l.BlockState(x, y-1, z), "sand") {
			return 10
		}
		return pathfindingCostFromLight(l, x, y, z)
	case "Guardian":
		if isWater(l, x, y, z) {
			return 10 + pathfindingCostFromLight(l, x, y, z)
		}
		return -pathfindingCostFromLight(l, x, y, z)
	}
	// PathfinderMob default and the overrides observed to return 0 on dry
	// land (Enderman, Axolotl, Hoglin, AbstractNautilus, Pillager).
	return 0
}

// mobCheckSpawnRules is Mob.checkSpawnRules(level, reason) for a fresh mob.
func (e *Engine) mobCheckSpawnRules(l Level, t *EntityType, x, y, z int) bool {
	if !t.is(classPathfinder) {
		return true
	}
	return e.walkTargetValue(l, t, x, y, z) >= 0
}

// checkSpawnObstruction is Mob.checkSpawnObstruction for a fresh mob.
func (e *Engine) checkSpawnObstruction(l Level, t *EntityType, box AABB, x, y, z int) bool {
	switch t.Declarer["checkSpawnObstruction"] {
	case "WaterAnimal", "AgeableWaterCreature", "Axolotl", "Guardian", "Drowned", "AbstractNautilus", "Strider":
		return l.IsUnobstructed(box)
	case "Ocelot":
		if l.IsUnobstructed(box) && !containsAnyLiquid(l, box) {
			if y < l.SeaLevel() {
				return false
			}
			below := l.BlockState(x, y-1, z)
			return e.is(below, "minecraft:grass_block") || e.tag(below, "leaves")
		}
		return false
	}
	return !containsAnyLiquid(l, box) && l.IsUnobstructed(box)
}

// nautilusRule: observed true exactly for sea-25 <= y <= sea-5 in water
// (sea level probed via an accessor override).
func nautilusRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	sea := l.SeaLevel()
	return y >= sea-25 && y <= sea-5 && isWater(l, x, y, z)
}

// alwaysRule: registered predicate observed to accept every probed
// position, difficulty and light level (sulfur cube).
func alwaysRule(e *Engine, l Level, t *EntityType, reason Reason, x, y, z int, r rng.Source) bool {
	return true
}
