package spawn

// finalizeSpawn hooks whose outcome distribution follows minecraft.wiki and
// reference observation (kremenmc.probe.mob.Mobs: 200 finalizeSpawn calls
// per type), but whose draw order is not known. They are kept out of
// finalizers so VerifiedFinalize stays false for these types.
//
// Results go into Mob.Attrs:
//
//	"equip.<slot>" item name    (slot: mainhand offhand head chest legs feet)
//	"drop.<slot>"  float32      (drop chance; > 1 means guaranteed, preserved)
//	"ench.<slot>"  bool         (enchant like an enchanting table at 5..22)
//	"baby", "variant", "type", "color", ...

// DefaultDropChance is the equipment drop chance of naturally spawned gear.
const DefaultDropChance = 0.085

// armorTiers are the material odds of naturally spawned armor (wiki,
// "Armor equipped on mobs"; copper since 1.21.9).
var armorTiers = []struct {
	name string
	p    float32
}{{"leather", 0.2360}, {"copper", 0.3223}, {"golden", 0.3329}, {"chainmail", 0.0973}, {"iron", 0.0110}, {"diamond", 0.0005}}

var armorPieces = [...]struct{ slot, item string }{{"head", "helmet"}, {"chest", "chestplate"}, {"legs", "leggings"}, {"feet", "boots"}}

func equip(m *Mob, slot, item string, drop float32) {
	m.set("equip."+slot, item)
	m.set("drop."+slot, drop)
}

func (c *FinalizeContext) enchant(m *Mob, slot string, p float32) {
	if c.R.NextFloat() < p*c.Difficulty.Special {
		m.set("ench."+slot, true)
	}
}

// populateArmor: 15% x special multiplier for any armor; one material for
// the whole set, helmet first then chest, legs, boots, stopping after each
// piece with 10% (25% for a hard difficulty instance). Armor is enchanted with 50% x special.
func (c *FinalizeContext) populateArmor(m *Mob) {
	if c.R.NextFloat() >= 0.15*c.Difficulty.Special {
		return
	}
	f, tier := c.R.NextFloat(), armorTiers[len(armorTiers)-1].name
	for _, t := range armorTiers {
		if f < t.p {
			tier = t.name
			break
		}
		f -= t.p
	}
	// The stop chance follows the regional difficulty instance (observed:
	// a hard instance in a normal level stops at 25%), the zombie weapon
	// chance the level difficulty.
	stop := float32(0.1)
	if c.Difficulty.Base == Hard {
		stop = 0.25
	}
	for i, p := range armorPieces {
		if i > 0 && c.R.NextFloat() < stop {
			break
		}
		equip(m, p.slot, tier+"_"+p.item, DefaultDropChance)
		c.enchant(m, p.slot, 0.5)
	}
}

func finalizeZombie(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	if c.R.NextFloat() < 0.05 {
		m.set("baby", true)
		if c.R.NextFloat() < 0.0475 {
			m.set("jockey", "chicken")
		}
	}
	c.populateArmor(m)
	weapon := float32(0.01)
	if c.L.Difficulty() == Hard {
		weapon = 0.05
	}
	if c.R.NextFloat() < weapon {
		switch c.R.NextIntn(6) {
		case 0:
			equip(m, "mainhand", "iron_sword", DefaultDropChance)
		case 1:
			equip(m, "mainhand", "iron_spear", DefaultDropChance)
		default:
			equip(m, "mainhand", "iron_shovel", DefaultDropChance)
		}
		c.enchant(m, "mainhand", 0.25)
	}
	return group
}

func finalizeDrowned(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	if c.R.NextFloat() < 0.05 {
		m.set("baby", true)
	}
	if f := c.R.NextFloat(); f < 0.0625 {
		equip(m, "mainhand", "trident", DefaultDropChance)
		c.enchant(m, "mainhand", 0.25)
	} else if f < 0.1 {
		equip(m, "mainhand", "fishing_rod", DefaultDropChance)
		c.enchant(m, "mainhand", 0.25)
	}
	if c.R.NextFloat() < 0.03 {
		equip(m, "offhand", "nautilus_shell", 2)
	}
	return group
}

func finalizeSkeleton(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	c.populateArmor(m)
	equip(m, "mainhand", "bow", DefaultDropChance)
	c.enchant(m, "mainhand", 0.25)
	return group
}

func fixedWeapon(item string, ench bool) FinalizeFunc {
	return func(c *FinalizeContext, m *Mob, group any) any {
		c.Base(m)
		equip(m, "mainhand", item, DefaultDropChance)
		if ench {
			c.enchant(m, "mainhand", 0.25)
		}
		return group
	}
}

