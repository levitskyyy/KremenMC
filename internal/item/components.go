package item

import "bytes"

// Component returns the effective encoded value of t: the patch entry if
// any, else the prototype default. ok is false when the stack has no such
// component (or its default is not modelled).
func (s Stack) Component(t Type) (v []byte, ok bool) {
	if s.IsEmpty() {
		return nil, false
	}
	if v, removed, in := s.Patch.Get(t); in {
		return v, !removed
	}
	return Defaults(s.Item).Wire(t)
}

// Has reports whether the stack has component t, modelled or not.
func (s Stack) Has(t Type) bool {
	if s.IsEmpty() {
		return false
	}
	if _, removed, in := s.Patch.Get(t); in {
		return !removed
	}
	return Defaults(s.Item).Has(t)
}

func (s Stack) varint(t Type, def int32) int32 {
	if v, ok := s.Component(t); ok {
		if n, err := DecodeVarInt(v); err == nil {
			return n
		}
	}
	return def
}

// MaxStackSize is the max_stack_size component (default 1).
func (s Stack) MaxStackSize() int32 { return s.varint(MaxStackSize, 1) }

func (s Stack) MaxDamage() int32 { return s.varint(MaxDamage, 0) }

func (s Stack) Damage() int32 { return min(max(s.varint(Damage, 0), 0), s.MaxDamage()) }

// IsDamageable: has max_damage and damage, and is not unbreakable.
func (s Stack) IsDamageable() bool {
	return s.Has(MaxDamage) && !s.Has(Unbreakable) && s.Has(Damage)
}

func (s Stack) IsDamaged() bool { return s.IsDamageable() && s.Damage() > 0 }

// IsStackable: max stack size above 1 and not a damaged damageable item.
func (s Stack) IsStackable() bool {
	return s.MaxStackSize() > 1 && (!s.IsDamageable() || !s.IsDamaged())
}

// Set returns s with component t set to v. A value equal to the
// prototype's default removes the patch entry, as vanilla does.
func (s Stack) Set(t Type, v []byte) Stack {
	if d, ok := Defaults(s.Item).Wire(t); ok && bytes.Equal(d, v) {
		s.Patch = s.Patch.Forget(t)
		return s
	}
	s.Patch = s.Patch.Set(t, v)
	return s
}

// Remove returns s without component t.
func (s Stack) Remove(t Type) Stack {
	if !Defaults(s.Item).Has(t) {
		s.Patch = s.Patch.Forget(t)
		return s
	}
	s.Patch = s.Patch.Remove(t)
	return s
}

func (s Stack) WithDamage(n int32) Stack {
	return s.Set(Damage, EncodeVarInt(min(max(n, 0), s.MaxDamage())))
}

func (s Stack) Food() (FoodInfo, bool) {
	v, ok := s.Component(Food)
	if !ok {
		return FoodInfo{}, false
	}
	f, err := DecodeFood(v)
	return f, err == nil
}

func (s Stack) Consumable() (ConsumableInfo, bool) {
	v, ok := s.Component(Consumable)
	if !ok {
		return ConsumableInfo{}, false
	}
	c, err := DecodeConsumable(v)
	return c, err == nil
}

func (s Stack) UseCooldown() (CooldownInfo, bool) {
	v, ok := s.Component(UseCooldown)
	if !ok {
		return CooldownInfo{}, false
	}
	c, err := DecodeUseCooldown(v)
	return c, err == nil
}

func (s Stack) Equippable() (EquipInfo, bool) {
	v, ok := s.Component(Equippable)
	if !ok {
		return EquipInfo{}, false
	}
	e, err := DecodeEquippable(v)
	return e, err == nil
}

func (s Stack) UseRemainder() (Stack, bool) {
	v, ok := s.Component(UseRemainder)
	if !ok {
		return Stack{}, false
	}
	r, err := DecodeTemplate(v)
	return r, err == nil && !r.IsEmpty()
}

func (s Stack) PotionContents() (PotionInfo, bool) {
	v, ok := s.Component(PotionContents)
	if !ok {
		return PotionInfo{Potion: -1}, false
	}
	p, err := DecodePotionContents(v)
	return p, err == nil
}

func (s Stack) DeathProtection() ([]ConsumeEffect, bool) {
	v, ok := s.Component(DeathProtection)
	if !ok {
		return nil, false
	}
	es, err := DecodeDeathProtection(v)
	return es, err == nil
}

func (s Stack) Enchantments() []Enchantment {
	v, ok := s.Component(Enchantments)
	if !ok {
		return nil
	}
	es, _ := DecodeEnchantments(v)
	return es
}

// EnchantmentLevel returns the level of a named enchantment (0 if absent).
func (s Stack) EnchantmentLevel(name string) int32 {
	id, ok := registryID("enchantment", name)
	if !ok {
		return 0
	}
	for _, e := range s.Enchantments() {
		if e.ID == id {
			return e.Level
		}
	}
	return 0
}

// EquipmentSlotFor is LivingEntity.getEquipmentSlotForItem for a player:
// the equippable slot when a player can use it, else the main hand.
func EquipmentSlotFor(s Stack) int32 {
	e, ok := s.Equippable()
	if !ok || e.Slot == SlotBody || e.Slot == SlotSaddle {
		return SlotMainHand
	}
	return e.Slot
}

// EquippableBy reports whether a player may equip s in slot (armor slot
// placement rule).
func EquippableBy(s Stack, slot int32, entityType int32) bool {
	e, ok := s.Equippable()
	if !ok {
		return slot == SlotMainHand
	}
	if e.Slot != slot || slot == SlotBody || slot == SlotSaddle {
		return false
	}
	return e.Allowed == nil || inHolderSet(*e.Allowed, "minecraft:entity_type", entityType)
}

func inHolderSet(h HolderSet, reg string, id int32) bool {
	if h.Tag != "" {
		return tagContains(reg, h.Tag, id)
	}
	for _, x := range h.IDs {
		if x == id {
			return true
		}
	}
	return false
}
