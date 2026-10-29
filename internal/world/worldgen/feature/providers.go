package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Random is the decoration random every feature draws from.
type Random = rng.WorldgenRandom

type typed struct {
	Type string `json:"type"`
}

func typeOf(b json.RawMessage) string {
	var t typed
	if json.Unmarshal(b, &t) != nil {
		return ""
	}
	return strings.TrimPrefix(t.Type, "minecraft:")
}

func isNumber(b json.RawMessage) bool {
	s := strings.TrimSpace(string(b))
	return s != "" && (s[0] == '-' || s[0] >= '0' && s[0] <= '9')
}

// IntProvider is a vanilla int_provider.
type IntProvider interface {
	Sample(r *Random) int
}

type constInt int

func (c constInt) Sample(*Random) int { return int(c) }

type uniformInt struct{ min, max int }

func (u uniformInt) Sample(r *Random) int {
	return int(r.NextIntBetweenInclusive(int32(u.min), int32(u.max)))
}

type biasedInt struct{ min, max int }

func (b biasedInt) Sample(r *Random) int {
	return b.min + int(r.NextIntn(r.NextIntn(int32(b.max-b.min+1))+1))
}

type clampedInt struct {
	src      IntProvider
	min, max int
}

func (c clampedInt) Sample(r *Random) int { return min(max(c.src.Sample(r), c.min), c.max) }

type clampedNormalInt struct {
	mean, dev float32
	min, max  int
}

func (c clampedNormalInt) Sample(r *Random) int {
	v := c.mean + float32(r.NextGaussian())*c.dev
	return int(min(max(v, float32(c.min)), float32(c.max)))
}

type trapezoidInt struct{ min, max, plateau int }

// Sample follows the draws measured in the reference (testdata/mods.txt):
// a difference of two uniform draws for the symmetric zero-plateau case,
// one uniform draw when the plateau spans the range, otherwise the sum of
// two uniform draws.
func (t trapezoidInt) Sample(r *Random) int {
	rg := t.max - t.min
	if t.plateau == 0 && t.min == -t.max {
		a := int(r.NextIntn(int32(t.max + 1)))
		return a - int(r.NextIntn(int32(t.max+1)))
	}
	if t.plateau >= rg {
		return int(r.NextIntBetweenInclusive(int32(t.min), int32(t.max)))
	}
	k := (rg - t.plateau) / 2
	l := rg - k
	return t.min + int(r.NextIntBetweenInclusive(0, int32(l))) + int(r.NextIntBetweenInclusive(0, int32(k)))
}

type weightedInt struct {
	items  []IntProvider
	weight []int
	total  int
}

func (w weightedInt) Sample(r *Random) int {
	i := pickWeighted(r, w.weight, w.total)
	return w.items[i].Sample(r)
}

// pickWeighted is WeightedList.getRandom: one nextInt over the total weight.
func pickWeighted(r *Random, ws []int, total int) int {
	n := int(r.NextIntn(int32(total)))
	for i, w := range ws {
		if n < w {
			return i
		}
		n -= w
	}
	return len(ws) - 1
}

type rawProvider struct {
	Type    string          `json:"type"`
	Value   json.RawMessage `json:"value"`
	Min     *int            `json:"min_inclusive"`
	Max     *int            `json:"max_inclusive"`
	MinT    int             `json:"min"`
	MaxT    int             `json:"max"`
	Plateau int             `json:"plateau"`
	Source  json.RawMessage `json:"source"`
	Mean    float32         `json:"mean"`
	Dev     float32         `json:"deviation"`
	Dist    []struct {
		Data   json.RawMessage `json:"data"`
		Weight int             `json:"weight"`
	} `json:"distribution"`
}

func parseInt(b json.RawMessage) (IntProvider, error) {
	if isNumber(b) {
		var v int
		err := json.Unmarshal(b, &v)
		return constInt(v), err
	}
	var p rawProvider
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("int provider: %w", err)
	}
	switch strings.TrimPrefix(p.Type, "minecraft:") {
	case "constant":
		return parseInt(p.Value)
	case "uniform":
		return uniformInt{*p.Min, *p.Max}, nil
	case "biased_to_bottom":
		return biasedInt{*p.Min, *p.Max}, nil
	case "clamped":
		s, err := parseInt(p.Source)
		if err != nil {
			return nil, err
		}
		return clampedInt{s, *p.Min, *p.Max}, nil
	case "clamped_normal":
		return clampedNormalInt{p.Mean, p.Dev, *p.Min, *p.Max}, nil
	case "trapezoid":
		return trapezoidInt{p.MinT, p.MaxT, p.Plateau}, nil
	case "weighted_list":
		var w weightedInt
		for _, d := range p.Dist {
			ip, err := parseInt(d.Data)
			if err != nil {
				return nil, err
			}
			w.items = append(w.items, ip)
			w.weight = append(w.weight, d.Weight)
			w.total += d.Weight
		}
		return w, nil
	}
	return nil, unsupported("int_provider", p.Type)
}

// FloatProvider is a vanilla float_provider.
type FloatProvider interface {
	Sample(r *Random) float32
}