func finalizeZombifiedPiglin(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	if c.R.NextFloat() < 0.05 {
		m.set("baby", true)
	}
	if c.R.NextIntn(20) == 0 {
		equip(m, "mainhand", "golden_spear", DefaultDropChance)
	} else {
		equip(m, "mainhand", "golden_sword", DefaultDropChance)
	}
	c.enchant(m, "mainhand", 0.25)
	return group
}

// finalizePiglin: 20% babies (no weapon); adults hold a crossbow or a
// golden melee weapon (sword, sometimes a spear); each armor slot is gold
// with 10%.
func finalizePiglin(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	baby := c.R.NextFloat() < 0.2
	if baby {
		m.set("baby", true)
	} else {
		switch {
		case c.R.NextFloat() < 0.5:
			equip(m, "mainhand", "crossbow", DefaultDropChance)
		case c.R.NextFloat() < 0.13:
			equip(m, "mainhand", "golden_spear", DefaultDropChance)
		default:
			equip(m, "mainhand", "golden_sword", DefaultDropChance)
		}
		c.enchant(m, "mainhand", 0.25)
	}
	for _, p := range armorPieces {
		if c.R.NextFloat() < 0.1 {
			equip(m, p.slot, "golden_"+p.item, DefaultDropChance)
			c.enchant(m, p.slot, 0.5)
		}
	}
	return group
}

// villagerTypeOf is the villager type of a biome (wiki, "Villager").
func villagerTypeOf(biome string) string {
	switch biome {
	case "minecraft:desert", "minecraft:badlands", "minecraft:eroded_badlands", "minecraft:wooded_badlands":
		return "desert"
	case "minecraft:jungle", "minecraft:sparse_jungle", "minecraft:bamboo_jungle":
		return "jungle"
	case "minecraft:savanna", "minecraft:savanna_plateau", "minecraft:windswept_savanna":
		return "savanna"
	case "minecraft:snowy_plains", "minecraft:ice_spikes", "minecraft:snowy_taiga", "minecraft:snowy_beach",
		"minecraft:snowy_slopes", "minecraft:frozen_peaks", "minecraft:jagged_peaks", "minecraft:frozen_river",
		"minecraft:frozen_ocean", "minecraft:deep_frozen_ocean", "minecraft:grove":
		return "snow"
	case "minecraft:swamp", "minecraft:mangrove_swamp":
		return "swamp"
	case "minecraft:taiga", "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga",
		"minecraft:windswept_hills", "minecraft:windswept_gravelly_hills", "minecraft:windswept_forest":
		return "taiga"
	}
	return "plains"
}

func (c *FinalizeContext) biomeOf(m *Mob) string {
	x, y, z := m.BlockPos()
	return c.L.Biome(x, y, z)
}

func finalizeVillager(c *FinalizeContext, m *Mob, group any) any {
	m.set("villager_type", villagerTypeOf(c.biomeOf(m)))
	return FinalizeAgeable(c, m, group)
}

func finalizeZombieVillager(c *FinalizeContext, m *Mob, group any) any {
	m.set("villager_type", villagerTypeOf(c.biomeOf(m)))
	return finalizeZombie(c, m, group)
}

// Rabbit types (Rabbit.Variant ids).
const (
	rabbitBrown, rabbitWhite, rabbitBlack, rabbitWhiteSplotched, rabbitGold, rabbitSalt = 0, 1, 2, 3, 4, 5
)

// rabbitGroup shares the type across a pack.
type rabbitGroup struct {
	AgeableGroup
	typ int32
}

func finalizeRabbit(c *FinalizeContext, m *Mob, group any) any {
	g, ok := group.(*rabbitGroup)
	if !ok {
		biome := c.biomeOf(m)
		var t int32
		switch {
		case c.E.Data.biomeSet("spawns_gold_rabbits")[biome]:
			t = rabbitGold
		case c.E.Data.biomeSet("spawns_white_rabbits")[biome]:
			t = rabbitWhite
			if c.R.NextIntn(100) >= 80 {
				t = rabbitWhiteSplotched
			}
		default:
			switch i := c.R.NextIntn(100); {
			case i < 50:
				t = rabbitBrown
			case i < 90:
				t = rabbitSalt
			default:
				t = rabbitBlack
			}
		}
		g = &rabbitGroup{AgeableGroup{ShouldSpawnBaby: true, BabyChance: 0.05}, t}
	}
	m.set("type", g.typ)
	finalizeAgeableGroup(c, m, &g.AgeableGroup)
	return g
}

