package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Feature is a configured vanilla feature.
type Feature interface {
	Place(c *placeCtx, r *Random, p Pos) bool
}

// Placed is a placed feature: a feature plus its placement modifiers.
type Placed struct {
	Key     string // registry key; "" when inline
	Feature Feature
	Mods    []Modifier
	id      int // index in registry.placed, -1 when inline
}

// place runs the modifiers depth-first and places the feature at every
// resulting position.
func (pf *Placed) place(lv *Level, r *Random, p Pos, top *Placed) bool {
	c := &placeCtx{lv: lv, top: top}
	placed := false
	var step func(i int, q Pos)
	step = func(i int, q Pos) {
		if i == len(pf.Mods) {
			if pf.Feature.Place(c, r, q) {
				placed = true
			}
			return
		}
		pf.Mods[i].Apply(c, r, q, func(n Pos) { step(i+1, n) })
	}
	step(0, p)
	return placed
}

// noOp is a feature type kremenmc does not implement yet (or vanilla no_op).
type noOp struct{ typ string }

// Place: vanilla no_op reports success (coral/block_decoration and sculk
// overlay probes); unimplemented types report failure.
func (n noOp) Place(*placeCtx, *Random, Pos) bool { return n.typ == "no_op" }

type simpleBlock struct {
	state        StateProvider
	scheduleTick bool
}

func (s simpleBlock) Place(c *placeCtx, r *Random, p Pos) bool {
	st := s.state.State(c.lv, r, p)
	if !canSurvive(c.lv, st, p) {
		return false
	}
	if blockName(st) == "pale_moss_carpet" {
		placeMossyCarpet(c.lv, st, p)
		return true
	}
	if isLowerHalf(st) {
		up := p.Add(Pos{0, 1, 0})
		if blockName(st) != "tall_seagrass" && !data.IsAir(c.lv.Block(up)) {
			return false
		}
		c.lv.SetBlock(p, st)
		upper, _ := data.Canonical().With(st, "half", "upper")
		c.lv.SetBlock(up, upper)
		return true
	}
	c.lv.SetBlock(p, st)
	return true
}

// isLowerHalf reports double plants (tall grass, large fern, tall flowers).
func isLowerHalf(st data.StateID) bool {
	reg := data.Canonical()
	h, ok := reg.Value(st, "half")
	if !ok || h != "lower" {
		return false
	}
	_, hasFacing := reg.Value(st, "facing")
	return !hasFacing || blockName(st) == "small_dripleaf"
}

type randomSelector struct {
	chances []float32
	options []*Placed
	def     *Placed
}

func (s randomSelector) Place(c *placeCtx, r *Random, p Pos) bool {
	for i, ch := range s.chances {
		if r.NextFloat() < ch {
			return s.options[i].place(c.lv, r, p, nil)
		}
	}
	return s.def.place(c.lv, r, p, nil)
}

type simpleRandomSelector []*Placed

func (s simpleRandomSelector) Place(c *placeCtx, r *Random, p Pos) bool {
	i := r.NextIntn(int32(len(s)))
	return s[i].place(c.lv, r, p, nil)
}

type weightedSelector struct {
	options []*Placed
	weight  []int
	total   int
}

func (s weightedSelector) Place(c *placeCtx, r *Random, p Pos) bool {
	return s.options[pickWeighted(r, s.weight, s.total)].place(c.lv, r, p, nil)
}

type booleanSelector struct{ t, f *Placed }

func (s booleanSelector) Place(c *placeCtx, r *Random, p Pos) bool {
	if r.NextBool() {
		return s.t.place(c.lv, r, p, nil)
	}
	return s.f.place(c.lv, r, p, nil)
}

type sequence []*Placed

// Place stops at the first feature that fails (sculk_patch probes: the
// catalyst draws nothing once the patch fails).
func (s sequence) Place(c *placeCtx, r *Random, p Pos) bool {
	for _, f := range s {
		if !f.place(c.lv, r, p, nil) {
			return false
		}
	}
	return true
}

type columnLayer struct {
	height IntProvider
	state  StateProvider
}

type blockColumn struct {
	layers   []columnLayer
	dir      Pos
	allowed  Predicate
	tipFirst bool
}

func (b blockColumn) Place(c *placeCtx, r *Random, p Pos) bool {
	hs := make([]int, len(b.layers))
	total := 0
	for i, l := range b.layers {
		hs[i] = l.height.Sample(r)
		total += hs[i]
	}
	if total == 0 {
		return false
	}
	// Measured (feat_misc.Place): the column only grows into a position
	// whose next position is allowed too, so free is one less than the
	// allowed run unless the run covers total+1 positions.
	free := 0
	for q := p; free <= total && b.allowed.Test(c.lv, q); q = q.Add(b.dir) {
		free++
	}
	free = max(free-1, 0)
	if free < total {
		cut := total - free
		order := make([]int, len(hs))
		for i := range order {
			order[i] = len(hs) - 1 - i
			if b.tipFirst {
				order[i] = i
			}
		}
		for _, i := range order {
			d := min(cut, hs[i])
			hs[i] -= d
			cut -= d
			if cut == 0 {
				break
			}
		}
	}
	q := p
	for i, l := range b.layers {
		for range hs[i] {
			c.lv.SetBlock(q, l.state.State(c.lv, r, q))
			q = q.Add(b.dir)
		}
	}
	return true
}

