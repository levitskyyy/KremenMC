package item

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Prototype holds an item's default components. Values are wire encoded
// for the types this package models (see encoded); other defaults are
// recorded as present only.
type Prototype struct {
	present [NumTypes]bool
	wire    [NumTypes][]byte
	known   [NumTypes]bool
}

// Has reports whether the prototype defines t.
func (p *Prototype) Has(t Type) bool { return p != nil && p.present[t] }

// Wire returns the encoded default of t; ok is false when t is absent or
// not modelled.
func (p *Prototype) Wire(t Type) ([]byte, bool) {
	if p == nil || !p.known[t] {
		return nil, false
	}
	return p.wire[t], true
}

var (
	protoOnce sync.Once
	protos    []*Prototype
	protoErr  error
)

// loadReport returns the default components report of an item. It is the
// single place that knows where vanilla data lives.
func loadReport(name string) ([]byte, bool) {
	p, err := wgdata.Load()
	if err != nil {
		return nil, false
	}
	return p.Report("minecraft/components/item/" + strings.TrimPrefix(name, "minecraft:") + ".json")
}

// Defaults returns the prototype of item id (nil for unknown ids).
func Defaults(id int32) *Prototype {
	protoOnce.Do(loadPrototypes)
	if id < 0 || int(id) >= len(protos) {
		return nil
	}
	return protos[id]
}

// PrototypeError reports a failure loading default components.
func PrototypeError() error {
	protoOnce.Do(loadPrototypes)
	return protoErr
}

func loadPrototypes() {
	items := data.Canonical().Items
	protos = make([]*Prototype, len(items))
	for i, it := range items {
		p := &Prototype{}
		protos[i] = p
		raw, ok := loadReport(it.Name)
		if !ok {
			if protoErr == nil {
				protoErr = fmt.Errorf("item: no default components for %s", it.Name)
			}
			p.setFallback(int32(it.StackSize))
			continue
		}
		var doc struct {
			Components map[string]json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			protoErr = fmt.Errorf("item: %s defaults: %w", it.Name, err)
			p.setFallback(int32(it.StackSize))
			continue
		}
		for k, v := range doc.Components {
			t, ok := TypeByName(k)
			if !ok {
				continue
			}
			p.present[t] = true
			if w, err := encodeJSON(t, v); err == nil && w != nil {
				p.wire[t], p.known[t] = w, true
			}
		}
	}
}

func (p *Prototype) setFallback(stack int32) {
	p.present[MaxStackSize], p.known[MaxStackSize] = true, true
	p.wire[MaxStackSize] = EncodeVarInt(max(stack, 1))
}

var errSkip = fmt.Errorf("item: not modelled")

