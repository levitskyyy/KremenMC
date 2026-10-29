package carver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Float is a reference FloatProvider (constant, uniform or trapezoid).
type Float struct {
	kind         byte // 'c', 'u', 't'
	min, max, pl float32
}

func (f Float) Sample(r rng.Source) float32 {
	switch f.kind {
	case 'u':
		return randomBetween(r, f.min, f.max)
	case 't':
		rg := f.max - f.min
		a := (rg - f.pl) / 2
		b := rg - a
		return f.min + float32(r.NextFloat()*b) + float32(r.NextFloat()*a)
	}
	return f.min
}

// randomBetween is the reference Mth.randomBetween for floats.
func randomBetween(r rng.Source, lo, hi float32) float32 { return float32(r.NextFloat()*(hi-lo)) + lo }

func ConstFloat(v float32) Float { return Float{kind: 'c', min: v, max: v} }

func (f *Float) UnmarshalJSON(b []byte) error {
	var v float32
	if json.Unmarshal(b, &v) == nil {
		*f = ConstFloat(v)
		return nil
	}
	var o struct {
		Type    string   `json:"type"`
		Value   *float32 `json:"value"`
		Min     float32  `json:"min_inclusive"`
		Max     float32  `json:"max_exclusive"`
		TMin    float32  `json:"min"`
		TMax    float32  `json:"max"`
		Plateau float32  `json:"plateau"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return fmt.Errorf("float provider: %w", err)
	}
	switch strings.TrimPrefix(o.Type, "minecraft:") {
	case "constant":
		if o.Value == nil {
			return fmt.Errorf("float provider: constant without value")
		}
		*f = ConstFloat(*o.Value)
	case "uniform":
		if !(o.Min < o.Max) {
			return fmt.Errorf("float provider: uniform needs min < max")
		}
		*f = Float{kind: 'u', min: o.Min, max: o.Max}
	case "trapezoid":
		if !(o.TMin < o.TMax) || o.Plateau > o.TMax-o.TMin {
			return fmt.Errorf("float provider: bad trapezoid")
		}
		*f = Float{kind: 't', min: o.TMin, max: o.TMax, pl: o.Plateau}
	default:
		return fmt.Errorf("float provider: unsupported type %q", o.Type)
	}
	return nil
}

// Int is a reference IntProvider (constant, uniform, biased_to_bottom or
// very_biased_to_bottom).
type Int struct {
	kind     byte // 'c', 'u', 'b', 'v'
	min, max int32
}

func ConstInt(v int32) Int { return Int{kind: 'c', min: v, max: v} }

func (p Int) Sample(r rng.Source) int32 {
	switch p.kind {
	case 'u':
		return r.NextIntn(p.max-p.min+1) + p.min
	case 'b':
		return p.min + r.NextIntn(r.NextIntn(p.max-p.min+1)+1)
	case 'v':
		return p.min + r.NextIntn(r.NextIntn(r.NextIntn(p.max-p.min+1)+1)+1)
	}
	return p.min
}

func (p *Int) UnmarshalJSON(b []byte) error {
	var v int32
	if json.Unmarshal(b, &v) == nil {
		*p = ConstInt(v)
		return nil
	}
	var o struct {
		Type  string `json:"type"`
		Value *int32 `json:"value"`
		Min   int32  `json:"min_inclusive"`
		Max   int32  `json:"max_inclusive"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return fmt.Errorf("int provider: %w", err)
	}
	t := strings.TrimPrefix(o.Type, "minecraft:")
	switch t {
	case "constant":
		if o.Value == nil {
			return fmt.Errorf("int provider: constant without value")
		}
		*p = ConstInt(*o.Value)
		return nil
	case "uniform":
		*p = Int{kind: 'u', min: o.Min, max: o.Max}
	case "biased_to_bottom":
		*p = Int{kind: 'b', min: o.Min, max: o.Max}
	case "very_biased_to_bottom":
		*p = Int{kind: 'v', min: o.Min, max: o.Max}
	default:
		return fmt.Errorf("int provider: unsupported type %q", o.Type)
	}
	if o.Min > o.Max {
		return fmt.Errorf("int provider: min > max")
	}
	return nil
}

// Context is the reference WorldGenerationContext.
type Context struct {
	MinY, Height, SeaLevel int
}

// Anchor is a reference VerticalAnchor.
type Anchor struct {
	kind byte // 'a' absolute, 'b' above_bottom, 't' below_top
	v    int32
}

func (a Anchor) Resolve(c Context) int32 {
	switch a.kind {
	case 'b':
		return int32(c.MinY) + a.v
	case 't':
		return int32(c.MinY+c.Height-1) - a.v
	}
	return a.v
}

func (a *Anchor) UnmarshalJSON(b []byte) error {
	var o map[string]int32
	if err := json.Unmarshal(b, &o); err != nil || len(o) != 1 {
		return fmt.Errorf("vertical anchor: %s", b)
	}
	for k, v := range o {
		switch k {
		case "absolute":
			*a = Anchor{'a', v}
		case "above_bottom":
			*a = Anchor{'b', v}
		case "below_top":
			*a = Anchor{'t', v}
		default:
			return fmt.Errorf("vertical anchor: unknown %q", k)
		}
	}
	return nil
}

// Height is a reference HeightProvider (constant or uniform).
type Height struct {
	kind     byte // 'c', 'u'
	min, max Anchor
}

func (h Height) Sample(r rng.Source, c Context) int32 {
	lo := h.min.Resolve(c)
	if h.kind == 'c' {
		return lo
	}
	hi := h.max.Resolve(c)
	if lo > hi {
		return lo
	}
	return r.NextIntn(hi-lo+1) + lo
}

func (h *Height) UnmarshalJSON(b []byte) error {
	var o struct {
		Type  string  `json:"type"`
		Value *Anchor `json:"value"`
		Min   Anchor  `json:"min_inclusive"`
		Max   Anchor  `json:"max_inclusive"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		var a Anchor
		if a.UnmarshalJSON(b) == nil {
			*h = Height{kind: 'c', min: a}
			return nil
		}
		return fmt.Errorf("height provider: %w", err)
	}
	switch strings.TrimPrefix(o.Type, "minecraft:") {
	case "constant":
		if o.Value == nil {
			return fmt.Errorf("height provider: constant without value")
		}
		*h = Height{kind: 'c', min: *o.Value}
	case "uniform":
		*h = Height{kind: 'u', min: o.Min, max: o.Max}
	default:
		return fmt.Errorf("height provider: unsupported type %q", o.Type)
	}
	return nil
}
