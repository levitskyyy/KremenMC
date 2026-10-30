package structure

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// drownedSpawn is an ocean ruin "drowned" marker: a persistent drowned at
// the marker finalized with reason STRUCTURE on the region random r.
// Black box (kremenmc.probe.oruin.DrownedSpawn, 3000 calls at the
// worldgen difficulty, special multiplier 0; every threshold separates all
// samples):
//
//	triangle(0, 0.11485)  follow_range random_spawn_bonus
//	nextFloat < 0.05      left-handed
//	nextFloat             (pick-up-loot roll; never at special 0)
//	nextFloat < 0.05      baby; a baby then draws nextFloat < 0.05 (look
//	                      for a chicken to ride: none in a worldgen region),
//	                      else nextFloat < 0.05: chicken jockey (chicken
//	                      variant pick, sound variant nextInt(2), triangle,
//	                      nextFloat)
//	nextFloat             (no effect seen)
//	nextFloat > 0.9       weapon: nextInt(16) < 10 trident, else fishing
//	                      rod; then nextFloat (enchant roll, never at
//	                      special 0)
//	nextFloat < 0.03      nautilus shell in the off hand (drop chance 2)
//	trident: nextFloat < 0.5 and not a baby: rides a zombie nautilus
//	                      (variant pick, nextInt(1201), triangle, nextFloat)
//
// The entity's own random (spawn reinforcements, knockback bonus, chicken
// egg time) is not deterministic in vanilla and is not written.
func drownedSpawn(reg *data.Registry, lv Level, r rng.Source, x, y, z int32) []nbt.Compound {
	pos := nbt.List{Type: nbt.TagDouble, Items: []any{float64(x) + 0.5, float64(y), float64(z) + 0.5}}
	rot := func() nbt.List { return nbt.List{Type: nbt.TagFloat, Items: []any{float32(0), float32(0)}} }
	follow := func(base float64, amount float64) nbt.Compound {
		return nbt.Compound{"id": "minecraft:follow_range", "base": base, "modifiers": nbt.List{Type: nbt.TagCompound, Items: []any{
			nbt.Compound{"id": "minecraft:random_spawn_bonus", "amount": amount, "operation": "add_multiplied_base"}}}}
	}
	biome := ""
	if b := reg.BiomeName(lv.Biome(x, y, z)); b != "" {
		biome = qualify(b)
	}
	d := nbt.Compound{"id": "minecraft:drowned", "Pos": pos, "Rotation": rot(), "PersistenceRequired": int8(1), "CanPickUpLoot": int8(0)}
	attrs := []any{follow(35, r.Triangle(0, 0.11485))}
	d["LeftHanded"] = b2b(r.NextFloat() < 0.05)
	r.NextFloat()
	baby := r.NextFloat() < 0.05
	d["IsBaby"] = b2b(baby)
	var vehicle nbt.Compound
	if baby {
		attrs = append(attrs, nbt.Compound{"id": "minecraft:movement_speed", "base": 0.23000000417232513})
		if !(r.NextFloat() < 0.05) && r.NextFloat() < 0.05 {
			vehicle = nbt.Compound{"id": "minecraft:chicken", "Pos": pos, "Rotation": rot(), "IsChickenJockey": int8(1), "PersistenceRequired": int8(0)}
			vehicle["variant"] = pickSpawnVariant(r, "chicken_variant", biome)
			if n := len(soundVariants("chicken_sound_variant")); n > 0 {
				vehicle["sound_variant"] = soundVariants("chicken_sound_variant")[r.NextIntn(int32(n))]
			}
			vehicle["attributes"] = nbt.List{Type: nbt.TagCompound, Items: []any{follow(16, r.Triangle(0, 0.11485))}}
			vehicle["LeftHanded"] = b2b(r.NextFloat() < 0.05)
		}
	}
	r.NextFloat()
	weapon := ""
	if r.NextFloat() > 0.9 {
		weapon = "minecraft:fishing_rod"
		if r.NextIntn(16) < 10 {
			weapon = "minecraft:trident"
		}
		r.NextFloat()
	}
	eq := nbt.Compound{}
	if weapon != "" {
		eq["mainhand"] = nbt.Compound{"id": weapon, "count": int32(1)}
	}
	if r.NextFloat() < 0.03 {
		eq["offhand"] = nbt.Compound{"id": "minecraft:nautilus_shell", "count": int32(1)}
		d["drop_chances"] = nbt.Compound{"offhand": float32(2)}
	}
	if len(eq) > 0 {
		d["equipment"] = eq
	}
	if weapon == "minecraft:trident" && r.NextFloat() < 0.5 && !baby {
		vehicle = nbt.Compound{"id": "minecraft:zombie_nautilus", "Pos": pos, "Rotation": rot(), "PersistenceRequired": int8(1)}
		vehicle["variant"] = pickSpawnVariant(r, "zombie_nautilus_variant", biome)
		r.NextIntn(1201)
		vehicle["attributes"] = nbt.List{Type: nbt.TagCompound, Items: []any{follow(16, r.Triangle(0, 0.11485))}}
		vehicle["LeftHanded"] = b2b(r.NextFloat() < 0.05)
	}
	d["attributes"] = nbt.List{Type: nbt.TagCompound, Items: attrs}
	if vehicle != nil {
		vehicle["Passengers"] = nbt.List{Type: nbt.TagCompound, Items: []any{d}}
		return []nbt.Compound{vehicle}
	}
	return []nbt.Compound{d}
}