func finalizeFox(c *FinalizeContext, m *Mob, group any) any {
	if c.E.Data.biomeSet("spawns_snow_foxes")[c.biomeOf(m)] {
		m.set("type", int32(1))
	}
	return FinalizeAgeable(c, m, group)
}

func uniformVariant(n int32) FinalizeFunc {
	return func(c *FinalizeContext, m *Mob, group any) any {
		m.set("type", c.R.NextIntn(n))
		return FinalizeAgeable(c, m, group)
	}
}

// llamaGroup shares the coat across a caravan.
type llamaGroup struct {
	AgeableGroup
	variant int32
}

func finalizeLlama(c *FinalizeContext, m *Mob, group any) any {
	g, ok := group.(*llamaGroup)
	if !ok {
		g = &llamaGroup{AgeableGroup{ShouldSpawnBaby: true, BabyChance: 0.05}, c.R.NextIntn(4)}
	}
	m.set("type", g.variant)
	// Strength odds observed on the reference (200 llamas).
	s := pickWeighted(c, []int32{1, 2, 3, 5}, []float32{0.30, 0.28, 0.40, 0.02})
	m.set("strength", s)
	finalizeAgeableGroup(c, m, &g.AgeableGroup)
	return g
}

func finalizeGoat(c *FinalizeContext, m *Mob, group any) any {
	if c.R.NextFloat() < 0.02 {
		m.set("screaming", true)
	}
	return FinalizeAgeable(c, m, group)
}

// pickWeighted draws one value with the given odds.
func pickWeighted(c *FinalizeContext, vals []int32, p []float32) int32 {
	f := c.R.NextFloat()
	for i, w := range p {
		if f < w {
			return vals[i]
		}
		f -= w
	}
	return vals[len(vals)-1]
}

// pandaGene: Panda.Gene ids with the odds observed on the reference (400
// genes of 200 pandas): normal, lazy, worried, playful, brown, weak,
// aggressive.
func pandaGene(c *FinalizeContext) int32 {
	return pickWeighted(c, []int32{0, 1, 2, 3, 4, 5, 6}, []float32{0.31, 0.04, 0.07, 0.08, 0.12, 0.34, 0.04})
}

func finalizePanda(c *FinalizeContext, m *Mob, group any) any {
	m.set("main_gene", pandaGene(c))
	m.set("hidden_gene", pandaGene(c))
	return FinalizeAgeable(c, m, group)
}

// sheepColorIn is the biome-dependent wool roll (wiki "Sheep": common
// 81.836%, uncommon 3x5%, rare 3%, epic 0.164%). Warm biomes swap the
// common (white) and rare (brown) colors, as observed on the reference.
func sheepColorIn(c *FinalizeContext, biome string) string {
	col := sheepColor(c)
	if c.E.Data.biomeSet("spawns_warm_variant_farm_animals")[biome] {
		switch col {
		case "white":
			return "brown"
		case "brown":
			return "white"
		}
	}
	return col
}

var approxFinalizers = map[string]FinalizeFunc{
	"minecraft:zombie":           finalizeZombie,
	"minecraft:husk":             finalizeZombie,
	"minecraft:zombie_villager":  finalizeZombieVillager,
	"minecraft:drowned":          finalizeDrowned,
	"minecraft:skeleton":         finalizeSkeleton,
	"minecraft:stray":            finalizeSkeleton,
	"minecraft:bogged":           finalizeSkeleton,
	"minecraft:wither_skeleton":  fixedWeapon("stone_sword", false),
	"minecraft:vindicator":       fixedWeapon("iron_axe", true),
	"minecraft:pillager":         fixedWeapon("crossbow", true),
	"minecraft:piglin_brute":     fixedWeapon("golden_axe", false),
	"minecraft:zombified_piglin": finalizeZombifiedPiglin,
	"minecraft:piglin":           finalizePiglin,
	"minecraft:villager":         finalizeVillager,
	"minecraft:rabbit":           finalizeRabbit,
	"minecraft:fox":              finalizeFox,
	"minecraft:parrot":           uniformVariant(5),
	"minecraft:axolotl":          uniformVariant(4),
	"minecraft:llama":            finalizeLlama,
	"minecraft:trader_llama":     finalizeLlama,
	"minecraft:goat":             finalizeGoat,
	"minecraft:panda":            finalizePanda,
	"minecraft:wolf":             variantFinalizer("wolf"),
	"minecraft:cat":              variantFinalizer("cat"),
	"minecraft:frog":             variantFinalizer("frog"),
	"minecraft:zombie_nautilus":  variantFinalizer("zombie_nautilus"),
}