// encodeJSON converts the codec (JSON) form of the modelled components to
// their wire form; nil, errSkip for the rest.
func encodeJSON(t Type, v json.RawMessage) ([]byte, error) {
	switch t {
	case MaxStackSize, MaxDamage, Damage, RepairCost, OminousBottleAmplifier, MapId:
		var n int32
		if err := json.Unmarshal(v, &n); err != nil {
			return nil, err
		}
		return EncodeVarInt(n), nil
	case Unbreakable, Glider, CreativeSlotLock, Waxed:
		return []byte{}, nil
	case Rarity:
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, err
		}
		return enumWire(rarityNames, s)
	case Enchantable:
		var e struct{ Value int32 }
		if err := json.Unmarshal(v, &e); err != nil {
			return nil, err
		}
		return EncodeVarInt(e.Value), nil
	case Food:
		var f struct {
			Nutrition    int32   `json:"nutrition"`
			Saturation   float32 `json:"saturation"`
			CanAlwaysEat bool    `json:"can_always_eat"`
		}
		if err := json.Unmarshal(v, &f); err != nil {
			return nil, err
		}
		return EncodeFood(FoodInfo{f.Nutrition, f.Saturation, f.CanAlwaysEat}), nil
	case Consumable:
		c, err := consumableJSON(v)
		if err != nil {
			return nil, err
		}
		return EncodeConsumable(c), nil
	case DeathProtection:
		var d struct {
			Effects []json.RawMessage `json:"death_effects"`
		}
		if err := json.Unmarshal(v, &d); err != nil {
			return nil, err
		}
		es, err := consumeEffectsJSON(d.Effects)
		if err != nil {
			return nil, err
		}
		return EncodeDeathProtection(es), nil
	case UseCooldown:
		var c struct {
			Seconds float32 `json:"seconds"`
			Group   string  `json:"cooldown_group"`
		}
		if err := json.Unmarshal(v, &c); err != nil {
			return nil, err
		}
		return EncodeUseCooldown(CooldownInfo{c.Seconds, c.Group}), nil
	case UseRemainder:
		var r struct {
			ID         string          `json:"id"`
			Count      *int32          `json:"count"`
			Components json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(v, &r); err != nil {
			return nil, err
		}
		if len(r.Components) > 0 {
			return nil, errSkip
		}
		id, ok := IDOf(r.ID)
		if !ok {
			return nil, fmt.Errorf("item: unknown item %s", r.ID)
		}
		n := int32(1)
		if r.Count != nil {
			n = *r.Count
		}
		return AppendTemplate(nil, Stack{Item: id, Count: n}), nil
	case Equippable:
		e, err := equippableJSON(v)
		if err != nil {
			return nil, err
		}
		return EncodeEquippable(e), nil
	case Enchantments, StoredEnchantments:
		var m map[string]int32
		if err := json.Unmarshal(v, &m); err != nil {
			return nil, err
		}
		es := make([]Enchantment, 0, len(m))
		for k, l := range m {
			id, ok := regsync.Index("minecraft:enchantment", k)
			if !ok {
				return nil, fmt.Errorf("item: unknown enchantment %s", k)
			}
			es = append(es, Enchantment{id, l})
		}
		if len(es) > 1 {
			return nil, errSkip
		}
		return EncodeEnchantments(es), nil
	case PotionContents:
		var p struct {
			Potion string `json:"potion"`
		}
		if err := json.Unmarshal(v, &p); err != nil {
			return nil, err
		}
		pc := PotionInfo{Potion: -1}
		if p.Potion != "" {
			id, ok := builtinID("potion", p.Potion)
			if !ok {
				return nil, fmt.Errorf("item: unknown potion %s", p.Potion)
			}
			pc.Potion = id
		}
		return EncodePotionContents(pc), nil
	case Weapon:
		var w struct {
			Damage  *int32  `json:"item_damage_per_attack"`
			Disable float32 `json:"disable_blocking_for_seconds"`
		}
		if err := json.Unmarshal(v, &w); err != nil {
			return nil, err
		}
		n := int32(1)
		if w.Damage != nil {
			n = *w.Damage
		}
		return appendF32(EncodeVarInt(n), w.Disable), nil
	case Tool:
		var tl struct {
			Rules []struct {
				Blocks  json.RawMessage `json:"blocks"`
				Speed   *float32        `json:"speed"`
				Correct *bool           `json:"correct_for_drops"`
			} `json:"rules"`
			Speed    *float32 `json:"default_mining_speed"`
			Damage   *int32   `json:"damage_per_block"`
			Creative *bool    `json:"can_destroy_blocks_in_creative"`
		}
		if err := json.Unmarshal(v, &tl); err != nil {
			return nil, err
		}
		b := EncodeVarInt(int32(len(tl.Rules)))
		for _, r := range tl.Rules {
			h, err := holderSetJSON("block", r.Blocks)
			if err != nil {
				return nil, err
			}
			b = appendHolderSet(b, h)
			if r.Speed != nil {
				b = appendF32(append(b, 1), *r.Speed)
			} else {
				b = append(b, 0)
			}
			if r.Correct != nil {
				b = append(b, 1, b2u(*r.Correct))
			} else {
				b = append(b, 0)
			}
		}
		speed, dmg, creative := float32(1), int32(1), true
		if tl.Speed != nil {
			speed = *tl.Speed
		}
		if tl.Damage != nil {
			dmg = *tl.Damage
		}
		if tl.Creative != nil {
			creative = *tl.Creative
		}
		b = appendF32(b, speed)
		b = append(proto.AppendVarInt(b, dmg), b2u(creative))
		return b, nil
	case DamageResistant:
		var d struct {
			Types json.RawMessage `json:"types"`
		}
		if err := json.Unmarshal(v, &d); err != nil {
			return nil, err
		}
		h, err := holderSetJSON("damage_type", d.Types)
		if err != nil {
			return nil, err
		}
		return appendHolderSet(nil, h), nil
	}
	return nil, errSkip
}

func enumWire(names []string, s string) ([]byte, error) {
	for i, n := range names {
		if n == s {
			return EncodeVarInt(int32(i)), nil
		}
	}
	return nil, fmt.Errorf("item: unknown value %q", s)
}

