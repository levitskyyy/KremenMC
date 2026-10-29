package item

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Typed views of the components gameplay reads. Decoders take the wire
// value; registry references are kept as network ids.

type Sound struct {
	ID    int32 // sound_event id, -1 when inline
	Name  string
	Range float32
	Fixed bool // inline range present
}

type Effect struct {
	ID            int32 // mob_effect id
	Amplifier     int32
	Duration      int32
	Ambient       bool
	ShowParticles bool
	ShowIcon      bool
	Hidden        *Effect
}

const (
	ConsumeApplyEffects int32 = iota
	ConsumeRemoveEffects
	ConsumeClearAllEffects
	ConsumeTeleportRandomly
	ConsumePlaySound
)

type HolderSet struct {
	Tag string
	IDs []int32
}

type ConsumeEffect struct {
	Kind        int32
	Effects     []Effect
	Probability float32
	Remove      HolderSet
	Diameter    float32
	Flag        bool
	Sound       Sound
}

type FoodInfo struct {
	Nutrition    int32
	Saturation   float32
	CanAlwaysEat bool
}

// Use animations (ItemUseAnimation ids).
const (
	AnimNone int32 = iota
	AnimEat
	AnimDrink
	AnimBlock
	AnimBow
	AnimSpear
	AnimCrossbow
	AnimSpyglass
	AnimTootHorn
	AnimBrush
	AnimBundle
	AnimTrident
)

type ConsumableInfo struct {
	Seconds   float32
	Animation int32
	Sound     Sound
	Particles bool
	Effects   []ConsumeEffect
}

// Ticks is the use duration in ticks.
func (c ConsumableInfo) Ticks() int32 { return int32(c.Seconds * 20) }

type CooldownInfo struct {
	Seconds float32
	Group   string
}

func (c CooldownInfo) Ticks() int32 { return int32(c.Seconds * 20) }

// Equipment slots (EquipmentSlot ids).
const (
	SlotMainHand int32 = iota
	SlotFeet
	SlotLegs
	SlotChest
	SlotHead
	SlotOffHand
	SlotBody
	SlotSaddle
)

type EquipInfo struct {
	Slot            int32
	EquipSound      Sound
	AssetID         string
	CameraOverlay   string
	Allowed         *HolderSet
	Dispensable     bool
	Swappable       bool
	DamageOnHurt    bool
	EquipOnInteract bool
	CanBeSheared    bool
	ShearingSound   Sound
}

type PotionInfo struct {
	Potion   int32 // -1 when absent
	Color    int32
	HasColor bool
	Effects  []Effect
	Name     string
}

type Enchantment struct {
	ID    int32
	Level int32
}

type rd struct {
	r   proto.Reader
	err error
}

func newRd(b []byte) *rd { return &rd{r: proto.Reader{B: b}} }

func (d *rd) fail(err error) {
	if d.err == nil && err != nil {
		d.err = err
	}
}

func (d *rd) vi() int32    { v, err := d.r.VarInt(); d.fail(err); return v }
func (d *rd) i32() int32   { v, err := d.r.I32(); d.fail(err); return v }
func (d *rd) f32() float32 { v, err := d.r.F32(); d.fail(err); return v }
func (d *rd) b() bool      { v, err := d.r.Bool(); d.fail(err); return v }
func (d *rd) s() string    { v, err := d.r.String(maxString); d.fail(err); return v }
func (d *rd) n() int       { n, err := count(&d.r, maxList); d.fail(err); return n }
func (d *rd) done() error {
	if d.err == nil && d.r.Remaining() != 0 {
		return ErrMalformed
	}
	return d.err
}

func (d *rd) sound() Sound {
	id := d.vi()
	if id != 0 {
		return Sound{ID: id - 1}
	}
	s := Sound{ID: -1, Name: d.s()}
	if d.b() {
		s.Range, s.Fixed = d.f32(), true
	}
	return s
}

func (d *rd) holderSet() HolderSet {
	n := d.vi()
	if n == 0 {
		return HolderSet{Tag: d.s()}
	}
	if n < 0 || int(n-1) > d.r.Remaining() {
		d.fail(ErrMalformed)
		return HolderSet{}
	}
	h := HolderSet{IDs: make([]int32, n-1)}
	for i := range h.IDs {
		h.IDs[i] = d.vi()
	}
	return h
}

func (d *rd) effectDetails(e *Effect) {
	e.Amplifier, e.Duration = d.vi(), d.vi()
	e.Ambient, e.ShowParticles, e.ShowIcon = d.b(), d.b(), d.b()
	if d.b() && d.err == nil {
		h := &Effect{ID: e.ID}
		d.effectDetails(h)
		e.Hidden = h
	}
}

