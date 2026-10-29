package server

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// DamageSource is a vanilla damage source: a damage_type plus the direct
// (projectile or attacker) and causing (owner) entities and, for
// positional sources such as explosions, a position.
type DamageSource struct {
	Type    string // damage_type key, e.g. "minecraft:explosion"
	Direct  api.Entity
	Causing api.Entity
	Pos     *api.Vec3
}

// orNil turns typed nil entity pointers into a nil interface.
func orNil(e api.Entity) api.Entity {
	switch v := e.(type) {
	case *Mob:
		if v == nil {
			return nil
		}
	case *Player:
		if v == nil {
			return nil
		}
	}
	return e
}

// NewDamage is an environmental source of type typ.
func NewDamage(typ string) DamageSource { return DamageSource{Type: ns(typ)} }

// EntityDamage is a melee-style source: attacker is both direct and causing.
func EntityDamage(typ string, attacker api.Entity) DamageSource {
	attacker = orNil(attacker)
	return DamageSource{Type: ns(typ), Direct: attacker, Causing: attacker}
}

// ProjectileDamage is a source whose direct entity is proj, fired by owner
// (which may be nil).
func ProjectileDamage(typ string, proj, owner api.Entity) DamageSource {
	return DamageSource{Type: ns(typ), Direct: orNil(proj), Causing: orNil(owner)}
}

// ExplosionDamage is "explosion" or, when a player caused it,
// "player_explosion", positioned at the explosion centre.
func ExplosionDamage(at api.Vec3, direct, causing api.Entity) DamageSource {
	direct, causing = orNil(direct), orNil(causing)
	t := "minecraft:explosion"
	if _, ok := causing.(*Player); ok {
		t = "minecraft:player_explosion"
	}
	return DamageSource{Type: t, Direct: direct, Causing: causing, Pos: &at}
}

func (s *DamageSource) isDirect() bool {
	return s.Direct != nil && s.Direct == s.Causing
}

// sourcePos is DamageSource.getSourcePosition.
func (s *DamageSource) sourcePos() (api.Vec3, bool) {
	if s.Pos != nil {
		return *s.Pos, true
	}
	if s.Direct != nil {
		return s.Direct.Location().Pos, true
	}
	return api.Vec3{}, false
}

func (s *DamageSource) attacker() combatant {
	c, _ := s.Causing.(combatant)
	return c
}

// apiCause maps a damage type to the plugin API cause.
func apiCause(t string) api.DamageCause {
	switch t {
	case "minecraft:player_attack", "minecraft:mob_attack", "minecraft:mob_attack_no_aggro", "minecraft:sting":
		return api.DamageEntityAttack
	case "minecraft:arrow", "minecraft:trident", "minecraft:thrown", "minecraft:mob_projectile", "minecraft:fireball", "minecraft:wind_charge":
		return api.DamageProjectile
	case "minecraft:fall", "minecraft:ender_pearl", "minecraft:stalagmite":
		return api.DamageFall
	case "minecraft:in_fire", "minecraft:campfire":
		return api.DamageFire
	case "minecraft:on_fire":
		return api.DamageFireTick
	case "minecraft:lava":
		return api.DamageLava
	case "minecraft:drown":
		return api.DamageDrowning
	case "minecraft:in_wall":
		return api.DamageSuffocation
	case "minecraft:out_of_world":
		return api.DamageVoid
	case "minecraft:starve":
		return api.DamageStarvation
	case "minecraft:magic", "minecraft:indirect_magic":
		return api.DamageMagic
	case "minecraft:wither":
		return api.DamageWither
	case "minecraft:explosion", "minecraft:player_explosion":
		return api.DamageExplosion
	case "minecraft:cactus", "minecraft:sweet_berry_bush":
		return api.DamageContact
	case "minecraft:lightning_bolt":
		return api.DamageLightning
	case "minecraft:freeze":
		return api.DamageFreeze
	case "minecraft:generic_kill":
		return api.DamageKill
	case "minecraft:thorns":
		return api.DamageThorns
	case "minecraft:cramming":
		return api.DamageCramming
	case "minecraft:fly_into_wall":
		return api.DamageFlyIntoWall
	case "minecraft:hot_floor":
		return api.DamageHotFloor
	case "minecraft:sonic_boom":
		return api.DamageSonicBoom
	case "minecraft:outside_border":
		return api.DamageWorldBorder
	}
	return api.DamageCustom
}

