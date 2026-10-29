package server

import (
	"math"
	"math/rand/v2"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

func (w *World) broadcastNear(pos api.Vec3, pk packets.Packet) {
	ck := world.ChunkAt(int(math.Floor(pos.X)), int(math.Floor(pos.Z))).Key()
	raw := encode(pk)
	for _, o := range w.playerList() {
		if o.hasChunk(ck) {
			o.c.SendRaw(raw)
		}
	}
}

// Animate ids.
const (
	animCrit      = 4
	animMagicCrit = 5
)

// attackStrengthScale is Player.getAttackStrengthScale(0.5).
func attackStrengthScale(ticker int32, speed float64) float32 {
	delay := float32(1 / speed * 20)
	return min(max((float32(ticker)+0.5)/delay, 0), 1)
}

func (p *Player) climbing() bool {
	pos := p.Location().Pos
	w := p.world.Load()
	b := data.BlockOf(w.w.Block(floorInt(pos.X), floorInt(pos.Y), floorInt(pos.Z)))
	return b != nil && inTag("block", "minecraft:climbable", "minecraft:"+b.Name)
}

func (p *Player) inWater() bool {
	pos := p.Location().Pos
	w := p.world.Load()
	f, _ := entity.FluidAt(w.w.Block(floorInt(pos.X), floorInt(pos.Y), floorInt(pos.Z)))
	return f == entity.FluidWater
}

// attackEntity is Player.attack for a melee hit from p.
func (p *Player) attackEntity(target api.Entity) {
	if p.GameMode() == api.Spectator || p.IsDead() {
		return
	}
	t, ok := target.(combatant)
	if !ok || target == api.Entity(p) || t.lv().attrs == nil || !target.IsValid() {
		return
	}
	if !p.canReach(t) {
		return
	}
	w := p.world.Load()
	weapon := p.Hand(api.MainHand)
	src := EntityDamage("minecraft:player_attack", p)
	l := &p.liv
	l.mu.Lock()
	dmg := float32(l.attrs.Value(entity.AttrAttackDamage))
	speed := l.attrs.Value(entity.AttrAttackSpeed)
	kbAttr := float32(l.attrs.Value(entity.AttrAttackKnockback))
	sweepRatio := float32(l.attrs.Value(entity.AttrSweepingDamageRatio))
	ticker := l.ticker
	l.ticker = 0
	_, blind := l.effect("blindness")
	_, slowFall := l.effect("slow_falling")
	l.mu.Unlock()
	scale := attackStrengthScale(ticker, speed)
	ench := weaponValue(weapon, "minecraft:damage", p, t, &src, 0)
	dmg *= 0.2 + scale*scale*0.8
	ench *= scale
	if dmg <= 0 && ench <= 0 {
		return
	}
	full := scale > 0.9
	loc := p.Location()
	p.mu.Lock()
	fall, onGround := p.e.FallDistance, p.e.OnGround
	p.mu.Unlock()
	sprintHit := p.Sprinting() && full
	crit := full && fall > 0 && !onGround && !p.climbing() && !p.inWater() && !blind && !slowFall && !p.Sprinting()
	if crit {
		dmg *= 1.5
	}
	total := dmg + ench
	sweep := full && !crit && !sprintHit && onGround && inTag("item", "minecraft:swords", itemName(weapon))
	victimEq := t.gear()
	if !hurt(t, src, total) {
		p.sendSound(soundID("minecraft:entity.player.attack.nodamage"), soundPlayers, 1, 1, true)
		return
	}
	kb := weaponValue(weapon, "minecraft:knockback", p, t, &src, kbAttr)
	if sprintHit {
		p.sendSound(soundID("minecraft:entity.player.attack.knockback"), soundPlayers, 1, 1, true)
		kb++
	}
	if kb > 0 {
		yaw := float64(loc.Yaw) * math.Pi / 180
		knockback(t, float64(kb)*0.5, math.Sin(yaw), -math.Cos(yaw))
		v := p.Velocity()
		p.SetVelocity(api.Vec3{X: v.X * 0.6, Y: v.Y, Z: v.Z * 0.6})
		p.mu.Lock()
		p.sprinting, p.metaDirty = false, true
		p.mu.Unlock()
	}
	if sweep {
		p.sweepAttack(w, t, 1+sweepRatio*dmg, loc)
	}
	switch {
	case crit:
		p.sendSound(soundID("minecraft:entity.player.attack.crit"), soundPlayers, 1, 1, true)
		w.broadcastNear(t.feet(), &packets.ClientboundAnimate{EntityID: t.EntityID(), Animation: animCrit})
	case !sweep && !sprintHit && full:
		p.sendSound(soundID("minecraft:entity.player.attack.strong"), soundPlayers, 1, 1, true)
	case !sweep && !sprintHit:
		p.sendSound(soundID("minecraft:entity.player.attack.weak"), soundPlayers, 1, 1, true)
	}
	if ench > 0 {
		w.broadcastNear(t.feet(), &packets.ClientboundAnimate{EntityID: t.EntityID(), Animation: animMagicCrit})
	}
	postAttack(victimEq, "victim", p, t, &src)
	postAttack(p.gear(), "attacker", p, t, &src)
	if n, ok := weaponItemDamage(weapon); ok && n > 0 {
		p.hurtHeld(api.MainHand, n)
	}
	p.exhaust(0.1)
}

// canReach is the server's lenient attack distance check: the target's box
// within entity_interaction_range + 3 of the eyes.
func (p *Player) canReach(t combatant) bool {
	p.liv.mu.Lock()
	r := p.liv.attrs.Value(entity.AttrEntityInteractionRange) + 3
	p.liv.mu.Unlock()
	eye := p.EyeLocation().Pos
	mn, mx := t.BoundingBox()
	cx := min(max(eye.X, mn.X), mx.X)
	cy := min(max(eye.Y, mn.Y), mx.Y)
	cz := min(max(eye.Z, mn.Z), mx.Z)
	dx, dy, dz := eye.X-cx, eye.Y-cy, eye.Z-cz
	return dx*dx+dy*dy+dz*dz <= r*r
}

// sweepAttack hits living entities beside the target (box inflated
// 1×0.25×1, within 3 blocks of the attacker) for amount with 0.4
// knockback along the attacker's yaw.
func (p *Player) sweepAttack(w *World, target combatant, amount float32, loc api.Location) {
	mn, mx := target.BoundingBox()
	box := entity.AABB{MinX: mn.X - 1, MinY: mn.Y - 0.25, MinZ: mn.Z - 1, MaxX: mx.X + 1, MaxY: mx.Y + 0.25, MaxZ: mx.Z + 1}
	yaw := float64(loc.Yaw) * math.Pi / 180
	for _, c := range w.combatants() {
		if c == combatant(p) || c == target || c.lv().attrs == nil {
			continue
		}
		cmn, cmx := c.BoundingBox()
		cb := entity.AABB{MinX: cmn.X, MinY: cmn.Y, MinZ: cmn.Z, MaxX: cmx.X, MaxY: cmx.Y, MaxZ: cmx.Z}
		if !cb.Intersects(box) || c.feet().DistanceSq(loc.Pos) >= 9 {
			continue
		}
		if m, ok := c.(*Mob); ok && m.typ.Name == "armor_stand" {
			continue
		}
		if hurt(c, EntityDamage("minecraft:player_attack", p), amount) {
			knockback(c, 0.4, math.Sin(yaw), -math.Cos(yaw))
		}
	}
	p.sendSound(soundID("minecraft:entity.player.attack.sweep"), soundPlayers, 1, 1, true)
}

// extraCombatants lets tests add detached entities to combatants().
var extraCombatants func(*World) []combatant

// combatants lists the players and living mobs of the world.
func (w *World) combatants() []combatant {
	var out []combatant
	if extraCombatants != nil {
		out = append(out, extraCombatants(w)...)
	}
	for _, o := range w.playerList() {
		if o.joined.Load() && !o.IsDead() && o.GameMode() != api.Spectator {
			out = append(out, o)
		}
	}
	for _, m := range w.mobList() {
		if m.IsValid() && m.liv.attrs != nil && !m.liv.isDead() {
			out = append(out, m)
		}
	}
	return out
}

func (l *living) isDead() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dead
}