var (
	builtinMu  sync.Mutex
	builtinIdx = map[string]map[string]int32{}
)

// BuiltinID resolves a code-defined (static) registry entry, e.g.
// BuiltinID("sound_event", "minecraft:entity.player.burp").
func BuiltinID(reg, name string) (int32, bool) { return builtinID(reg, name) }

// builtinID resolves a code-defined (static) registry entry.
func builtinID(reg, name string) (int32, bool) {
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	builtinMu.Lock()
	defer builtinMu.Unlock()
	m, ok := builtinIdx[reg]
	if !ok {
		m = map[string]int32{}
		if p, err := wgdata.Load(); err == nil {
			es, _ := p.Builtin(reg)
			for i, e := range es {
				m[e] = int32(i)
			}
		}
		builtinIdx[reg] = m
	}
	id, ok := m[name]
	return id, ok
}

func soundJSON(v json.RawMessage, def string) (Sound, error) {
	if len(v) == 0 {
		v = json.RawMessage(`"` + def + `"`)
	}
	var name string
	if json.Unmarshal(v, &name) == nil {
		id, ok := builtinID("sound_event", name)
		if !ok {
			return Sound{ID: -1, Name: name}, nil
		}
		return Sound{ID: id}, nil
	}
	var s struct {
		ID    string   `json:"sound_id"`
		Range *float32 `json:"range"`
	}
	if err := json.Unmarshal(v, &s); err != nil {
		return Sound{}, err
	}
	out := Sound{ID: -1, Name: s.ID}
	if s.Range != nil {
		out.Range, out.Fixed = *s.Range, true
	}
	return out, nil
}

func holderSetJSON(reg string, v json.RawMessage) (HolderSet, error) {
	var one string
	var names []string
	if json.Unmarshal(v, &one) == nil {
		if strings.HasPrefix(one, "#") {
			return HolderSet{Tag: one[1:]}, nil
		}
		names = []string{one}
	} else if err := json.Unmarshal(v, &names); err != nil {
		return HolderSet{}, err
	}
	h := HolderSet{IDs: make([]int32, 0, len(names))}
	for _, n := range names {
		id, ok := registryID(reg, n)
		if !ok {
			return HolderSet{}, fmt.Errorf("item: unknown %s %s", reg, n)
		}
		h.IDs = append(h.IDs, id)
	}
	return h, nil
}

func registryID(reg, name string) (int32, bool) {
	switch reg {
	case "item":
		return IDOf(name)
	}
	if id, ok := regsync.Index("minecraft:"+reg, name); ok {
		return id, true
	}
	return builtinID(reg, name)
}

func effectJSON(v json.RawMessage) (Effect, error) {
	var e struct {
		ID        string          `json:"id"`
		Amplifier int32           `json:"amplifier"`
		Duration  int32           `json:"duration"`
		Ambient   bool            `json:"ambient"`
		Particles *bool           `json:"show_particles"`
		Icon      *bool           `json:"show_icon"`
		Hidden    json.RawMessage `json:"hidden_effect"`
	}
	if err := json.Unmarshal(v, &e); err != nil {
		return Effect{}, err
	}
	id, ok := builtinID("mob_effect", e.ID)
	if !ok {
		return Effect{}, fmt.Errorf("item: unknown effect %s", e.ID)
	}
	out := Effect{ID: id, Amplifier: e.Amplifier, Duration: e.Duration, Ambient: e.Ambient, ShowParticles: true}
	if e.Particles != nil {
		out.ShowParticles = *e.Particles
	}
	out.ShowIcon = out.ShowParticles
	if e.Icon != nil {
		out.ShowIcon = *e.Icon
	}
	if len(e.Hidden) > 0 {
		h, err := effectJSON(e.Hidden)
		if err != nil {
			return Effect{}, err
		}
		out.Hidden = &h
	}
	return out, nil
}