type spawnVariant struct {
	name  string
	conds []struct {
		priority int
		tag      string // biome tag without '#', "" = unconditional
	}
}

var spawnVariants sync.Map

// pickSpawnVariant picks a data-driven variant: highest priority among the
// matching spawn conditions, nextInt(ties) among them (the reference draws
// nextInt(1) for chicken and zombie nautilus).
func pickSpawnVariant(r rng.Source, registry, biome string) string {
	vs := loadSpawnVariants(registry)
	best := -1 << 30
	var cands []string
	for _, v := range vs {
		p, ok := -1<<30, false
		for _, c := range v.conds {
			if c.tag == "" || biomeTagged(c.tag, biome) {
				if !ok || c.priority > p {
					p, ok = c.priority, true
				}
			}
		}
		if !ok {
			continue
		}
		if p > best {
			best, cands = p, cands[:0]
		}
		if p == best {
			cands = append(cands, v.name)
		}
	}
	if len(cands) == 0 {
		return ""
	}
	return cands[r.NextIntn(int32(len(cands)))]
}

func loadSpawnVariants(registry string) []spawnVariant {
	if v, ok := spawnVariants.Load(registry); ok {
		return v.([]spawnVariant)
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	var out []spawnVariant
	for _, k := range sortedKeys(pk, registry) {
		b, _ := pk.JSON(registry, k)
		var j struct {
			SpawnConditions []struct {
				Priority  int `json:"priority"`
				Condition *struct {
					Biomes string `json:"biomes"`
				} `json:"condition"`
			} `json:"spawn_conditions"`
		}
		if json.Unmarshal(b, &j) != nil {
			continue
		}
		v := spawnVariant{name: qualify(k)}
		for _, c := range j.SpawnConditions {
			tag := ""
			if c.Condition != nil {
				tag = strings.TrimPrefix(c.Condition.Biomes, "#")
			}
			v.conds = append(v.conds, struct {
				priority int
				tag      string
			}{c.Priority, tag})
		}
		out = append(out, v)
	}
	spawnVariants.Store(registry, out)
	return out
}

// soundVariants lists a sound variant registry in key order (the reference
// maps nextInt(2) 0 to classic and 1 to picky).
func soundVariants(registry string) []string {
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	ks := sortedKeys(pk, registry)
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = qualify(k)
	}
	return out
}

var biomeTags sync.Map

func biomeTagged(tag, biome string) bool {
	tag = qualify(tag)
	if v, ok := biomeTags.Load(tag); ok {
		return v.(map[string]bool)[biome]
	}
	pk, err := wgdata.Load()
	if err != nil {
		return false
	}
	set := map[string]bool{}
	names, _ := pk.Tag("worldgen/biome", strings.TrimPrefix(tag, "minecraft:"))
	for _, n := range names {
		set[qualify(n)] = true
	}
	biomeTags.Store(tag, set)
	return set[biome]
}

func sortedKeys(pk *wgdata.Pack, registry string) []string {
	ks := append([]string(nil), pk.Keys(registry)...)
	sort.Strings(ks)
	return ks
}