// tickCombat runs after the player's gameplay tick.
func (p *Player) tickCombat() {
	if !p.joined.Load() {
		return
	}
	p.runLater()
	p.updateBlocking()
	if !p.liv.synced {
		p.trk.SetSpawnExtra(func() [][]byte {
			p.liv.mu.Lock()
			defer p.liv.mu.Unlock()
			return [][]byte{encode(attrPacket(p.e.ID, p.liv.attrs, p.liv.attrs.Synced()))}
		})
	}
	if p.IsDead() {
		return
	}
	tickLiving(p)
}

// onPlayerLanded applies fall damage for a client-reported landing.
func (p *Player) onPlayerLanded(dist float64) {
	if gm := p.GameMode(); gm == api.Creative || gm == api.Spectator || p.Flying() || p.inWater() {
		return
	}
	onLanded(p, dist)
}

// resetFallInFluid clears fall distance in water and on climbables
// (Entity.updateInWaterStateAndDoFluidPushing / onClimbable).
func (p *Player) resetFallInFluid() {
	if p.inWater() || p.climbing() {
		p.mu.Lock()
		p.e.FallDistance = 0
		p.mu.Unlock()
	}
}

// deathMessage builds death.attack.<msg> with the victim and killer names.
func deathMessage(victim api.Component, src DamageSource) api.Component {
	dt := damageTypeOf(src.Type)
	key := "death.attack." + dt.message
	if dt.deathMsg == "fall_variants" {
		key = "death.fell.accident.generic"
	}
	if dt.deathMsg == "intentional_game_design" {
		return api.Translatable("death.attack."+dt.message+".message", victim, api.Translatable("death.attack.badRespawnPoint.link"))
	}
	if c := src.Causing; c != nil {
		var name api.Component
		switch e := c.(type) {
		case *Player:
			name = e.DisplayName()
		case *Mob:
			if n, ok := e.CustomName(); ok {
				name = n
			} else {
				name = api.Translatable("entity.minecraft." + e.typ.Name)
			}
		}
		if dt.deathMsg != "fall_variants" {
			return api.Translatable(key+".player", victim, name)
		}
	}
	return api.Translatable(key, victim)
}

