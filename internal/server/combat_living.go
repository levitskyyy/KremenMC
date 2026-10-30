package server

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand/v2"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Synched data indices (26.3, from the reference accessors).
const (
	metaHealth     = 9
	metaAbsorption = 17
	serFloat       = 3
)

func floatMeta(index byte, v float32) []byte {
	b := []byte{index, serFloat, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[2:], math.Float32bits(v))
	return entity.EndMeta(b)
}

func (m *Mob) healthMeta() {
	m.trk.Push(encode(&packets.ClientboundSetEntityData{EntityID: m.e.ID, Raw: floatMeta(metaHealth, m.liv.health)}))
}

func (p *Player) absorbMeta(v float32) {
	raw := encode(&packets.ClientboundSetEntityData{EntityID: p.e.ID, Raw: floatMeta(metaAbsorption, v)})
	p.c.SendRaw(raw)
	p.mu.Lock()
	p.extra = append(p.extra, raw)
	p.mu.Unlock()
}

func (p *Player) sendHealthNow() { p.sendHealth() }

// exhaustSafe adds exhaustion from any goroutine via the player's queue.
func (p *Player) exhaustSafe(x float32) {
	if x <= 0 {
		return
	}
	p.later(func() { p.exhaust(x) })
}

// later runs fn on the player's own tick (tick-owned state such as
// effects, cooldowns and item use must not be touched by other players'
// goroutines). Safe to call with p.liv.mu held.
func (p *Player) later(fn func()) {
	p.laterMu.Lock()
	p.deferred = append(p.deferred, fn)
	p.laterMu.Unlock()
}

func (p *Player) runLater() {
	p.laterMu.Lock()
	fs := p.deferred
	p.deferred = nil
	p.laterMu.Unlock()
	for _, f := range fs {
		f()
	}
}

// --- attribute modifiers from items ---

type itemMod struct {
	attr  entity.Attr
	mod   entity.Modifier
	group string
}

var (
	protoModsMu sync.Mutex
	protoMods   = map[int32][]itemMod{}
)

var slotGroups = []string{"any", "mainhand", "offhand", "hand", "feet", "legs", "chest", "head", "armor", "body", "saddle"}

// itemModifiers returns the attribute_modifiers of st.
func itemModifiers(st item.Stack) []itemMod {
	if st.IsEmpty() {
		return nil
	}
	if v, removed, in := st.Patch.Get(item.AttributeModifiers); in {
		if removed {
			return nil
		}
		return decodeItemMods(v)
	}
	protoModsMu.Lock()
	defer protoModsMu.Unlock()
	if m, ok := protoMods[st.Item]; ok {
		return m
	}
	var out []itemMod
	if p, err := wgdata.Load(); err == nil {
		if raw, ok := p.Report("minecraft/components/item/" + item.NameOf(st.Item)[len("minecraft:"):] + ".json"); ok {
			var doc struct {
				C struct {
					Mods []struct {
						Type   string  `json:"type"`
						Amount float64 `json:"amount"`
						ID     string  `json:"id"`
						Op     string  `json:"operation"`
						Slot   string  `json:"slot"`
					} `json:"minecraft:attribute_modifiers"`
				} `json:"components"`
			}
			if json.Unmarshal(raw, &doc) == nil {
				for _, m := range doc.C.Mods {
					a, ok := entity.AttrByName(m.Type)
					op, ok2 := entity.OpByName(m.Op)
					if !ok || !ok2 {
						continue
					}
					g := m.Slot
					if g == "" {
						g = "any"
					}
					out = append(out, itemMod{a, entity.Modifier{ID: ns(m.ID), Amount: m.Amount, Op: op}, g})
				}
			}
		}
	}
	protoMods[st.Item] = out
	return out
}

