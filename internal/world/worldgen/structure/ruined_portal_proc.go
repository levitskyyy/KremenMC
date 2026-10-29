package structure

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Ruined portal processors, inferred from the black-box probe
// kremenmc.probe.structlegacy.Proc (testdata/ruined_portal/place:
// block_age_all.txt, every template block at 300 positions for mossiness
// 0, 0.2, 0.5, 0.8 with every draw logged; blackstone_replace.txt). Each
// processor uses a fresh legacy random seeded with getSeed(pos).

// copyState returns block name with the properties of s that it shares.
func copyState(reg *data.Registry, name string, s data.StateID, keys ...string) data.StateID {
	out, ok := reg.StateFrom(name, func(k string) (string, bool) {
		if len(keys) > 0 {
			found := false
			for _, kk := range keys {
				found = found || kk == k
			}
			if !found {
				return "", false
			}
		}
		return reg.Value(s, k)
	})
	if !ok {
		out, _ = reg.DefaultState(name)
	}
	return out
}

var stairFacings = [4]string{"north", "east", "south", "west"}
var stairHalves = [2]string{"top", "bottom"}

func randomStairs(reg *data.Registry, name string, r *rng.Legacy) data.StateID {
	f := stairFacings[r.NextIntn(4)]
	h := stairHalves[r.NextIntn(2)]
	s, _ := reg.StateFrom(name, func(k string) (string, bool) {
		switch k {
		case "facing":
			return f, true
		case "half":
			return h, true
		}
		return "", false
	})
	return s
}

func defaultState(reg *data.Registry, name string) data.StateID {
	s, _ := reg.DefaultState(name)
	return s
}

// blockAge is minecraft:block_age.
//   - obsidian: f < 0.15 -> crying_obsidian;
//   - stone_bricks, stone, chiseled_stone_bricks: f < 0.5 -> random
//     stone_brick_stairs A, random mossy_stone_brick_stairs B (facing
//     nextInt(4) N,E,S,W; half nextInt(2) top,bottom), f2, k = nextInt(2):
//     f2 < mossiness ? [mossy_stone_bricks, B][k] : [cracked_stone_bricks, A][k];
//   - stone_brick_stairs: f < 0.5 -> f2, k: f2 < mossiness ? [mossy stairs
//     (same properties), mossy_stone_brick_slab][k] : [stone_slab,
//     stone_brick_slab][k];
//   - stone_brick_slab, stone_slab, smooth_stone_slab: f < mossiness ->
//     mossy_stone_brick_slab (same type);
//   - stone_brick_wall: f < mossiness -> mossy_stone_brick_wall (same
//     connections).
func blockAge(reg *data.Registry, s data.StateID, mossiness float32, x, y, z int32) data.StateID {
	b := reg.BlockOf(s)
	if b == nil {
		return s
	}
	r := rng.NewLegacy(rng.GetSeed(x, y, z))
	switch b.Name {
	case "obsidian":
		if r.NextFloat() < 0.15 {
			return defaultState(reg, "crying_obsidian")
		}
	case "stone_bricks", "stone", "chiseled_stone_bricks":
		if r.NextFloat() < 0.5 {
			a := randomStairs(reg, "stone_brick_stairs", r)
			m := randomStairs(reg, "mossy_stone_brick_stairs", r)
			if r.NextFloat() < mossiness {
				if r.NextIntn(2) == 0 {
					return defaultState(reg, "mossy_stone_bricks")
				}
				return m
			}
			if r.NextIntn(2) == 0 {
				return defaultState(reg, "cracked_stone_bricks")
			}
			return a
		}
	case "stone_brick_stairs":
		if r.NextFloat() < 0.5 {
			if r.NextFloat() < mossiness {
				if r.NextIntn(2) == 0 {
					return copyState(reg, "mossy_stone_brick_stairs", s)
				}
				return defaultState(reg, "mossy_stone_brick_slab")
			}
			if r.NextIntn(2) == 0 {
				return defaultState(reg, "stone_slab")
			}
			return defaultState(reg, "stone_brick_slab")
		}
	case "stone_brick_slab", "stone_slab", "smooth_stone_slab":
		if r.NextFloat() < mossiness {
			return copyState(reg, "mossy_stone_brick_slab", s)
		}
	case "stone_brick_wall":
		if r.NextFloat() < mossiness {
			return copyState(reg, "mossy_stone_brick_wall", s)
		}
	}
	return s
}

// blackstoneReplace is minecraft:blackstone_replace: a fixed block mapping
// that keeps facing, half and (slab) type and resets everything else.
var blackstoneMap = map[string]string{
	"stone":                    "polished_blackstone",
	"stone_bricks":             "polished_blackstone_bricks",
	"mossy_stone_bricks":       "polished_blackstone_bricks",
	"cracked_stone_bricks":     "cracked_polished_blackstone_bricks",
	"chiseled_stone_bricks":    "chiseled_polished_blackstone",
	"stone_slab":               "polished_blackstone_slab",
	"smooth_stone_slab":        "polished_blackstone_slab",
	"stone_brick_slab":         "polished_blackstone_brick_slab",
	"mossy_stone_brick_slab":   "polished_blackstone_brick_slab",
	"stone_brick_stairs":       "polished_blackstone_brick_stairs",
	"mossy_stone_brick_stairs": "polished_blackstone_brick_stairs",
	"stone_brick_wall":         "polished_blackstone_brick_wall",
	"mossy_stone_brick_wall":   "polished_blackstone_brick_wall",
	"iron_bars":                "iron_chain",
}

func blackstoneReplace(reg *data.Registry, s data.StateID) data.StateID {
	b := reg.BlockOf(s)
	if b == nil {
		return s
	}
	to, ok := blackstoneMap[b.Name]
	if !ok {
		return s
	}
	return copyState(reg, to, s, "facing", "half", "type")
}