// playerDied is the server side of Player.die: death screen, broadcast
// message, inventory drops unless keep_inventory.
func (p *Player) playerDied(src DamageSource, killer *Player) {
	w := p.world.Load()
	msg := deathMessage(p.DisplayName(), src)
	keep := w.ruleBool("keep_inventory", "keepInventory", false)
	var drops []item.Stack
	if !keep {
		p.inv.mu.Lock()
		for i := range p.inv.m.Items {
			if st := p.inv.m.Items[i]; !st.IsEmpty() {
				if st.EnchantmentLevel("minecraft:vanishing_curse") == 0 {
					drops = append(drops, st)
				}
				p.inv.m.Items[i] = item.Stack{}
			}
		}
		p.inv.mu.Unlock()
	}
	show := true
	if p.s.bus.HasHandlers(api.KindEntityDeath) {
		ev := &api.EntityDeathEvent{Entity: p, Source: src.api(), DeathMessage: &msg, KeepInventory: keep}
		for _, d := range drops {
			ev.Drops = append(ev.Drops, stackToAPI(d))
		}
		p.s.bus.Dispatch(ev)
		drops = drops[:0]
		for _, d := range ev.Drops {
			drops = append(drops, stackFromAPI(d))
		}
		if ev.DeathMessage == nil {
			show = false
		} else {
			msg = *ev.DeathMessage
		}
	}
	p.send(&packets.ClientboundPlayerCombatKill{PlayerID: p.e.ID, Message: toText(msg)})
	if show && w.ruleBool("show_death_messages", "showDeathMessages", true) {
		raw := systemChat(msg, false)
		for _, o := range p.s.list {
			o.c.SendRaw(raw)
		}
	}
	for _, d := range drops {
		p.dropItem(d, true)
	}
	if !keep {
		p.later(func() {
			p.SetExp(0, 0)
			p.syncInventory()
		})
	}
	_ = killer
}

