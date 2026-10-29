// Package item implements protocol 777 item stacks: data component patches,
// their wire and hashed encodings, default (prototype) components, stack
// equality and merge rules, and the typed components gameplay relies on.
package item

import (
	"bytes"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Stack is an item stack value. The zero value is the empty stack. Patch
// values are immutable and may be shared between copies.
type Stack struct {
	Item  int32
	Count int32
	Patch Patch
}

// Patch is a data component patch in insertion order, as vanilla keeps it.
// A removed entry clears a prototype component.
type Patch struct {
	e []entry
}

type entry struct {
	t       Type
	removed bool
	v       []byte
}

func (s Stack) IsEmpty() bool { return s.Count <= 0 || s.Item == 0 }

// Empty normalises any empty stack to the zero value.
func (s Stack) Norm() Stack {
	if s.IsEmpty() {
		return Stack{}
	}
	return s
}

func (s Stack) WithCount(n int32) Stack {
	if n <= 0 {
		return Stack{}
	}
	s.Count = n
	return s
}

// Split removes up to n items, returning them; s keeps the rest.
func (s *Stack) Split(n int32) Stack {
	n = min(n, s.Count)
	out := s.WithCount(n)
	*s = s.WithCount(s.Count - n)
	return out
}

func (s *Stack) Grow(n int32)   { *s = s.WithCount(s.Count + n) }
func (s *Stack) Shrink(n int32) { *s = s.WithCount(s.Count - n) }

func (p Patch) Len() int { return len(p.e) }

func (p Patch) index(t Type) int {
	for i := range p.e {
		if p.e[i].t == t {
			return i
		}
	}
	return -1
}

// Get returns the raw value of t in the patch; removed reports an explicit
// removal, ok whether the patch mentions t at all.
func (p Patch) Get(t Type) (v []byte, removed, ok bool) {
	if i := p.index(t); i >= 0 {
		return p.e[i].v, p.e[i].removed, true
	}
	return nil, false, false
}

func (p Patch) with(e entry) Patch {
	out := make([]entry, len(p.e), len(p.e)+1)
	copy(out, p.e)
	for i := range out {
		if out[i].t == e.t {
			out[i] = e
			return Patch{out}
		}
	}
	return Patch{append(out, e)}
}

// Set returns p with t set to the encoded value v.
func (p Patch) Set(t Type, v []byte) Patch { return p.with(entry{t: t, v: bytes.Clone(v)}) }

// Remove returns p with t marked removed.
func (p Patch) Remove(t Type) Patch { return p.with(entry{t: t, removed: true}) }

// Forget returns p without any entry for t.
func (p Patch) Forget(t Type) Patch {
	i := p.index(t)
	if i < 0 {
		return p
	}
	out := make([]entry, 0, len(p.e)-1)
	out = append(out, p.e[:i]...)
	return Patch{append(out, p.e[i+1:]...)}
}

// Each calls f for every entry in order.
func (p Patch) Each(f func(t Type, v []byte, removed bool)) {
	for _, e := range p.e {
		f(e.t, e.v, e.removed)
	}
}

// Equal compares patches as maps.
func (p Patch) Equal(o Patch) bool {
	if len(p.e) != len(o.e) {
		return false
	}
	for _, e := range p.e {
		v, rem, ok := o.Get(e.t)
		if !ok || rem != e.removed || !bytes.Equal(v, e.v) {
			return false
		}
	}
	return true
}

// SameItemSameComponents is vanilla's merge test: same item and equal
// component maps. Two empty stacks are equal.
func SameItemSameComponents(a, b Stack) bool {
	if a.IsEmpty() || b.IsEmpty() {
		return a.IsEmpty() && b.IsEmpty()
	}
	return a.Item == b.Item && a.Patch.Equal(b.Patch)
}

// Matches is vanilla ItemStack.matches: same item, count and components.
func Matches(a, b Stack) bool {
	if a.IsEmpty() || b.IsEmpty() {
		return a.IsEmpty() && b.IsEmpty()
	}
	return a.Count == b.Count && SameItemSameComponents(a, b)
}

// AppendPatch encodes a patch: added and removed counts, then the entries.
func AppendPatch(b []byte, p Patch) []byte {
	add, rem := 0, 0
	for _, e := range p.e {
		if e.removed {
			rem++
		} else {
			add++
		}
	}
	b = proto.AppendVarInt(b, int32(add))
	b = proto.AppendVarInt(b, int32(rem))
	for _, e := range p.e {
		if !e.removed {
			b = proto.AppendVarInt(b, int32(e.t))
			b = append(b, e.v...)
		}
	}
	for _, e := range p.e {
		if e.removed {
			b = proto.AppendVarInt(b, int32(e.t))
		}
	}
	return b
}

// ReadPatch decodes a patch, validating every component value.
func ReadPatch(r *proto.Reader) (Patch, error) { return readPatch(r, 0) }

func readPatch(r *proto.Reader, d int) (Patch, error) {
	add, err := count(r, maxPatchLen)
	if err != nil {
		return Patch{}, err
	}
	rem, err := count(r, maxPatchLen)
	if err != nil {
		return Patch{}, err
	}
	if add+rem == 0 {
		return Patch{}, nil
	}
	e := make([]entry, 0, add+rem)
	for range add {
		t, err := typeOf(r)
		if err != nil {
			return Patch{}, err
		}
		start := r.Off
		if err := skippers[t](r, d+1); err != nil {
			return Patch{}, fmt.Errorf("component %s: %w", t, err)
		}
		e = append(e, entry{t: t, v: bytes.Clone(r.B[start:r.Off])})
	}
	for range rem {
		t, err := typeOf(r)
		if err != nil {
			return Patch{}, err
		}
		e = append(e, entry{t: t, removed: true})
	}
	p := Patch{}
	for _, x := range e {
		p.e = appendUnique(p.e, x)
	}
	return p, nil
}

func appendUnique(es []entry, x entry) []entry {
	for i := range es {
		if es[i].t == x.t {
			es[i] = x
			return es
		}
	}
	return append(es, x)
}

// AppendStack encodes s with ItemStack.OPTIONAL_STREAM_CODEC.
func AppendStack(b []byte, s Stack) []byte {
	if s.IsEmpty() {
		return append(b, 0)
	}
	b = proto.AppendVarInt(b, s.Count)
	b = proto.AppendVarInt(b, s.Item)
	return AppendPatch(b, s.Patch)
}

// ReadStack decodes ItemStack.OPTIONAL_STREAM_CODEC.
func ReadStack(r *proto.Reader) (Stack, error) {
	n, err := r.VarInt()
	if err != nil {
		return Stack{}, err
	}
	if n <= 0 {
		return Stack{}, nil
	}
	id, err := r.VarInt()
	if err != nil {
		return Stack{}, err
	}
	if id <= 0 || int(id) >= numItems() {
		return Stack{}, fmt.Errorf("%w: item id %d", ErrMalformed, id)
	}
	p, err := readPatch(r, 0)
	if err != nil {
		return Stack{}, err
	}
	return Stack{Item: id, Count: n, Patch: p}, nil
}

// DecodeStack decodes a whole buffer holding one optional stack.
func DecodeStack(b []byte) (Stack, error) {
	r := proto.Reader{B: b}
	s, err := ReadStack(&r)
	if err != nil {
		return Stack{}, err
	}
	if r.Remaining() != 0 {
		return Stack{}, fmt.Errorf("%w: %d trailing bytes", ErrMalformed, r.Remaining())
	}
	return s, nil
}

// AppendTemplate encodes ItemStackTemplate (item, count, patch).
func AppendTemplate(b []byte, s Stack) []byte {
	b = proto.AppendVarInt(b, s.Item)
	b = proto.AppendVarInt(b, s.Count)
	return AppendPatch(b, s.Patch)
}

func ReadTemplate(r *proto.Reader) (Stack, error) {
	id, err := r.VarInt()
	if err != nil {
		return Stack{}, err
	}
	n, err := r.VarInt()
	if err != nil {
		return Stack{}, err
	}
	p, err := readPatch(r, 1)
	if err != nil {
		return Stack{}, err
	}
	return Stack{Item: id, Count: n, Patch: p}, nil
}

func (t Type) String() string {
	if t >= 0 && t < NumTypes {
		return "minecraft:" + typeNames[t]
	}
	return fmt.Sprintf("component#%d", int32(t))
}

// TypeByName resolves a component name with or without namespace.
func TypeByName(name string) (Type, bool) {
	if len(name) > 10 && name[:10] == "minecraft:" {
		name = name[10:]
	}
	for i, n := range typeNames {
		if n == name {
			return Type(i), true
		}
	}
	return 0, false
}
