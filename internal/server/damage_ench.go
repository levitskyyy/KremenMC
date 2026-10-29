package server

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Data-driven enchantment effects (26.3 enchantment JSON). Only the effect
// components combat reads are evaluated; unknown requirement types are
// treated as not met.

type enchDef struct {
	name    string
	slots   []string
	effects map[string][]json.RawMessage
}

var (
	cEnchOnce sync.Once
	cEnchByID []*enchDef
)

func loadEnchantments() {
	var names []string
	for _, r := range regsync.Registries() {
		if r.Name == "minecraft:enchantment" {
			names = r.Entries
		}
	}
	p, err := wgdata.Load()
	cEnchByID = make([]*enchDef, len(names))
	for i, n := range names {
		d := &enchDef{name: n, effects: map[string][]json.RawMessage{}}
		cEnchByID[i] = d
		if err != nil {
			continue
		}
		raw, ok := p.JSON("enchantment", n)
		if !ok {
			continue
		}
		var j struct {
			Slots   []string                   `json:"slots"`
			Effects map[string]json.RawMessage `json:"effects"`
		}
		if json.Unmarshal(raw, &j) != nil {
			continue
		}
		d.slots = j.Slots
		for k, v := range j.Effects {
			var list []json.RawMessage
			if json.Unmarshal(v, &list) == nil {
				d.effects[ns(k)] = list
			}
		}
	}
}

func combatEnch(id int32) *enchDef {
	cEnchOnce.Do(loadEnchantments)
	if id < 0 || int(id) >= len(cEnchByID) {
		return nil
	}
	return cEnchByID[id]
}

// slotMatches is EquipmentSlotGroup.test for an enchantment slot group.
func slotMatches(group string, slot int) bool {
	switch group {
	case "any":
		return true
	case "mainhand":
		return slot == slotMain
	case "offhand":
		return slot == slotOff
	case "hand":
		return slot == slotMain || slot == slotOff
	case "armor":
		return slot >= slotFeet && slot <= slotHead
	case "feet":
		return slot == slotFeet
	case "legs":
		return slot == slotLegs
	case "chest":
		return slot == slotChest
	case "head":
		return slot == slotHead
	case "body":
		return slot == slotBody
	}
	return false
}

// Equipment slot indices (main hand, off hand, feet, legs, chest, head, body).
const (
	slotMain = iota
	slotOff
	slotFeet
	slotLegs
	slotChest
	slotHead
	slotBody
	numSlots
)

var slotNames = [numSlots]string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body"}

func (d *enchDef) activeIn(slot int) bool {
	for _, g := range d.slots {
		if slotMatches(g, slot) {
			return true
		}
	}
	return false
}

// levelBased evaluates a LevelBasedValue.
func levelBased(raw json.RawMessage, lvl int32) float32 {
	var f float32
	if json.Unmarshal(raw, &f) == nil {
		return f
	}
	var v struct {
		Type        string            `json:"type"`
		Base        float32           `json:"base"`
		PerLevel    float32           `json:"per_level_above_first"`
		Value       json.RawMessage   `json:"value"`
		Min         float32           `json:"min"`
		Max         float32           `json:"max"`
		Numerator   json.RawMessage   `json:"numerator"`
		Denominator json.RawMessage   `json:"denominator"`
		Added       float32           `json:"added"`
		Values      []json.RawMessage `json:"values"`
		Fallback    json.RawMessage   `json:"fallback"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	switch ns(v.Type) {
	case "minecraft:linear":
		return v.Base + v.PerLevel*float32(lvl-1)
	case "minecraft:clamped":
		return min(max(levelBased(v.Value, lvl), v.Min), v.Max)
	case "minecraft:fraction":
		d := levelBased(v.Denominator, lvl)
		if d == 0 {
			return 0
		}
		return levelBased(v.Numerator, lvl) / d
	case "minecraft:levels_squared":
		return float32(lvl*lvl) + v.Added
	case "minecraft:lookup":
		if int(lvl) <= len(v.Values) && lvl >= 1 {
			return levelBased(v.Values[lvl-1], lvl)
		}
		return levelBased(v.Fallback, lvl)
	}
	return 0
}

// valueEffect applies an EnchantmentValueEffect to x.
func valueEffect(raw json.RawMessage, lvl int32, x float32) float32 {
	var v struct {
		Type    string            `json:"type"`
		Value   json.RawMessage   `json:"value"`
		Factor  json.RawMessage   `json:"factor"`
		Chance  json.RawMessage   `json:"chance"`
		Effects []json.RawMessage `json:"effects"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return x
	}
	switch ns(v.Type) {
	case "minecraft:add":
		return x + levelBased(v.Value, lvl)
	case "minecraft:multiply":
		return x * levelBased(v.Factor, lvl)
	case "minecraft:set":
		return levelBased(v.Value, lvl)
	case "minecraft:remove_binomial":
		c := levelBased(v.Chance, lvl)
		n := x
		for i := float32(0); i < x; i++ {
			if rand.Float32() < c {
				n--
			}
		}
		return n
	case "minecraft:all_of":
		for _, e := range v.Effects {
			x = valueEffect(e, lvl, x)
		}
		return x
	}
	return x
}

