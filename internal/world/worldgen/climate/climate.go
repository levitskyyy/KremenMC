// Package climate implements multi-noise biome selection: quantized climate
// parameters, parameter points and the parameter list lookup, matching the
// reference 26.3 results.
package climate

import (
	"encoding/json"
	"fmt"
)

const quantization = 10000

// Quantize converts a climate coordinate like the reference (float32 multiply,
// truncation toward zero).
func Quantize(f float32) int64 { return int64(f * quantization) }

func Unquantize(v int64) float32 { return float32(v) / quantization }

// Target is a quantized climate sample.
type Target struct {
	Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness int64
}

func (t Target) axes() [7]int64 {
	return [7]int64{t.Temperature, t.Humidity, t.Continentalness, t.Erosion, t.Depth, t.Weirdness, 0}
}

// Param is a closed quantized interval.
type Param struct{ Min, Max int64 }

func Point(f float32) Param     { v := Quantize(f); return Param{v, v} }
func Span(lo, hi float32) Param { return Param{Quantize(lo), Quantize(hi)} }

func (p Param) Distance(v int64) int64 {
	if v > p.Max {
		return v - p.Max
	}
	if v < p.Min {
		return p.Min - v
	}
	return 0
}

func (p Param) union(o Param) Param { return Param{min(p.Min, o.Min), max(p.Max, o.Max)} }

// ParamPoint is one biome's region: six intervals plus a fitness offset.
type ParamPoint struct {
	Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness Param
	Offset                                                            int64
}

func (p *ParamPoint) space() [7]Param {
	return [7]Param{p.Temperature, p.Humidity, p.Continentalness, p.Erosion, p.Depth, p.Weirdness, {p.Offset, p.Offset}}
}

// Fitness is the squared distance from t; lower is better.
func (p *ParamPoint) Fitness(t Target) int64 {
	var s int64
	ax := t.axes()
	for i, q := range p.space() {
		d := q.Distance(ax[i])
		s += d * d
	}
	return s
}

// Entry pairs a parameter point with a value index (e.g. a biome).
type Entry[T any] struct {
	Point ParamPoint
	Value T
}

// List is a parameter list; Find is the reference lookup. A List is
// immutable and safe for concurrent use.
type List[T any] struct {
	entries []Entry[T]
	tree    *tree
}

// NewList builds the search tree with the reference default fan-out.
func NewList[T any](entries []Entry[T]) *List[T] {
	return NewListChildren(entries, DefaultChildrenPerNode)
}

// NewListChildren is the reference rebuildWithChildrenPerNode(k).
func NewListChildren[T any](entries []Entry[T], k int) *List[T] {
	return &List[T]{entries: entries, tree: build(entries, k)}
}

// FindIndex returns the index into Entries of the reference findValue result
// for a thread with no remembered result (a fresh thread), or -1 for an
// empty list.
func (l *List[T]) FindIndex(t Target) int { return l.tree.find(t, -1) }

// Find returns the value of FindIndex (zero for an empty list).
func (l *List[T]) Find(t Target) T { return l.value(l.tree.find(t, -1)) }

func (l *List[T]) value(i int) T {
	if i < 0 {
		var zero T
		return zero
	}
	return l.entries[i].Value
}

// Cursor reproduces the reference's per-thread memory of the last result:
// among equally fit entries the remembered one wins, otherwise the first in
// tree order. The zero Cursor remembers nothing. A Cursor is not safe for
// concurrent use; it may be reused across lists (it resets on a new list).
type Cursor struct {
	tree *tree
	last int
}

// FindIndexWith is FindIndex as seen by a reference thread whose previous
// queries on this list went through c.
func (l *List[T]) FindIndexWith(t Target, c *Cursor) int {
	last := -1
	if c.tree == l.tree {
		last = c.last
	}
	i := l.tree.find(t, last)
	c.tree, c.last = l.tree, i
	return i
}

// FindWith returns the value of FindIndexWith.
func (l *List[T]) FindWith(t Target, c *Cursor) T { return l.value(l.FindIndexWith(t, c)) }

func (l *List[T]) Entries() []Entry[T] { return l.entries }

// BruteForce returns the first entry with minimal fitness.
func (l *List[T]) BruteForce(t Target) T {
	best, bi := int64(-1), 0
	for i := range l.entries {
		f := l.entries[i].Point.Fitness(t)
		if best < 0 || f < best {
			best, bi = f, i
		}
	}
	return l.entries[bi].Value
}

type jsonParam struct{ Param }

func (p *jsonParam) UnmarshalJSON(b []byte) error {
	var f float32
	if err := json.Unmarshal(b, &f); err == nil {
		p.Param = Point(f)
		return nil
	}
	var a [2]float32
	if err := json.Unmarshal(b, &a); err != nil {
		return fmt.Errorf("climate: parameter %s: %w", b, err)
	}
	p.Param = Span(a[0], a[1])
	return nil
}

// ParseReport decodes a biome_parameters report ({"biomes":[{biome, parameters}]}).
func ParseReport(b []byte) ([]Entry[string], error) {
	var r struct {
		Biomes []struct {
			Biome      string `json:"biome"`
			Parameters struct {
				Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness jsonParam
				Offset                                                            float32
			} `json:"parameters"`
		} `json:"biomes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("climate: report: %w", err)
	}
	out := make([]Entry[string], len(r.Biomes))
	for i, e := range r.Biomes {
		p := e.Parameters
		out[i] = Entry[string]{Value: e.Biome, Point: ParamPoint{
			Temperature: p.Temperature.Param, Humidity: p.Humidity.Param, Continentalness: p.Continentalness.Param,
			Erosion: p.Erosion.Param, Depth: p.Depth.Param, Weirdness: p.Weirdness.Param, Offset: Quantize(p.Offset),
		}}
	}
	return out, nil
}
