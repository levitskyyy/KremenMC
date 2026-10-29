package entity

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// vanilla-26.3.txt is produced by the reference probe kremenmc.probe.combat.Attrs
// (see TestVanillaTable): the attribute registry, default attributes per
// living type, per-type gravity/drag and mob effect modifiers.
//
//go:embed data/vanilla-26.3.txt
var vanillaTable string

// Attr is an attribute registry id (network id).
type Attr int32

const (
	AttrAirDragModifier Attr = iota
	AttrArmor
	AttrArmorToughness
	AttrAttackDamage
	AttrAttackKnockback
	AttrAttackSpeed
	AttrBelowNameDistance
	AttrBlockBreakSpeed
	AttrBlockInteractionRange
	AttrBounciness
	AttrBurningTime
	AttrCameraDistance
	AttrExplosionKnockbackResistance
	AttrEntityInteractionRange
	AttrFallDamageMultiplier
	AttrFlyingSpeed
	AttrFollowRange
	AttrFrictionModifier
	AttrGravity
	AttrJumpStrength
	AttrKnockbackResistance
	AttrLuck
	AttrMaxAbsorption
	AttrMaxHealth
	AttrMiningEfficiency
	AttrMovementEfficiency
	AttrMovementSpeed
	AttrNameTagDistance
	AttrOxygenBonus
	AttrSafeFallDistance
	AttrScale
	AttrSneakingSpeed
	AttrSpawnReinforcements
	AttrStepHeight
	AttrSubmergedMiningSpeed
	AttrSweepingDamageRatio
	AttrTemptRange
	AttrWaterMovementEfficiency
	AttrWaypointTransmitRange
	AttrWaypointReceiveRange
	NumAttrs
)

type AttrInfo struct {
	Name              string // without namespace
	Default, Min, Max float64
	Sync              bool
}

// Op is an attribute modifier operation (wire ids).
type Op int8

const (
	OpAddValue Op = iota
	OpAddMultipliedBase
	OpAddMultipliedTotal
)

// OpByName parses a modifier operation name.
func OpByName(s string) (Op, bool) {
	switch s {
	case "add_value":
		return OpAddValue, true
	case "add_multiplied_base":
		return OpAddMultipliedBase, true
	case "add_multiplied_total":
		return OpAddMultipliedTotal, true
	}
	return 0, false
}

type Modifier struct {
	ID     string // namespaced identifier
	Amount float64
	Op     Op
}

// EffectModifier is a mob effect's modifier at amplifier 0; vanilla scales
// the amount by amplifier+1.
type EffectModifier struct {
	Attr Attr
	Modifier
}

// Physics is a type's default gravity and air drag.
type Physics struct {
	Gravity, Drag float64
	Width, Height float64
	Eye           float64
	Class         string
}

type typeAttrs struct {
	has  [NumAttrs]bool
	base [NumAttrs]float64
}

var (
	tableOnce sync.Once
	attrInfo  [NumAttrs]AttrInfo
	attrByNm  map[string]Attr
	typeTable map[string]*typeAttrs
	physTable map[string]Physics
	effTable  map[string][]EffectModifier
)

func loadTable() {
	attrByNm = map[string]Attr{}
	typeTable = map[string]*typeAttrs{}
	physTable = map[string]Physics{}
	effTable = map[string][]EffectModifier{}
	for line := range strings.Lines(vanillaTable) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "attr":
			id, _ := strconv.Atoi(f[1])
			if id < 0 || id >= int(NumAttrs) {
				panic(fmt.Sprintf("entity: attribute table: id %d out of range", id))
			}
			name := strings.TrimPrefix(f[2], "minecraft:")
			attrInfo[id] = AttrInfo{Name: name, Default: num(f[3]), Min: num(f[4]), Max: num(f[5]), Sync: f[6] == "true"}
			attrByNm[name] = Attr(id)
		case "base":
			t := &typeAttrs{}
			for _, kv := range f[2:] {
				k, v, hasV := strings.Cut(kv, "=")
				a, ok := attrByNm[k]
				if !ok {
					continue
				}
				t.has[a] = true
				t.base[a] = attrInfo[a].Default
				if hasV {
					t.base[a] = num(v)
				}
			}
			typeTable[strings.TrimPrefix(f[1], "minecraft:")] = t
		case "phys":
			var p Physics
			for _, kv := range f[2:] {
				k, v, _ := strings.Cut(kv, "=")
				switch k {
				case "gravity":
					p.Gravity = num(v)
				case "drag":
					p.Drag = num(v)
				case "w":
					p.Width = num(v)
				case "h":
					p.Height = num(v)
				case "eye":
					p.Eye = num(v)
				case "class":
					p.Class = v
				}
			}
			physTable[strings.TrimPrefix(f[1], "minecraft:")] = p
		case "eff":
			if f[2] != "0" {
				continue
			}
			a, ok := attrByNm[f[3]]
			op, ok2 := OpByName(f[6])
			if !ok || !ok2 {
				continue
			}
			k := strings.TrimPrefix(f[1], "minecraft:")
			effTable[k] = append(effTable[k], EffectModifier{a, Modifier{ID: f[4], Amount: num(f[5]), Op: op}})
		}
	}
}

func num(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(fmt.Sprintf("entity: attribute table: %q: %v", s, err))
	}
	return v
}

