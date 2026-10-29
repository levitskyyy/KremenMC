package surface

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Registry resolves data-pack JSON by registry and key (wgdata.Pack).
type Registry interface {
	JSON(registry, key string) ([]byte, bool)
}

type ruleKind uint8

const (
	rBlock ruleKind = iota
	rBandlands
	rSequence
	rCondition
	rOreVein
)

// Rule is a parsed material rule.
type Rule struct {
	Kind     ruleKind
	State    string // block: result_state as a normalized state string
	Sequence []*Rule
	If       *Condition
	Then     *Rule
	Ore      *OreVein
}

// OreVein is the ore_vein rule's configuration.
type OreVein struct {
	OreBlock, RawOreBlock, FillerBlock string
	RawOreChance                       float32
	Density, Richness, FillerGap       json.RawMessage
}

type condKind uint8

const (
	cBiome condKind = iota
	cNoiseThreshold
	cVerticalGradient
	cYAbove
	cWater
	cTemperature
	cSteep
	cNot
	cHole
	cAbovePreliminarySurface
	cStoneDepth
)

// Anchor is a vertical anchor.
type Anchor struct {
	Kind  uint8 // 0 absolute, 1 above_bottom, 2 below_top
	Value int32
}

func (a Anchor) resolve(minY, height int32) int32 {
	switch a.Kind {
	case 1:
		return minY + a.Value
	case 2:
		return minY + height - 1 - a.Value
	}
	return a.Value
}

// Condition is a parsed material condition.
type Condition struct {
	Kind condKind

	Biomes []string // biome

	Noise    string // noise_threshold
	Min, Max float64
	Is3D     bool

	RandomName      string // vertical_gradient
	TrueAt, FalseAt Anchor

	Anchor                 Anchor // y_above
	Offset                 int32  // water, stone_depth
	SurfaceDepthMultiplier int32  // y_above, water
	AddStoneDepth          bool   // y_above, water
	AddSurfaceDepth        bool   // stone_depth
	SecondaryDepthRange    int32  // stone_depth
	Ceiling                bool   // stone_depth surface_type
	Invert                 *Condition
}

type parser struct {
	reg   Registry
	rules map[string]*Rule
	conds map[string]*Condition
	stack map[string]bool
}

// ParseRule parses a material rule (an inline object or a registry
// reference) and every rule and condition it references.
func ParseRule(reg Registry, raw json.RawMessage) (*Rule, error) {
	p := &parser{reg: reg, rules: map[string]*Rule{}, conds: map[string]*Condition{}, stack: map[string]bool{}}
	return p.rule(raw)
}

func qualify(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

func (p *parser) ref(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return qualify(s), true
	}
	return "", false
}

func (p *parser) rule(raw json.RawMessage) (*Rule, error) {
	if key, ok := p.ref(raw); ok {
		if r, ok := p.rules[key]; ok {
			return r, nil
		}
		if p.stack["r"+key] {
			return nil, fmt.Errorf("surface: material rule cycle at %s", key)
		}
		b, ok := p.reg.JSON("worldgen/material_rule", key)
		if !ok {
			return nil, fmt.Errorf("surface: unknown material rule %s", key)
		}
		p.stack["r"+key] = true
		r, err := p.rule(b)
		delete(p.stack, "r"+key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		p.rules[key] = r
		return r, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("surface: material rule: %w", err)
	}
	var typ string
	_ = json.Unmarshal(m["type"], &typ)
	r := &Rule{}
	switch qualify(typ) {
	case "minecraft:block":
		r.Kind = rBlock
		s, err := parseState(m["result_state"])
		if err != nil {
			return nil, err
		}
		r.State = s
	case "minecraft:bandlands":
		r.Kind = rBandlands
	case "minecraft:sequence":
		r.Kind = rSequence
		var seq []json.RawMessage
		if err := json.Unmarshal(m["sequence"], &seq); err != nil {
			return nil, fmt.Errorf("surface: sequence: %w", err)
		}
		for _, e := range seq {
			c, err := p.rule(e)
			if err != nil {
				return nil, err
			}
			r.Sequence = append(r.Sequence, c)
		}
	case "minecraft:condition":
		r.Kind = rCondition
		c, err := p.cond(m["if_true"])
		if err != nil {
			return nil, err
		}
		t, err := p.rule(m["then_run"])
		if err != nil {
			return nil, err
		}
		r.If, r.Then = c, t
	case "minecraft:ore_vein":
		r.Kind = rOreVein
		o := &OreVein{Density: m["density"], Richness: m["richness"], FillerGap: m["filler_gap"]}
		var err error
		if o.OreBlock, err = parseState(m["ore_block"]); err != nil {
			return nil, err
		}
		if o.RawOreBlock, err = parseState(m["raw_ore_block"]); err != nil {
			return nil, err
		}
		if o.FillerBlock, err = parseState(m["filler_block"]); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(m["raw_ore_chance"], &o.RawOreChance); err != nil {
			return nil, fmt.Errorf("surface: ore_vein raw_ore_chance: %w", err)
		}
		r.Ore = o
	default:
		return nil, fmt.Errorf("surface: unknown material rule type %q", typ)
	}
	return r, nil
}

// parseState accepts a block state as "name" or {"Name":..., "Properties":{...}}
// and returns "minecraft:name[k=v,...]" (properties as given).
func parseState(raw json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return qualify(s), nil
	}
	var o struct {
		Name       string            `json:"Name"`
		Properties map[string]string `json:"Properties"`
	}
	if err := json.Unmarshal(raw, &o); err != nil || o.Name == "" {
		return "", fmt.Errorf("surface: bad block state %s", raw)
	}
	if len(o.Properties) == 0 {
		return qualify(o.Name), nil
	}
	var b strings.Builder
	b.WriteString(qualify(o.Name))
	b.WriteByte('[')
	first := true
	for k, v := range o.Properties {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(k + "=" + v)
	}
	b.WriteByte(']')
	return b.String(), nil
}