func decodeItemMods(v []byte) []itemMod {
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	if err != nil || n < 0 || int(n) > len(v) {
		return nil
	}
	var out []itemMod
	for range n {
		a, e1 := r.VarInt()
		id, e2 := r.String(32767)
		amt, e3 := r.F64()
		op, e4 := r.VarInt()
		g, e5 := r.VarInt()
		disp, e6 := r.VarInt()
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
			return out
		}
		if g >= 0 && int(g) < len(slotGroups) && a >= 0 && a < int32(entity.NumAttrs) {
			out = append(out, itemMod{entity.Attr(a), entity.Modifier{ID: id, Amount: amt, Op: entity.Op(op)}, slotGroups[g]})
		}
		if disp == 2 {
			return out // override display text; not decoded
		}
	}
	return out
}

func groupHas(group string, slot int) bool {
	switch group {
	case "armor":
		return slot >= slotFeet && slot <= slotHead
	case "saddle":
		return false
	}
	return slotMatches(group, slot)
}

// slotModifiers lists the modifiers st grants in slot (item + enchantments).
func slotModifiers(st item.Stack, slot int) []itemMod {
	var out []itemMod
	for _, m := range itemModifiers(st) {
		if groupHas(m.group, slot) {
			out = append(out, m)
		}
	}
	enchantAttributes(st, slot, func(attr, id string, amount float64, op string) {
		a, ok := entity.AttrByName(attr)
		o, ok2 := entity.OpByName(op)
		if ok && ok2 {
			out = append(out, itemMod{a, entity.Modifier{ID: id, Amount: amount, Op: o}, slotNames[slot]})
		}
	})
	return out
}

// refreshEquipment re-applies attribute modifiers for changed equipment
// (LivingEntity.collectEquipmentChanges). Called with l.mu held.
func (l *living) refreshEquipment(eq [numSlots]item.Stack) {
	if l.attrs == nil {
		return
	}
	for slot := range eq {
		if item.Matches(eq[slot], l.applied[slot]) && eq[slot].Count == l.applied[slot].Count && eq[slot].Damage() == l.applied[slot].Damage() {
			continue
		}
		for _, m := range slotModifiers(l.applied[slot], slot) {
			l.attrs.RemoveModifier(m.attr, m.mod.ID)
		}
		for _, m := range slotModifiers(eq[slot], slot) {
			l.attrs.SetModifier(m.attr, m.mod)
		}
		l.applied[slot] = eq[slot]
	}
}

// attrPacket encodes Update Attributes for attrs (nil: all syncable).
func attrPacket(id int32, a *entity.Attrs, list []entity.Attr) *packets.ClientboundUpdateAttributes {
	pk := &packets.ClientboundUpdateAttributes{EntityID: id}
	for _, at := range list {
		prop := packets.ClientboundUpdateAttributesProperty{Key: int32(at), Value: a.Base(at)}
		for _, m := range a.Modifiers(at) {
			prop.Modifiers = append(prop.Modifiers, packets.ClientboundUpdateAttributesPropertyModifier{UUID: m.ID, Amount: m.Amount, Operation: int8(m.Op)})
		}
		pk.Properties = append(pk.Properties, prop)
	}
	return pk
}

// --- effects ---

func effectName(id int32) string {
	for _, r := range builtinNames("mob_effect") {
		if r.id == id {
			return r.name
		}
	}
	return ""
}

type idName struct {
	id   int32
	name string
}

var (
	builtinMu    sync.Mutex
	builtinCache = map[string][]idName{}
)

func builtinNames(reg string) []idName {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	if l, ok := builtinCache[reg]; ok {
		return l
	}
	var out []idName
	if p, err := wgdata.Load(); err == nil {
		if names, ok := p.Builtin(reg); ok {
			for i, n := range names {
				out = append(out, idName{int32(i), ns(n)})
			}
		}
	}
	builtinCache[reg] = out
	return out
}

