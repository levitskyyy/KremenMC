package spawn

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// FinalizeContext is passed to finalizeSpawn hooks. R is the level random
// (ServerLevelAccessor.getRandom), which the reference uses for every roll in
// finalizeSpawn.
type FinalizeContext struct {
	E          *Engine
	L          Level
	R          rng.Source
	Difficulty DifficultyInstance
	Reason     Reason
}

// FinalizeFunc is a per-type finalizeSpawn. It must call ctx.Base(m) where
// the reference calls super.finalizeSpawn, and return the group data for the
// next pack member.
type FinalizeFunc func(ctx *FinalizeContext, m *Mob, group any) any

// FinalizeSpawn runs the finalizeSpawn hook for m.
func (e *Engine) FinalizeSpawn(l Level, r rng.Source, m *Mob, d DifficultyInstance, reason Reason, group any) any {
	ctx := &FinalizeContext{E: e, L: l, R: r, Difficulty: d, Reason: reason}
	if f := e.Finalize[m.Type.Name]; f != nil {
		return f(ctx, m, group)
	}
	if f := finalizers[m.Type.Name]; f != nil {
		return f(ctx, m, group)
	}
	ctx.Base(m)
	return group
}

// RandomSpawnBonus is the follow-range modifier spread rolled by Mob.finalizeSpawn.
const RandomSpawnBonus = 0.11485000000000001

// Base is Mob.finalizeSpawn: the follow-range bonus roll and handedness.
func (c *FinalizeContext) Base(m *Mob) {
	m.set("follow_range_bonus", c.R.Triangle(0, RandomSpawnBonus))
	m.LeftHanded = c.R.NextFloat() < 0.05
}

// finalizers holds the built-in hooks whose draw sequence is implemented,
// keyed by entity name.
var finalizers = map[string]FinalizeFunc{}

// AgeableGroup is AgeableMob.AgeableMobGroupData.
type AgeableGroup struct {
	ShouldSpawnBaby bool
	Size            int
	BabyChance      float32
}

// FinalizeAgeable is AgeableMob.finalizeSpawn: pack members after the
// first become babies with probability 0.05, then Mob.finalizeSpawn.
func FinalizeAgeable(c *FinalizeContext, m *Mob, group any) any {
	g, ok := group.(*AgeableGroup)
	if !ok {
		g = &AgeableGroup{ShouldSpawnBaby: true, BabyChance: 0.05}
		group = g
	}
	if g.ShouldSpawnBaby && g.Size > 0 && c.R.NextFloat() <= g.BabyChance {
		m.set("baby", true)
	}
	g.Size++
	c.Base(m)
	return group
}

// VerifiedFinalize reports whether a type's finalizeSpawn draw sequence is
// implemented and was checked against the reference. Types not listed use
// Base only, which is exact for types that do not override finalizeSpawn.
func VerifiedFinalize(t *EntityType) bool {
	if _, ok := finalizers[t.Name]; ok {
		return true
	}
	return finalizeIsBase[t.Name]
}

// finalizeIsBase lists spawnable types whose finalizeSpawn is declared only
// on Mob (reference class signatures), so Base is complete for them.
var finalizeIsBase = map[string]bool{}

// tropicalGroup is TropicalFish.TropicalFishGroupData.
type tropicalGroup struct{ variant int32 }

// FinalizeTropicalFish: Mob base rolls, then 90% one of the 22 common
// variants shared by the pack, else a random pattern (12) and two colors
// (16 each) for a solitary fish whose pack stops growing.
func FinalizeTropicalFish(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	if g, ok := group.(*tropicalGroup); ok {
		m.set("variant", g.variant)
		return group
	}
	if c.R.NextFloat() < 0.9 {
		g := &tropicalGroup{c.R.NextIntn(22)}
		m.set("common_variant", g.variant)
		return g
	}
	m.set("pattern", c.R.NextIntn(12))
	m.set("base_color", c.R.NextIntn(16))
	m.set("pattern_color", c.R.NextIntn(16))
	m.set("solitary", true)
	return group
}

// groupLimitReached is Mob.isMaxGroupSizeReached for a finalized mob.
func groupLimitReached(m *Mob, inPack int) bool {
	if m.Type.Declarer["isMaxGroupSizeReached"] == "TropicalFish" {
		solitary, _ := m.Attrs["solitary"].(bool)
		return solitary
	}
	return m.Type.GroupLimit > 0 && inPack >= m.Type.GroupLimit
}

func init() {
	finalizers["minecraft:tropical_fish"] = FinalizeTropicalFish
	for _, t := range entityTypes {
		if t.Mob && finalizeBaseOnly(t) {
			finalizeIsBase[t.Name] = true
		}
		if t.Declarer["finalizeSpawn"] == "AgeableMob" {
			finalizers[t.Name] = FinalizeAgeable
		}
	}
}

func finalizeBaseOnly(t *EntityType) bool { return t.Declarer["finalizeSpawn"] == "Mob" }