type constFloat float32

func (c constFloat) Sample(*Random) float32 { return float32(c) }

type uniformFloat struct{ min, max float32 }

func (u uniformFloat) Sample(r *Random) float32 { return r.NextFloat()*(u.max-u.min) + u.min }

func parseFloat(b json.RawMessage) (FloatProvider, error) {
	if isNumber(b) {
		var v float32
		err := json.Unmarshal(b, &v)
		return constFloat(v), err
	}
	var p struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
		Min   float32         `json:"min_inclusive"`
		Max   float32         `json:"max_exclusive"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("float provider: %w", err)
	}
	switch strings.TrimPrefix(p.Type, "minecraft:") {
	case "constant":
		return parseFloat(p.Value)
	case "uniform":
		return uniformFloat{p.Min, p.Max}, nil
	}
	return nil, unsupported("float_provider", p.Type)
}

// anchor is a vanilla vertical anchor.
type anchor struct {
	kind byte // a absolute, b above_bottom, t below_top
	v    int
}

func (a anchor) resolve(minY, height int) int {
	switch a.kind {
	case 'b':
		return minY + a.v
	case 't':
		return minY + height - 1 - a.v
	}
	return a.v
}

func parseAnchor(b json.RawMessage) (anchor, error) {
	var m map[string]int
	if err := json.Unmarshal(b, &m); err != nil {
		return anchor{}, fmt.Errorf("vertical anchor: %w", err)
	}
	for k, v := range m {
		switch k {
		case "absolute":
			return anchor{'a', v}, nil
		case "above_bottom":
			return anchor{'b', v}, nil
		case "below_top":
			return anchor{'t', v}, nil
		}
	}
	return anchor{}, fmt.Errorf("vertical anchor %s", b)
}

// HeightProvider is a vanilla height_provider.
type HeightProvider interface {
	Sample(r *Random, minY, height int) int
}

type constHeight anchor

func (c constHeight) Sample(_ *Random, minY, height int) int { return anchor(c).resolve(minY, height) }

type uniformHeight struct{ lo, hi anchor }

func (u uniformHeight) Sample(r *Random, minY, height int) int {
	a, b := u.lo.resolve(minY, height), u.hi.resolve(minY, height)
	if a > b {
		return a
	}
	return int(r.NextIntBetweenInclusive(int32(a), int32(b)))
}

type trapezoidHeight struct {
	lo, hi  anchor
	plateau int
}

func (t trapezoidHeight) Sample(r *Random, minY, height int) int {
	a, b := t.lo.resolve(minY, height), t.hi.resolve(minY, height)
	if a > b {
		return a
	}
	return trapezoidInt{a, b, t.plateau}.Sample(r)
}

func parseHeight(b json.RawMessage) (HeightProvider, error) {
	var p struct {
		Type    string          `json:"type"`
		Value   json.RawMessage `json:"value"`
		Min     json.RawMessage `json:"min_inclusive"`
		Max     json.RawMessage `json:"max_inclusive"`
		Plateau int             `json:"plateau"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("height provider: %w", err)
	}
	if p.Type == "" {
		a, err := parseAnchor(b)
		return constHeight(a), err
	}
	switch strings.TrimPrefix(p.Type, "minecraft:") {
	case "constant":
		a, err := parseAnchor(p.Value)
		return constHeight(a), err
	case "uniform", "trapezoid":
		lo, err := parseAnchor(p.Min)
		if err != nil {
			return nil, err
		}
		hi, err := parseAnchor(p.Max)
		if err != nil {
			return nil, err
		}
		if p.Type == "minecraft:uniform" {
			return uniformHeight{lo, hi}, nil
		}
		return trapezoidHeight{lo, hi, p.Plateau}, nil
	}
	if f, ok := heightTypes[strings.TrimPrefix(p.Type, "minecraft:")]; ok {
		return f(b)
	}
	return nil, unsupported("height_provider", p.Type)
}

// StateProvider is a vanilla block_state_provider.
type StateProvider interface {
	State(lv *Level, r *Random, p Pos) data.StateID
}

type simpleState data.StateID

func (s simpleState) State(*Level, *Random, Pos) data.StateID { return data.StateID(s) }

type weightedState struct {
	states []data.StateID
	weight []int
	total  int
}

func (w weightedState) State(_ *Level, r *Random, _ Pos) data.StateID {
	return w.states[pickWeighted(r, w.weight, w.total)]
}

// noiseThresholdState is the noise_threshold provider: below the threshold
// a random low state, above it a high state with high_chance (else a
// low one, or the default when there are no low states...).
type noiseThresholdState struct {
	noise             noise.Noise
	scale             float64
	threshold, chance float32
	def               data.StateID
	low, high         []data.StateID
}

func (n *noiseThresholdState) State(_ *Level, r *Random, p Pos) data.StateID {
	v := n.noise.Get(float64(p.X)*n.scale, float64(p.Y)*n.scale, float64(p.Z)*n.scale)
	if v < n.threshold {
		return n.low[r.NextIntn(int32(len(n.low)))]
	}
	if r.NextFloat() < n.chance {
		return n.high[r.NextIntn(int32(len(n.high)))]
	}
	return n.def
}