// setEffect mirrors an effect change into the combat state: attribute
// modifiers (scaled by amplifier+1), absorption and health clamps.
func (l *living) setEffect(id, amp int32, on bool) {
	if l.attrs == nil {
		return
	}
	name := effectName(id)
	mods := entity.EffectModifiers(name)
	if on {
		l.fx[id] = amp
		for _, m := range mods {
			mm := m.Modifier
			mm.Amount *= float64(amp + 1)
			l.attrs.RemoveModifier(m.Attr, mm.ID)
			l.attrs.SetModifier(m.Attr, mm)
		}
		if name == "minecraft:absorption" {
			l.absorb = max(l.absorb, float32(l.attrs.Value(entity.AttrMaxAbsorption)))
		}
	} else {
		delete(l.fx, id)
		delete(l.fxLeft, id)
		for _, m := range mods {
			l.attrs.RemoveModifier(m.Attr, m.ID)
		}
	}
	l.absorb = min(l.absorb, float32(l.attrs.Value(entity.AttrMaxAbsorption)))
}

// onEffectChanged is called by the player effect code when an effect is
// added (on) or removed.
func (p *Player) onEffectChanged(id, amp int32, on bool) {
	l := &p.liv
	l.mu.Lock()
	before := l.absorb
	l.setEffect(id, amp, on)
	after := l.absorb
	mh := float32(p.maxHealthAttr())
	l.mu.Unlock()
	if after != before {
		p.absorbMeta(after)
	}
	l.mu.Lock()
	if h := p.curHealth(); h > mh {
		p.setCurHealth(mh)
	}
	l.mu.Unlock()
}

// --- per-tick state ---

// tickLiving runs the combat part of LivingEntity.baseTick/aiStep shared by
// mobs and players: cooldowns, attribute sync, fire, air, void.
func tickLiving(t combatant) {
	l := t.lv()
	if l.attrs == nil {
		return
	}
	eq := t.gear()
	l.mu.Lock()
	if l.cooldown > 0 {
		l.cooldown--
	}
	if l.hurtTime > 0 {
		l.hurtTime--
	}
	l.ticker++
	if !item.Matches(eq[slotMain], l.applied[slotMain]) && t.isPlayer() {
		l.ticker = 0
	}
	l.refreshEquipment(eq)
	dirty := l.attrs.TakeDirty()
	if !l.synced {
		l.synced = true
		dirty = l.attrs.Synced()
	}
	var pk *packets.ClientboundUpdateAttributes
	if len(dirty) > 0 {
		pk = attrPacket(t.EntityID(), l.attrs, dirty)
	}
	dead := l.dead
	fire := l.fire
	if l.fire > 0 {
		l.fire--
	}
	mh := float32(l.attrs.Value(entity.AttrMaxHealth))
	l.mu.Unlock()
	if pk != nil {
		raw := encode(pk)
		switch v := t.(type) {
		case *Player:
			v.c.SendRaw(raw)
			v.mu.Lock()
			v.extra = append(v.extra, raw)
			v.mu.Unlock()
		case *Mob:
			v.trk.Push(raw)
		}
	}
	l.mu.Lock()
	if h := t.curHealth(); h > mh {
		t.setCurHealth(mh)
	}
	l.mu.Unlock()
	if dead {
		return
	}
	w := t.wld()
	pos := t.feet()
	if pos.Y < float64(w.MinY()-64) {
		hurt(t, NewDamage("minecraft:out_of_world"), 4)
	}
	feetFluid, _ := entity.FluidAt(w.w.Block(floorInt(pos.X), floorInt(pos.Y), floorInt(pos.Z)))
	eyeFluid, _ := entity.FluidAt(w.w.Block(floorInt(pos.X), floorInt(pos.Y+t.eyeY()), floorInt(pos.Z)))
	if feetFluid == entity.FluidLava {
		if _, ok := l.effect("fire_resistance"); !ok {
			if hurt(t, NewDamage("minecraft:lava"), 4) {
				t.lv().ignite(300)
			}
		} else {
			t.lv().ignite(300)
		}
	}
	if fire > 0 {
		if feetFluid == entity.FluidWater {
			l.mu.Lock()
			l.fire = 0
			l.mu.Unlock()
		} else if fire%20 == 0 {
			hurt(t, NewDamage("minecraft:on_fire"), 1)
		}
	}
	tickAir(t, eyeFluid == entity.FluidWater)
	syncFireAndAir(t)
}