func (s DamageSource) api() api.DamageSource {
	return api.DamageSource{Type: api.Key(s.Type), Cause: apiCause(s.Type), Direct: s.Direct, Causing: s.Causing, Position: s.Pos}
}

func fromAPI(s api.DamageSource) DamageSource {
	t := string(s.Type)
	if t == "" {
		t = "minecraft:generic"
	}
	return DamageSource{Type: ns(t), Direct: s.Direct, Causing: s.Causing, Pos: s.Position}
}

// living is the combat state of a living entity (or of an item/orb that
// can be destroyed by damage). Guarded by mu; lock order is mu before the
// owner's own lock.
type living struct {
	mu        sync.Mutex
	attrs     *entity.Attrs
	health    float32 // mobs; players keep Player.health
	absorb    float32
	cooldown  int32 // damageCooldownTime: >10 means hurt-immune
	hurtTime  int32
	lastHurt  float32
	deathTime int32
	dead      bool
	fire      int32 // remaining fire ticks
	air       int32
	equip     [numSlots]item.Stack // mobs only
	applied   [numSlots]item.Stack // equipment whose modifiers are in attrs
	fx        map[int32]int32      // active effect id -> amplifier
	fxLeft    map[int32]int32      // mobs: remaining ticks (-1 infinite)
	killer    *Player
	lastBy    api.Entity
	lastByAt  int64
	ticker    int32 // players: attack strength ticker
	shieldOff int64 // tick until which blocking is disabled
	motion    bool  // velocity changed by combat; sync to viewers
	block     shieldState
	synced    bool // full attribute set sent
	shownFire bool
	shownAir  int32
}

func (l *living) init(typeName string) {
	l.attrs = entity.NewAttrs(typeName)
	l.air, l.shownAir = 300, 300
	l.fx, l.fxLeft = map[int32]int32{}, map[int32]int32{}
	if l.attrs != nil {
		l.health = float32(l.attrs.Value(entity.AttrMaxHealth))
	}
	if typeName == "item" || typeName == "experience_orb" {
		l.health = 5
	}
}

func (l *living) ignite(ticks int32) {
	l.mu.Lock()
	if l.attrs != nil {
		ticks = int32(float64(ticks) * l.attrs.Value(entity.AttrBurningTime))
	}
	l.fire = max(l.fire, ticks)
	l.mu.Unlock()
}

func (l *living) effect(name string) (int32, bool) {
	id, ok := item.BuiltinID("mob_effect", name)
	if !ok {
		return 0, false
	}
	amp, ok := l.fx[id]
	return amp, ok
}

// combatant is implemented by *Mob and *Player.
type combatant interface {
	api.Entity
	lv() *living
	isPlayer() bool
	typeName() string
	curHealth() float32
	setCurHealth(h float32)
	gear() [numSlots]item.Stack
	damageEquipment(slot int, amount int32)
	setEquipment(slot int, st item.Stack)
	feet() api.Vec3
	vel() api.Vec3
	setVel(v api.Vec3)
	grounded() bool
	eyeY() float64
	wld() *World
}

func (p *Player) lv() *living      { return &p.liv }
func (p *Player) isPlayer() bool   { return true }
func (p *Player) typeName() string { return "minecraft:player" }
func (p *Player) wld() *World      { return p.world.Load() }
func (p *Player) eyeY() float64    { return 1.62 }
func (p *Player) feet() api.Vec3   { return p.Location().Pos }
func (p *Player) vel() api.Vec3    { return p.Velocity() }
func (p *Player) grounded() bool   { return p.OnGround() }