// performRespawn handles the client's "perform respawn" command after death.
func (p *Player) performRespawn() {
	if !p.IsDead() {
		return
	}
	w := p.respawnWorld()
	p.send(&packets.ClientboundRespawn{WorldState: packets.SpawnInfo{
		Dimension: w.dimID, Name: string(w.key), HashedSeed: w.hashedSeed(),
		Gamemode: int32(p.GameMode()), PreviousGamemode: -1, IsFlat: w.flat, SeaLevel: w.dim.seaLevel(),
	}})
	l := &p.liv
	l.mu.Lock()
	for id := range l.fx {
		l.setEffect(id, 0, false)
	}
	l.dead, l.absorb, l.fire, l.air, l.cooldown, l.hurtTime, l.lastHurt, l.killer = false, 0, 0, 300, 0, 0, 0, nil
	full := l.attrs.Synced()
	pk := attrPacket(p.e.ID, l.attrs, full)
	mh := p.maxHealthAttr()
	l.mu.Unlock()
	for id := range p.use.effects {
		delete(p.use.effects, id)
	}
	p.mu.Lock()
	p.health = mh
	p.food, p.saturation = 20, 5
	p.e.FallDistance, p.e.Vel = 0, api.Vec3{}
	p.metaDirty = true
	p.mu.Unlock()
	p.use.hunger = item.NewHunger()
	p.use.synced = false
	p.send(pk)
	p.sendAbilities()
	spawn := w.Spawn()
	if r, ok := p.RespawnLocation(); ok && (r.World == "" || r.World == w.name) {
		spawn = r
	}
	p.teleport(spawn)
	p.send(w.timePacket())
	p.send(w.spawnPacket())
	p.send(&packets.ClientboundGameEvent{Reason: 13})
	p.sendHealth()
	// Viewers re-pair with the respawned player, as for a new entity.
	gone, id := entity.Despawn([]int32{p.e.ID}), p.e.ID
	for _, o := range w.playerList() {
		if o == p {
			continue
		}
		o.later(func() {
			if _, ok := o.tracking[id]; ok {
				delete(o.tracking, id)
				o.c.SendRaw(gone)
			}
		})
	}
	p.mu.Lock()
	lvl, prog := p.expLevel, p.expProgress
	p.mu.Unlock()
	p.SetExp(lvl, prog)
	p.sendInventory()
}

// --- mobs ---

// tickMob is one tick of a non-player entity: physics, fall damage,
// living state, death animation and brain.
func (m *Mob) tickMob() {
	w := m.w
	if m.proj != nil {
		m.tickProjectile()
	} else {
		slow := false
		if m.liv.attrs != nil {
			m.liv.mu.Lock()
			_, slow = m.liv.effect("slow_falling")
			g := m.liv.attrs.Value(entity.AttrGravity)
			m.liv.mu.Unlock()
			m.mu.Lock()
			m.e.Gravity, m.e.SlowFalling = g, slow
		} else {
			m.mu.Lock()
		}
		fell := m.e.Tick(w.w)
		m.mu.Unlock()
		if fell > 0 && m.liv.attrs != nil {
			onLanded(m, fell)
		}
	}
	m.mu.Lock()
	m.ticks++
	m.mu.Unlock()
	if m.liv.attrs != nil {
		m.tickMobEffects()
		tickLiving(m)
		if m.liv.motion {
			m.liv.motion = false
			v := m.Velocity()
			m.trk.Push(encode(&packets.ClientboundSetEntityMotion{EntityID: m.e.ID, Velocity: proto.LpVec3{X: v.X, Y: v.Y, Z: v.Z}}))
		}
		m.liv.mu.Lock()
		dead := m.liv.dead
		if dead {
			m.liv.deathTime++
		}
		dt := m.liv.deathTime
		m.liv.mu.Unlock()
		if dead {
			if dt >= 20 {
				w.broadcastNear(m.feet(), &packets.ClientboundEntityEvent{EntityID: m.e.ID, EntityStatus: 60})
				m.Remove()
			}
			return
		}
	}
	if m.onTick != nil {
		m.onTick(m)
	} else {
		m.think()
	}
}