// enchCtx is what enchantment requirements can look at.
type enchCtx struct {
	lvl      int32
	src      *DamageSource
	dt       *damageType
	this     combatant // the entity the effect is evaluated for ("this")
	attacker combatant
	victim   combatant
}

func (c *enchCtx) requirement(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var r struct {
		Type      string            `json:"type"`
		Predicate json.RawMessage   `json:"predicate"`
		Entity    string            `json:"entity"`
		Terms     []json.RawMessage `json:"terms"`
		Term      json.RawMessage   `json:"term"`
		Chance    json.RawMessage   `json:"chance"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	switch ns(r.Type) {
	case "minecraft:damage_source_properties":
		return c.damagePredicate(r.Predicate)
	case "minecraft:entity_properties":
		name := ""
		switch r.Entity {
		case "this":
			if c.this != nil {
				name = c.this.typeName()
			}
		case "attacker":
			if c.attacker != nil {
				name = c.attacker.typeName()
			}
		case "direct_attacker":
			if c.src != nil && c.src.Direct != nil {
				name = string(c.src.Direct.Type())
			}
		}
		return name != "" && entityPredicate(r.Predicate, name)
	case "minecraft:all_of":
		for _, t := range r.Terms {
			if !c.requirement(t) {
				return false
			}
		}
		return true
	case "minecraft:any_of":
		for _, t := range r.Terms {
			if c.requirement(t) {
				return true
			}
		}
		return false
	case "minecraft:inverted":
		return !c.requirement(r.Term)
	case "minecraft:random_chance":
		var ch struct {
			Type   string          `json:"type"`
			Amount json.RawMessage `json:"amount"`
		}
		var p float32
		if json.Unmarshal(r.Chance, &p) != nil {
			if json.Unmarshal(r.Chance, &ch) == nil && ns(ch.Type) == "minecraft:enchantment_level" {
				p = levelBased(ch.Amount, c.lvl)
			}
		}
		return rand.Float32() < p
	}
	return false
}

func (c *enchCtx) damagePredicate(raw json.RawMessage) bool {
	if c.dt == nil {
		return false
	}
	var p struct {
		Tags []struct {
			ID       string `json:"id"`
			Expected bool   `json:"expected"`
		} `json:"tags"`
		IsDirect *bool          `json:"is_direct"`
		Direct   map[string]any `json:"direct_entity"`
		Source   map[string]any `json:"source_entity"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	for _, t := range p.Tags {
		if c.dt.is(ns(strings.TrimPrefix(t.ID, "#"))) != t.Expected {
			return false
		}
	}
	if p.IsDirect != nil && c.src.isDirect() != *p.IsDirect {
		return false
	}
	return p.Direct == nil && p.Source == nil
}

// entityPredicate supports the entity type sub-predicate.
func entityPredicate(raw json.RawMessage, typeName string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for k, v := range m {
		switch k {
		case "type", "minecraft:entity_type":
			if !matchHolder("entity_type", v, typeName) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// eachEnchant calls fn for every enchantment active in the entity's
// equipment slots (for slot-filtered effects) or on stack alone.
func eachEnchant(eq [numSlots]item.Stack, fn func(d *enchDef, lvl int32, slot int)) {
	for slot, st := range eq {
		if st.IsEmpty() {
			continue
		}
		for _, e := range st.Enchantments() {
			d := combatEnch(e.ID)
			if d != nil && d.activeIn(slot) {
				fn(d, e.Level, slot)
			}
		}
	}
}

// conditionalValue runs the value effects of component in d whose
// requirements hold.
func (c *enchCtx) conditionalValue(d *enchDef, component string, x float32) float32 {
	for _, raw := range d.effects[component] {
		var ce struct {
			Effect       json.RawMessage `json:"effect"`
			Requirements json.RawMessage `json:"requirements"`
		}
		if json.Unmarshal(raw, &ce) != nil || !c.requirement(ce.Requirements) {
			continue
		}
		x = valueEffect(ce.Effect, c.lvl, x)
	}
	return x
}

// enchantDamageProtection sums damage_protection over the victim's gear.
func enchantDamageProtection(v combatant, src *DamageSource, dt *damageType) float32 {
	var total float32
	eachEnchant(v.gear(), func(d *enchDef, lvl int32, slot int) {
		c := enchCtx{lvl: lvl, src: src, dt: dt, this: v, victim: v, attacker: src.attacker()}
		total = c.conditionalValue(d, "minecraft:damage_protection", total)
	})
	return total
}

// weaponValue evaluates a weapon-scoped component (damage, knockback,
// armor_effectiveness, smash_damage_per_fallen_block) on stack against victim.
func weaponValue(st item.Stack, component string, attacker, victim combatant, src *DamageSource, x float32) float32 {
	var dt *damageType
	if src != nil {
		dt = damageTypeOf(src.Type)
	}
	for _, e := range st.Enchantments() {
		d := combatEnch(e.ID)
		if d == nil {
			continue
		}
		c := enchCtx{lvl: e.Level, src: src, dt: dt, this: victim, victim: victim, attacker: attacker}
		x = c.conditionalValue(d, component, x)
	}
	return x
}

// enchantAttributes returns the attribute modifiers granted by the
// enchantments of st in slot (the minecraft:attributes component).
func enchantAttributes(st item.Stack, slot int, emit func(attr string, id string, amount float64, op string)) {
	for _, e := range st.Enchantments() {
		d := combatEnch(e.ID)
		if d == nil || !d.activeIn(slot) {
			continue
		}
		for _, raw := range d.effects["minecraft:attributes"] {
			var a struct {
				Attribute string          `json:"attribute"`
				ID        string          `json:"id"`
				Amount    json.RawMessage `json:"amount"`
				Operation string          `json:"operation"`
			}
			if json.Unmarshal(raw, &a) != nil {
				continue
			}
			id := ns(a.ID) + "." + slotNames[slot]
			emit(a.Attribute, id, float64(levelBased(a.Amount, e.Level)), a.Operation)
		}
	}
}

// postAttack runs post_attack effects of d (enchanted entity's role
// "attacker" or "victim") that we model: ignite and damage_entity.
func postAttack(eq [numSlots]item.Stack, role string, attacker, victim combatant, src *DamageSource) {
	dt := damageTypeOf(src.Type)
	eachEnchant(eq, func(d *enchDef, lvl int32, slot int) {
		for _, raw := range d.effects["minecraft:post_attack"] {
			var pa struct {
				Enchanted    string          `json:"enchanted"`
				Affected     string          `json:"affected"`
				Effect       json.RawMessage `json:"effect"`
				Requirements json.RawMessage `json:"requirements"`
			}
			if json.Unmarshal(raw, &pa) != nil || pa.Enchanted != role {
				continue
			}
			c := enchCtx{lvl: lvl, src: src, dt: dt, this: victim, attacker: attacker, victim: victim}
			if !c.requirement(pa.Requirements) {
				continue
			}
			target := victim
			if pa.Affected == "attacker" {
				target = attacker
			}
			if target == nil {
				continue
			}
			entityEffect(pa.Effect, lvl, target, attacker, victim)
		}
	})
}

func entityEffect(raw json.RawMessage, lvl int32, target, attacker, victim combatant) {
	var e struct {
		Type       string            `json:"type"`
		Duration   json.RawMessage   `json:"duration"`
		DamageType string            `json:"damage_type"`
		MinDamage  json.RawMessage   `json:"min_damage"`
		MaxDamage  json.RawMessage   `json:"max_damage"`
		Effects    []json.RawMessage `json:"effects"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	switch ns(e.Type) {
	case "minecraft:all_of":
		for _, x := range e.Effects {
			entityEffect(x, lvl, target, attacker, victim)
		}
	case "minecraft:ignite":
		target.lv().ignite(int32(levelBased(e.Duration, lvl) * 20))
	case "minecraft:damage_entity":
		lo, hi := levelBased(e.MinDamage, lvl), levelBased(e.MaxDamage, lvl)
		amount := lo + rand.Float32()*(hi-lo)
		src := DamageSource{Type: e.DamageType}
		if victim != nil && target != victim {
			src.Direct, src.Causing = victim, victim
		}
		hurt(target, src, float32(math.Floor(float64(amount)*1000)/1000))
	}
}