func (d *rd) effect() Effect {
	e := Effect{ID: d.vi()}
	d.effectDetails(&e)
	return e
}

func (d *rd) consumeEffect() ConsumeEffect {
	c := ConsumeEffect{Kind: d.vi(), Probability: 1}
	switch c.Kind {
	case ConsumeApplyEffects:
		n := d.n()
		for range n {
			c.Effects = append(c.Effects, d.effect())
		}
		c.Probability = d.f32()
	case ConsumeRemoveEffects:
		c.Remove = d.holderSet()
	case ConsumeClearAllEffects:
	case ConsumeTeleportRandomly:
		c.Diameter, c.Flag = d.f32(), d.b()
	case ConsumePlaySound:
		c.Sound = d.sound()
	default:
		d.fail(ErrMalformed)
	}
	return c
}

func DecodeVarInt(v []byte) (int32, error) {
	d := newRd(v)
	x := d.vi()
	return x, d.done()
}

func DecodeFood(v []byte) (FoodInfo, error) {
	d := newRd(v)
	f := FoodInfo{Nutrition: d.vi(), Saturation: d.f32(), CanAlwaysEat: d.b()}
	return f, d.done()
}

func DecodeConsumable(v []byte) (ConsumableInfo, error) {
	d := newRd(v)
	c := ConsumableInfo{Seconds: d.f32(), Animation: d.vi(), Sound: d.sound(), Particles: d.b()}
	n := d.n()
	for range n {
		c.Effects = append(c.Effects, d.consumeEffect())
	}
	return c, d.done()
}

func DecodeDeathProtection(v []byte) ([]ConsumeEffect, error) {
	d := newRd(v)
	n := d.n()
	var out []ConsumeEffect
	for range n {
		out = append(out, d.consumeEffect())
	}
	return out, d.done()
}

func DecodeUseCooldown(v []byte) (CooldownInfo, error) {
	d := newRd(v)
	c := CooldownInfo{Seconds: d.f32()}
	if d.b() {
		c.Group = d.s()
	}
	return c, d.done()
}

func DecodeEquippable(v []byte) (EquipInfo, error) {
	d := newRd(v)
	e := EquipInfo{Slot: d.vi(), EquipSound: d.sound()}
	if d.b() {
		e.AssetID = d.s()
	}
	if d.b() {
		e.CameraOverlay = d.s()
	}
	if d.b() {
		h := d.holderSet()
		e.Allowed = &h
	}
	e.Dispensable, e.Swappable, e.DamageOnHurt, e.EquipOnInteract, e.CanBeSheared = d.b(), d.b(), d.b(), d.b(), d.b()
	e.ShearingSound = d.sound()
	return e, d.done()
}

func DecodePotionContents(v []byte) (PotionInfo, error) {
	d := newRd(v)
	p := PotionInfo{Potion: -1}
	if d.b() {
		p.Potion = d.vi()
	}
	if d.b() {
		p.Color, p.HasColor = d.i32(), true
	}
	n := d.n()
	for range n {
		p.Effects = append(p.Effects, d.effect())
	}
	if d.b() {
		p.Name = d.s()
	}
	return p, d.done()
}

func DecodeEnchantments(v []byte) ([]Enchantment, error) {
	d := newRd(v)
	n := d.n()
	out := make([]Enchantment, 0, n)
	for range n {
		out = append(out, Enchantment{ID: d.vi(), Level: d.vi()})
	}
	return out, d.done()
}

func DecodeTemplate(v []byte) (Stack, error) {
	r := proto.Reader{B: v}
	s, err := ReadTemplate(&r)
	if err == nil && r.Remaining() != 0 {
		err = ErrMalformed
	}
	return s, err
}

func EncodeVarInt(v int32) []byte { return proto.AppendVarInt(nil, v) }

func appendSound(b []byte, s Sound) []byte {
	if s.ID >= 0 {
		return proto.AppendVarInt(b, s.ID+1)
	}
	b = append(b, 0)
	b = appendString(b, s.Name)
	if s.Fixed {
		b = append(b, 1)
		return appendF32(b, s.Range)
	}
	return append(b, 0)
}

func appendHolderSet(b []byte, h HolderSet) []byte {
	if h.IDs == nil && h.Tag != "" {
		b = append(b, 0)
		return appendString(b, h.Tag)
	}
	b = proto.AppendVarInt(b, int32(len(h.IDs)+1))
	for _, id := range h.IDs {
		b = proto.AppendVarInt(b, id)
	}
	return b
}