func (p *Player) curHealth() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return float32(p.health)
}

func (p *Player) setCurHealth(h float32) {
	p.mu.Lock()
	p.health = float64(min(max(h, 0), float32(p.maxHealthAttr())))
	p.mu.Unlock()
}

func (p *Player) maxHealthAttr() float64 {
	if a := p.liv.attrs; a != nil {
		return a.Value(entity.AttrMaxHealth)
	}
	return 20
}

func (p *Player) equipment7() [numSlots]item.Stack {
	e := p.equipmentSlots()
	return [numSlots]item.Stack{e[0], e[1], e[2], e[3], e[4], e[5], {}}
}

func (p *Player) gear() [numSlots]item.Stack { return p.equipment7() }

// equipmentSlots returns main hand, off hand, feet, legs, chest, head.
func (p *Player) equipmentSlots() [6]item.Stack {
	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	return [6]item.Stack{s.get(s.selected()), s.get(invOffhand), s.get(36), s.get(37), s.get(38), s.get(39)}
}

func playerSlotIndex(p *Player, slot int) int {
	switch slot {
	case slotMain:
		return p.inv.selected()
	case slotOff:
		return invOffhand
	case slotFeet:
		return 36
	case slotLegs:
		return 37
	case slotChest:
		return 38
	case slotHead:
		return 39
	}
	return -1
}

func (p *Player) setEquipment(slot int, st item.Stack) {
	i := playerSlotIndex(p, slot)
	if i < 0 {
		return
	}
	p.inv.mu.Lock()
	p.inv.set(i, st)
	p.inv.mu.Unlock()
}

func (p *Player) damageEquipment(slot int, amount int32) {
	if gm := p.GameMode(); gm == api.Creative || gm == api.Spectator {
		return
	}
	i := playerSlotIndex(p, slot)
	if i < 0 {
		return
	}
	p.inv.mu.Lock()
	st := p.inv.get(i)
	p.inv.set(i, damageStack(st, amount))
	p.inv.mu.Unlock()
}

func (p *Player) setVel(v api.Vec3) {
	p.mu.Lock()
	p.e.Vel = v
	p.mu.Unlock()
	p.send(&packets.ClientboundSetEntityMotion{EntityID: p.e.ID, Velocity: proto.LpVec3{X: v.X, Y: v.Y, Z: v.Z}})
}

// damageStack applies durability damage with unbreaking (item_damage).
func damageStack(st item.Stack, amount int32) item.Stack {
	if st.IsEmpty() || !st.IsDamageable() || amount <= 0 {
		return st
	}
	n := amount
	for _, e := range st.Enchantments() {
		if d := combatEnch(e.ID); d != nil {
			c := enchCtx{lvl: e.Level}
			n = int32(c.conditionalValue(d, "minecraft:item_damage", float32(n)))
		}
	}
	if n <= 0 {
		return st
	}
	d := st.Damage() + n
	if d >= st.MaxDamage() {
		return item.Stack{}
	}
	return st.WithDamage(d)
}

func (m *Mob) lv() *living      { return &m.liv }
func (m *Mob) isPlayer() bool   { return false }
func (m *Mob) typeName() string { return "minecraft:" + m.typ.Name }
func (m *Mob) wld() *World      { return m.w }
func (m *Mob) feet() api.Vec3   { return m.Location().Pos }
func (m *Mob) vel() api.Vec3    { return m.Velocity() }
func (m *Mob) grounded() bool   { return m.OnGround() }

func (m *Mob) eyeY() float64 {
	if p, ok := entity.PhysicsOf(m.typ.Name); ok {
		return p.Eye
	}
	return float64(m.typ.Height) * 0.85
}

func (m *Mob) curHealth() float32 { return m.liv.health }

