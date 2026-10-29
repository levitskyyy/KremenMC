package data

import (
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data/gen"
)

const Air StateID = 0

func (r *Registry) NumStates() int { return len(r.stateBlock) }

func (r *Registry) Valid(s StateID) bool { return int(s) < len(r.stateBlock) }

// BlockOf returns the block owning s, or nil for an invalid ID.
func (r *Registry) BlockOf(s StateID) *Block {
	if int(s) >= len(r.stateBlock) {
		return nil
	}
	return &r.Blocks[r.stateBlock[s]]
}

func (r *Registry) BlockID(s StateID) BlockID {
	if int(s) >= len(r.stateBlock) {
		return 0
	}
	return r.stateBlock[s]
}

func (r *Registry) BlockByName(name string) (*Block, bool) {
	id, ok := r.blockByName[trimNS(name)]
	if !ok {
		return nil, false
	}
	return &r.Blocks[id], true
}

func (r *Registry) DefaultState(name string) (StateID, bool) {
	b, ok := r.BlockByName(name)
	if !ok {
		return 0, false
	}
	return b.DefaultState, true
}

func (b *Block) PropIndex(name string) int {
	for i := range b.Props {
		if b.Props[i].Name == name {
			return i
		}
	}
	return -1
}

func (p *Property) ValueIndex(v string) int {
	for i, x := range p.Values {
		if x == v {
			return i
		}
	}
	return -1
}

// ValueIndex returns the index of property prop's value in state s.
func (b *Block) ValueIndex(s StateID, prop int) int {
	p := &b.Props[prop]
	return int((uint32(s-b.MinState) / p.stride) % uint32(len(p.Values)))
}

// WithIndex returns s with property prop set to value index v.
func (b *Block) WithIndex(s StateID, prop, v int) StateID {
	p := &b.Props[prop]
	cur := (uint32(s-b.MinState) / p.stride) % uint32(len(p.Values))
	return StateID(int64(s) + (int64(v)-int64(cur))*int64(p.stride))
}

// StateAt builds a state from value indices in property order.
func (b *Block) StateAt(vals []int) StateID {
	s := b.MinState
	for i := range b.Props {
		s += StateID(uint32(vals[i]) * b.Props[i].stride)
	}
	return s
}

// Value returns the string value of a property in state s.
func (r *Registry) Value(s StateID, prop string) (string, bool) {
	b := r.BlockOf(s)
	if b == nil {
		return "", false
	}
	i := b.PropIndex(prop)
	if i < 0 {
		return "", false
	}
	return b.Props[i].Values[b.ValueIndex(s, i)], true
}

// With returns s with prop set to value. ok is false if the block lacks the property or value.
func (r *Registry) With(s StateID, prop, value string) (StateID, bool) {
	b := r.BlockOf(s)
	if b == nil {
		return s, false
	}
	i := b.PropIndex(prop)
	if i < 0 {
		return s, false
	}
	v := b.Props[i].ValueIndex(value)
	if v < 0 {
		return s, false
	}
	return b.WithIndex(s, i, v), true
}

// StateFrom resolves a block name plus property lookup to a state.
// Missing or unknown property values keep the default state's value.
func (r *Registry) StateFrom(name string, prop func(key string) (string, bool)) (StateID, bool) {
	b, ok := r.BlockByName(name)
	if !ok {
		return 0, false
	}
	s := b.DefaultState
	if prop == nil {
		return s, true
	}
	for i := range b.Props {
		if v, ok := prop(b.Props[i].Name); ok {
			if vi := b.Props[i].ValueIndex(v); vi >= 0 {
				s = b.WithIndex(s, i, vi)
			}
		}
	}
	return s, true
}

// ParseState parses "minecraft:name[k=v,...]".
func (r *Registry) ParseState(str string) (StateID, bool) {
	name, rest, hasProps := strings.Cut(str, "[")
	if !hasProps {
		return r.DefaultState(name)
	}
	rest = strings.TrimSuffix(rest, "]")
	return r.StateFrom(name, func(key string) (string, bool) {
		for kv := range strings.SplitSeq(rest, ",") {
			k, v, _ := strings.Cut(kv, "=")
			if strings.TrimSpace(k) == key {
				return strings.TrimSpace(v), true
			}
		}
		return "", false
	})
}