type ruleState struct {
	rules []struct {
		pred Predicate
		then StateProvider
	}
	fallback StateProvider // nil: keep the block
}

// stateRule returns the state a rule_based provider picks; ok is false when
// no rule matched and there is no fallback.
func (s *ruleState) pick(lv *Level, r *Random, p Pos) (data.StateID, bool) {
	for _, rl := range s.rules {
		if rl.pred.Test(lv, p) {
			return rl.then.State(lv, r, p), true
		}
	}
	if s.fallback != nil {
		return s.fallback.State(lv, r, p), true
	}
	return 0, false
}

func (s *ruleState) State(lv *Level, r *Random, p Pos) data.StateID {
	st, ok := s.pick(lv, r, p)
	if !ok {
		return lv.Block(p)
	}
	return st
}

type rawState struct {
	ID    string            `json:"id"`
	Props map[string]string `json:"properties"`
}

// parseBlockState reads a block state: "minecraft:id" or {id, properties}.
func parseBlockState(b json.RawMessage) (data.StateID, error) {
	var rs rawState
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &rs.ID); err != nil {
			return 0, err
		}
	} else if err := json.Unmarshal(b, &rs); err != nil {
		return 0, fmt.Errorf("block state: %w", err)
	}
	name := strings.TrimPrefix(rs.ID, "minecraft:")
	st, ok := data.Canonical().StateFrom(name, func(k string) (string, bool) {
		v, ok := rs.Props[k]
		return v, ok
	})
	if !ok {
		return 0, fmt.Errorf("block state %s: unknown", b)
	}
	return st, nil
}

func (rg *registry) parseStateProvider(b json.RawMessage) (StateProvider, error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		var key string
		if err := json.Unmarshal(b, &key); err != nil {
			return nil, err
		}
		raw, ok := rg.pack.JSON("worldgen/block_state_provider", key)
		if !ok {
			return nil, fmt.Errorf("block_state_provider %s: missing", key)
		}
		return rg.parseStateProvider(raw)
	}
	t := typeOf(b)
	if t == "" {
		st, err := parseBlockState(b)
		return simpleState(st), err
	}
	var p struct {
		State   json.RawMessage `json:"state"`
		Entries []struct {
			Data   json.RawMessage `json:"data"`
			Weight int             `json:"weight"`
		} `json:"entries"`
		Noise     noise.NormalParams `json:"noise"`
		Seed      int64              `json:"seed"`
		Scale     float64            `json:"scale"`
		Threshold float32            `json:"threshold"`
		Chance    float32            `json:"high_chance"`
		Default   json.RawMessage    `json:"default_state"`
		Low       []json.RawMessage  `json:"low_states"`
		High      []json.RawMessage  `json:"high_states"`
		Rules     []struct {
			If   json.RawMessage `json:"if_true"`
			Then json.RawMessage `json:"then"`
		} `json:"rules"`
		Fallback json.RawMessage `json:"fallback"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("block_state_provider %s: %w", t, err)
	}
	switch t {
	case "simple":
		st, err := parseBlockState(p.State)
		return simpleState(st), err
	case "weighted":
		var w weightedState
		for _, e := range p.Entries {
			st, err := parseBlockState(e.Data)
			if err != nil {
				return nil, err
			}
			w.states = append(w.states, st)
			w.weight = append(w.weight, e.Weight)
			w.total += e.Weight
		}
		return w, nil
	case "noise_threshold":
		nn, err := noise.NewNormalNoise(p.Noise)
		if err != nil {
			return nil, err
		}
		n := &noiseThresholdState{
			noise: nn.Create(rng.NewWorldgenRandom(rng.NewLegacy(p.Seed))), scale: p.Scale,
			threshold: p.Threshold, chance: p.Chance,
		}
		if n.def, err = parseBlockState(p.Default); err != nil {
			return nil, err
		}
		for _, s := range p.Low {
			st, err := parseBlockState(s)
			if err != nil {
				return nil, err
			}
			n.low = append(n.low, st)
		}
		for _, s := range p.High {
			st, err := parseBlockState(s)
			if err != nil {
				return nil, err
			}
			n.high = append(n.high, st)
		}
		return n, nil
	case "rule_based":
		rs := &ruleState{}
		for _, rl := range p.Rules {
			pr, err := rg.parsePredicate(rl.If)
			if err != nil {
				return nil, err
			}
			th, err := rg.parseStateProvider(rl.Then)
			if err != nil {
				return nil, err
			}
			rs.rules = append(rs.rules, struct {
				pred Predicate
				then StateProvider
			}{pr, th})
		}
		if len(p.Fallback) > 0 {
			fb, err := rg.parseStateProvider(p.Fallback)
			if err != nil {
				return nil, err
			}
			rs.fallback = fb
		}
		return rs, nil
	}
	if f, ok := providerTypes[t]; ok {
		return f(rg, b)
	}
	return nil, unsupported("block_state_provider", t)
}
