package spawn

import (
	"encoding/json"
	"strings"
)

// Per-type finalizeSpawn hooks whose draw order was matched against the
// reference in the scene goldens (see doc.go for the covered conditions).

// sheepColor is the classic Sheep.getRandomSheepColor roll.
func sheepColor(c *FinalizeContext) string {
	i := c.R.NextIntn(100)
	switch {
	case i < 5:
		return "black"
	case i < 10:
		return "gray"
	case i < 15:
		return "light_gray"
	case i < 18:
		return "brown"
	}
	if c.R.NextIntn(500) == 0 {
		return "pink"
	}
	return "white"
}

func finalizeSheep(c *FinalizeContext, m *Mob, group any) any {
	m.set("color", sheepColor(c))
	return FinalizeAgeable(c, m, group)
}

// variantFinalizer selects a data-driven variant (highest-priority matching
// spawn condition, uniform among ties) and a uniform sound variant, then
// runs AgeableMob.finalizeSpawn.
func variantFinalizer(registry string) FinalizeFunc {
	return func(c *FinalizeContext, m *Mob, group any) any {
		if v := c.E.Data.variants[registry]; len(v) > 0 {
			x, y, z := m.BlockPos()
			m.set("variant", pickVariant(c, v, c.L.Biome(x, y, z)))
		}
		if n := c.E.Data.soundVariants[registry]; n > 0 {
			m.set("sound_variant", c.R.NextIntn(int32(n)))
		}
		return FinalizeAgeable(c, m, group)
	}
}

func pickVariant(c *FinalizeContext, vs []variantDef, biome string) string {
	best := -1 << 31
	var cands []string
	for _, v := range vs {
		p, ok := v.match(c.E.Data, biome)
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
	return cands[c.R.NextIntn(int32(len(cands)))]
}

// horseGroup is Horse.HorseGroupData: the pack shares one coat color.
type horseGroup struct {
	AgeableGroup
	color int32
}

func finalizeHorse(c *FinalizeContext, m *Mob, group any) any {
	g, ok := group.(*horseGroup)
	if !ok {
		g = &horseGroup{AgeableGroup{ShouldSpawnBaby: true, BabyChance: 0.05}, c.R.NextIntn(7)}
	}
	m.set("color", g.color)
	m.set("markings", c.R.NextIntn(5))
	randomizeHorseAttributes(c, m)
	finalizeAgeableGroup(c, m, &g.AgeableGroup)
	return g
}

// randomizeHorseAttributes is AbstractHorse.randomizeAttributes: max
// health 15+nextInt(8)+nextInt(9), then speed and jump from three
// nextDouble each.
func randomizeHorseAttributes(c *FinalizeContext, m *Mob) {
	m.set("max_health", 15+c.R.NextIntn(8)+c.R.NextIntn(9))
	s := 0.44999998807907104 + c.R.NextDouble()*0.3 + c.R.NextDouble()*0.3 + c.R.NextDouble()*0.3
	m.set("speed", s*0.25)
	j := 0.4000000059604645 + c.R.NextDouble()*0.2 + c.R.NextDouble()*0.2 + c.R.NextDouble()*0.2
	m.set("jump", j)
}

func finalizeAgeableGroup(c *FinalizeContext, m *Mob, g *AgeableGroup) {
	if g.ShouldSpawnBaby && g.Size > 0 && c.R.NextFloat() <= g.BabyChance {
		m.set("baby", true)
	}
	g.Size++
	c.Base(m)
}

// finalizeSpider: Mob base, then a 1/100 skeleton-jockey roll. The jockey
// branch and the hard-difficulty effect roll were not observed against the
// reference; the jockey is reported via Attrs only.
func finalizeSpider(c *FinalizeContext, m *Mob, group any) any {
	c.Base(m)
	if c.R.NextIntn(100) == 0 {
		m.set("jockey", true)
	}
	if group == nil {
		group = struct{}{}
		if c.Difficulty.Base == Hard && c.R.NextFloat() < 0.1*c.Difficulty.Special {
			m.set("effect", c.R.NextIntn(4))
		}
	}
	return group
}

type variantDef struct {
	name  string
	conds []variantCond
}

type variantCond struct {
	priority int
	kind     string
	biomes   []string // literal biomes or "#tag"
}

func (v variantDef) match(d *Data, biome string) (int, bool) {
	best, ok := 0, false
	for _, c := range v.conds {
		hit := false
		switch c.kind {
		case "":
			hit = true
		case "minecraft:biome":
			for _, b := range c.biomes {
				if t, isTag := strings.CutPrefix(b, "#"); isTag {
					hit = hit || d.biomeSet(t)[biome]
				} else {
					hit = hit || qualify(b) == biome
				}
			}
		}
		if hit && (!ok || c.priority > best) {
			best, ok = c.priority, true
		}
	}
	return best, ok
}

func parseVariant(name string, b []byte) (variantDef, error) {
	var j struct {
		SpawnConditions []struct {
			Priority  int `json:"priority"`
			Condition *struct {
				Type   string          `json:"type"`
				Biomes json.RawMessage `json:"biomes"`
			} `json:"condition"`
		} `json:"spawn_conditions"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return variantDef{}, err
	}
	v := variantDef{name: name}
	for _, sc := range j.SpawnConditions {
		c := variantCond{priority: sc.Priority}
		if sc.Condition != nil {
			c.kind = qualify(sc.Condition.Type)
			var one string
			if json.Unmarshal(sc.Condition.Biomes, &one) == nil {
				c.biomes = []string{one}
			} else {
				_ = json.Unmarshal(sc.Condition.Biomes, &c.biomes)
			}
		}
		v.conds = append(v.conds, c)
	}
	return v, nil
}

func init() {
	finalizers["minecraft:sheep"] = finalizeSheep
	finalizers["minecraft:cow"] = variantFinalizer("cow")
	finalizers["minecraft:pig"] = variantFinalizer("pig")
	finalizers["minecraft:chicken"] = variantFinalizer("chicken")
	finalizers["minecraft:horse"] = finalizeHorse
	finalizers["minecraft:spider"] = finalizeSpider
	finalizers["minecraft:squid"] = FinalizeAgeable
	finalizers["minecraft:glow_squid"] = FinalizeAgeable
	finalizers["minecraft:dolphin"] = FinalizeAgeable
}