var directions = map[string]Pos{
	"up": {0, 1, 0}, "down": {0, -1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
}

func (rg *registry) parseFeature(b json.RawMessage) (Feature, error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		var key string
		if err := json.Unmarshal(b, &key); err != nil {
			return nil, err
		}
		raw, ok := rg.pack.JSON("worldgen/feature", key)
		if !ok {
			return nil, fmt.Errorf("feature %s: missing", key)
		}
		return rg.parseFeature(raw)
	}
	t := typeOf(b)
	if f, ok := featureTypes[t]; ok {
		return f(rg, b)
	}
	switch t {
	case "simple_block", "random_selector", "simple_random_selector", "weighted_random_selector",
		"random_boolean_selector", "sequence", "block_column", "no_op":
	case "tree":
		return rg.parseTree(b)
	default:
		return nil, unsupported("feature", t)
	}
	var p struct {
		ToPlace  json.RawMessage   `json:"to_place"`
		Schedule bool              `json:"schedule_tick"`
		Features []json.RawMessage `json:"features"`
		Default  json.RawMessage   `json:"default"`
		True     json.RawMessage   `json:"feature_true"`
		False    json.RawMessage   `json:"feature_false"`
		Layers   []struct {
			Height   json.RawMessage `json:"height"`
			Provider json.RawMessage `json:"provider"`
		} `json:"layers"`
		Direction string          `json:"direction"`
		Allowed   json.RawMessage `json:"allowed_placement"`
		Tip       bool            `json:"prioritize_tip"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("feature %s: %w", t, err)
	}
	switch t {
	case "simple_block":
		sp, err := rg.parseStateProvider(p.ToPlace)
		return simpleBlock{sp, p.Schedule}, err
	case "random_selector":
		var s randomSelector
		for _, raw := range p.Features {
			var f selectorEntry
			if err := json.Unmarshal(raw, &f); err != nil {
				return nil, err
			}
			pf, err := rg.parsePlaced(f.Feature)
			if err != nil {
				return nil, err
			}
			s.chances = append(s.chances, f.Chance)
			s.options = append(s.options, pf)
		}
		var err error
		s.def, err = rg.parsePlaced(p.Default)
		return s, err
	case "simple_random_selector":
		var s simpleRandomSelector
		for _, raw := range p.Features {
			pf, err := rg.parsePlaced(raw)
			if err != nil {
				return nil, err
			}
			s = append(s, pf)
		}
		return s, nil
	case "weighted_random_selector":
		var s weightedSelector
		for _, raw := range p.Features {
			var f selectorEntry
			if err := json.Unmarshal(raw, &f); err != nil {
				return nil, err
			}
			pf, err := rg.parsePlaced(f.Data)
			if err != nil {
				return nil, err
			}
			s.options = append(s.options, pf)
			s.weight = append(s.weight, f.Weight)
			s.total += f.Weight
		}
		return s, nil
	case "random_boolean_selector":
		tf, err := rg.parsePlaced(p.True)
		if err != nil {
			return nil, err
		}
		ff, err := rg.parsePlaced(p.False)
		return booleanSelector{tf, ff}, err
	case "sequence":
		var s sequence
		for _, raw := range p.Features {
			pf, err := rg.parsePlaced(raw)
			if err != nil {
				return nil, err
			}
			s = append(s, pf)
		}
		return s, nil
	case "block_column":
		bc := blockColumn{dir: directions[p.Direction], tipFirst: p.Tip}
		var err error
		if bc.allowed, err = rg.parsePredicate(p.Allowed); err != nil {
			return nil, err
		}
		for _, l := range p.Layers {
			h, err := parseInt(l.Height)
			if err != nil {
				return nil, err
			}
			sp, err := rg.parseStateProvider(l.Provider)
			if err != nil {
				return nil, err
			}
			bc.layers = append(bc.layers, columnLayer{h, sp})
		}
		return bc, nil
	case "no_op":
		return noOp{t}, nil
	}
	return nil, unsupported("feature", t)
}

type selectorEntry struct {
	Chance  float32         `json:"chance"`
	Feature json.RawMessage `json:"feature"`
	Data    json.RawMessage `json:"data"`
	Weight  int             `json:"weight"`
}

// parsePlaced reads a placed feature reference: a placed_feature key or an
// inline {feature, placement} object.
func (rg *registry) parsePlaced(b json.RawMessage) (*Placed, error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		var key string
		if err := json.Unmarshal(b, &key); err != nil {
			return nil, err
		}
		return rg.placedByKey(key)
	}
	return rg.parseInlinePlaced("", b)
}

func (rg *registry) parseInlinePlaced(key string, b json.RawMessage) (*Placed, error) {
	var p struct {
		Feature   json.RawMessage   `json:"feature"`
		Placement []json.RawMessage `json:"placement"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("placed feature %s: %w", key, err)
	}
	pf := &Placed{Key: key, id: -1}
	f, err := rg.parseFeature(p.Feature)
	if err != nil {
		var u *unsupportedError
		if !asUnsupported(err, &u) {
			return nil, err
		}
		rg.noteUnsupported(u)
		f = noOp{u.typ}
	}
	pf.Feature = f
	for _, m := range p.Placement {
		mod, err := rg.parseModifier(m)
		if err != nil {
			var u *unsupportedError
			if !asUnsupported(err, &u) {
				return nil, err
			}
			rg.noteUnsupported(u)
			pf.Feature = noOp{u.typ}
			pf.Mods = nil
			break
		}
		pf.Mods = append(pf.Mods, mod)
	}
	return pf, nil
}
