package server

import (
	"math/rand/v2"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// interactMob is Mob.interact / mobInteract for the data-driven player
// interactions (wiki behaviour): name tags, shearing, milking, dyeing,
// feeding (breeding, growth, healing) and taming. It reports whether the
// interaction consumed the click. Runs on the player's goroutine; mob
// state changes go through the mob's own lock or its tick inbox.
func (p *Player) interactMob(m *Mob, hand api.Hand) bool {
	if m.IsDead() || p.GameMode() == api.Spectator {
		return false
	}
	st := p.Hand(hand)
	name := item.NameOf(st.Item)
	creative := p.GameMode() == api.Creative
	switch {
	case name == "minecraft:name_tag":
		return p.useNameTag(m, hand, st, creative)
	case name == "minecraft:shears":
		return p.shearMob(m, hand, st)
	case name == "minecraft:bucket" && (m.typ.Name == "cow" || m.typ.Name == "goat" || m.typ.Name == "mooshroom") && !m.IsBaby():
		snd := "minecraft:entity.cow.milk"
		if m.typ.Name == "goat" {
			snd = "minecraft:entity.goat.milk"
		}
		m.w.playSoundAt(m.feet(), snd, 6, 1, 1)
		p.exchangeHeld(hand, st, "minecraft:milk_bucket", creative)
		return true
	case name == "minecraft:bowl" && m.typ.Name == "mooshroom" && !m.IsBaby():
		m.w.playSoundAt(m.feet(), "minecraft:entity.mooshroom.milk", 6, 1, 1)
		p.exchangeHeld(hand, st, "minecraft:mushroom_stew", creative)
		return true
	case strings.HasSuffix(name, "_dye"):
		return p.dyeMob(m, hand, st, strings.TrimSuffix(strings.TrimPrefix(name, "minecraft:"), "_dye"), creative)
	}
	if p.tameMob(m, hand, st, name, creative) {
		return true
	}
	if m.feed(p, st) {
		m.healFromFood(st)
		p.consumeHeld(hand, st, creative)
		return true
	}
	return false
}

func (p *Player) consumeHeld(hand api.Hand, st item.Stack, creative bool) {
	if creative {
		return
	}
	st.Count--
	if st.Count <= 0 {
		st = item.Stack{}
	}
	p.setHand(hand, st)
}

// exchangeHeld is ItemUtils.createFilledResult: one held item becomes
// result (creative keeps the held item and adds result if missing).
func (p *Player) exchangeHeld(hand api.Hand, st item.Stack, result string, creative bool) {
	id, ok := item.IDOf(result)
	if !ok {
		return
	}
	out := api.ItemStack{Type: api.Key(result), Count: 1}
	if creative {
		if !p.Inventory().Contains(api.Key(result), 1) {
			p.Inventory().AddItem(out)
		}
		return
	}
	if st.Count <= 1 {
		p.setHand(hand, item.Stack{Item: id, Count: 1})
		return
	}
	st.Count--
	p.setHand(hand, st)
	for _, left := range p.Inventory().AddItem(out) {
		p.world.Load().DropItem(p.Location().Pos, left)
	}
}

func (p *Player) useNameTag(m *Mob, hand api.Hand, st item.Stack, creative bool) bool {
	raw, ok := st.Component(item.CustomName)
	if !ok {
		return false
	}
	c, ok := textLossless(raw)
	if !ok {
		return false
	}
	m.SetCustomName(&c)
	if m.sp != nil {
		m.sp.persistent = true
	}
	b := []byte{2}
	b = proto.AppendVarInt(b, 6)
	b = append(b, 1)
	b = append(b, raw...)
	m.trk.Push(encode(&packets.ClientboundSetEntityData{EntityID: m.e.ID, Raw: entity.EndMeta(b)}))
	p.consumeHeld(hand, st, creative)
	return true
}

// shearMob: sheep drop the shearing/sheep/<color> loot (1-3 wool) and
// become sheared; mooshrooms drop shearing/mooshroom/<type> and turn into
// a cow. Shears take 1 durability.
func (p *Player) shearMob(m *Mob, hand api.Hand, st item.Stack) bool {
	if m.IsBaby() {
		return false
	}
	var table string
	switch m.typ.Name {
	case "sheep":
		m.life.mu.Lock()
		if m.life.sheared {
			m.life.mu.Unlock()
			return false
		}
		m.life.sheared = true
		table = "minecraft:shearing/sheep/" + dyeColors[m.life.color&15]
		m.syncLife(&m.life, true)
		m.life.mu.Unlock()
		m.w.playSoundAt(m.feet(), "minecraft:entity.sheep.shear", 7, 1, 1)
	case "mooshroom":
		typ := "red"
		if t, _ := m.life.vals["type"].(int32); t == 1 {
			typ = "brown"
		}
		table = "minecraft:shearing/mooshroom/" + typ
		m.w.playSoundAt(m.feet(), "minecraft:entity.mooshroom.shear", 7, 1, 1)
		m.w.convertMob(m, "cow")
	case "snow_golem":
		table = "minecraft:shearing/snow_golem"
	case "bogged":
		table = "minecraft:shearing/bogged"
	default:
		return false
	}
	pos := m.feet()
	pos.Y += 1
	if t := loot.Get(table); t != nil {
		for _, d := range m.w.RollLoot(t, &loot.Context{Origin: pos, HasOrigin: true, This: lootMob{m}}) {
			m.w.spawnAtLocation(pos, d)
		}
	}
	if p.GameMode() != api.Creative {
		p.hurtHeld(hand, 1)
	}
	return true
}

// convertMob is Mob.convertTo: a new mob of type at the same place with the
// custom name kept; the old one is removed.
func (w *World) convertMob(m *Mob, to string) {
	t, ok := data.Canonical().EntityByName(to)
	if !ok {
		return
	}
	n := newMob(w, t, m.Location())
	if c, ok := m.CustomName(); ok {
		n.SetCustomName(&c)
	}
	n.applySpawnAttrs(map[string]any{}, false)
	m.Remove()
	w.mobsMu.Lock()
	w.addMobLocked(n)
	w.mobsMu.Unlock()
}

// dyeMob: sheep take the dye color; a tamed wolf or cat of the player
// changes its collar.
func (p *Player) dyeMob(m *Mob, hand api.Hand, st item.Stack, color string, creative bool) bool {
	id := dyeID(color)
	if id == 0 && color != "white" {
		return false
	}
	l := &m.life
	l.mu.Lock()
	defer l.mu.Unlock()
	switch m.typ.Name {
	case "sheep":
		if l.sheared || l.color == id {
			return false
		}
		l.color = id
	case "wolf", "cat":
		if !l.tame || l.owner != p.UUID() || l.collar == id {
			return false
		}
		l.collar = id
	default:
		return false
	}
	m.syncLife(l, true)
	m.w.playSoundAt(m.feet(), "minecraft:item.dye.use", 7, 1, 1)
	if !creative {
		st.Count--
		if st.Count <= 0 {
			st = item.Stack{}
		}
		p.setHand(hand, st)
	}
	return true
}

// tameItems are the taming items and per-attempt chances (wiki: wolves
// 1/3 with a bone, cats 1/3 with raw cod or salmon, parrots 1/10 with
// seeds).
func tameChance(mob, it string) (int, bool) {
	switch mob {
	case "wolf":
		return 3, it == "minecraft:bone"
	case "cat":
		return 3, it == "minecraft:cod" || it == "minecraft:salmon"
	case "parrot":
		return 10, inTag("item", "parrot_food", it)
	}
	return 0, false
}

func (p *Player) tameMob(m *Mob, hand api.Hand, st item.Stack, name string, creative bool) bool {
	n, ok := tameChance(m.typ.Name, name)
	if !ok {
		return false
	}
	l := &m.life
	l.mu.Lock()
	if l.tame {
		l.mu.Unlock()
		return false
	}
	won := rand.IntN(n) == 0
	ev := int8(6) // smoke
	if won {
		l.tame, l.owner, ev = true, p.UUID(), 7 // hearts
		if m.sp != nil {
			m.sp.persistent = true
		}
		m.syncLife(l, true)
	}
	l.mu.Unlock()
	if won && m.liv.attrs != nil && m.typ.Name == "wolf" {
		m.liv.mu.Lock()
		m.liv.attrs.SetBase(entity.AttrMaxHealth, 40)
		m.setCurHealth(40)
		m.liv.mu.Unlock()
	}
	m.w.broadcastNear(m.feet(), &packets.ClientboundEntityEvent{EntityID: m.e.ID, EntityStatus: ev})
	p.consumeHeld(hand, st, creative)
	return true
}

// healFromFood: horses, llamas and tamed wolves heal from food (wolves by
// the food's nutrition, horse family by 2 per item as a simplification).
func (m *Mob) healFromFood(st item.Stack) {
	var amount float32
	switch m.typ.Name {
	case "wolf":
		if f, ok := st.Food(); ok {
			amount = float32(f.Nutrition)
		}
	case "horse", "donkey", "mule", "llama", "trader_llama", "camel":
		amount = 2
	}
	if amount <= 0 || m.liv.attrs == nil {
		return
	}
	m.liv.mu.Lock()
	m.setCurHealth(m.liv.health + amount)
	m.liv.mu.Unlock()
}