func table() { tableOnce.Do(loadTable) }

// AttrInfoOf describes attribute a.
func AttrInfoOf(a Attr) AttrInfo { table(); return attrInfo[a] }

// AttrByName resolves an attribute by name, with or without namespace.
func AttrByName(name string) (Attr, bool) {
	table()
	a, ok := attrByNm[strings.TrimPrefix(name, "minecraft:")]
	return a, ok
}

// PhysicsOf returns the default gravity/drag of entity type name.
func PhysicsOf(name string) (Physics, bool) {
	table()
	p, ok := physTable[strings.TrimPrefix(name, "minecraft:")]
	return p, ok
}

// EffectModifiers returns the attribute modifiers of a mob effect at
// amplifier 0.
func EffectModifiers(effect string) []EffectModifier {
	table()
	return effTable[strings.TrimPrefix(effect, "minecraft:")]
}

// HasAttributes reports whether the type has a default attribute supplier
// (it is a living entity).
func HasAttributes(name string) bool {
	table()
	_, ok := typeTable[strings.TrimPrefix(name, "minecraft:")]
	return ok
}

type attrInst struct {
	base   float64
	mods   []Modifier
	value  float64
	cached bool
}

// Attrs is an entity's attribute map. Not safe for concurrent use.
type Attrs struct {
	has   [NumAttrs]bool
	inst  [NumAttrs]attrInst
	dirty [NumAttrs]bool
}

// NewAttrs returns the default attributes of type name; nil when the type
// is not living.
func NewAttrs(name string) *Attrs {
	table()
	t, ok := typeTable[strings.TrimPrefix(name, "minecraft:")]
	if !ok {
		return nil
	}
	a := &Attrs{has: t.has}
	for i := range a.inst {
		a.inst[i].base = t.base[i]
	}
	return a
}

// NewAttrsFor is NewAttrs for a data entity type.
func NewAttrsFor(t *data.EntityType) *Attrs {
	if t == nil {
		return nil
	}
	return NewAttrs(t.Name)
}

func (a *Attrs) Has(at Attr) bool { return a != nil && at >= 0 && at < NumAttrs && a.has[at] }

// Base returns the base value (the attribute default when absent).
func (a *Attrs) Base(at Attr) float64 {
	if !a.Has(at) {
		return AttrInfoOf(at).Default
	}
	return a.inst[at].base
}

func (a *Attrs) SetBase(at Attr, v float64) {
	if !a.Has(at) || a.inst[at].base == v {
		return
	}
	a.inst[at].base = v
	a.changed(at)
}

func (a *Attrs) changed(at Attr) {
	a.inst[at].cached = false
	a.dirty[at] = true
}

// Value is the modified, sanitized value; absent attributes yield the
// registry default.
func (a *Attrs) Value(at Attr) float64 {
	if !a.Has(at) {
		return AttrInfoOf(at).Default
	}
	in := &a.inst[at]
	if !in.cached {
		in.value = compute(at, in.base, in.mods)
		in.cached = true
	}
	return in.value
}

func compute(at Attr, base float64, mods []Modifier) float64 {
	x := base
	for _, m := range mods {
		if m.Op == OpAddValue {
			x += m.Amount
		}
	}
	y := x
	for _, m := range mods {
		if m.Op == OpAddMultipliedBase {
			y += x * m.Amount
		}
	}
	for _, m := range mods {
		if m.Op == OpAddMultipliedTotal {
			y *= 1 + m.Amount
		}
	}
	info := AttrInfoOf(at)
	return min(max(y, info.Min), info.Max)
}

// Modifiers returns the modifiers of at; the slice must not be modified.
func (a *Attrs) Modifiers(at Attr) []Modifier {
	if !a.Has(at) {
		return nil
	}
	return a.inst[at].mods
}

// SetModifier adds m, replacing a modifier with the same id.
func (a *Attrs) SetModifier(at Attr, m Modifier) {
	if !a.Has(at) {
		return
	}
	in := &a.inst[at]
	for i := range in.mods {
		if in.mods[i].ID == m.ID {
			if in.mods[i] == m {
				return
			}
			in.mods[i] = m
			a.changed(at)
			return
		}
	}
	in.mods = append(in.mods, m)
	a.changed(at)
}

func (a *Attrs) RemoveModifier(at Attr, id string) bool {
	if !a.Has(at) {
		return false
	}
	in := &a.inst[at]
	for i := range in.mods {
		if in.mods[i].ID == id {
			in.mods = append(in.mods[:i], in.mods[i+1:]...)
			a.changed(at)
			return true
		}
	}
	return false
}

// TakeDirty returns and clears the attributes changed since the last call
// that clients see (syncable).
func (a *Attrs) TakeDirty() []Attr {
	if a == nil {
		return nil
	}
	var out []Attr
	for i := range a.dirty {
		if a.dirty[i] {
			a.dirty[i] = false
			if attrInfo[i].Sync {
				out = append(out, Attr(i))
			}
		}
	}
	return out
}

// Synced lists every present syncable attribute (the full spawn sync).
func (a *Attrs) Synced() []Attr {
	if a == nil {
		return nil
	}
	table()
	var out []Attr
	for i := range a.has {
		if a.has[i] && attrInfo[i].Sync {
			out = append(out, Attr(i))
		}
	}
	return out
}
