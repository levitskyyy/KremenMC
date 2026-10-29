package loot

import (
	"encoding/json"
	"fmt"
	"math"
)

// number is a NumberProvider.
type number interface {
	Float(ctx *Context) float32
	Int(ctx *Context) int32
}

type constant float32

func (c constant) Float(*Context) float32 { return float32(c) }
func (c constant) Int(*Context) int32     { return roundF(float32(c)) }

// roundF is Mth.floor(f + 0.5), i.e. Math.round for floats.
func roundF(f float32) int32 { return int32(math.Floor(float64(f) + 0.5)) }

type uniform struct{ lo, hi number }

func (u uniform) Int(ctx *Context) int32 {
	return nextIntInclusive(ctx, u.lo.Int(ctx), u.hi.Int(ctx))
}

func (u uniform) Float(ctx *Context) float32 {
	lo, hi := u.lo.Float(ctx), u.hi.Float(ctx)
	if lo >= hi {
		return lo
	}
	return ctx.Rand.NextFloat()*(hi-lo) + lo
}

// nextIntInclusive is Mth.nextInt(random, lo, hi).
func nextIntInclusive(ctx *Context, lo, hi int32) int32 {
	if lo >= hi {
		return lo
	}
	return ctx.Rand.NextIntn(hi-lo+1) + lo
}

type binomial struct {
	n number
	p number
}

func (b binomial) Int(ctx *Context) int32 {
	n, p := b.n.Int(ctx), b.p.Float(ctx)
	k := int32(0)
	for range n {
		if ctx.Rand.NextFloat() < p {
			k++
		}
	}
	return k
}

func (b binomial) Float(ctx *Context) float32 { return float32(b.Int(ctx)) }

func parseNumber(raw json.RawMessage) (number, error) {
	if len(raw) == 0 {
		return constant(0), nil
	}
	var c float64
	if json.Unmarshal(raw, &c) == nil {
		return constant(c), nil
	}
	var j struct {
		Type  string          `json:"type"`
		Min   json.RawMessage `json:"min"`
		Max   json.RawMessage `json:"max"`
		N     json.RawMessage `json:"n"`
		P     json.RawMessage `json:"p"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	switch typeOf(raw) {
	case "constant":
		var v float64
		json.Unmarshal(j.Value, &v)
		return constant(v), nil
	case "uniform":
		lo, err := parseNumber(j.Min)
		if err != nil {
			return nil, err
		}
		hi, err := parseNumber(j.Max)
		if err != nil {
			return nil, err
		}
		return uniform{lo, hi}, nil
	case "binomial":
		n, err := parseNumber(j.N)
		if err != nil {
			return nil, err
		}
		p, err := parseNumber(j.P)
		if err != nil {
			return nil, err
		}
		return binomial{n, p}, nil
	}
	return nil, fmt.Errorf("unsupported number provider %s", raw)
}

// intRange is IntRange: optional min and max number providers.
type intRange struct{ lo, hi number }

func parseIntRange(raw json.RawMessage) (intRange, error) {
	var c float64
	if json.Unmarshal(raw, &c) == nil {
		return intRange{constant(c), constant(c)}, nil
	}
	var j struct {
		Min json.RawMessage `json:"min"`
		Max json.RawMessage `json:"max"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return intRange{}, err
	}
	var r intRange
	var err error
	if j.Min != nil {
		if r.lo, err = parseNumber(j.Min); err != nil {
			return r, err
		}
	}
	if j.Max != nil {
		if r.hi, err = parseNumber(j.Max); err != nil {
			return r, err
		}
	}
	return r, nil
}

func (r intRange) clamp(ctx *Context, v int32) int32 {
	if r.lo != nil {
		v = max(v, r.lo.Int(ctx))
	}
	if r.hi != nil {
		v = min(v, r.hi.Int(ctx))
	}
	return v
}

// levelValue is a LevelBasedValue.
type levelValue struct {
	kind                  string
	base, perLevel, added float32
	lo, hi                float32
	inner, fallback       *levelValue
	values                []float32
}

func parseLevelValue(raw json.RawMessage) (*levelValue, error) {
	var c float32
	if json.Unmarshal(raw, &c) == nil {
		return &levelValue{kind: "constant", base: c}, nil
	}
	var j struct {
		Base     float32         `json:"base"`
		PerLevel float32         `json:"per_level_above_first"`
		Added    float32         `json:"added"`
		Min      float32         `json:"min"`
		Max      float32         `json:"max"`
		Value    json.RawMessage `json:"value"`
		Values   []float32       `json:"values"`
		Fallback json.RawMessage `json:"fallback"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	v := &levelValue{kind: typeOf(raw), base: j.Base, perLevel: j.PerLevel, added: j.Added, lo: j.Min, hi: j.Max, values: j.Values}
	var err error
	if j.Value != nil {
		if v.inner, err = parseLevelValue(j.Value); err != nil {
			return nil, err
		}
	}
	if j.Fallback != nil {
		if v.fallback, err = parseLevelValue(j.Fallback); err != nil {
			return nil, err
		}
	}
	switch v.kind {
	case "constant", "linear", "levels_squared", "clamped", "lookup", "fraction":
	default:
		return nil, fmt.Errorf("unsupported level-based value %s", raw)
	}
	return v, nil
}

func (v *levelValue) eval(level int32) float32 {
	switch v.kind {
	case "constant":
		return v.base
	case "linear":
		return v.base + v.perLevel*float32(level-1)
	case "levels_squared":
		return float32(level*level) + v.added
	case "clamped":
		return min(max(v.inner.eval(level), v.lo), v.hi)
	case "lookup":
		if level > 0 && int(level) <= len(v.values) {
			return v.values[level-1]
		}
		return v.fallback.eval(level)
	}
	return 0
}