// syncFireAndAir sends the on-fire shared flag and, to players, the air
// supply when they change.
func syncFireAndAir(t combatant) {
	l := t.lv()
	l.mu.Lock()
	burning := l.fire > 0
	air := l.air
	fireChanged := burning != l.shownFire
	airChanged := air != l.shownAir
	l.shownFire, l.shownAir = burning, air
	l.mu.Unlock()
	var flags byte
	if burning {
		flags |= 0x01
	}
	switch v := t.(type) {
	case *Mob:
		if fireChanged {
			v.mu.Lock()
			if v.glowing {
				flags |= 0x40
			}
			v.mu.Unlock()
			v.trk.Push(encode(&packets.ClientboundSetEntityData{EntityID: v.e.ID, Raw: entity.EndMeta(entity.AppendMetaByte(nil, entity.IndexFlags, flags))}))
		}
	case *Player:
		var b []byte
		if fireChanged {
			v.mu.Lock()
			if v.sneaking {
				flags |= entity.FlagCrouching
			}
			if v.sprinting {
				flags |= entity.FlagSprinting
			}
			v.mu.Unlock()
			b = entity.AppendMetaByte(b, entity.IndexFlags, flags)
			raw := encode(&packets.ClientboundSetEntityData{EntityID: v.e.ID, Raw: entity.EndMeta(b)})
			v.mu.Lock()
			v.extra = append(v.extra, raw)
			v.mu.Unlock()
		}
		if fireChanged || airChanged {
			b = append(b, metaAir, serVarInt)
			b = proto.AppendVarInt(b, air)
			v.send(&packets.ClientboundSetEntityData{EntityID: v.e.ID, Raw: entity.EndMeta(b)})
		}
	}
}

const (
	metaAir   = 1
	serVarInt = 1
)

// tickAir is the air supply: −1 per tick under water (respiration may
// skip), drowning for 2 at −20; +4 per tick out of water.
func tickAir(t combatant, underwater bool) {
	l := t.lv()
	if inTag("entity_type", "minecraft:can_breathe_under_water", t.typeName()) {
		return
	}
	if p, ok := t.(*Player); ok {
		if gm := p.GameMode(); gm == api.Creative || gm == api.Spectator {
			underwater = false
		}
	}
	l.mu.Lock()
	maxAir := int32(300)
	if underwater {
		if _, ok := l.effect("water_breathing"); !ok {
			bonus := l.attrs.Value(entity.AttrOxygenBonus)
			if bonus <= 0 || rand.Float64() >= 1/(bonus+1) {
				l.air--
			}
		}
	} else if l.air < maxAir {
		l.air = min(l.air+4, maxAir)
	}
	drown := l.air <= -20
	if drown {
		l.air = 0
	}
	l.mu.Unlock()
	if drown {
		hurt(t, NewDamage("minecraft:drown"), 2)
	}
}

func floorInt(v float64) int { return int(math.Floor(v)) }

// --- falling ---

// fallDamage is LivingEntity.calculateFallDamage plus the landing block's
// multiplier.
func fallDamage(t combatant, dist float64, mult float32) float32 {
	l := t.lv()
	if l.attrs == nil || inTag("entity_type", "minecraft:fall_damage_immune", t.typeName()) {
		return 0
	}
	l.mu.Lock()
	safe := l.attrs.Value(entity.AttrSafeFallDistance)
	fm := l.attrs.Value(entity.AttrFallDamageMultiplier)
	l.mu.Unlock()
	d := (dist - safe) * float64(mult) * fm
	if d <= 0 {
		return 0
	}
	return float32(math.Floor(d))
}

// landingMultiplier is Block.fallOn's damage multiplier for the block
// landed on (minecraft.wiki: hay bale and honey −80%, beds −50%, slime 0).
func landingMultiplier(s data.StateID, sneaking bool) float32 {
	b := data.BlockOf(s)
	if b == nil {
		return 1
	}
	switch {
	case b.Name == "slime_block":
		if sneaking {
			return 1
		}
		return 0
	case b.Name == "hay_block", b.Name == "honey_block":
		return 0.2
	case len(b.Name) > 4 && b.Name[len(b.Name)-4:] == "_bed":
		return 0.5
	case b.Name == "powder_snow", b.Name == "water", b.Name == "bubble_column", b.Name == "cobweb":
		return 0
	}
	return 1
}

