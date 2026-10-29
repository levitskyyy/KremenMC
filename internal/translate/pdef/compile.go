// Package pdef interprets protodef schemas (as used by minecraft-data) to skip
// or copy values whose layout is only known at runtime: item components,
// particles and entity metadata of other protocol versions.
package pdef

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type kind uint8

const (
	kVoid kind = iota
	kFixed
	kVarInt
	kVarLong
	kString
	kBuffer
	kRest
	kNBT
	kLpVec3
	kOption
	kArray
	kContainer
	kSwitch
	kBitfield
	kHolder
	kHolderSet
	kMetaLoop
	kTopBitArray
)

// Node is a compiled schema type. Nodes are immutable after Compile.
type Node struct {
	kind   kind
	name   string
	size   int  // kFixed byte size; kBuffer/kArray fixed count (-1: VarInt prefix)
	signed bool // kFixed integers
	elem   *Node
	alt    *Node
	fields []field
	nslots int
	count  *ref
	cmp    ref
	cases  map[int64]*Node
	def    *Node
	parts  []bitPart
	endVal int
	names  []string // mapper names indexed by value
	flags  []string // bitflags names indexed by bit
	keys   []string // switch: names of the compared mapper's values
}

type field struct {
	n    *Node
	slot int
	name string
}

type bitPart struct {
	size   int
	signed bool
	slot   int
}

type ref struct {
	up, slot int
	mask     int64
}

// Name returns the schema type name the node was compiled from, if any.
func (n *Node) Name() string { return n.name }

// Mapping returns the names of a mapper node, or nil.
func (n *Node) Mapping() []string { return n.names }

type Schema struct {
	raw   map[string]json.RawMessage
	named map[string]*Node
}

var errSchema = errors.New("pdef: unsupported schema")

func Compile(types map[string]json.RawMessage) *Schema {
	return &Schema{raw: types, named: map[string]*Node{}}
}

// Type compiles (once) and returns a named type.
func (s *Schema) Type(name string) (*Node, error) {
	return s.named1(name)
}

func (s *Schema) named1(name string) (*Node, error) {
	if n, ok := s.named[name]; ok {
		if n == nil {
			return nil, fmt.Errorf("%w: %s", errSchema, name)
		}
		return n, nil
	}
	if n := native(name); n != nil {
		return n, nil
	}
	raw, ok := s.raw[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown type %q", errSchema, name)
	}
	var def any
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("pdef %s: %w", name, err)
	}
	ph := &Node{name: name}
	s.named[name] = ph
	n, err := s.compile(def, nil, false)
	if err != nil {
		s.named[name] = nil
		return nil, fmt.Errorf("pdef %s: %w", name, err)
	}
	*ph = *n
	ph.name = name
	return ph, nil
}

func native(name string) *Node {
	switch name {
	case "void":
		return &Node{kind: kVoid}
	case "bool", "i8", "u8":
		return &Node{kind: kFixed, size: 1, signed: name == "i8"}
	case "i16", "u16":
		return &Node{kind: kFixed, size: 2, signed: name == "i16"}
	case "i32", "u32", "f32":
		return &Node{kind: kFixed, size: 4, signed: name == "i32"}
	case "i64", "u64", "f64":
		return &Node{kind: kFixed, size: 8, signed: name == "i64"}
	case "UUID":
		return &Node{kind: kFixed, size: 16}
	case "varint", "optvarint", "ContainerID":
		return &Node{kind: kVarInt}
	case "varlong":
		return &Node{kind: kVarLong}
	case "anonymousNbt", "anonOptionalNbt", "nbt", "optionalNbt":
		return &Node{kind: kNBT}
	case "restBuffer":
		return &Node{kind: kRest}
	case "string":
		return &Node{kind: kString}
	case "lpVec3":
		return &Node{kind: kLpVec3}
	}
	return nil
}

type scope struct {
	parent  *scope
	slots   map[string]int
	mappers map[string][]string
	n       *int
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, slots: map[string]int{}, mappers: map[string][]string{}, n: new(int)}
}

func (sc *scope) add(name string) int {
	i := *sc.n
	*sc.n++
	if name != "" {
		sc.slots[name] = i
	}
	return i
}

