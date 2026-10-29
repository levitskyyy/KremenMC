package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// initCombat sets up the spawn-time sync of a mob's attributes.
func (m *Mob) initCombat() {
	if m.liv.attrs == nil {
		return
	}
	m.trk.SetSpawnExtra(func() [][]byte {
		m.liv.mu.Lock()
		defer m.liv.mu.Unlock()
		pk := attrPacket(m.e.ID, m.liv.attrs, m.liv.attrs.Synced())
		return [][]byte{encode(pk)}
	})
}

func (m *Mob) Health() float64 {
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	return float64(m.liv.health)
}

func (m *Mob) SetHealth(h float64) {
	m.liv.mu.Lock()
	m.setCurHealth(float32(h))
	dead := m.liv.health <= 0 && m.liv.attrs != nil
	m.liv.mu.Unlock()
	if dead {
		die(m, NewDamage("minecraft:generic"))
	}
}

func (m *Mob) MaxHealth() float64 {
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	if m.liv.attrs == nil {
		return 0
	}
	return m.liv.attrs.Value(entity.AttrMaxHealth)
}

// Damage applies src through the vanilla damage pipeline.
func (m *Mob) Damage(amount float64, src api.DamageSource) {
	if amount > 0 {
		hurt(m, fromAPI(src), float32(amount))
	}
}

func (m *Mob) IsDead() bool { return m.liv.isDead() || !m.IsValid() }

func (m *Mob) EyeLocation() api.Location {
	l := m.Location()
	l.Pos.Y += m.eyeY()
	return l
}

func (m *Mob) Equipment() api.Equipment { return mobEquipment{m} }

type mobEquipment struct{ m *Mob }

func apiSlot(s api.EquipmentSlot) int {
	switch s {
	case api.SlotMainHand:
		return slotMain
	case api.SlotOffHand:
		return slotOff
	case api.SlotFeet:
		return slotFeet
	case api.SlotLegs:
		return slotLegs
	case api.SlotChest:
		return slotChest
	case api.SlotHead:
		return slotHead
	}
	return -1
}

func (e mobEquipment) Get(s api.EquipmentSlot) api.ItemStack {
	i := apiSlot(s)
	if i < 0 {
		return api.ItemStack{}
	}
	e.m.liv.mu.Lock()
	defer e.m.liv.mu.Unlock()
	return stackToAPI(e.m.liv.equip[i])
}

func (e mobEquipment) Set(s api.EquipmentSlot, it api.ItemStack) {
	i := apiSlot(s)
	if i < 0 {
		return
	}
	e.m.liv.mu.Lock()
	e.m.liv.equip[i] = stackFromAPI(it).Norm()
	e.m.liv.mu.Unlock()
}

func (m *Mob) AddEffect(e api.PotionEffect) bool {
	id, ok := item.BuiltinID("mob_effect", string(e.Type))
	if !ok || m.liv.attrs == nil {
		return false
	}
	m.addEffect(item.Effect{ID: id, Amplifier: int32(e.Amplifier), Duration: int32(e.Duration)})
	return true
}

func (m *Mob) RemoveEffect(k api.Key) {
	id, ok := item.BuiltinID("mob_effect", string(k))
	if !ok {
		return
	}
	m.liv.mu.Lock()
	m.liv.setEffect(id, 0, false)
	m.liv.mu.Unlock()
}

func (m *Mob) Effects() []api.PotionEffect {
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	var out []api.PotionEffect
	for id, amp := range m.liv.fx {
		out = append(out, api.PotionEffect{Type: api.Key(effectName(id)), Amplifier: int(amp), Duration: api.Ticks(m.liv.fxLeft[id])})
	}
	return out
}

// Knockback applies vanilla knockback of strength away from (dx, dz)
// (the direction from the entity towards the source).
func (m *Mob) Knockback(strength, dx, dz float64) { knockback(m, strength, dx, dz) }

// Knockback applies vanilla knockback to the player.
func (p *Player) Knockback(strength, dx, dz float64) { knockback(p, strength, dx, dz) }

// AttributeValue returns the current value of a living entity attribute
// (0, false for non-living entities or unknown names).
func (m *Mob) AttributeValue(name string) (float64, bool) { return attrValue(&m.liv, name) }

func (p *Player) AttributeValue(name string) (float64, bool) { return attrValue(&p.liv, name) }

func attrValue(l *living, name string) (float64, bool) {
	a, ok := entity.AttrByName(name)
	if !ok || l.attrs == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.attrs.Value(a), true
}

var _ api.LivingEntity = (*Mob)(nil)
