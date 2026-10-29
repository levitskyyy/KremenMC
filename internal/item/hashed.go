package item

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Hashed is the client's hashed view of a stack (HashedStack), sent in
// container click packets.
type Hashed struct {
	Item    int32
	Count   int32
	Added   []HashedComponent
	Removed []Type
}

type HashedComponent struct {
	Type Type
	Hash int32
}

func (h Hashed) IsEmpty() bool { return h.Item == 0 }

// ReadHashed decodes HashedStack.STREAM_CODEC.
func ReadHashed(r *proto.Reader) (Hashed, error) {
	ok, err := r.Bool()
	if err != nil || !ok {
		return Hashed{}, err
	}
	var h Hashed
	if h.Item, err = r.VarInt(); err != nil {
		return Hashed{}, err
	}
	if h.Count, err = r.VarInt(); err != nil {
		return Hashed{}, err
	}
	n, err := count(r, maxPatchLen)
	if err != nil {
		return Hashed{}, err
	}
	h.Added = make([]HashedComponent, 0, n)
	for range n {
		t, err := typeOf(r)
		if err != nil {
			return Hashed{}, err
		}
		v, err := r.I32()
		if err != nil {
			return Hashed{}, err
		}
		h.Added = append(h.Added, HashedComponent{t, v})
	}
	if n, err = count(r, maxPatchLen); err != nil {
		return Hashed{}, err
	}
	h.Removed = make([]Type, 0, n)
	for range n {
		t, err := typeOf(r)
		if err != nil {
			return Hashed{}, err
		}
		h.Removed = append(h.Removed, t)
	}
	return h, nil
}

// AppendHashed encodes s as a HashedStack. Components we cannot hash get
// hash 0, which never matches a real client hash in practice.
func AppendHashed(b []byte, s Stack) []byte {
	if s.IsEmpty() {
		return append(b, 0)
	}
	b = append(b, 1)
	b = proto.AppendVarInt(b, s.Item)
	b = proto.AppendVarInt(b, s.Count)
	add, rem := 0, 0
	for _, e := range s.Patch.e {
		if e.removed {
			rem++
		} else {
			add++
		}
	}
	b = proto.AppendVarInt(b, int32(add))
	for _, e := range s.Patch.e {
		if !e.removed {
			h, _ := HashComponent(e.t, e.v)
			b = proto.AppendVarInt(b, int32(e.t))
			b = append(b, byte(h>>24), byte(h>>16), byte(h>>8), byte(h))
		}
	}
	b = proto.AppendVarInt(b, int32(rem))
	for _, e := range s.Patch.e {
		if e.removed {
			b = proto.AppendVarInt(b, int32(e.t))
		}
	}
	return b
}

// Matches reports whether the client's hashed view equals s, as vanilla's
// HashedStack.matches. Components we cannot hash never match.
func (h Hashed) Matches(s Stack) bool {
	if s.IsEmpty() || h.IsEmpty() {
		return s.IsEmpty() && h.IsEmpty()
	}
	if h.Item != s.Item || h.Count != s.Count {
		return false
	}
	add, rem := 0, 0
	for _, e := range s.Patch.e {
		if e.removed {
			rem++
			if !containsType(h.Removed, e.t) {
				return false
			}
			continue
		}
		add++
		want, ok := hashedOf(h.Added, e.t)
		if !ok {
			return false
		}
		got, err := HashComponent(e.t, e.v)
		if err != nil || got != want {
			return false
		}
	}
	return add == len(h.Added) && rem == len(h.Removed)
}

func containsType(ts []Type, t Type) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

func hashedOf(hs []HashedComponent, t Type) (int32, bool) {
	for _, x := range hs {
		if x.Type == t {
			return x.Hash, true
		}
	}
	return 0, false
}