func (sc *scope) resolve(path string) (ref, []string, error) {
	segs := strings.Split(path, "/")
	cur, up := sc, 0
	for len(segs) > 0 && segs[0] == ".." {
		if cur == nil || cur.parent == nil {
			return ref{}, nil, fmt.Errorf("%w: path %q escapes type", errSchema, path)
		}
		cur, up, segs = cur.parent, up+1, segs[1:]
	}
	if cur == nil || len(segs) == 0 {
		return ref{}, nil, fmt.Errorf("%w: path %q", errSchema, path)
	}
	full := strings.Join(segs, "/")
	if i, ok := cur.slots[full]; ok {
		return ref{up: up, slot: i}, cur.mappers[full], nil
	}
	if len(segs) == 2 {
		if i, ok := cur.slots[segs[0]]; ok {
			if m, ok := cur.mappers[segs[0]+"#flags"]; ok {
				for bi, f := range m {
					if f == segs[1] {
						return ref{up: up, slot: i, mask: 1 << bi}, nil, nil
					}
				}
			}
		}
	}
	return ref{}, nil, fmt.Errorf("%w: unresolved path %q", errSchema, path)
}

func (s *Schema) compile(def any, sc *scope, anon bool) (*Node, error) {
	switch d := def.(type) {
	case string:
		if n := native(d); n != nil {
			return n, nil
		}
		return s.named1(d)
	case []any:
		if len(d) != 2 {
			return nil, fmt.Errorf("%w: %v", errSchema, d)
		}
		k, _ := d[0].(string)
		switch k {
		case "container":
			return s.container(d[1], sc, anon)
		case "pstring":
			return &Node{kind: kString}, nil
		case "buffer":
			o, _ := d[1].(map[string]any)
			if c, ok := o["count"].(float64); ok {
				return &Node{kind: kBuffer, size: int(c)}, nil
			}
			return &Node{kind: kBuffer, size: -1}, nil
		case "option":
			e, err := s.compile(d[1], sc, false)
			if err != nil {
				return nil, err
			}
			return &Node{kind: kOption, elem: e}, nil
		case "array":
			o, _ := d[1].(map[string]any)
			e, err := s.compile(o["type"], sc, false)
			if err != nil {
				return nil, err
			}
			n := &Node{kind: kArray, elem: e, size: -1}
			switch c := o["count"].(type) {
			case float64:
				n.size = int(c)
			case string:
				if sc == nil {
					return nil, fmt.Errorf("%w: count %q outside container", errSchema, c)
				}
				r, _, err := sc.resolve(c)
				if err != nil {
					return nil, err
				}
				n.count = &r
			}
			return n, nil
		case "mapper":
			o, _ := d[1].(map[string]any)
			base, err := s.compile(o["type"], sc, false)
			if err != nil {
				return nil, err
			}
			mp, _ := o["mappings"].(map[string]any)
			names := make([]string, 0, len(mp))
			for key, v := range mp {
				i, err := strconv.ParseInt(key, 0, 32)
				if err != nil || i < 0 || i > 1<<16 {
					return nil, fmt.Errorf("%w: mapper key %q", errSchema, key)
				}
				for int(i) >= len(names) {
					names = append(names, "")
				}
				names[i], _ = v.(string)
			}
			n := *base
			n.names = names
			n.name = ""
			return &n, nil
		case "bitflags":
			o, _ := d[1].(map[string]any)
			base, err := s.compile(o["type"], sc, false)
			if err != nil {
				return nil, err
			}
			n := *base
			n.name = ""
			switch f := o["flags"].(type) {
			case []any:
				for _, x := range f {
					name, _ := x.(string)
					n.flags = append(n.flags, name)
				}
			case map[string]any:
				for name, v := range f {
					mask, _ := v.(float64)
					b := 0
					for b < 63 && int64(1)<<b != int64(mask) {
						b++
					}
					for b >= len(n.flags) {
						n.flags = append(n.flags, "")
					}
					n.flags[b] = name
				}
			}
			return &n, nil
		case "bitfield":
			parts, _ := d[1].([]any)
			n := &Node{kind: kBitfield}
			total := 0
			for _, p := range parts {
				o, _ := p.(map[string]any)
				size, _ := o["size"].(float64)
				signed, _ := o["signed"].(bool)
				bp := bitPart{size: int(size), signed: signed, slot: -1}
				total += int(size)
				n.parts = append(n.parts, bp)
			}
			if total%8 != 0 || total == 0 || total > 64 {
				return nil, fmt.Errorf("%w: bitfield of %d bits", errSchema, total)
			}
			n.size = total / 8
			return n, nil
		case "switch":
			return s.sw(d[1], sc, anon)
		case "registryEntryHolder":
			o, _ := d[1].(map[string]any)
			ow, _ := o["otherwise"].(map[string]any)
			alt, err := s.compile(ow["type"], sc, false)
			if err != nil {
				return nil, err
			}
			return &Node{kind: kHolder, alt: alt}, nil
		case "registryEntryHolderSet":
			o, _ := d[1].(map[string]any)
			b, _ := o["base"].(map[string]any)
			ow, _ := o["otherwise"].(map[string]any)
			base, err := s.compile(b["type"], sc, false)
			if err != nil {
				return nil, err
			}
			alt, err := s.compile(ow["type"], sc, false)
			if err != nil {
				return nil, err
			}
			return &Node{kind: kHolderSet, elem: alt, alt: base}, nil
		case "entityMetadataLoop":
			o, _ := d[1].(map[string]any)
			e, err := s.compile(o["type"], sc, false)
			if err != nil {
				return nil, err
			}
			end, _ := o["endVal"].(float64)
			return &Node{kind: kMetaLoop, elem: e, endVal: int(end)}, nil
		case "topBitSetTerminatedArray":
			o, _ := d[1].(map[string]any)
			e, err := s.compile(o["type"], sc, false)
			if err != nil {
				return nil, err
			}
			return &Node{kind: kTopBitArray, elem: e}, nil
		}
		return nil, fmt.Errorf("%w: %q", errSchema, k)
	}
	return nil, fmt.Errorf("%w: %T", errSchema, def)
}