func (m *Mob) setCurHealth(h float32) {
	mx := float32(20)
	if m.liv.attrs != nil {
		mx = float32(m.liv.attrs.Value(entity.AttrMaxHealth))
	}
	m.liv.health = min(max(h, 0), mx)
	m.healthMeta()
}

func (m *Mob) gear() [numSlots]item.Stack { return m.liv.equip }

func (m *Mob) setEquipment(slot int, st item.Stack) {
	if slot >= 0 && slot < numSlots {
		m.liv.equip[slot] = st.Norm()
	}
}

func (m *Mob) damageEquipment(slot int, amount int32) {
	if slot >= 0 && slot < numSlots {
		m.liv.equip[slot] = damageStack(m.liv.equip[slot], amount)
	}
}

func (m *Mob) setVel(v api.Vec3) {
	m.mu.Lock()
	m.e.Vel = v
	m.mu.Unlock()
	m.liv.motion = true
}

// Hurt applies src to the mob through the vanilla damage pipeline and
// reports whether it was hurt.
func (m *Mob) Hurt(src DamageSource, amount float32) bool { return hurt(m, src, amount) }

// Hurt applies src to the player through the vanilla damage pipeline.
func (p *Player) Hurt(src DamageSource, amount float32) bool { return hurt(p, src, amount) }

func scaleForDifficulty(dt *damageType, src *DamageSource, diff int32, amount float32) float32 {
	scales := false
	switch dt.scaling {
	case "always":
		scales = true
	case "when_caused_by_living_non_player":
		if c := src.attacker(); c != nil && !c.isPlayer() {
			_, isMob := c.(*Mob)
			scales = isMob && entity.HasAttributes(c.typeName())
		}
	}
	if !scales {
		return amount
	}
	switch diff {
	case item.Peaceful:
		return 0
	case item.Easy:
		return min(amount/2+1, amount)
	case item.Hard:
		return amount * 3 / 2
	}
	return amount
}

// armorFraction is the share of damage armor absorbs (minecraft.wiki
// Armor): min(20, max(armor/5, armor − 4·damage/(toughness+8))) / 25.
func armorFraction(amount, armor, toughness float32) float32 {
	return min(20, max(armor/5, armor-4*amount/(toughness+8))) / 25
}