// AppendState appends the "minecraft:name[k=v,...]" form of s.
func (r *Registry) AppendState(dst []byte, s StateID) []byte {
	b := r.BlockOf(s)
	if b == nil {
		return append(dst, "minecraft:air"...)
	}
	dst = append(dst, "minecraft:"...)
	dst = append(dst, b.Name...)
	if len(b.Props) == 0 {
		return dst
	}
	dst = append(dst, '[')
	for i := range b.Props {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, b.Props[i].Name...)
		dst = append(dst, '=')
		dst = append(dst, b.Props[i].Values[b.ValueIndex(s, i)]...)
	}
	return append(dst, ']')
}

func (r *Registry) StateString(s StateID) string { return string(r.AppendState(nil, s)) }

func (r *Registry) Flags(s StateID) uint8 {
	if int(s) >= len(r.stateFlags) {
		return 0
	}
	return r.stateFlags[s]
}

func (r *Registry) IsAir(s StateID) bool       { return r.Flags(s)&FlagAir != 0 }
func (r *Registry) IsSolid(s StateID) bool     { return r.Flags(s)&FlagSolid != 0 }
func (r *Registry) HasFluid(s StateID) bool    { return r.Flags(s)&FlagFluid != 0 }
func (r *Registry) IsLeaves(s StateID) bool    { return r.Flags(s)&FlagLeaves != 0 }
func (r *Registry) IsFullCube(s StateID) bool  { return r.Flags(s)&FlagFullCube != 0 }
func (r *Registry) Transparent(s StateID) bool { return r.Flags(s)&FlagTransparent != 0 }

func (r *Registry) LightEmission(s StateID) uint8 {
	if int(s) >= len(r.stateLight) {
		return 0
	}
	return r.stateLight[s] >> 4
}

func (r *Registry) LightFilter(s StateID) uint8 {
	if int(s) >= len(r.stateLight) {
		return 15
	}
	return r.stateLight[s] & 15
}

// Collision returns the collision boxes of s in block-local coordinates. Do not modify.
func (r *Registry) Collision(s StateID) []Box {
	if int(s) >= len(r.stateShape) {
		return nil
	}
	return r.shapes[r.stateShape[s]]
}

func (r *Registry) ItemByName(name string) (ItemID, bool) {
	id, ok := r.itemByName[trimNS(name)]
	return id, ok
}

func (r *Registry) ItemName(id ItemID) string {
	if id < 0 || int(id) >= len(r.Items) {
		return ""
	}
	return r.Items[id].Name
}

func (r *Registry) EntityByName(name string) (*EntityType, bool) {
	id, ok := r.entityByName[trimNS(name)]
	if !ok {
		return nil, false
	}
	return &r.Entities[id], true
}

func (r *Registry) Entity(id EntityID) *EntityType {
	if id < 0 || int(id) >= len(r.Entities) {
		return nil
	}
	return &r.Entities[id]
}

func (r *Registry) BiomeByName(name string) (BiomeID, bool) {
	id, ok := r.biomeByName[trimNS(name)]
	return id, ok
}

func (r *Registry) BiomeName(id BiomeID) string {
	if id < 0 || int(id) >= len(r.Biomes) {
		return ""
	}
	return r.Biomes[id].Name
}

var canon = mustLoad(gen.Canonical)

func mustLoad(v string) *Registry {
	r, err := Load(v)
	if err != nil {
		panic(err)
	}
	return r
}

// Canonical returns the registry of the native protocol version.
func Canonical() *Registry { return canon }

func NumStates() int { return len(canon.stateBlock) }

func IsAir(s StateID) bool {
	f := canon.stateFlags
	return int(s) < len(f) && f[s]&FlagAir != 0
}

func Flags(s StateID) uint8 {
	f := canon.stateFlags
	if int(s) >= len(f) {
		return 0
	}
	return f[s]
}

func LightEmission(s StateID) uint8 {
	l := canon.stateLight
	if int(s) >= len(l) {
		return 0
	}
	return l[s] >> 4
}

func LightFilter(s StateID) uint8 {
	l := canon.stateLight
	if int(s) >= len(l) {
		return 15
	}
	return l[s] & 15
}

func Collision(s StateID) []Box { return canon.Collision(s) }

func BlockOf(s StateID) *Block { return canon.BlockOf(s) }

func DefaultState(name string) (StateID, bool) { return canon.DefaultState(name) }

func ParseState(s string) (StateID, bool) { return canon.ParseState(s) }

func With(s StateID, prop, value string) (StateID, bool) { return canon.With(s, prop, value) }

func StateString(s StateID) string { return canon.StateString(s) }

// MustState resolves a state string against the canonical registry and panics if unknown.
// Intended for package-level vars referring to vanilla blocks.
func MustState(s string) StateID {
	id, ok := canon.ParseState(s)
	if !ok {
		panic("data: unknown block state " + s)
	}
	return id
}