func (s *Schema) container(def any, sc *scope, anon bool) (*Node, error) {
	list, _ := def.([]any)
	own := sc
	n := &Node{kind: kContainer}
	if !anon || sc == nil {
		own = newScope(sc)
	}
	for _, x := range list {
		o, _ := x.(map[string]any)
		name, _ := o["name"].(string)
		isAnon, _ := o["anon"].(bool)
		var t *Node
		var err error
		if isAnon {
			t, err = s.compileInline(o["type"], own)
		} else {
			t, err = s.compile(o["type"], own, false)
		}
		if err != nil {
			return nil, err
		}
		slot := -1
		if !isAnon {
			slot = own.add(name)
		}
		if name != "" && len(t.names) > 0 {
			own.mappers[name] = t.names
		}
		if name != "" && len(t.flags) > 0 {
			own.mappers[name+"#flags"] = t.flags
		}
		if t.kind == kBitfield && name != "" {
			parts := append([]bitPart(nil), t.parts...)
			raw, _ := o["type"].([]any)
			var defs []any
			if len(raw) == 2 {
				defs, _ = raw[1].([]any)
			}
			for i := range parts {
				pn := ""
				if i < len(defs) {
					po, _ := defs[i].(map[string]any)
					pn, _ = po["name"].(string)
				}
				parts[i].slot = own.add(name + "/" + pn)
			}
			cp := *t
			cp.parts = parts
			t = &cp
		}
		n.fields = append(n.fields, field{n: t, slot: slot, name: name})
	}
	if own != sc {
		n.nslots = *own.n
	}
	return n, nil
}

// compileInline compiles the type of an anonymous field into the enclosing
// scope, expanding named container types in place.
func (s *Schema) compileInline(def any, sc *scope) (*Node, error) {
	if name, ok := def.(string); ok && native(name) == nil {
		raw, ok := s.raw[name]
		if !ok {
			return nil, fmt.Errorf("%w: unknown type %q", errSchema, name)
		}
		var d any
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		def = d
	}
	return s.compile(def, sc, true)
}

func (s *Schema) sw(def any, sc *scope, anon bool) (*Node, error) {
	o, _ := def.(map[string]any)
	path, _ := o["compareTo"].(string)
	if sc == nil {
		return nil, fmt.Errorf("%w: switch outside container", errSchema)
	}
	r, names, err := sc.resolve(path)
	if err != nil {
		return nil, err
	}
	n := &Node{kind: kSwitch, cmp: r, cases: map[int64]*Node{}, keys: names}
	fields, _ := o["fields"].(map[string]any)
	for key, t := range fields {
		v, ok := caseValue(key, names)
		if !ok {
			return nil, fmt.Errorf("%w: switch key %q on %q", errSchema, key, path)
		}
		c, err := s.compile(t, sc, anon)
		if err != nil {
			return nil, err
		}
		n.cases[v] = c
	}
	if d, ok := o["default"]; ok {
		if n.def, err = s.compile(d, sc, anon); err != nil {
			return nil, err
		}
	} else {
		n.def = &Node{kind: kVoid}
	}
	return n, nil
}

func caseValue(key string, names []string) (int64, bool) {
	for i, n := range names {
		if n == key {
			return int64(i), true
		}
	}
	switch key {
	case "true":
		return 1, true
	case "false":
		return 0, true
	}
	v, err := strconv.ParseInt(key, 0, 64)
	return v, err == nil
}