func appendEffectDetails(b []byte, e Effect) []byte {
	b = proto.AppendVarInt(b, e.Amplifier)
	b = proto.AppendVarInt(b, e.Duration)
	b = append(b, b2u(e.Ambient), b2u(e.ShowParticles), b2u(e.ShowIcon))
	if e.Hidden == nil {
		return append(b, 0)
	}
	return appendEffectDetails(append(b, 1), *e.Hidden)
}

func appendEffect(b []byte, e Effect) []byte {
	return appendEffectDetails(proto.AppendVarInt(b, e.ID), e)
}

func appendConsumeEffect(b []byte, c ConsumeEffect) []byte {
	b = proto.AppendVarInt(b, c.Kind)
	switch c.Kind {
	case ConsumeApplyEffects:
		b = proto.AppendVarInt(b, int32(len(c.Effects)))
		for _, e := range c.Effects {
			b = appendEffect(b, e)
		}
		b = appendF32(b, c.Probability)
	case ConsumeRemoveEffects:
		b = appendHolderSet(b, c.Remove)
	case ConsumeTeleportRandomly:
		b = append(appendF32(b, c.Diameter), b2u(c.Flag))
	case ConsumePlaySound:
		b = appendSound(b, c.Sound)
	}
	return b
}

func EncodeFood(f FoodInfo) []byte {
	b := proto.AppendVarInt(nil, f.Nutrition)
	return append(appendF32(b, f.Saturation), b2u(f.CanAlwaysEat))
}

func EncodeConsumable(c ConsumableInfo) []byte {
	b := appendF32(nil, c.Seconds)
	b = proto.AppendVarInt(b, c.Animation)
	b = appendSound(b, c.Sound)
	b = append(b, b2u(c.Particles))
	b = proto.AppendVarInt(b, int32(len(c.Effects)))
	for _, e := range c.Effects {
		b = appendConsumeEffect(b, e)
	}
	return b
}

func EncodeDeathProtection(es []ConsumeEffect) []byte {
	b := proto.AppendVarInt(nil, int32(len(es)))
	for _, e := range es {
		b = appendConsumeEffect(b, e)
	}
	return b
}

func EncodeUseCooldown(c CooldownInfo) []byte {
	b := appendF32(nil, c.Seconds)
	if c.Group == "" {
		return append(b, 0)
	}
	return appendString(append(b, 1), c.Group)
}

func EncodeEquippable(e EquipInfo) []byte {
	b := proto.AppendVarInt(nil, e.Slot)
	b = appendSound(b, e.EquipSound)
	b = appendOptString(b, e.AssetID)
	b = appendOptString(b, e.CameraOverlay)
	if e.Allowed == nil {
		b = append(b, 0)
	} else {
		b = appendHolderSet(append(b, 1), *e.Allowed)
	}
	b = append(b, b2u(e.Dispensable), b2u(e.Swappable), b2u(e.DamageOnHurt), b2u(e.EquipOnInteract), b2u(e.CanBeSheared))
	return appendSound(b, e.ShearingSound)
}

func EncodePotionContents(p PotionInfo) []byte {
	var b []byte
	if p.Potion < 0 {
		b = append(b, 0)
	} else {
		b = proto.AppendVarInt(append(b, 1), p.Potion)
	}
	if p.HasColor {
		b = append(b, 1)
		b = append(b, byte(p.Color>>24), byte(p.Color>>16), byte(p.Color>>8), byte(p.Color))
	} else {
		b = append(b, 0)
	}
	b = proto.AppendVarInt(b, int32(len(p.Effects)))
	for _, e := range p.Effects {
		b = appendEffect(b, e)
	}
	return appendOptString(b, p.Name)
}

func EncodeEnchantments(es []Enchantment) []byte {
	b := proto.AppendVarInt(nil, int32(len(es)))
	for _, e := range es {
		b = proto.AppendVarInt(proto.AppendVarInt(b, e.ID), e.Level)
	}
	return b
}

func appendString(b []byte, s string) []byte {
	b = proto.AppendVarInt(b, int32(len(s)))
	return append(b, s...)
}

func appendOptString(b []byte, s string) []byte {
	if s == "" {
		return append(b, 0)
	}
	return appendString(append(b, 1), s)
}

func appendF32(b []byte, f float32) []byte {
	w := proto.Writer{B: b}
	w.F32(f)
	return w.B
}