func consumeEffectsJSON(vs []json.RawMessage) ([]ConsumeEffect, error) {
	var out []ConsumeEffect
	for _, v := range vs {
		var c struct {
			Type        string          `json:"type"`
			Effects     json.RawMessage `json:"effects"`
			Probability *float32        `json:"probability"`
			Diameter    *float32        `json:"diameter"`
			Sound       json.RawMessage `json:"sound"`
		}
		if err := json.Unmarshal(v, &c); err != nil {
			return nil, err
		}
		kind, ok := builtinID("consume_effect_type", c.Type)
		if !ok {
			return nil, fmt.Errorf("item: unknown consume effect %s", c.Type)
		}
		ce := ConsumeEffect{Kind: kind, Probability: 1, Diameter: 16, Flag: true}
		switch kind {
		case ConsumeApplyEffects:
			var es []json.RawMessage
			if err := json.Unmarshal(c.Effects, &es); err != nil {
				return nil, err
			}
			for _, e := range es {
				x, err := effectJSON(e)
				if err != nil {
					return nil, err
				}
				ce.Effects = append(ce.Effects, x)
			}
			if c.Probability != nil {
				ce.Probability = *c.Probability
			}
		case ConsumeRemoveEffects:
			h, err := holderSetJSON("mob_effect", c.Effects)
			if err != nil {
				return nil, err
			}
			ce.Remove = h
		case ConsumeTeleportRandomly:
			if c.Diameter != nil {
				ce.Diameter = *c.Diameter
			}
		case ConsumePlaySound:
			s, err := soundJSON(c.Sound, "")
			if err != nil {
				return nil, err
			}
			ce.Sound = s
		}
		out = append(out, ce)
	}
	return out, nil
}

func consumableJSON(v json.RawMessage) (ConsumableInfo, error) {
	var c struct {
		Seconds   *float32          `json:"consume_seconds"`
		Animation string            `json:"animation"`
		Sound     json.RawMessage   `json:"sound"`
		Particles *bool             `json:"has_consume_particles"`
		Effects   []json.RawMessage `json:"on_consume_effects"`
	}
	if err := json.Unmarshal(v, &c); err != nil {
		return ConsumableInfo{}, err
	}
	out := ConsumableInfo{Seconds: 1.6, Animation: AnimEat, Particles: true}
	if c.Seconds != nil {
		out.Seconds = *c.Seconds
	}
	if c.Animation != "" {
		w, err := enumWire(animNames, c.Animation)
		if err != nil {
			return ConsumableInfo{}, err
		}
		out.Animation, _ = DecodeVarInt(w)
	}
	s, err := soundJSON(c.Sound, "minecraft:entity.generic.eat")
	if err != nil {
		return ConsumableInfo{}, err
	}
	out.Sound = s
	if c.Particles != nil {
		out.Particles = *c.Particles
	}
	if out.Effects, err = consumeEffectsJSON(c.Effects); err != nil {
		return ConsumableInfo{}, err
	}
	return out, nil
}

func equippableJSON(v json.RawMessage) (EquipInfo, error) {
	var e struct {
		Slot            string          `json:"slot"`
		EquipSound      json.RawMessage `json:"equip_sound"`
		AssetID         string          `json:"asset_id"`
		CameraOverlay   string          `json:"camera_overlay"`
		Allowed         json.RawMessage `json:"allowed_entities"`
		Dispensable     *bool           `json:"dispensable"`
		Swappable       *bool           `json:"swappable"`
		DamageOnHurt    *bool           `json:"damage_on_hurt"`
		EquipOnInteract bool            `json:"equip_on_interact"`
		CanBeSheared    bool            `json:"can_be_sheared"`
		ShearingSound   json.RawMessage `json:"shearing_sound"`
	}
	if err := json.Unmarshal(v, &e); err != nil {
		return EquipInfo{}, err
	}
	w, err := enumWire(equipSlotNames, e.Slot)
	if err != nil {
		return EquipInfo{}, err
	}
	out := EquipInfo{AssetID: e.AssetID, CameraOverlay: e.CameraOverlay, Dispensable: true, Swappable: true, DamageOnHurt: true,
		EquipOnInteract: e.EquipOnInteract, CanBeSheared: e.CanBeSheared}
	out.Slot, _ = DecodeVarInt(w)
	if out.EquipSound, err = soundJSON(e.EquipSound, "minecraft:item.armor.equip_generic"); err != nil {
		return EquipInfo{}, err
	}
	if out.ShearingSound, err = soundJSON(e.ShearingSound, "minecraft:item.shears.snip"); err != nil {
		return EquipInfo{}, err
	}
	if len(e.Allowed) > 0 {
		h, err := holderSetJSON("entity_type", e.Allowed)
		if err != nil {
			return EquipInfo{}, err
		}
		out.Allowed = &h
	}
	for _, f := range []struct {
		src *bool
		dst *bool
	}{{e.Dispensable, &out.Dispensable}, {e.Swappable, &out.Swappable}, {e.DamageOnHurt, &out.DamageOnHurt}} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	return out, nil
}