// Cases returns, for a container type holding a mapper field and a switch on
// it (SlotComponent, Particle, entityMetadataEntry), the switch case per name.
// Names without a case map to a void node.
func (s *Schema) Cases(typeName string) (map[string]*Node, []string, error) {
	n, err := s.Type(typeName)
	if err != nil {
		return nil, nil, err
	}
	if n.kind != kContainer {
		return nil, nil, fmt.Errorf("%w: %s is not a container", errSchema, typeName)
	}
	var names []string
	var sw *Node
	for _, f := range n.fields {
		if names == nil && len(f.n.names) > 0 {
			names = f.n.names
		}
		if f.n.kind == kSwitch && sw == nil {
			sw = f.n
		}
	}
	if names == nil || sw == nil {
		return nil, nil, fmt.Errorf("%w: %s has no mapper switch", errSchema, typeName)
	}
	out := make(map[string]*Node, len(names))
	for i, name := range names {
		if c, ok := sw.cases[int64(i)]; ok {
			out[name] = c
		} else {
			out[name] = sw.def
		}
	}
	return out, names, nil
}

// Opaque types are compared by name only: their contents are rewritten by hooks.
var Opaque = map[string]bool{
	"Slot": true, "UntrustedSlot": true, "HashedSlot": true, "ItemStackTemplate": true,
	"SlotComponent": true, "UntrustedSlotComponent": true, "Particle": true, "entityMetadata": true,
}

// Convertible lists opaque type pairs whose hooks can write the second form.
var Convertible = map[[2]string]bool{{"ItemStackTemplate", "Slot"}: true}

// Equal reports whether a and b have the same wire layout and enum values.
func Equal(a, b *Node) bool {
	return equal(a, b, map[[2]*Node]bool{})
}

func equal(a, b *Node, seen map[[2]*Node]bool) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if Opaque[a.name] || Opaque[b.name] {
		return a.name == b.name || Convertible[[2]string{a.name, b.name}]
	}
	k := [2]*Node{a, b}
	if seen[k] {
		return true
	}
	seen[k] = true
	if a.kind != b.kind || a.size != b.size || a.signed != b.signed || a.nslots != b.nslots ||
		a.endVal != b.endVal || a.cmp != b.cmp || len(a.fields) != len(b.fields) ||
		len(a.parts) != len(b.parts) || len(a.cases) != len(b.cases) ||
		!slicesEqual(a.names, b.names) || !slicesEqual(a.flags, b.flags) {
		return false
	}
	if (a.count == nil) != (b.count == nil) || (a.count != nil && *a.count != *b.count) {
		return false
	}
	for i := range a.parts {
		if a.parts[i] != b.parts[i] {
			return false
		}
	}
	for i := range a.fields {
		if a.fields[i].slot != b.fields[i].slot || !equal(a.fields[i].n, b.fields[i].n, seen) {
			return false
		}
	}
	for v, c := range a.cases {
		if !equal(c, b.cases[v], seen) {
			return false
		}
	}
	return equal(a.elem, b.elem, seen) && equal(a.alt, b.alt, seen) && equal(a.def, b.def, seen)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Void reports whether the node encodes nothing.
func (n *Node) Void() bool { return n == nil || n.kind == kVoid }

// FindSwitch searches typeName for a container field called field holding a
// switch on a mapper and returns its cases by mapper name.
func (s *Schema) FindSwitch(typeName, field string) (map[string]*Node, []string, error) {
	n, err := s.Type(typeName)
	if err != nil {
		return nil, nil, err
	}
	sw := findField(n, field, map[*Node]bool{})
	if sw == nil || sw.keys == nil {
		return nil, nil, fmt.Errorf("%w: no switch %q in %s", errSchema, field, typeName)
	}
	out := make(map[string]*Node, len(sw.keys))
	for i, name := range sw.keys {
		if c, ok := sw.cases[int64(i)]; ok {
			out[name] = c
		} else {
			out[name] = sw.def
		}
	}
	return out, sw.keys, nil
}

func findField(n *Node, field string, seen map[*Node]bool) *Node {
	if n == nil || seen[n] {
		return nil
	}
	seen[n] = true
	for _, f := range n.fields {
		if f.name == field && f.n.kind == kSwitch {
			return f.n
		}
		if r := findField(f.n, field, seen); r != nil {
			return r
		}
	}
	for _, c := range n.cases {
		if r := findField(c, field, seen); r != nil {
			return r
		}
	}
	for _, x := range []*Node{n.def, n.elem, n.alt} {
		if r := findField(x, field, seen); r != nil {
			return r
		}
	}
	return nil
}