// mobAttack routes a mob's melee hit through the damage pipeline
// (Mob.doHurtTarget: attack_damage + enchantments, knockback attribute).
func (m *Mob) mobAttack(t combatant) {
	m.liv.mu.Lock()
	dmg := float32(m.liv.attrs.Value(entity.AttrAttackDamage))
	kb := float32(m.liv.attrs.Value(entity.AttrAttackKnockback))
	m.liv.mu.Unlock()
	weapon := m.liv.equip[slotMain]
	src := EntityDamage("minecraft:mob_attack", m)
	dmg = weaponValue(weapon, "minecraft:damage", m, t, &src, dmg)
	victimEq := t.gear()
	if !hurt(t, src, dmg) {
		return
	}
	kb = weaponValue(weapon, "minecraft:knockback", m, t, &src, kb)
	if kb > 0 {
		yaw := float64(m.Location().Yaw) * math.Pi / 180
		knockback(t, float64(kb)*0.5, math.Sin(yaw), -math.Cos(yaw))
		v := m.Velocity()
		m.SetVelocity(api.Vec3{X: v.X * 0.6, Y: v.Y, Z: v.Z * 0.6})
	}
	postAttack(victimEq, "victim", m, t, &src)
	postAttack(m.liv.equip, "attacker", m, t, &src)
}

// think is the placeholder brain (mob AI is not vanilla yet): monsters
// chase and hit the nearest player within 16 blocks, other mobs wander.
func (m *Mob) think() {
	if m.typ.Category == "misc" || m.liv.attrs == nil {
		return
	}
	m.mu.Lock()
	e := m.e
	speed := 0.0
	var dx, dz float64
	var target *Player
	if m.typ.Category == "monster" {
		bd := 16.0 * 16
		for _, p := range m.w.playerList() {
			if gm := p.GameMode(); gm == api.Creative || gm == api.Spectator || p.IsDead() {
				continue
			}
			pp := p.Location().Pos
			if d := pp.DistanceSq(e.Pos); d < bd {
				target, bd = p, d
				dx, dz = pp.X-e.Pos.X, pp.Z-e.Pos.Z
			}
		}
		if target != nil {
			speed = 0.12
			if bd < 2.5 && m.ticks-m.lastHit >= 20 {
				m.lastHit = m.ticks
				yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
				e.Yaw, e.HeadYaw = yaw, yaw
				m.mu.Unlock()
				m.mobAttack(target)
				m.mu.Lock()
			}
		}
	}
	defer m.mu.Unlock()
	if speed == 0 {
		if m.wanderLeft <= 0 && rand.IntN(80) == 0 {
			m.wanderLeft = 40 + rand.IntN(40)
			a := rand.Float64() * 2 * math.Pi
			m.wanderX, m.wanderZ = math.Cos(a), math.Sin(a)
		}
		if m.wanderLeft > 0 {
			m.wanderLeft--
			speed, dx, dz = 0.06, m.wanderX, m.wanderZ
		}
	}
	if speed == 0 {
		return
	}
	l := math.Hypot(dx, dz)
	if l < 1e-6 {
		return
	}
	e.Vel.X, e.Vel.Z = dx/l*speed, dz/l*speed
	yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	e.Yaw, e.HeadYaw = yaw, yaw
	if e.OnGround && e.HorizontalCollision {
		e.Vel.Y = 0.42
	}
}