// hurt is LivingEntity.hurtServer.
func hurt(t combatant, src DamageSource, amount float32) bool {
	if !t.IsValid() || amount != amount {
		return false
	}
	w := t.wld()
	if w == nil {
		return false
	}
	dt := damageTypeOf(src.Type)
	src.Type = dt.key
	src.Direct, src.Causing = orNil(src.Direct), orNil(src.Causing)
	l := t.lv()
	if l.attrs == nil {
		return hurtNonLiving(t, src, amount)
	}
	if p, ok := t.(*Player); ok {
		if gm := p.GameMode(); (gm == api.Creative || gm == api.Spectator) && !dt.is("minecraft:bypasses_invulnerability") {
			return false
		}
		if !p.joined.Load() {
			return false
		}
		if src.attacker() != nil && src.attacker().isPlayer() && !w.pvp() && src.Causing != t {
			return false
		}
	} else if m, ok := t.(*Mob); ok && m.Invulnerable() && !dt.is("minecraft:bypasses_invulnerability") {
		return false
	}
	if w.s.bus.HasHandlers(api.KindEntityDamage) {
		ev := &api.EntityDamageEvent{Entity: t, Source: src.api(), Damage: float64(amount)}
		if w.s.bus.Dispatch(ev).(*api.EntityDamageEvent).Cancelled() {
			return false
		}
		amount = float32(ev.Damage)
	}
	l.mu.Lock()
	if l.dead || t.curHealth() <= 0 {
		l.mu.Unlock()
		return false
	}
	if _, ok := l.effect("fire_resistance"); ok && dt.is("minecraft:is_fire") {
		l.mu.Unlock()
		return false
	}
	if t.isPlayer() {
		amount = scaleForDifficulty(dt, &src, int32(w.Difficulty()), amount)
	}
	if amount <= 0 && !t.isPlayer() {
		// still counts for mobs; players with scaled 0 are not hurt
	} else if amount <= 0 {
		l.mu.Unlock()
		return false
	}
	blocked, shieldHit := float32(0), false
	if amount > 0 {
		blocked, shieldHit = blockWithShield(t, &src, dt, amount)
		amount -= blocked
	}
	fullyBlocked := blocked > 0 && amount <= 0
	if fullyBlocked {
		l.mu.Unlock()
		afterShield(t, &src, blocked, shieldHit)
		return false
	}
	fresh := true
	if l.cooldown > 10 && !dt.is("minecraft:bypasses_cooldown") {
		if amount <= l.lastHurt {
			l.mu.Unlock()
			return false
		}
		actuallyHurt(t, &src, dt, amount-l.lastHurt)
		l.lastHurt = amount
		fresh = false
	} else {
		l.lastHurt = amount
		l.cooldown = 20
		actuallyHurt(t, &src, dt, amount)
		l.hurtTime = 10
	}
	if c, ok := src.Causing.(*Player); ok {
		l.killer = c
	}
	if src.Causing != nil {
		l.lastBy, l.lastByAt = src.Causing, w.s.CurrentTick()
	}
	dead := t.curHealth() <= 0
	l.mu.Unlock()

	if fresh {
		w.broadcastNear(t.feet(), damageEvent(t, &src, dt))
		if !dt.is("minecraft:no_knockback") {
			if pos, ok := src.sourcePos(); ok {
				f := t.feet()
				dx, dz := pos.X-f.X, pos.Z-f.Z
				for dx*dx+dz*dz < 1e-4 {
					dx, dz = (rand.Float64()-rand.Float64())*0.01, (rand.Float64()-rand.Float64())*0.01
				}
				knockback(t, 0.4, dx, dz)
			}
		}
	}
	if blocked > 0 {
		afterShield(t, &src, blocked, shieldHit)
	}
	if dead {
		if !tryTotem(t, dt) {
			die(t, src)
		}
	}
	if p, ok := t.(*Player); ok {
		p.sendHealthNow()
	}
	return true
}

func damageEvent(t combatant, src *DamageSource, dt *damageType) *packets.ClientboundDamageEvent {
	pk := &packets.ClientboundDamageEvent{EntityID: t.EntityID(), SourceTypeID: dt.id}
	if src.Causing != nil {
		pk.SourceCauseID = src.Causing.EntityID() + 1
	}
	if src.Direct != nil {
		pk.SourceDirectID = src.Direct.EntityID() + 1
	}
	if src.Pos != nil && src.Direct == nil {
		pk.SourcePosition = proto.Some(packets.Vec3f64{X: src.Pos.X, Y: src.Pos.Y, Z: src.Pos.Z})
	}
	return pk
}