// onLanded applies fall damage for a landing after falling dist.
func onLanded(t combatant, dist float64) {
	if dist <= 0 {
		return
	}
	w := t.wld()
	pos := t.feet()
	below := w.w.Block(floorInt(pos.X), floorInt(pos.Y-0.2), floorInt(pos.Z))
	sneak := false
	if p, ok := t.(*Player); ok {
		sneak = p.Sneaking()
	}
	if d := fallDamage(t, dist, landingMultiplier(below, sneak)); d > 0 {
		fallSounds(t, d)
		hurt(t, NewDamage("minecraft:fall"), d)
	}
}

// --- shields ---

type blocksAttacks struct {
	delay      float32
	disableMul float32
	threshold  float32
	base       float32
	factor     float32
	bypass     string
}

var (
	blocksMu    sync.Mutex
	blocksCache = map[int32]*blocksAttacks{}
)

func blocksAttacksOf(st item.Stack) *blocksAttacks {
	if st.IsEmpty() || !st.Has(item.BlocksAttacks) {
		return nil
	}
	blocksMu.Lock()
	defer blocksMu.Unlock()
	if b, ok := blocksCache[st.Item]; ok {
		return b
	}
	b := &blocksAttacks{disableMul: 1, threshold: 1, factor: 1, bypass: ""}
	if p, err := wgdata.Load(); err == nil {
		if raw, ok := p.Report("minecraft/components/item/" + item.NameOf(st.Item)[len("minecraft:"):] + ".json"); ok {
			var doc struct {
				C struct {
					B *struct {
						Delay      float32  `json:"block_delay_seconds"`
						DisableMul *float32 `json:"disable_cooldown_scale"`
						Bypass     string   `json:"bypassed_by"`
						Dmg        *struct {
							Threshold float32 `json:"threshold"`
							Base      float32 `json:"base"`
							Factor    float32 `json:"factor"`
						} `json:"item_damage"`
					} `json:"minecraft:blocks_attacks"`
				} `json:"components"`
			}
			if json.Unmarshal(raw, &doc) == nil && doc.C.B != nil {
				x := doc.C.B
				b.delay, b.bypass = x.Delay, x.Bypass
				if x.DisableMul != nil {
					b.disableMul = *x.DisableMul
				}
				if x.Dmg != nil {
					b.threshold, b.base, b.factor = x.Dmg.Threshold, x.Dmg.Base, x.Dmg.Factor
				}
			}
		}
	}
	blocksCache[st.Item] = b
	return b
}

type shieldState struct {
	stack item.Stack
	slot  int
	used  int32
}

// updateBlocking snapshots the player's item use for blocking checks by
// other goroutines; runs on the player's tick.
func (p *Player) updateBlocking() {
	u := &p.use
	var b shieldState
	if u.active && u.stack.Has(item.BlocksAttacks) {
		b.stack, b.used = u.stack, useDuration(u.stack)-u.remaining
		if u.hand == api.OffHand {
			b.slot = slotOff
		}
	}
	p.liv.mu.Lock()
	p.liv.block = b
	p.liv.mu.Unlock()
}

// blockingWith returns the shield a player is actively blocking with.
// Called with p.liv.mu held.
func blockingWith(p *Player) (item.Stack, int, *blocksAttacks) {
	u := p.liv.block
	if u.stack.IsEmpty() {
		return item.Stack{}, 0, nil
	}
	b := blocksAttacksOf(u.stack)
	if b == nil || u.used < int32(b.delay*20) {
		return item.Stack{}, 0, nil
	}
	return u.stack, u.slot, b
}

