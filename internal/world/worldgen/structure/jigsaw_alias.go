package structure

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Pool aliases (worldgen/structure "pool_aliases", trial chambers): each
// binding maps alias pool names to target pools; random and random_group
// bindings pick by weight with the start random. Draw order is inferred
// from JigNBT probe starts (see jigsaw_probe_test.go).

type aliasTarget struct {
	data   string
	weight int32
}

type aliasGroup struct {
	data   []*aliasBinding
	weight int32
}

type aliasBinding struct {
	typ     string // direct, random, random_group
	alias   string
	target  string
	targets []aliasTarget
	groups  []aliasGroup
}

func parseAliases(raw json.RawMessage) ([]*aliasBinding, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var l []json.RawMessage
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("pool_aliases: %w", err)
	}
	out := make([]*aliasBinding, 0, len(l))
	for _, b := range l {
		a, err := parseAlias(b)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func parseAlias(b []byte) (*aliasBinding, error) {
	var j struct {
		Type    string `json:"type"`
		Alias   string `json:"alias"`
		Target  string `json:"target"`
		Targets []struct {
			Data   string `json:"data"`
			Weight int32  `json:"weight"`
		} `json:"targets"`
		Groups []struct {
			Data   []json.RawMessage `json:"data"`
			Weight int32             `json:"weight"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("pool alias: %w", err)
	}
	a := &aliasBinding{typ: trimNS(j.Type), alias: qualify(j.Alias), target: qualify(j.Target)}
	for _, t := range j.Targets {
		a.targets = append(a.targets, aliasTarget{qualify(t.Data), t.Weight})
	}
	for _, g := range j.Groups {
		ag := aliasGroup{weight: g.Weight}
		for _, d := range g.Data {
			c, err := parseAlias(d)
			if err != nil {
				return nil, err
			}
			ag.data = append(ag.data, c)
		}
		a.groups = append(a.groups, ag)
	}
	switch a.typ {
	case "direct", "random", "random_group":
	default:
		return nil, fmt.Errorf("pool alias type %s", j.Type)
	}
	return a, nil
}

// weightedIndex picks an index by weight: one nextInt(total) draw.
func weightedIndex(r rng.Source, ws []int32) int {
	var total int32
	for _, w := range ws {
		total += w
	}
	if total <= 0 {
		return -1
	}
	v := r.NextIntn(total)
	for i, w := range ws {
		if v < w {
			return i
		}
		v -= w
	}
	return -1
}

func (a *aliasBinding) resolve(r rng.Source, m map[string]string) {
	switch a.typ {
	case "direct":
		m[a.alias] = a.target
	case "random":
		ws := make([]int32, len(a.targets))
		for i, t := range a.targets {
			ws[i] = t.weight
		}
		if i := weightedIndex(r, ws); i >= 0 {
			m[a.alias] = a.targets[i].data
		}
	case "random_group":
		ws := make([]int32, len(a.groups))
		for i, g := range a.groups {
			ws[i] = g.weight
		}
		if i := weightedIndex(r, ws); i >= 0 {
			for _, d := range a.groups[i].data {
				d.resolve(r, m)
			}
		}
	}
}

// resolveAliases binds every alias of a start, in list order.
func resolveAliases(r rng.Source, bs []*aliasBinding) map[string]string {
	if len(bs) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, b := range bs {
		b.resolve(r, m)
	}
	return m
}

// heightProvider is a worldgen height provider for start_height: constant
// ({"absolute": y}) or uniform (min_inclusive..max_inclusive).
type heightProvider struct {
	uniform  bool
	min, max int32
}

func parseHeightProvider(raw json.RawMessage) (heightProvider, error) {
	var j struct {
		Type     string          `json:"type"`
		Absolute *int32          `json:"absolute"`
		Value    json.RawMessage `json:"value"`
		Min      struct {
			Absolute int32 `json:"absolute"`
		} `json:"min_inclusive"`
		Max struct {
			Absolute int32 `json:"absolute"`
		} `json:"max_inclusive"`
	}
	if len(raw) == 0 {
		return heightProvider{}, nil
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return heightProvider{}, fmt.Errorf("start_height: %w", err)
	}
	switch trimNS(j.Type) {
	case "", "constant":
		if j.Absolute != nil {
			return heightProvider{min: *j.Absolute, max: *j.Absolute}, nil
		}
		var v struct {
			Absolute int32 `json:"absolute"`
		}
		if len(j.Value) > 0 {
			if err := json.Unmarshal(j.Value, &v); err != nil {
				return heightProvider{}, fmt.Errorf("start_height: %w", err)
			}
		}
		return heightProvider{min: v.Absolute, max: v.Absolute}, nil
	case "uniform":
		return heightProvider{uniform: true, min: j.Min.Absolute, max: j.Max.Absolute}, nil
	}
	return heightProvider{}, fmt.Errorf("start_height type %s not supported", j.Type)
}

func (h heightProvider) sample(r *rng.WorldgenRandom) int32 {
	if !h.uniform {
		return h.min
	}
	return h.min + jn(r, h.max-h.min+1)
}

// aliasRandom is the random pool aliases are bound with: a legacy
// positional random of the world seed at the start position (AliasProbe:
// PoolAliasLookup.create(aliases, pos, seed) is reproduced on 216/216
// positions and seeds; the position is fitted on the JigNBT starts).
func aliasRandom(ctx *GenContext, x, y, z int32) rng.Source {
	return rng.NewLegacy(ctx.Seed).ForkPositional().At(x, y, z)
}

// ignoreWaterlogging reports liquid_settings ignore_waterlogging (trial
// chambers): template blocks keep their own waterlogged state.
func (p *JigsawPiece) ignoreWaterlogging() bool { return p.liquid == "ignore_waterlogging" }