// actuallyHurt applies armor, resistance, enchantment protection and
// absorption, then removes health. Called with l.mu held.
func actuallyHurt(t combatant, src *DamageSource, dt *damageType, amount float32) {
	l := t.lv()
	if !dt.is("minecraft:bypasses_armor") {
		if t.isPlayer() {
			hurtArmor(t, dt, amount)
		}
		f := armorFraction(amount, float32(l.attrs.Value(entity.AttrArmor)), float32(l.attrs.Value(entity.AttrArmorToughness)))
		if a := src.attacker(); a != nil && src.isDirect() {
			// Breach (armor_effectiveness) shifts the absorbed share.
			f = weaponValue(a.gear()[slotMain], "minecraft:armor_effectiveness", a, t, src, f)
		}
		amount *= 1 - min(max(f, 0), 1)
	}
	if !dt.is("minecraft:bypasses_effects") && !dt.is("minecraft:bypasses_resistance") {
		if amp, ok := l.effect("resistance"); ok {
			amount = max(amount*float32(25-(amp+1)*5)/25, 0)
		}
	}
	if amount <= 0 {
		return
	}
	if !dt.is("minecraft:bypasses_enchantments") {
		epf := enchantDamageProtection(t, src, dt)
		if epf > 0 {
			amount *= 1 - min(max(epf, 0), 20)/25
		}
	}
	if amount <= 0 {
		return
	}
	absorbed := min(l.absorb, amount)
	l.absorb -= absorbed
	amount -= absorbed
	if absorbed > 0 {
		if p, ok := t.(*Player); ok {
			p.absorbMeta(l.absorb)
		}
	}
	if amount == 0 {
		return
	}
	t.setCurHealth(t.curHealth() - amount)
	if p, ok := t.(*Player); ok {
		p.exhaustSafe(dt.exhaustion)
	}
}

// hurtArmor is LivingEntity.hurtArmor: every damageable armor piece loses
// max(1, floor(amount/4)) durability.
func hurtArmor(t combatant, dt *damageType, amount float32) {
	if amount <= 0 {
		return
	}
	n := int32(max(1, math.Floor(float64(amount/4))))
	eq := t.gear()
	for slot := slotFeet; slot <= slotHead; slot++ {
		st := eq[slot]
		if st.IsEmpty() || !st.IsDamageable() {
			continue
		}
		if dt.is("minecraft:is_fire") && st.Has(item.DamageResistant) {
			continue
		}
		if e, ok := st.Equippable(); !ok || int(e.Slot) != equipSlotItem(slot) {
			continue
		}
		t.damageEquipment(slot, n)
	}
}

// equipSlotItem maps our slot index to item.Slot* ids.
func equipSlotItem(slot int) int {
	switch slot {
	case slotMain:
		return int(item.SlotMainHand)
	case slotOff:
		return int(item.SlotOffHand)
	case slotFeet:
		return int(item.SlotFeet)
	case slotLegs:
		return int(item.SlotLegs)
	case slotChest:
		return int(item.SlotChest)
	case slotHead:
		return int(item.SlotHead)
	case slotBody:
		return int(item.SlotBody)
	}
	return -1
}

// knockback is LivingEntity.knockback: strength scaled by knockback
// resistance, half the old velocity kept, and on the ground a vertical
// kick capped at 0.4.
func knockback(t combatant, strength, dx, dz float64) {
	l := t.lv()
	l.mu.Lock()
	res := 0.0
	if l.attrs != nil {
		res = l.attrs.Value(entity.AttrKnockbackResistance)
	}
	l.mu.Unlock()
	strength *= 1 - res
	if strength <= 0 {
		return
	}
	n := math.Sqrt(dx*dx + dz*dz)
	ox, oz := dx/n*strength, dz/n*strength
	v := t.vel()
	y := v.Y
	if t.grounded() {
		y = min(0.4, v.Y/2+strength)
	}
	t.setVel(api.Vec3{X: v.X/2 - ox, Y: y, Z: v.Z/2 - oz})
}

// hurtNonLiving handles damage to items and experience orbs (health 5).
func hurtNonLiving(t combatant, src DamageSource, amount float32) bool {
	m, ok := t.(*Mob)
	if !ok {
		return false
	}
	dt := damageTypeOf(src.Type)
	if dt.is("minecraft:bypasses_invulnerability") {
		m.Remove()
		return true
	}
	if m.itm != nil {
		return false // item entities take damage in itement_entity.go
	}
	if m.typ.Name != "experience_orb" {
		return false
	}
	l := &m.liv
	l.mu.Lock()
	l.health -= amount
	dead := l.health <= 0
	l.mu.Unlock()
	if dead {
		m.Remove()
	}
	return true
}

func (w *World) pvp() bool { return w.ruleBool("pvp", "", true) }