func parseAnchor(raw json.RawMessage) (Anchor, error) {
	var m map[string]int32
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 1 {
		return Anchor{}, fmt.Errorf("surface: bad vertical anchor %s", raw)
	}
	for k, v := range m {
		switch k {
		case "absolute":
			return Anchor{0, v}, nil
		case "above_bottom":
			return Anchor{1, v}, nil
		case "below_top":
			return Anchor{2, v}, nil
		}
	}
	return Anchor{}, fmt.Errorf("surface: bad vertical anchor %s", raw)
}

func (p *parser) cond(raw json.RawMessage) (*Condition, error) {
	if key, ok := p.ref(raw); ok {
		if c, ok := p.conds[key]; ok {
			return c, nil
		}
		if p.stack["c"+key] {
			return nil, fmt.Errorf("surface: material condition cycle at %s", key)
		}
		b, ok := p.reg.JSON("worldgen/material_condition", key)
		if !ok {
			return nil, fmt.Errorf("surface: unknown material condition %s", key)
		}
		p.stack["c"+key] = true
		c, err := p.cond(b)
		delete(p.stack, "c"+key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		p.conds[key] = c
		return c, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("surface: material condition: %w", err)
	}
	var typ string
	_ = json.Unmarshal(m["type"], &typ)
	c := &Condition{}
	get := func(k string, v any) error {
		b, ok := m[k]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(b, v); err != nil {
			return fmt.Errorf("surface: %s.%s: %w", typ, k, err)
		}
		return nil
	}
	var err error
	switch qualify(typ) {
	case "minecraft:biome":
		c.Kind = cBiome
		var one string
		if json.Unmarshal(m["biome_is"], &one) == nil {
			if strings.HasPrefix(one, "#") {
				return nil, fmt.Errorf("surface: biome tags are not supported (%s)", one)
			}
			c.Biomes = []string{qualify(one)}
		} else {
			var list []string
			if err := json.Unmarshal(m["biome_is"], &list); err != nil {
				return nil, fmt.Errorf("surface: biome_is: %w", err)
			}
			for _, b := range list {
				c.Biomes = append(c.Biomes, qualify(b))
			}
		}
	case "minecraft:noise_threshold":
		c.Kind = cNoiseThreshold
		c.Max = math.MaxFloat64
		if err = get("noise", &c.Noise); err == nil {
			if err = get("min_threshold", &c.Min); err == nil {
				if err = get("max_threshold", &c.Max); err == nil {
					err = get("is_3d", &c.Is3D)
				}
			}
		}
		c.Noise = qualify(c.Noise)
	case "minecraft:vertical_gradient":
		c.Kind = cVerticalGradient
		if err = get("random_name", &c.RandomName); err != nil {
			return nil, err
		}
		c.RandomName = qualify(c.RandomName)
		if c.TrueAt, err = parseAnchor(m["true_at_and_below"]); err != nil {
			return nil, err
		}
		c.FalseAt, err = parseAnchor(m["false_at_and_above"])
	case "minecraft:y_above":
		c.Kind = cYAbove
		if c.Anchor, err = parseAnchor(m["anchor"]); err != nil {
			return nil, err
		}
		if err = get("surface_depth_multiplier", &c.SurfaceDepthMultiplier); err == nil {
			err = get("add_stone_depth", &c.AddStoneDepth)
		}
	case "minecraft:water":
		c.Kind = cWater
		if err = get("offset", &c.Offset); err == nil {
			if err = get("surface_depth_multiplier", &c.SurfaceDepthMultiplier); err == nil {
				err = get("add_stone_depth", &c.AddStoneDepth)
			}
		}
	case "minecraft:temperature":
		c.Kind = cTemperature
	case "minecraft:steep":
		c.Kind = cSteep
	case "minecraft:hole":
		c.Kind = cHole
	case "minecraft:above_preliminary_surface":
		c.Kind = cAbovePreliminarySurface
	case "minecraft:not":
		c.Kind = cNot
		c.Invert, err = p.cond(m["invert"])
	case "minecraft:stone_depth":
		c.Kind = cStoneDepth
		var st string
		if err = get("offset", &c.Offset); err == nil {
			if err = get("add_surface_depth", &c.AddSurfaceDepth); err == nil {
				if err = get("secondary_depth_range", &c.SecondaryDepthRange); err == nil {
					err = get("surface_type", &st)
				}
			}
		}
		switch st {
		case "floor":
		case "ceiling":
			c.Ceiling = true
		default:
			if err == nil {
				err = fmt.Errorf("surface: stone_depth surface_type %q", st)
			}
		}
	default:
		return nil, fmt.Errorf("surface: unknown material condition type %q", typ)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}