// blockWithShield is the blocks_attacks part of hurtServer: the blocked
// amount (all of it for the default reductions) when the source is in
// front of a blocking player. Called with the victim's l.mu held.
func blockWithShield(t combatant, src *DamageSource, dt *damageType, amount float32) (float32, bool) {
	p, ok := t.(*Player)
	if !ok {
		return 0, false
	}
	st, _, b := blockingWith(p)
	if b == nil || (b.bypass != "" && dt.is(ns(b.bypass[1:]))) {
		return 0, false
	}
	if p.liv.shieldOff > p.s.CurrentTick() {
		return 0, false
	}
	_ = st
	pos, ok := src.sourcePos()
	if !ok {
		return 0, false
	}
	loc := p.Location()
	look := lookDir(loc.Yaw, 0)
	dx, dz := pos.X-loc.Pos.X, pos.Z-loc.Pos.Z
	n := math.Hypot(dx, dz)
	if n < 1e-9 {
		return 0, false
	}
	// Default damage reduction: horizontal blocking angle 90°.
	if (dx*look.X+dz*look.Z)/n < math.Cos(math.Pi/2) {
		return 0, false
	}
	return amount, true
}

// afterShield damages the shield, knocks back a melee attacker and
// handles axe disabling.
func afterShield(t combatant, src *DamageSource, blocked float32, ok bool) {
	p, isP := t.(*Player)
	if !isP || !ok {
		return
	}
	p.liv.mu.Lock()
	st, slot, b := blockingWith(p)
	p.liv.mu.Unlock()
	if b == nil {
		return
	}
	if blocked >= b.threshold {
		p.damageEquipment(slot, int32(math.Floor(float64(b.base+b.factor*blocked))))
	}
	p.sendSound(soundID("minecraft:item.shield.block"), soundPlayers, 1, 0.8+rand.Float32()*0.4, true)
	if a, ok := src.Direct.(combatant); ok && src.isDirect() {
		if !a.isPlayer() {
			pp, ap := p.feet(), a.feet()
			knockback(a, 0.5, ap.X-pp.X, ap.Z-pp.Z)
		}
		if secs := weaponDisableSeconds(a.gear()[slotMain]); secs > 0 {
			ticks := int64(secs * b.disableMul * 20)
			p.liv.mu.Lock()
			p.liv.shieldOff = p.s.CurrentTick() + ticks
			p.liv.mu.Unlock()
			p.later(func() {
				p.addCooldown(st, int32(ticks))
				p.stopUsing()
			})
			p.sendSound(soundID("minecraft:item.shield.break"), soundPlayers, 0.8, 0.8+rand.Float32()*0.4, true)
		}
	}
}

func weaponDisableSeconds(st item.Stack) float32 {
	v, ok := st.Component(item.Weapon)
	if !ok {
		return 0
	}
	r := proto.Reader{B: v}
	if _, err := r.VarInt(); err != nil {
		return 0
	}
	f, err := r.F32()
	if err != nil {
		return 0
	}
	return f
}

func weaponItemDamage(st item.Stack) (int32, bool) {
	v, ok := st.Component(item.Weapon)
	if !ok {
		return 0, false
	}
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	return n, err == nil
}

// --- totem and death ---

// tryTotem is LivingEntity.checkTotemDeathProtection.
func tryTotem(t combatant, dt *damageType) bool {
	if dt.is("minecraft:bypasses_invulnerability") {
		return false
	}
	eq := t.gear()
	for _, slot := range []int{slotMain, slotOff} {
		st := eq[slot]
		effects, ok := st.DeathProtection()
		if !ok {
			continue
		}
		st.Count--
		t.setEquipment(slot, st.Norm())
		l := t.lv()
		l.mu.Lock()
		t.setCurHealth(1)
		l.mu.Unlock()
		switch v := t.(type) {
		case *Player:
			l.mu.Lock()
			for _, e := range effects {
				switch e.Kind {
				case item.ConsumeClearAllEffects:
					for id := range l.fx {
						l.setEffect(id, 0, false)
					}
				case item.ConsumeApplyEffects:
					for _, x := range e.Effects {
						l.setEffect(x.ID, x.Amplifier, true)
					}
				}
			}
			abs := l.absorb
			l.mu.Unlock()
			v.absorbMeta(abs)
			v.later(func() {
				for _, e := range effects {
					v.applyConsumeEffect(e)
				}
			})
			v.sendHealthNow()
		case *Mob:
			for _, e := range effects {
				v.applyConsume(e)
			}
		}
		t.wld().broadcastNear(t.feet(), &packets.ClientboundEntityEvent{EntityID: t.EntityID(), EntityStatus: 35})
		return true
	}
	return false
}

