package server

import (
	"math/rand/v2"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

type useResult uint8

const (
	usePass useResult = iota
	useFail
	useSuccess // client-side swing
	useConsume // no swing
)

const longUse = 72000

// handleUseItem is ServerGamePacketListenerImpl.handleUseItem followed by
// ServerPlayerGameMode.useItem.
func (p *Player) handleUseItem(m *packets.ServerboundUseItem) {
	p.ack(m.Sequence)
	hand := api.Hand(m.Hand)
	if hand != api.MainHand && hand != api.OffHand {
		return
	}
	p.mu.Lock()
	p.e.Yaw, p.e.Pitch = entity.WrapDegrees(m.Rotation.X), max(min(entity.WrapDegrees(m.Rotation.Y), 90), -90)
	gm := p.gm
	p.mu.Unlock()
	if gm == api.Spectator {
		return
	}
	if p.interact(api.RightClickAir, nil, api.FaceSelf, hand, api.Vec3{}) {
		p.sendInventory()
		return
	}
	st := p.Hand(hand)
	if st.IsEmpty() || p.onCooldown(st) {
		return
	}
	count, damage := st.Count, st.Damage()
	res, transformed, changed := p.useStack(st, hand)
	cur := p.Hand(hand)
	if changed && item.Matches(transformed, cur) && transformed.Count == count {
		changed = false
	}
	newStack := cur
	if changed {
		newStack = transformed
	}
	if !changed && item.Matches(cur, st) && newStack.Count == count && useDuration(newStack) <= 0 && newStack.Damage() == damage {
		return
	}
	if res == useFail && useDuration(newStack) > 0 && !p.use.active {
		return
	}
	if changed {
		p.setHand(hand, newStack)
	}
	if gm == api.Creative && !newStack.IsEmpty() {
		newStack.Count = count
		if newStack.IsDamageable() && newStack.Damage() != damage {
			newStack = newStack.WithDamage(damage)
		}
		p.setHand(hand, newStack)
	}
	if newStack.IsEmpty() {
		p.setHand(hand, item.Stack{})
	}
	if !p.use.active {
		p.sendInventory()
	}
}

// useDuration is ItemStack.getUseDuration for the items we model.
func useDuration(st item.Stack) int32 {
	if st.IsEmpty() {
		return 0
	}
	if c, ok := st.Consumable(); ok {
		return c.Ticks()
	}
	if st.Has(item.BlocksAttacks) {
		return longUse
	}
	switch item.NameOf(st.Item) {
	case "minecraft:bow", "minecraft:trident":
		return longUse
	case "minecraft:spyglass":
		return 1200
	}
	return 0
}

// useStack is ItemStack.use / Item.use for the modelled behaviours.
func (p *Player) useStack(st item.Stack, hand api.Hand) (useResult, item.Stack, bool) {
	if c, ok := st.Consumable(); ok {
		if !p.canConsume(st) {
			return useFail, item.Stack{}, false
		}
		if c.Ticks() > 0 {
			p.startUsing(hand, st, c.Ticks())
			return useConsume, item.Stack{}, false
		}
		before := st
		out := p.consume(st, c)
		return useConsume, p.afterUse(out, before), true
	}
	if e, ok := st.Equippable(); ok && e.Swappable {
		return p.swapEquipment(st, e)
	}
	if st.Has(item.BlocksAttacks) {
		p.startUsing(hand, st, longUse)
		return useConsume, item.Stack{}, false
	}
	switch item.NameOf(st.Item) {
	case "minecraft:water_bucket", "minecraft:lava_bucket", "minecraft:bucket", "minecraft:powder_snow_bucket":
		return p.useBucket(st, hand)
	case "minecraft:bow", "minecraft:trident", "minecraft:spyglass":
		p.startUsing(hand, st, useDuration(st))
		return useConsume, item.Stack{}, false
	case "minecraft:ender_pearl", "minecraft:snowball", "minecraft:egg", "minecraft:blue_egg", "minecraft:brown_egg":
		out, _ := p.throwItem(st)
		return useSuccess, out, true
	case "minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:experience_bottle", "minecraft:wind_charge",
		"minecraft:fishing_rod", "minecraft:crossbow", "minecraft:goat_horn":
		// Projectiles and fluids are not simulated yet: refuse the use and
		// resync so the client's prediction is rolled back.
		p.sendInventory()
		return useFail, item.Stack{}, false
	}
	return usePass, item.Stack{}, false
}

func (p *Player) canConsume(st item.Stack) bool {
	f, ok := st.Food()
	if !ok {
		return true
	}
	return p.GameMode() == api.Creative || f.CanAlwaysEat || p.Food() < 20
}

func (p *Player) startUsing(hand api.Hand, st item.Stack, ticks int32) {
	if p.use.active || st.IsEmpty() {
		return
	}
	p.use.active, p.use.hand, p.use.stack, p.use.remaining = true, hand, st, ticks
	p.setUsingFlag(true, hand)
}

func (p *Player) stopUsing() {
	if !p.use.active {
		return
	}
	p.use.active = false
	p.use.stack = item.Stack{}
	p.setUsingFlag(false, p.use.hand)
}

// releaseUsing handles player action RELEASE_USE_ITEM.
func (p *Player) releaseUsing() {
	if p.use.active {
		switch item.NameOf(p.use.stack.Item) {
		case "minecraft:bow":
			p.releaseBow(useDuration(p.use.stack) - p.use.remaining)
		case "minecraft:trident":
			p.releaseTrident(useDuration(p.use.stack)-p.use.remaining, p.use.hand)
		}
	}
	p.stopUsing()
}

// tickUsing is LivingEntity.updatingUsingItem on the server.
func (p *Player) tickUsing() {
	u := &p.use
	if !u.active {
		return
	}
	cur := p.Hand(u.hand)
	if cur.IsEmpty() || cur.Item != u.stack.Item {
		p.stopUsing()
		return
	}
	u.stack = cur
	if c, ok := cur.Consumable(); ok && shouldEmit(c, u.remaining) {
		p.playItemSound(c.Sound, soundPlayers, 0.5+0.5*float32(rand.IntN(2)), triangle(1, 0.2), false)
	}
	if u.remaining--; u.remaining == 0 {
		p.completeUsing()
	}
}

func shouldEmit(c item.ConsumableInfo, remaining int32) bool {
	used := c.Ticks() - remaining
	return used > int32(float32(c.Ticks())*0.21875) && remaining%4 == 0
}

// completeUsing is LivingEntity.completeUsingItem.
func (p *Player) completeUsing() {
	u := &p.use
	hand := u.hand
	cur := p.Hand(hand)
	if !item.Matches(cur, u.stack) {
		p.stopUsing()
		return
	}
	if c, ok := cur.Consumable(); ok {
		before := cur
		out := p.afterUse(p.consume(cur, c), before)
		p.setHand(hand, out)
	}
	p.stopUsing()
}

// consume is Consumable.onConsume: listeners (food, potion contents,
// stew effects), consume effects, then shrink unless creative.
func (p *Player) consume(st item.Stack, c item.ConsumableInfo) item.Stack {
	p.playItemSound(c.Sound, soundPlayers, 0.5+0.5*float32(rand.IntN(2)), triangle(1, 0.2), false)
	if f, ok := st.Food(); ok {
		p.playItemSound(c.Sound, soundNeutral, 1, triangle(1, 0.4), true)
		p.mu.Lock()
		p.use.hunger.Food, p.use.hunger.Saturation = int32(p.food), p.saturation
		p.use.hunger.Eat(f)
		p.food, p.saturation = int(p.use.hunger.Food), p.use.hunger.Saturation
		p.mu.Unlock()
		if id := soundID("minecraft:entity.player.burp"); id >= 0 {
			p.sendSound(id, soundPlayers, 0.5, randBetween(0.9, 1), true)
		}
	}
	if pc, ok := st.PotionContents(); ok {
		scale := float32(1)
		if v, ok := st.Component(item.PotionDurationScale); ok && len(v) == 4 {
			var r proto.Reader
			r.B = v
			scale, _ = r.F32()
		}
		if pc.Potion >= 0 {
			for _, e := range item.PotionEffects(pc.Potion) {
				id, ok := item.BuiltinID("mob_effect", e.Effect)
				if ok {
					p.addEffect(item.Effect{ID: id, Amplifier: e.Amplifier, Duration: scaleDuration(e.Duration, scale), ShowParticles: true, ShowIcon: true})
				}
			}
		}
		for _, e := range pc.Effects {
			e.Duration = scaleDuration(e.Duration, scale)
			p.addEffect(e)
		}
	}
	if st.Has(item.SuspiciousStewEffects) {
		if v, ok := st.Component(item.SuspiciousStewEffects); ok {
			for _, e := range decodeStew(v) {
				p.addEffect(e)
			}
		}
	}
	if st.Has(item.OminousBottleAmplifier) {
		amp := int32(0)
		if v, ok := st.Component(item.OminousBottleAmplifier); ok {
			amp, _ = item.DecodeVarInt(v)
		}
		if id, ok := item.BuiltinID("mob_effect", "bad_omen"); ok {
			p.addEffect(item.Effect{ID: id, Amplifier: amp, Duration: 120000, ShowIcon: true})
		}
	}
	for _, e := range c.Effects {
		p.applyConsumeEffect(e)
	}
	if p.GameMode() != api.Creative {
		st.Shrink(1)
	}
	return st
}

func scaleDuration(d int32, scale float32) int32 {
	if d < 0 || d <= 1 || scale == 1 {
		return d
	}
	return max(int32(float32(d)*scale), 1)
}

func (p *Player) applyConsumeEffect(e item.ConsumeEffect) {
	switch e.Kind {
	case item.ConsumeApplyEffects:
		if rand.Float32() >= e.Probability {
			return
		}
		for _, x := range e.Effects {
			p.addEffect(x)
		}
	case item.ConsumeRemoveEffects:
		for id := range p.use.effects {
			if inSet(e.Remove, id) {
				p.removeEffect(id)
			}
		}
	case item.ConsumeClearAllEffects:
		for id := range p.use.effects {
			p.removeEffect(id)
		}
	case item.ConsumePlaySound:
		p.playItemSound(e.Sound, soundPlayers, 1, 1, true)
	}
}

func inSet(h item.HolderSet, id int32) bool {
	for _, x := range h.IDs {
		if x == id {
			return true
		}
	}
	return false
}

// afterUse is ItemStack.applyAfterUseComponentSideEffects: use remainder
// and use cooldown of the stack as it was before use.
func (p *Player) afterUse(st, before item.Stack) item.Stack {
	if r, ok := before.UseRemainder(); ok && p.GameMode() != api.Creative && st.Count < before.Count {
		if st.IsEmpty() {
			st = r
		} else {
			p.inv.mu.Lock()
			rest := p.inv.m.AddToInventory(r, menuPlayer{p})
			p.inv.mu.Unlock()
			if !rest.IsEmpty() {
				p.dropItem(rest, false)
			}
		}
	}
	if c, ok := before.UseCooldown(); ok {
		p.addCooldown(before, c.Ticks())
	}
	return st
}

// swapEquipment is Equippable.swapWithEquipmentSlot (right-click equip).
func (p *Player) swapEquipment(st item.Stack, e item.EquipInfo) (useResult, item.Stack, bool) {
	inv := equipInvIndex(e.Slot)
	if inv < 0 || !item.EquippableBy(st, e.Slot, playerEntityType()) {
		return usePass, item.Stack{}, false
	}
	creative := p.GameMode() == api.Creative
	s := p.inv
	s.mu.Lock()
	worn := s.get(inv)
	if !creative && worn.EnchantmentLevel("minecraft:binding_curse") > 0 || item.SameItemSameComponents(st, worn) {
		s.mu.Unlock()
		return useFail, item.Stack{}, false
	}
	var held, overflow item.Stack
	if st.Count <= 1 {
		switch {
		case !worn.IsEmpty():
			held = worn
		case creative:
			held = st
		}
		s.set(inv, st)
	} else {
		one := st.WithCount(1)
		if !creative {
			st.Shrink(1)
		}
		s.set(inv, one)
		held, overflow = st, worn
	}
	if !overflow.IsEmpty() {
		overflow = s.m.AddToInventory(overflow, menuPlayer{p})
	}
	s.mu.Unlock()
	if !overflow.IsEmpty() {
		p.dropItem(overflow, false)
	}
	p.playItemSound(e.EquipSound, soundPlayers, 1, 1, false)
	return useSuccess, held, true
}

func equipInvIndex(slot int32) int {
	switch slot {
	case item.SlotFeet:
		return 36
	case item.SlotLegs:
		return 37
	case item.SlotChest:
		return 38
	case item.SlotHead:
		return 39
	case item.SlotOffHand:
		return invOffhand
	}
	return -1
}

func (p *Player) addEffect(e item.Effect) {
	u := &p.use
	name := ""
	if n, ok := mobEffectName(e.ID); ok {
		name = n
	}
	switch name {
	case "minecraft:instant_health":
		p.heal(float64(int32(4) << min(e.Amplifier, 30)))
		return
	case "minecraft:instant_damage":
		p.Damage(float64(int32(6)<<min(e.Amplifier, 30)), api.DamageSource{Type: "minecraft:magic"})
		return
	case "minecraft:saturation":
		p.mu.Lock()
		p.use.hunger.Food, p.use.hunger.Saturation = int32(p.food), p.saturation
		p.use.hunger.Eat(item.FoodInfo{Nutrition: e.Amplifier + 1, Saturation: float32(e.Amplifier+1) * 2})
		p.food, p.saturation = int(p.use.hunger.Food), p.use.hunger.Saturation
		p.mu.Unlock()
		if e.Duration <= 1 {
			return
		}
	}
	if old, ok := u.effects[e.ID]; ok {
		better := e.Amplifier > old.amplifier || e.Amplifier == old.amplifier && (e.Duration == -1 || old.duration != -1 && e.Duration > old.duration)
		if !better {
			return
		}
	}
	u.effects[e.ID] = &activeEffect{id: e.ID, amplifier: e.Amplifier, duration: e.Duration, ambient: e.Ambient, particles: e.ShowParticles, icon: e.ShowIcon}
	p.onEffectChanged(e.ID, e.Amplifier, true)
	p.noteEffectEnd(e.ID, e.Duration)
	var flags uint8
	if e.Ambient {
		flags |= 1
	}
	if e.ShowParticles {
		flags |= 2
	}
	if e.ShowIcon {
		flags |= 4
	}
	p.send(&packets.ClientboundUpdateMobEffect{EntityID: p.e.ID, EffectID: e.ID, Amplifier: e.Amplifier, Duration: e.Duration, Flags: flags})
}

func (p *Player) removeEffect(id int32) {
	if _, ok := p.use.effects[id]; !ok {
		return
	}
	delete(p.use.effects, id)
	p.send(&packets.ClientboundRemoveMobEffect{EntityID: p.e.ID, EffectID: id})
	p.onEffectChanged(id, 0, false)
}

func mobEffectName(id int32) (string, bool) {
	for _, n := range []string{"instant_health", "instant_damage", "saturation", "regeneration", "poison", "wither", "hunger"} {
		if x, ok := item.BuiltinID("mob_effect", n); ok && x == id {
			return "minecraft:" + n, true
		}
	}
	return "", false
}

// tickEffects applies the periodic effects we model and expires effects.
func (p *Player) tickEffects() {
	for id, e := range p.use.effects {
		name, _ := mobEffectName(id)
		interval := func(base int32) bool {
			k := base >> min(e.amplifier, 30)
			return k <= 0 || e.duration%k == 0
		}
		switch name {
		case "minecraft:regeneration":
			if interval(50) && p.Health() < 20 {
				p.heal(1)
			}
		case "minecraft:poison":
			if interval(25) && p.Health() > 1 {
				p.Damage(1, api.DamageSource{Type: "minecraft:magic"})
			}
		case "minecraft:wither":
			if interval(40) {
				p.Damage(1, api.DamageSource{Type: "minecraft:wither"})
			}
		case "minecraft:hunger":
			p.exhaust(0.005 * float32(e.amplifier+1))
		}
		if e.duration > 0 {
			e.duration--
			if e.duration == 0 {
				p.removeEffect(id)
			}
		}
	}
}

// syncEquipment broadcasts visible equipment changes (SetEquipment) to
// players tracking p, and sends the equipment of newly tracked players.
func (p *Player) syncEquipment() {
	cur := p.equipment()
	u := &p.use
	var changed []int
	for i := range cur {
		if !u.equipInit || !item.Matches(cur[i], u.equip[i]) {
			changed = append(changed, i)
		}
	}
	u.equip, u.equipInit = cur, true
	if len(changed) > 0 {
		raw := encode(&packets.ClientboundSetEquipment{EntityID: p.e.ID, Raw: equipmentRaw(cur, changed)})
		p.mu.Lock()
		p.extra = append(p.extra, raw)
		p.mu.Unlock()
	}
	w := p.world.Load()
	for _, o := range w.playerList() {
		if o == p {
			continue
		}
		tracked := p.isTracking(o.e.ID)
		if tracked && !u.seen[o.e.ID] {
			all := o.equipment()
			p.send(&packets.ClientboundSetEquipment{EntityID: o.e.ID, Raw: equipmentRaw(all, []int{0, 1, 2, 3, 4, 5})})
		}
		if tracked {
			u.seen[o.e.ID] = true
		} else {
			delete(u.seen, o.e.ID)
		}
	}
}

// equipment returns main hand, off hand, feet, legs, chest, head.
func (p *Player) equipment() [6]item.Stack {
	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	return [6]item.Stack{s.get(s.selected()), s.get(invOffhand), s.get(36), s.get(37), s.get(38), s.get(39)}
}

func equipmentRaw(eq [6]item.Stack, slots []int) []byte {
	var b []byte
	for k, i := range slots {
		id := byte(i)
		if k < len(slots)-1 {
			id |= 0x80
		}
		b = append(b, id)
		b = item.AppendStack(b, eq[i])
	}
	return b
}

func decodeStew(v []byte) []item.Effect {
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	if err != nil || n < 0 || int(n) > len(v) {
		return nil
	}
	out := make([]item.Effect, 0, n)
	for range n {
		id, err1 := r.VarInt()
		d, err2 := r.VarInt()
		if err1 != nil || err2 != nil {
			return out
		}
		out = append(out, item.Effect{ID: id, Duration: d, ShowParticles: true, ShowIcon: true})
	}
	return out
}
