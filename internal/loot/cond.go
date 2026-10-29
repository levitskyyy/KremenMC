package loot

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// cond is a compiled loot item condition.
type cond struct {
	kind string

	terms []*cond // any_of, all_of, inverted (one term)

	chance     float32 // random_chance
	chanceN    number
	chances    []float32 // table_bonus
	ench       string    // table_bonus, random_chance_with_enchanted_bonus
	unench     float32
	enchanted  *levelValue
	item       *itemPred  // match_tool
	block      *blockPred // match_block, location_check
	entityRole string     // entity_properties
	entity     *entityPred
	damage     *damagePred
	off        [3]int32 // location_check
}

func parseConds(raw json.RawMessage) ([]*cond, error) {
	var out []*cond
	for _, r := range oneOrMany(raw) {
		c, err := parseCond(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func allConds(cs []*cond, ctx *Context) bool {
	for _, c := range cs {
		if !c.test(ctx) {
			return false
		}
	}
	return true
}

func parseCond(raw json.RawMessage) (*cond, error) {
	var ref string
	if json.Unmarshal(raw, &ref) == nil {
		pk, err := wgdata.Load()
		if err != nil {
			return nil, err
		}
		b, ok := pk.JSON("predicate", qualify(ref))
		if !ok {
			return nil, fmt.Errorf("unknown predicate %s", ref)
		}
		return parseCond(b)
	}
	var list []json.RawMessage
	if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &list) == nil {
		c := &cond{kind: "all_of"}
		for _, r := range list {
			t, err := parseCond(r)
			if err != nil {
				return nil, err
			}
			c.terms = append(c.terms, t)
		}
		return c, nil
	}
	var j struct {
		Terms       []json.RawMessage `json:"terms"`
		Term        json.RawMessage   `json:"term"`
		Chance      json.RawMessage   `json:"chance"`
		Chances     []float32         `json:"chances"`
		Enchantment string            `json:"enchantment"`
		Unench      float32           `json:"unenchanted_chance"`
		Enchanted   json.RawMessage   `json:"enchanted_chance"`
		Predicate   json.RawMessage   `json:"predicate"`
		Entity      string            `json:"entity"`
		Blocks      json.RawMessage   `json:"blocks"`
		State       json.RawMessage   `json:"state"`
		OffsetX     int32             `json:"offsetX"`
		OffsetY     int32             `json:"offsetY"`
		OffsetZ     int32             `json:"offsetZ"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	c := &cond{kind: typeOf(raw)}
	var err error
	switch c.kind {
	case "any_of", "all_of":
		for _, r := range j.Terms {
			t, err := parseCond(r)
			if err != nil {
				return nil, err
			}
			c.terms = append(c.terms, t)
		}
	case "inverted":
		t, err := parseCond(j.Term)
		if err != nil {
			return nil, err
		}
		c.terms = []*cond{t}
	case "survives_explosion", "killed_by_player":
	case "random_chance":
		if c.chanceN, err = parseNumber(j.Chance); err != nil {
			return nil, err
		}
	case "random_chance_with_enchanted_bonus":
		c.ench, c.unench = qualify(j.Enchantment), j.Unench
		if c.enchanted, err = parseLevelValue(j.Enchanted); err != nil {
			return nil, err
		}
	case "table_bonus":
		c.chances, c.ench = j.Chances, qualify(j.Enchantment)
	case "match_tool":
		if c.item, err = parseItemPred(j.Predicate); err != nil {
			return nil, err
		}
	case "match_block":
		if c.block, err = parseBlockPred(j.Blocks, j.State); err != nil {
			return nil, err
		}
	case "location_check":
		var lp struct {
			Block json.RawMessage `json:"block"`
		}
		if j.Predicate != nil {
			json.Unmarshal(j.Predicate, &lp)
		}
		if lp.Block != nil {
			var bp struct {
				Blocks json.RawMessage `json:"blocks"`
				State  json.RawMessage `json:"state"`
			}
			json.Unmarshal(lp.Block, &bp)
			if c.block, err = parseBlockPred(bp.Blocks, bp.State); err != nil {
				return nil, err
			}
		}
		c.off = [3]int32{j.OffsetX, j.OffsetY, j.OffsetZ}
	case "entity_properties":
		c.entityRole = j.Entity
		if c.entity, err = parseEntityPred(j.Predicate); err != nil {
			return nil, err
		}
	case "damage_source_properties":
		if c.damage, err = parseDamagePred(j.Predicate); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported condition %s", c.kind)
	}
	return c, nil
}

func (c *cond) test(ctx *Context) bool {
	switch c.kind {
	case "all_of":
		for _, t := range c.terms {
			if !t.test(ctx) {
				return false
			}
		}
		return true
	case "any_of":
		for _, t := range c.terms {
			if t.test(ctx) {
				return true
			}
		}
		return false
	case "inverted":
		return !c.terms[0].test(ctx)
	case "survives_explosion":
		if !ctx.HasExplosion {
			return true
		}
		return ctx.Rand.NextFloat() <= 1/ctx.ExplosionRadius
	case "killed_by_player":
		return ctx.LastPlayer != nil
	case "random_chance":
		return ctx.Rand.NextFloat() < c.chanceN.Float(ctx)
	case "random_chance_with_enchanted_bonus":
		lvl := attackerLevel(ctx, c.ench)
		ch := c.unench
		if lvl > 0 {
			ch = c.enchanted.eval(lvl)
		}
		return ctx.Rand.NextFloat() < ch
	case "table_bonus":
		lvl := int32(0)
		if ctx.HasTool {
			lvl = enchantLevel(ctx.Tool, c.ench)
		}
		ch := c.chances[min(int(lvl), len(c.chances)-1)]
		return ctx.Rand.NextFloat() < ch
	case "match_tool":
		return ctx.HasTool && c.item.test(ctx.Tool)
	case "match_block":
		return ctx.HasState && c.block.test(ctx.State)
	case "location_check":
		if !ctx.HasOrigin {
			return false
		}
		if c.block == nil {
			return true
		}
		if ctx.World == nil {
			return false
		}
		x := int(math.Floor(ctx.Origin.X)) + int(c.off[0])
		y := int(math.Floor(ctx.Origin.Y)) + int(c.off[1])
		z := int(math.Floor(ctx.Origin.Z)) + int(c.off[2])
		return c.block.test(ctx.World.Block(x, y, z))
	case "entity_properties":
		e := ctx.role(c.entityRole)
		return c.entity.test(e, ctx)
	case "damage_source_properties":
		return ctx.Damage != nil && c.damage.test(ctx.Damage, ctx)
	}
	return false
}

func (ctx *Context) role(r string) Entity {
	switch r {
	case "this":
		return ctx.This
	case "attacker":
		return ctx.Attacker
	case "direct_attacker":
		return ctx.DirectAttacker
	case "attacking_player", "last_damage_player":
		return ctx.LastPlayer
	}
	return nil
}

// blockPred matches a block (set) and state properties.
type blockPred struct {
	blocks map[data.BlockID]bool // nil means any
	props  []propMatch
}

type propMatch struct {
	name         string
	exact        bool
	val, lo, hi  string
	hasLo, hasHi bool
}

func parseBlockPred(blocks, state json.RawMessage) (*blockPred, error) {
	p := &blockPred{}
	if blocks != nil {
		names, err := resolveSet("block", blocks)
		if err != nil {
			return nil, err
		}
		p.blocks = map[data.BlockID]bool{}
		for _, n := range names {
			if b, ok := data.Canonical().BlockByName(n); ok {
				p.blocks[b.ID] = true
			}
		}
	}
	if state != nil {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(state, &m); err != nil {
			return nil, err
		}
		for k, v := range m {
			pm := propMatch{name: k}
			var s string
			if json.Unmarshal(v, &s) == nil {
				pm.exact, pm.val = true, s
			} else {
				var r struct {
					Min *json.RawMessage `json:"min"`
					Max *json.RawMessage `json:"max"`
				}
				json.Unmarshal(v, &r)
				if r.Min != nil {
					pm.lo, pm.hasLo = rawString(*r.Min), true
				}
				if r.Max != nil {
					pm.hi, pm.hasHi = rawString(*r.Max), true
				}
			}
			p.props = append(p.props, pm)
		}
	}
	return p, nil
}

func rawString(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}

func (p *blockPred) test(st data.StateID) bool {
	b := data.BlockOf(st)
	if b == nil {
		return false
	}
	if p.blocks != nil && !p.blocks[b.ID] {
		return false
	}
	for _, m := range p.props {
		pi := b.PropIndex(m.name)
		if pi < 0 {
			return false
		}
		pr := &b.Props[pi]
		v := b.ValueIndex(st, pi)
		if m.exact {
			if pr.Values[v] != m.val {
				return false
			}
			continue
		}
		if m.hasLo {
			if l := pr.ValueIndex(m.lo); l < 0 || v < l {
				return false
			}
		}
		if m.hasHi {
			if h := pr.ValueIndex(m.hi); h < 0 || v > h {
				return false
			}
		}
	}
	return true
}

func blockOfState(st data.StateID) *data.Block { return data.BlockOf(st) }

func stateValue(st data.StateID, prop string) (string, bool) {
	return data.Canonical().Value(st, prop)
}
