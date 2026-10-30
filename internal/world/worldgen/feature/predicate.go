package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Predicate is a vanilla block_predicate evaluated at a position.
type Predicate interface {
	Test(lv *Level, p Pos) bool
}

type predTrue struct{}

func (predTrue) Test(*Level, Pos) bool { return true }

type predNot struct{ p Predicate }

func (n predNot) Test(lv *Level, p Pos) bool { return !n.p.Test(lv, p) }

type predAll []Predicate

func (a predAll) Test(lv *Level, p Pos) bool {
	for _, q := range a {
		if !q.Test(lv, p) {
			return false
		}
	}
	return true
}

type predAny []Predicate

func (a predAny) Test(lv *Level, p Pos) bool {
	for _, q := range a {
		if q.Test(lv, p) {
			return true
		}
	}
	return false
}

type predBlocks struct {
	off    Pos
	blocks blockSet
}

func (m predBlocks) Test(lv *Level, p Pos) bool { return m.blocks.has(lv.Block(p.Add(m.off))) }

type predFluids struct {
	off    Pos
	fluids map[string]bool
}

func (m predFluids) Test(lv *Level, p Pos) bool { return m.fluids[fluidOf(lv.Block(p.Add(m.off)))] }

type predReplaceable struct{ off Pos }

func (m predReplaceable) Test(lv *Level, p Pos) bool {
	return data.Flags(lv.Block(p.Add(m.off)))&data.FlagReplaceable != 0
}

type predSolid struct{ off Pos }

func (m predSolid) Test(lv *Level, p Pos) bool { return isSolid(lv.Block(p.Add(m.off))) }

type predSurvive struct {
	off   Pos
	state data.StateID
}

func (m predSurvive) Test(lv *Level, p Pos) bool { return canSurvive(lv, m.state, p.Add(m.off)) }

type predInWorld struct{ off Pos }

func (m predInWorld) Test(lv *Level, p Pos) bool {
	y := p.Y + m.off.Y
	return y >= lv.MinY() && y < lv.MaxY()
}

type predHeightRange struct {
	off      Pos
	min, max int
}

func (m predHeightRange) Test(lv *Level, p Pos) bool {
	y := p.Y + m.off.Y
	return y >= m.min && y <= m.max
}

func (rg *registry) parsePredicate(b json.RawMessage) (Predicate, error) {
	var p struct {
		Type       string            `json:"type"`
		Offset     []int             `json:"offset"`
		Predicate  json.RawMessage   `json:"predicate"`
		Predicates []json.RawMessage `json:"predicates"`
		Blocks     json.RawMessage   `json:"blocks"`
		Tag        string            `json:"tag"`
		Fluids     json.RawMessage   `json:"fluids"`
		State      json.RawMessage   `json:"state"`
		Min        json.RawMessage   `json:"min_inclusive"`
		Max        json.RawMessage   `json:"max_inclusive"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("block predicate: %w", err)
	}
	var off Pos
	if len(p.Offset) == 3 {
		off = Pos{p.Offset[0], p.Offset[1], p.Offset[2]}
	}
	t := strings.TrimPrefix(p.Type, "minecraft:")
	switch t {
	case "true":
		return predTrue{}, nil
	case "not":
		q, err := rg.parsePredicate(p.Predicate)
		return predNot{q}, err
	case "all_of", "any_of":
		var l []Predicate
		for _, s := range p.Predicates {
			q, err := rg.parsePredicate(s)
			if err != nil {
				return nil, err
			}
			l = append(l, q)
		}
		if t == "all_of" {
			return predAll(l), nil
		}
		return predAny(l), nil
	case "matching_blocks":
		names, err := stringList(p.Blocks)
		if err != nil {
			return nil, err
		}
		bs, err := rg.blocks(names)
		return predBlocks{off, bs}, err
	case "matching_block_tag":
		bs, err := rg.tag(p.Tag)
		return predBlocks{off, bs}, err
	case "matching_fluids":
		names, err := stringList(p.Fluids)
		if err != nil {
			return nil, err
		}
		m := map[string]bool{}
		for _, n := range names {
			m[strings.TrimPrefix(n, "minecraft:")] = true
		}
		return predFluids{off, m}, nil
	case "replaceable":
		return predReplaceable{off}, nil
	case "solid":
		return predSolid{off}, nil
	case "would_survive":
		st, err := parseBlockState(p.State)
		return predSurvive{off, st}, err
	case "inside_world_bounds":
		return predInWorld{off}, nil
	case "height_range":
		return parseHeightRangePred(off, p.Min, p.Max)
	}
	if f, ok := predicateTypes[t]; ok {
		return f(rg, b)
	}
	return nil, unsupported("block_predicate", p.Type)
}

// stringList reads "a", ["a", "b"] or "#tag" lists.
func stringList(b json.RawMessage) ([]string, error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		var v string
		err := json.Unmarshal(b, &v)
		return []string{v}, err
	}
	var l []string
	err := json.Unmarshal(b, &l)
	return l, err
}

// blockSet is a set of blocks by BlockID.
type blockSet []bool

func (s blockSet) has(st data.StateID) bool {
	id := int(data.Canonical().BlockID(st))
	return id < len(s) && s[id]
}

func (rg *registry) blocks(names []string) (blockSet, error) {
	reg := data.Canonical()
	var set blockSet
	for _, n := range names {
		if strings.HasPrefix(n, "#") {
			ts, err := rg.tag(n[1:])
			if err != nil {
				return nil, err
			}
			set = set.union(ts)
			continue
		}
		b, ok := reg.BlockByName(strings.TrimPrefix(n, "minecraft:"))
		if !ok {
			return nil, fmt.Errorf("unknown block %s", n)
		}
		for int(b.ID) >= len(set) {
			set = append(set, false)
		}
		set[b.ID] = true
	}
	return set, nil
}

func (s blockSet) union(o blockSet) blockSet {
	for len(s) < len(o) {
		s = append(s, false)
	}
	for i, v := range o {
		s[i] = s[i] || v
	}
	return s
}

// tag resolves a block tag ("minecraft:air" or "air").
func (rg *registry) tag(name string) (blockSet, error) {
	name = strings.TrimPrefix(name, "#")
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	rg.mu.Lock()
	s, ok := rg.tags[name]
	rg.mu.Unlock()
	if ok {
		return s, nil
	}
	ids, ok := rg.pack.Tag("block", name)
	if !ok {
		return nil, fmt.Errorf("block tag %s: missing", name)
	}
	s, err := rg.blocks(ids)
	if err != nil {
		return nil, err
	}
	rg.mu.Lock()
	rg.tags[name] = s
	rg.mu.Unlock()
	return s, nil
}

// fluidOf names the fluid of a state ("water", "flowing_water", "lava",
// "flowing_lava" or "").
func fluidOf(st data.StateID) string {
	reg := data.Canonical()
	if !reg.HasFluid(st) {
		return ""
	}
	b := reg.BlockOf(st)
	name := strings.TrimPrefix(b.Name, "minecraft:")
	if name == "lava" {
		if lvl, _ := reg.Value(st, "level"); lvl == "0" {
			return "lava"
		}
		return "flowing_lava"
	}
	if name == "water" {
		if lvl, _ := reg.Value(st, "level"); lvl != "0" {
			return "flowing_water"
		}
	}
	return "water"
}

func isSolid(st data.StateID) bool { return data.Flags(st)&data.FlagSolid != 0 }
