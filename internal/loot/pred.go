package loot

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// resolveSet expands an id, "#tag" or list of either into namespaced
// entry names of registry reg ("block", "item", "entity_type", ...).
func resolveSet(reg string, raw json.RawMessage) ([]string, error) {
	var list []string
	var one string
	if json.Unmarshal(raw, &one) == nil {
		list = []string{one}
	} else if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range list {
		if t, ok := strings.CutPrefix(s, "#"); ok {
			names, ok := pk.Tag(reg, qualify(t))
			if !ok {
				return nil, fmt.Errorf("unknown %s tag %s", reg, s)
			}
			out = append(out, names...)
			continue
		}
		out = append(out, qualify(s))
	}
	for i, n := range out {
		out[i] = strings.TrimPrefix(n, "minecraft:")
	}
	return out, nil
}

func nameSet(reg string, raw json.RawMessage) (map[string]bool, error) {
	names, err := resolveSet(reg, raw)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

// enchantLevel is EnchantmentHelper.getItemEnchantmentLevel.
func enchantLevel(st item.Stack, ench string) int32 {
	if st.IsEmpty() {
		return 0
	}
	id, ok := regsync.Index("minecraft:enchantment", ench)
	if !ok {
		return 0
	}
	for _, e := range st.Enchantments() {
		if e.ID == id {
			return e.Level
		}
	}
	return 0
}

var equipSlots = [...]string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body"}

// entityLevel is EnchantmentHelper.getEnchantmentLevel(enchantment, entity):
// the highest level among the entity's equipment.
func entityLevel(e Entity, ench string) int32 {
	if e == nil {
		return 0
	}
	lvl := int32(0)
	for _, s := range equipSlots {
		lvl = max(lvl, enchantLevel(e.Equipment(s), ench))
	}
	return lvl
}

func attackerLevel(ctx *Context, ench string) int32 { return entityLevel(ctx.Attacker, ench) }

// minMax is MinMaxBounds.Ints / Doubles.
type minMax struct {
	lo, hi       float64
	hasLo, hasHi bool
}

func parseMinMax(raw json.RawMessage) (minMax, error) {
	var v float64
	if json.Unmarshal(raw, &v) == nil {
		return minMax{v, v, true, true}, nil
	}
	var j struct {
		Min *float64 `json:"min"`
		Max *float64 `json:"max"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return minMax{}, err
	}
	m := minMax{}
	if j.Min != nil {
		m.lo, m.hasLo = *j.Min, true
	}
	if j.Max != nil {
		m.hi, m.hasHi = *j.Max, true
	}
	return m, nil
}

func (m minMax) ok(v float64) bool {
	return (!m.hasLo || v >= m.lo) && (!m.hasHi || v <= m.hi)
}

// itemPred is ItemPredicate.
type itemPred struct {
	items    map[string]bool
	count    *minMax
	enchants []enchPred
	never    bool
}

type enchPred struct {
	set    map[string]bool // nil: any
	levels *minMax
}

func parseItemPred(raw json.RawMessage) (*itemPred, error) {
	p := &itemPred{}
	if raw == nil {
		return p, nil
	}
	var j map[string]json.RawMessage
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	for k, v := range j {
		switch k {
		case "items":
			s, err := nameSet("item", v)
			if err != nil {
				return nil, err
			}
			p.items = s
		case "count":
			m, err := parseMinMax(v)
			if err != nil {
				return nil, err
			}
			p.count = &m
		case "predicates":
			var sub map[string]json.RawMessage
			if err := json.Unmarshal(v, &sub); err != nil {
				return nil, err
			}
			for sk, sv := range sub {
				if sk != "minecraft:enchantments" {
					p.never = true
					continue
				}
				var es []struct {
					Enchantments json.RawMessage `json:"enchantments"`
					Levels       json.RawMessage `json:"levels"`
				}
				if err := json.Unmarshal(sv, &es); err != nil {
					return nil, err
				}
				for _, e := range es {
					ep := enchPred{}
					if e.Enchantments != nil {
						s, err := nameSet("enchantment", e.Enchantments)
						if err != nil {
							return nil, err
						}
						ep.set = s
					}
					if e.Levels != nil {
						m, err := parseMinMax(e.Levels)
						if err != nil {
							return nil, err
						}
						ep.levels = &m
					}
					p.enchants = append(p.enchants, ep)
				}
			}
		default:
			p.never = true
		}
	}
	return p, nil
}

func (p *itemPred) test(st item.Stack) bool {
	if p.never {
		return false
	}
	if p.items != nil && (st.IsEmpty() || !p.items[strings.TrimPrefix(item.NameOf(st.Item), "minecraft:")]) {
		return false
	}
	if p.count != nil && !p.count.ok(float64(st.Count)) {
		return false
	}
	if len(p.enchants) == 0 {
		return true
	}
	have := st.Enchantments()
	for _, ep := range p.enchants {
		found := false
		for _, e := range have {
			name := enchName(e.ID)
			if ep.set != nil && !ep.set[name] {
				continue
			}
			if ep.levels != nil && !ep.levels.ok(float64(e.Level)) {
				continue
			}
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func enchName(id int32) string {
	for _, r := range regsync.Registries() {
		if r.Name == "minecraft:enchantment" {
			if int(id) < len(r.Entries) && id >= 0 {
				return strings.TrimPrefix(r.Entries[id], "minecraft:")
			}
		}
	}
	return ""
}

// entityPred is EntityPredicate (26.3 sub-predicate map form).
type entityPred struct {
	types      map[string]bool
	flags      map[string]bool
	equipment  map[string]*itemPred
	components map[string]any
	specific   []specificPred
	vehicle    *entityPred
	never      bool
}

type specificPred struct {
	kind, field string
	val         any
	rng         *minMax
}

func parseEntityPred(raw json.RawMessage) (*entityPred, error) {
	p := &entityPred{}
	if raw == nil {
		return p, nil
	}
	var j map[string]json.RawMessage
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	for k, v := range j {
		k = strings.TrimPrefix(k, "minecraft:")
		switch {
		case k == "entity_type" || k == "type":
			s, err := nameSet("entity_type", v)
			if err != nil {
				return nil, err
			}
			p.types = s
		case k == "flags":
			if err := json.Unmarshal(v, &p.flags); err != nil {
				return nil, err
			}
		case k == "equipment":
			var m map[string]json.RawMessage
			if err := json.Unmarshal(v, &m); err != nil {
				return nil, err
			}
			p.equipment = map[string]*itemPred{}
			for slot, ip := range m {
				x, err := parseItemPred(ip)
				if err != nil {
					return nil, err
				}
				p.equipment[slot] = x
			}
		case k == "components":
			if err := json.Unmarshal(v, &p.components); err != nil {
				return nil, err
			}
		case strings.HasPrefix(k, "type_specific/"):
			var m map[string]json.RawMessage
			if err := json.Unmarshal(v, &m); err != nil {
				return nil, err
			}
			kind := strings.TrimPrefix(k, "type_specific/")
			for f, fv := range m {
				sp := specificPred{kind: kind, field: f}
				var b bool
				if json.Unmarshal(fv, &b) == nil {
					sp.val = b
				} else if mm, err := parseMinMax(fv); err == nil {
					sp.rng = &mm
				} else {
					json.Unmarshal(fv, &sp.val)
				}
				p.specific = append(p.specific, sp)
			}
		case k == "vehicle":
			x, err := parseEntityPred(v)
			if err != nil {
				return nil, err
			}
			p.vehicle = x
		default:
			p.never = true
		}
	}
	return p, nil
}

func (p *entityPred) test(e Entity, ctx *Context) bool {
	if e == nil || p.never {
		return false
	}
	if p.types != nil && !p.types[strings.TrimPrefix(e.TypeName(), "minecraft:")] {
		return false
	}
	for f, want := range p.flags {
		if e.Flag(f) != want {
			return false
		}
	}
	for slot, ip := range p.equipment {
		if !ip.test(e.Equipment(slot)) {
			return false
		}
	}
	for name, want := range p.components {
		raw, ok := e.Component(qualify(name))
		if !ok {
			return false
		}
		var got any
		if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(normJSON(got), normJSON(want)) {
			return false
		}
	}
	for _, sp := range p.specific {
		v, ok := e.TypeSpecific(sp.kind, sp.field)
		if !ok {
			return false
		}
		if sp.rng != nil {
			f, ok := toFloat(v)
			if !ok || !sp.rng.ok(f) {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(v, sp.val) {
			return false
		}
	}
	if p.vehicle != nil && !p.vehicle.test(e.Vehicle(), ctx) {
		return false
	}
	return true
}

func normJSON(v any) any {
	if s, ok := v.(string); ok {
		return strings.TrimPrefix(s, "minecraft:")
	}
	return v
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	return 0, false
}

// damagePred is DamageSourcePredicate.
type damagePred struct {
	tags     []tagPred
	source   *entityPred
	direct   *entityPred
	isDirect *bool
}

type tagPred struct {
	id       string
	expected bool
}

func parseDamagePred(raw json.RawMessage) (*damagePred, error) {
	var j struct {
		Tags []struct {
			ID       string `json:"id"`
			Expected bool   `json:"expected"`
		} `json:"tags"`
		Source   json.RawMessage `json:"source_entity"`
		Direct   json.RawMessage `json:"direct_entity"`
		IsDirect *bool           `json:"is_direct"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	p := &damagePred{isDirect: j.IsDirect}
	for _, t := range j.Tags {
		p.tags = append(p.tags, tagPred{qualify(strings.TrimPrefix(t.ID, "#")), t.Expected})
	}
	var err error
	if j.Source != nil {
		if p.source, err = parseEntityPred(j.Source); err != nil {
			return nil, err
		}
	}
	if j.Direct != nil {
		if p.direct, err = parseEntityPred(j.Direct); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *damagePred) test(d *DamageSource, ctx *Context) bool {
	for _, t := range p.tags {
		has := d.Tags != nil && d.Tags(t.id)
		if has != t.expected {
			return false
		}
	}
	if p.source != nil && !p.source.test(d.Source, ctx) {
		return false
	}
	if p.direct != nil && !p.direct.test(d.Direct, ctx) {
		return false
	}
	if p.isDirect != nil && (d.Source != nil && d.Source == d.Direct) != *p.isDirect {
		return false
	}
	return true
}
