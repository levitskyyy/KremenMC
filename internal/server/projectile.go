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
)

// Arrow pickup rules (AbstractArrow.Pickup).
const (
	pickupDisallowed = iota
	pickupAllowed
	pickupCreative
)

// Synched data of arrows (from the reference accessors).
const (
	metaArrowFlags    = 8
	metaArrowInGround = 10
	arrowFlagCrit     = 0x01
	serBool           = 8
)

// projectile is the state of an arrow, trident or thrown item entity.
type projectile struct {
	owner    api.Entity
	baseDmg  float64
	crit     bool
	pickup   int
	weapon   item.Stack // the bow that fired it
	stack    item.Stack // picked-up / thrown item
	life     int        // ticks in ground
	dealt    bool       // trident hit something
	left     bool       // has left the owner's box
	fire     bool
	inGround bool
}

const arrowDespawn = 1200

// spawnProjectile creates a projectile entity of kind moving along
// (yaw, pitch) at speed with vanilla inaccuracy (each axis offset by a
// triangle distribution of width 0.0172275 × inaccuracy).
func (w *World) spawnProjectile(kind string, owner api.Entity, at api.Vec3, yaw, pitch float32, speed, inaccuracy float64, pr *projectile) *Mob {
	t, ok := data.Canonical().EntityByName(kind)
	if !ok {
		return nil
	}
	d := lookDir(yaw, pitch)
	n := math.Sqrt(d.X*d.X + d.Y*d.Y + d.Z*d.Z)
	sp := 0.0172275 * inaccuracy
	tri := func() float64 { return sp * (rand.Float64() - rand.Float64()) }
	v := api.Vec3{X: (d.X/n + tri()) * speed, Y: (d.Y/n + tri()) * speed, Z: (d.Z/n + tri()) * speed}
	hy := math.Sqrt(v.X*v.X + v.Z*v.Z)
	loc := api.Location{World: w.name, Pos: at,
		Yaw:   float32(math.Atan2(v.X, v.Z) * 180 / math.Pi),
		Pitch: float32(math.Atan2(v.Y, hy) * 180 / math.Pi)}
	m := newMob(w, t, loc)
	m.e.Vel = v
	m.proj = pr
	pr.owner = owner
	m.trk.Vel = v
	if owner != nil {
		m.trk.Data = owner.EntityID()
	}
	switch m.e.Kind {
	case entity.KindThrown:
		if !pr.stack.IsEmpty() {
			m.trk.SetMeta(itemMeta(pr.stack))
		}
	case entity.KindArrow:
		if pr.crit {
			m.trk.SetMeta(entity.EndMeta(entity.AppendMetaByte(nil, metaArrowFlags, arrowFlagCrit)))
		}
	}
	if w.s.bus.HasHandlers(api.KindEntitySpawn) {
		ev := &api.EntitySpawnEvent{Entity: m, Location: loc, Reason: api.SpawnPlugin}
		if w.s.bus.Dispatch(ev).(*api.EntitySpawnEvent).Cancelled() {
			return nil
		}
	}
	w.mobsMu.Lock()
	w.mobs[m.e.ID] = m
	w.mobsMu.Unlock()
	return m
}

// hitBoxes finds the nearest living entity whose box, inflated by 0.3,
// the segment crosses; the owner is skipped until the projectile left it.
func (m *Mob) hitEntities(from, to api.Vec3) (any, api.Vec3, bool) {
	var best combatant
	var at api.Vec3
	bd := math.MaxFloat64
	for _, c := range m.w.combatants() {
		if !m.proj.left && api.Entity(c) == m.proj.owner {
			continue
		}
		mn, mx := c.BoundingBox()
		b := entity.AABB{MinX: mn.X - 0.3, MinY: mn.Y - 0.3, MinZ: mn.Z - 0.3, MaxX: mx.X + 0.3, MaxY: mx.Y + 0.3, MaxZ: mx.Z + 0.3}
		if p, ok := entity.ClipSegment(b, from, to); ok {
			if d := p.DistanceSq(from); d < bd {
				best, at, bd = c, p, d
			}
		}
	}
	if best == nil {
		return nil, api.Vec3{}, false
	}
	return best, at, true
}

// leftOwner reports whether the projectile box no longer touches the
// owner's box inflated by 1 (Projectile.checkLeftOwner).
func (m *Mob) leftOwner() bool {
	o := m.proj.owner
	if o == nil || !o.IsValid() {
		return true
	}
	mn, mx := o.BoundingBox()
	ob := entity.AABB{MinX: mn.X - 1, MinY: mn.Y - 1, MinZ: mn.Z - 1, MaxX: mx.X + 1, MaxY: mx.Y + 1, MaxZ: mx.Z + 1}
	m.mu.Lock()
	b := m.e.Box()
	m.mu.Unlock()
	return !b.Intersects(ob)
}

