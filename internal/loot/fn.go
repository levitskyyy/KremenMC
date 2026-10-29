package loot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// modifier is a LootItemFunction.
type modifier struct {
	kind  string
	conds []*cond

	count   number // set_count, enchanted_count_increase, set_ominous_bottle_amplifier
	add     bool
	limit   int32 // enchanted_count_increase
	rng     intRange
	ench    string
	formula string
	extra   int32
	prob    float32
	mult    int32
	include []item.Type
	block   string
	props   []string
	potion  int32

	enchs      []randomEnch // enchant_randomly
	compatOnly bool
}

func parseModifiers(raw json.RawMessage) ([]*modifier, error) {
	var out []*modifier
	for _, r := range oneOrMany(raw) {
		m, err := parseModifier(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func parseModifier(raw json.RawMessage) (*modifier, error) {
	var j struct {
		Condition   json.RawMessage `json:"condition"`
		Count       json.RawMessage `json:"count"`
		Add         bool            `json:"add"`
		Limit       json.RawMessage `json:"limit"`
		Enchantment string          `json:"enchantment"`
		Formula     string          `json:"formula"`
		Parameters  struct {
			Extra       int32   `json:"extra"`
			Probability float32 `json:"probability"`
			Multiplier  int32   `json:"bonusMultiplier"`
		} `json:"parameters"`
		Include    []string        `json:"include"`
		Source     string          `json:"source"`
		Block      string          `json:"block"`
		Properties []string        `json:"properties"`
		ID         string          `json:"id"`
		Amplifier  json.RawMessage `json:"amplifier"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	m := &modifier{kind: typeOf(raw)}
	var err error
	if m.conds, err = parseConds(j.Condition); err != nil {
		return nil, err
	}
	switch m.kind {
	case "set_count":
		if m.count, err = parseNumber(j.Count); err != nil {
			return nil, err
		}
		m.add = j.Add
	case "apply_bonus":
		m.ench, m.formula = qualify(j.Enchantment), strings.TrimPrefix(j.Formula, "minecraft:")
		m.extra, m.prob, m.mult = j.Parameters.Extra, j.Parameters.Probability, j.Parameters.Multiplier
		switch m.formula {
		case "ore_drops", "uniform_bonus_count", "binomial_with_bonus_count":
		default:
			return nil, fmt.Errorf("unsupported apply_bonus formula %s", j.Formula)
		}
	case "explosion_decay", "furnace_smelt":
	case "limit_count":
		if m.rng, err = parseIntRange(j.Limit); err != nil {
			return nil, err
		}
	case "enchanted_count_increase":
		m.ench = qualify(j.Enchantment)
		if m.count, err = parseNumber(j.Count); err != nil {
			return nil, err
		}
		if j.Limit != nil {
			json.Unmarshal(j.Limit, &m.limit)
		}
	case "copy_components":
		if j.Source != "block_entity" {
			return nil, fmt.Errorf("unsupported copy_components source %s", j.Source)
		}
		for _, n := range j.Include {
			t, ok := item.TypeByName(strings.TrimPrefix(n, "minecraft:"))
			if !ok {
				t, ok = item.TypeByName(n)
			}
			if !ok {
				return nil, fmt.Errorf("unknown component %s", n)
			}
			m.include = append(m.include, t)
		}
	case "copy_state":
		m.block, m.props = strings.TrimPrefix(j.Block, "minecraft:"), j.Properties
	case "set_potion":
		id, ok := item.BuiltinID("potion", strings.TrimPrefix(j.ID, "minecraft:"))
		if !ok {
			return nil, fmt.Errorf("unknown potion %s", j.ID)
		}
		m.potion = id
	case "set_ominous_bottle_amplifier":
		if m.count, err = parseNumber(j.Amplifier); err != nil {
			return nil, err
		}
	case "enchant_randomly":
		if err := parseEnchantRandomly(m, raw); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported modifier %s", m.kind)
	}
	return m, nil
}

// apply is LootItemConditionalFunction.apply.
func (m *modifier) apply(s item.Stack, ctx *Context) item.Stack {
	if !allConds(m.conds, ctx) {
		return s
	}
	switch m.kind {
	case "set_count":
		n := m.count.Int(ctx)
		if m.add {
			n += s.Count
		}
		return limitSize(s, n)
	case "apply_bonus":
		if !ctx.HasTool {
			return s
		}
		lvl := enchantLevel(ctx.Tool, m.ench)
		n := s.Count
		switch m.formula {
		case "ore_drops":
			if lvl > 0 {
				k := ctx.Rand.NextIntn(lvl+2) - 1
				if k < 0 {
					k = 0
				}
				n *= k + 1
			}
		case "uniform_bonus_count":
			n += ctx.Rand.NextIntn(m.mult*lvl + 1)
		case "binomial_with_bonus_count":
			for range lvl + m.extra {
				if ctx.Rand.NextFloat() < m.prob {
					n++
				}
			}
		}
		return withCount(s, n)
	case "explosion_decay":
		if !ctx.HasExplosion {
			return s
		}
		p := 1 / ctx.ExplosionRadius
		k := int32(0)
		for range s.Count {
			if ctx.Rand.NextFloat() <= p {
				k++
			}
		}
		return withCount(s, k)
	case "limit_count":
		return withCount(s, m.rng.clamp(ctx, s.Count))
	case "enchanted_count_increase":
		lvl := entityLevel(ctx.Attacker, m.ench)
		if lvl == 0 {
			return s
		}
		f := m.count.Float(ctx) * float32(lvl)
		n := s.Count + roundF(f)
		if m.limit > 0 {
			n = min(n, m.limit)
		}
		return withCount(s, n)
	case "furnace_smelt":
		if s.IsEmpty() {
			return s
		}
		if out, ok := smeltResult(s.Item); ok {
			return item.Stack{Item: out.Item, Count: s.Count * out.Count, Patch: out.Patch}
		}
		return s
	case "copy_components":
		if ctx.BlockEntity == nil {
			return s
		}
		comps := ctx.BlockEntity.Components()
		for _, t := range m.include {
			if v, ok := comps[t]; ok {
				s = setComp(s, t, v)
			}
		}
		return s
	case "copy_state":
		if !ctx.HasState {
			return s
		}
		return copyState(s, ctx, m)
	case "set_potion":
		return setComp(s, item.PotionContents, item.EncodePotionContents(item.PotionInfo{Potion: m.potion}))
	case "enchant_randomly":
		return enchantRandomly(s, ctx, m)
	case "set_ominous_bottle_amplifier":
		a := min(max(m.count.Int(ctx), 0), 4)
		return setComp(s, item.OminousBottleAmplifier, item.EncodeVarInt(a))
	}
	return s
}

// limitSize is ItemStack.setCount followed by limitSize. Counts may drop
// to zero or below while the item is kept, as in the reference, where an
// empty stack still remembers its item until it leaves the loot pipeline.
func limitSize(s item.Stack, n int32) item.Stack {
	return withCount(s, max(min(n, maxStack(s)), 0))
}

func maxStack(s item.Stack) int32 {
	s.Count = 1
	return s.MaxStackSize()
}

func withCount(s item.Stack, n int32) item.Stack {
	s.Count = n
	return s
}

func copyState(s item.Stack, ctx *Context, m *modifier) item.Stack {
	b := blockOfState(ctx.State)
	if b == nil || b.Name != m.block {
		return s
	}
	type kv struct{ k, v string }
	var cur []kv
	if v, ok := s.Component(item.BlockState); ok {
		r := proto.Reader{B: v}
		n, _ := r.VarInt()
		for range n {
			k, _ := r.String(32767)
			x, _ := r.String(32767)
			cur = append(cur, kv{k, x})
		}
	}
	for _, p := range m.props {
		val, ok := stateValue(ctx.State, p)
		if !ok {
			continue
		}
		replaced := false
		for i := range cur {
			if cur[i].k == p {
				cur[i].v, replaced = val, true
			}
		}
		if !replaced {
			cur = append(cur, kv{p, val})
		}
	}
	if def := protoBlockState(s.Item); def != nil && len(def) == len(cur) {
		same := true
		for _, e := range cur {
			if v, ok := def[e.k]; !ok || v != e.v {
				same = false
			}
		}
		if same {
			s.Patch = s.Patch.Forget(item.BlockState)
			return s
		}
	}
	b2 := proto.AppendVarInt(nil, int32(len(cur)))
	for _, e := range cur {
		b2 = appendString(b2, e.k)
		b2 = appendString(b2, e.v)
	}
	return setComp(s, item.BlockState, b2)
}

var (
	smeltOnce sync.Once
	smelt     map[int32]item.Stack
)

// smeltResult looks up the minecraft:smelting recipe for an input item.
func smeltResult(in int32) (item.Stack, bool) {
	smeltOnce.Do(func() {
		smelt = map[int32]item.Stack{}
		pk, err := wgdata.Load()
		if err != nil {
			return
		}
		for _, k := range pk.Keys("recipe") {
			raw, _ := pk.JSON("recipe", k)
			var r struct {
				Type       string          `json:"type"`
				Ingredient json.RawMessage `json:"ingredient"`
				Result     struct {
					ID    string `json:"id"`
					Count int32  `json:"count"`
				} `json:"result"`
			}
			if json.Unmarshal(raw, &r) != nil || r.Type != "minecraft:smelting" {
				continue
			}
			out, ok := item.IDOf(r.Result.ID)
			if !ok {
				continue
			}
			n := r.Result.Count
			if n == 0 {
				n = 1
			}
			names, err := resolveSet("item", r.Ingredient)
			if err != nil {
				continue
			}
			for _, nm := range names {
				if id, ok := item.IDOf(nm); ok {
					if _, dup := smelt[id]; !dup {
						smelt[id] = item.Stack{Item: out, Count: n}
					}
				}
			}
		}
	})
	s, ok := smelt[in]
	return s, ok
}

func appendString(b []byte, s string) []byte {
	return append(proto.AppendVarInt(b, int32(len(s))), s...)
}

// setComp is ItemStack.set: a value equal to the prototype's leaves no
// patch entry.
func setComp(s item.Stack, t item.Type, v []byte) item.Stack {
	if d, ok := item.Defaults(s.Item).Wire(t); ok && bytes.Equal(d, v) {
		s.Patch = s.Patch.Forget(t)
		return s
	}
	return s.Set(t, v)
}

// protoBlockState is the item's default minecraft:block_state component.
func protoBlockState(id int32) map[string]string {
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	raw, ok := pk.Report("minecraft/components/item/" + strings.TrimPrefix(item.NameOf(id), "minecraft:") + ".json")
	if !ok {
		return nil
	}
	var doc struct {
		Components struct {
			BlockState map[string]string `json:"minecraft:block_state"`
		} `json:"components"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	return doc.Components.BlockState
}
