package spawn

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// DespawnState is the per-mob state Mob.checkDespawn reads.
type DespawnState struct {
	Type    *EntityType
	X, Y, Z float64
	// PersistenceRequired is set by name tags, picking up items, leashing,
	// taming, spawners with custom data, etc.
	PersistenceRequired bool
	// CustomPersistence is requiresCustomPersistence(): true for passengers
	// and type-specific cases (fish from buckets, raiders in a raid,
	// endermen carrying blocks, ...). Use DefaultCustomPersistence for a
	// fresh instance.
	CustomPersistence bool
	// FarAway is removeWhenFarAway(dist); use Type.FarAway for a fresh
	// instance (some types consult state such as a custom name or a bucket
	// origin, see doc.go).
	FarAway      bool
	NoActionTime int
}

// DespawnResult is the outcome of one checkDespawn call.
type DespawnResult struct {
	Remove       bool
	NoActionTime int // updated idle counter
}

// CheckDespawn is Mob.checkDespawn. r is the mob's own random
// (Entity.random), which the 1/800 roll draws from.
func CheckDespawn(l Level, s DespawnState, r rng.Source) DespawnResult {
	res := DespawnResult{NoActionTime: s.NoActionTime}
	if l.Difficulty() == Peaceful && !s.Type.Peaceful {
		res.Remove = true
		return res
	}
	if s.PersistenceRequired || s.CustomPersistence {
		return res
	}
	_, d, ok := nearestPlayer(l, s.X, s.Y, s.Z)
	if !ok {
		return res
	}
	c := s.Type.Category.Info()
	if d > float64(c.Despawn*c.Despawn) && s.FarAway {
		res.Remove = true
	}
	nd := float64(c.NoDespawn * c.NoDespawn)
	if s.NoActionTime > 600 && r.NextIntn(800) == 0 && d > nd && s.FarAway {
		res.Remove = true
	} else if d < nd {
		res.NoActionTime = 0
	}
	return res
}

// keepsNamed reports whether removeWhenFarAway is false for a named
// instance of the type (AbstractFish: !fromBucket && !hasCustomName).
func keepsNamed(t *EntityType) bool { return t.Declarer["removeWhenFarAway"] == "AbstractFish" }