func (m *Mob) tickProjectile() {
	pr := m.proj
	w := m.w
	if !pr.left && m.leftOwner() {
		pr.left = true
	}
	m.mu.Lock()
	e := *m.e
	m.mu.Unlock()
	h := e.TickProjectile(w.w, m.hitEntities)
	m.mu.Lock()
	m.e.Prev, m.e.Pos, m.e.Vel, m.e.Age = e.Prev, e.Pos, e.Vel, e.Age
	m.e.InWater, m.e.InLava, m.e.InGround = e.InWater, e.InLava, e.InGround
	if v := e.Vel; v.X != 0 || v.Z != 0 || v.Y != 0 {
		hy := math.Sqrt(v.X*v.X + v.Z*v.Z)
		m.e.Yaw = float32(math.Atan2(v.X, v.Z) * 180 / math.Pi)
		m.e.Pitch = float32(math.Atan2(v.Y, hy) * 180 / math.Pi)
	}
	m.mu.Unlock()
	switch m.e.Kind {
	case entity.KindThrown:
		if h.Kind != entity.HitNone {
			m.thrownHit(h)
		} else if m.ticks > 1200 || e.Pos.Y < float64(w.MinY()-64) {
			m.Remove()
		}
	case entity.KindArrow:
		m.arrowTick(h)
	}
}

// thrownHit is the onHit of snowballs, eggs and ender pearls.
func (m *Mob) thrownHit(h entity.Hit) {
	pr := m.proj
	w := m.w
	if t, ok := h.Target.(combatant); ok && h.Kind == entity.HitEntity {
		dmg := float32(0)
		if m.typ.Name == "snowball" && t.typeName() == "minecraft:blaze" {
			dmg = 3
		}
		hurt(t, ProjectileDamage("minecraft:thrown", m, pr.owner), dmg)
	}
	switch m.typ.Name {
	case "egg":
		if rand.IntN(8) == 0 {
			n := 1
			if rand.IntN(32) == 0 {
				n = 4
			}
			for range n {
				loc := api.Location{World: w.name, Pos: h.Pos, Yaw: m.Location().Yaw}
				if c, err := w.SpawnEntity("minecraft:chicken", loc, nil); err == nil {
					c.(*Mob).setBaby()
				}
			}
		}
	case "ender_pearl":
		if p, ok := pr.owner.(*Player); ok && p.IsValid() && !p.IsDead() && p.world.Load() == w {
			if rand.Float32() < 0.05 {
				w.SpawnEntity("minecraft:endermite", api.Location{World: w.name, Pos: p.Location().Pos}, nil)
			}
			l := p.Location()
			l.Pos = h.Pos
			if p.Teleport(l, api.TeleportEnderPearl) {
				p.mu.Lock()
				p.e.FallDistance = 0
				p.mu.Unlock()
				hurt(p, ProjectileDamage("minecraft:ender_pearl", m, p), 5)
			}
		}
	}
	m.Remove()
}

// arrowTick handles arrow and trident hits, ground life and pickup.
func (m *Mob) arrowTick(h entity.Hit) {
	pr := m.proj
	w := m.w
	switch h.Kind {
	case entity.HitEntity:
		m.arrowHitEntity(h)
	case entity.HitBlock:
		pr.inGround = true
		pr.crit = false
		m.trk.SetMeta(entity.EndMeta(append(entity.AppendMetaByte(nil, metaArrowFlags, 0), metaArrowInGround, serBool, 1)))
		snd := "minecraft:entity.arrow.hit"
		if m.typ.Name == "trident" {
			snd = "minecraft:item.trident.hit_ground"
		}
		w.playSoundAt(h.Pos, snd, soundNeutral, 1, 1.2/(rand.Float32()*0.2+0.9))
	}
	if pr.inGround {
		pr.life++
		if pr.life >= arrowDespawn && m.typ.Name != "trident" {
			m.Remove()
			return
		}
		m.tryPickup()
	} else if m.ticks > 1200 || m.Location().Pos.Y < float64(w.MinY()-64) {
		m.Remove()
	}
	if pr.dealt && m.typ.Name == "trident" {
		m.tryPickup()
	}
}