// entityLoot rolls the death loot of a mob; nil until the loot module is
// wired in (see docs/vanilla-parity.md).
var entityLoot func(m *Mob, src DamageSource, killer *Player) []item.Stack

func die(t combatant, src DamageSource) {
	l := t.lv()
	l.mu.Lock()
	if l.dead {
		l.mu.Unlock()
		return
	}
	l.dead = true
	l.deathTime = 0
	killer := l.killer
	l.mu.Unlock()
	w := t.wld()
	w.broadcastNear(t.feet(), &packets.ClientboundEntityEvent{EntityID: t.EntityID(), EntityStatus: 3})
	switch v := t.(type) {
	case *Mob:
		v.healthMeta()
		var drops []item.Stack
		if entityLoot != nil {
			drops = entityLoot(v, src, killer)
		}
		drops = append(drops, v.dropEquipment(killer, lootingOf(killer))...)
		if w.s.bus.HasHandlers(api.KindEntityDeath) {
			ev := &api.EntityDeathEvent{Entity: v, Source: src.api()}
			for _, d := range drops {
				ev.Drops = append(ev.Drops, stackToAPI(d))
			}
			w.s.bus.Dispatch(ev)
			drops = drops[:0]
			for _, d := range ev.Drops {
				drops = append(drops, stackFromAPI(d))
			}
		}
		if w.ruleBool("do_mob_loot", "doMobLoot", true) {
			pos := v.feet()
			for _, d := range drops {
				w.spawnAtLocation(pos, d)
			}
			if killer != nil {
				w.dropXP(pos, v.deathXP())
			}
		}
		mobDied(v)
	case *Player:
		v.playerDied(src, killer)
	}
}

// spawnAtLocation is Entity.spawnAtLocation: an item at the entity with
// the default random toss.
func (w *World) spawnAtLocation(pos api.Vec3, st item.Stack) {
	if st.IsEmpty() {
		return
	}
	vel := api.Vec3{X: rand.Float64()*0.2 - 0.1, Y: 0.2, Z: rand.Float64()*0.2 - 0.1}
	w.spawnItem(pos, vel, st, 10)
}

func (m *Mob) applyConsume(e item.ConsumeEffect) {
	switch e.Kind {
	case item.ConsumeClearAllEffects:
		m.liv.mu.Lock()
		for id := range m.liv.fx {
			m.liv.setEffect(id, 0, false)
		}
		m.liv.mu.Unlock()
	case item.ConsumeApplyEffects:
		for _, x := range e.Effects {
			m.addEffect(x)
		}
	}
}

func (m *Mob) addEffect(e item.Effect) {
	m.liv.mu.Lock()
	if old, ok := m.liv.fx[e.ID]; ok && old > e.Amplifier {
		m.liv.mu.Unlock()
		return
	}
	m.liv.setEffect(e.ID, e.Amplifier, true)
	m.liv.fxLeft[e.ID] = e.Duration
	m.liv.mu.Unlock()
}

// tickMobEffects counts mob effect durations down.
func (m *Mob) tickMobEffects() {
	l := &m.liv
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, left := range l.fxLeft {
		if left < 0 {
			continue
		}
		if left <= 1 {
			l.setEffect(id, 0, false)
			continue
		}
		l.fxLeft[id] = left - 1
	}
}

func lookDir(yaw, pitch float32) api.Vec3 {
	y, p := float64(yaw)*math.Pi/180, float64(pitch)*math.Pi/180
	return api.Vec3{X: -math.Sin(y) * math.Cos(p), Y: -math.Sin(p), Z: math.Cos(y) * math.Cos(p)}
}