func (m *Mob) arrowHitEntity(h entity.Hit) {
	pr := m.proj
	t := h.Target.(combatant)
	v := m.Velocity()
	speed := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	var src DamageSource
	var dmg float32
	if m.typ.Name == "trident" {
		if pr.dealt {
			return
		}
		src = ProjectileDamage("minecraft:trident", m, pr.owner)
		dmg = weaponValue(pr.stack, "minecraft:damage", ownerCombatant(pr.owner), t, &src, 8)
		pr.dealt = true
	} else {
		src = ProjectileDamage("minecraft:arrow", m, pr.owner)
		base := float64(weaponValue(pr.weapon, "minecraft:damage", ownerCombatant(pr.owner), t, &src, float32(pr.baseDmg)))
		n := int(math.Ceil(min(max(speed*base, 0), math.MaxInt32)))
		if pr.crit {
			n += rand.IntN(n/2 + 2)
		}
		dmg = float32(n)
	}
	if m.fireTicks() > 0 || pr.fire {
		t.lv().ignite(100)
	}
	if hurt(t, src, dmg) {
		if kb := weaponValue(pr.weapon, "minecraft:knockback", ownerCombatant(pr.owner), t, &src, 0); kb > 0 && m.typ.Name != "trident" {
			hl := math.Hypot(v.X, v.Z)
			if hl > 1e-7 {
				f := float64(kb) * 0.6
				l := t.lv()
				l.mu.Lock()
				res := l.attrs.Value(entity.AttrKnockbackResistance)
				l.mu.Unlock()
				f *= max(0, 1-res)
				tv := t.vel()
				t.setVel(api.Vec3{X: tv.X + v.X/hl*f, Y: tv.Y + 0.1, Z: tv.Z + v.Z/hl*f})
			}
		}
		if a := ownerCombatant(pr.owner); a != nil {
			postAttack(a.gear(), "attacker", a, t, &src)
		}
		postAttack(t.gear(), "victim", ownerCombatant(pr.owner), t, &src)
		m.w.playSoundAt(m.Location().Pos, "minecraft:entity.arrow.hit", soundNeutral, 1, 1.2/(rand.Float32()*0.2+0.9))
		if m.typ.Name == "trident" {
			m.SetVelocity(api.Vec3{X: v.X * -0.01, Y: v.Y * -0.1, Z: v.Z * -0.01})
			return
		}
		m.Remove()
		return
	}
	// Deflected (immune target): bounce back slowly.
	m.SetVelocity(api.Vec3{X: v.X * -0.1, Y: v.Y * -0.1, Z: v.Z * -0.1})
}

func ownerCombatant(e api.Entity) combatant {
	c, _ := e.(combatant)
	return c
}

func (m *Mob) fireTicks() int32 {
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	return m.liv.fire
}

// tryPickup gives a stuck arrow or a landed trident to a touching player.
func (m *Mob) tryPickup() {
	pr := m.proj
	if pr.pickup == pickupDisallowed {
		return
	}
	mn, mx := m.BoundingBox()
	box := entity.AABB{MinX: mn.X, MinY: mn.Y, MinZ: mn.Z, MaxX: mx.X, MaxY: mx.Y, MaxZ: mx.Z}
	for _, p := range m.w.playerList() {
		if p.IsDead() || p.GameMode() == api.Spectator {
			continue
		}
		pmn, pmx := p.BoundingBox()
		pb := entity.AABB{MinX: pmn.X - 1, MinY: pmn.Y - 0.5, MinZ: pmn.Z - 1, MaxX: pmx.X + 1, MaxY: pmx.Y + 0.5, MaxZ: pmx.Z + 1}
		if !pb.Intersects(box) {
			continue
		}
		if m.typ.Name == "trident" && pr.owner != nil && api.Entity(p) != pr.owner {
			continue
		}
		creative := p.GameMode() == api.Creative
		if pr.pickup == pickupCreative && !creative {
			continue
		}
		if pr.pickup == pickupAllowed && !creative {
			if left := p.Inventory().AddItem(stackToAPI(pr.stack)); len(left) > 0 && left[0].Count > 0 {
				continue
			}
		}
		m.w.broadcastNear(m.Location().Pos, &packets.ClientboundTakeItemEntity{CollectedEntityID: m.e.ID, CollectorEntityID: p.e.ID, PickupItemCount: 1})
		m.Remove()
		return
	}
}

func (m *Mob) setBaby() {
	// AgeableMob age -24000; metadata index 16 (boolean baby) for ageable mobs.
	m.trk.SetMeta(entity.EndMeta([]byte{16, serBool, 1}))
}

func (w *World) playSoundAt(pos api.Vec3, name string, category int32, volume, pitch float32) {
	id := soundID(name)
	if id < 0 {
		return
	}
	w.broadcastNear(pos, &packets.ClientboundSound{
		Sound: proto.Holder[packets.ItemSoundEvent]{ID: id}, SoundCategory: category,
		X: int32(pos.X * 8), Y: int32(pos.Y * 8), Z: int32(pos.Z * 8),
		Volume: volume, Pitch: pitch, Seed: rand.Int64(),
	})
}
