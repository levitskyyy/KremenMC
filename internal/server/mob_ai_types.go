package server

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity/ai"
)

// Goal lists per mob type. Priorities and flags are the ones observed on
// the reference (kremenmc.probe.ai.Observe goal dump); speed modifiers
// of 1.0 for strolling and chasing are measured, the others come from
// minecraft.wiki or are estimates (docs/vanilla-parity.md).

const (
	mv = ai.FlagMove
	lk = ai.FlagLook
	jp = ai.FlagJump
	tg = ai.FlagTarget
)

// strollInterval is the mean number of ticks between idle strolls,
// measured on the reference (27 strolls of 8 mobs over 2400 ticks with a
// player in range).
const strollInterval = 600

func isPlayer(e *aiEnt) bool { return e.isPlayer() }

func typeIs(names ...string) func(*aiEnt) bool {
	return func(e *aiEnt) bool {
		for _, n := range names {
			if e.typ == n {
				return true
			}
		}
		return false
	}
}

var zombieTypes = map[string]bool{"zombie": true, "husk": true, "drowned": true, "zombie_villager": true}
var skeletonTypes = map[string]bool{"skeleton": true, "stray": true, "bogged": true, "wither_skeleton": true}
var flyingTypes = map[string]bool{"bat": true, "bee": true, "allay": true, "parrot": true, "vex": true, "ghast": true, "happy_ghast": true, "phantom": true, "blaze": true}
var swimTypes = map[string]bool{"cod": true, "salmon": true, "tropical_fish": true, "pufferfish": true, "squid": true, "glow_squid": true, "dolphin": true, "tadpole": true}

func setupGoals(b *mobBrain) bool {
	m := b.m
	g, t := &b.goals, &b.targets
	gb := goalBase{b: b}
	name := m.typ.Name
	passive := func(panic, breed, tempt, follow float64, base int) {
		g.Add(0, jp, &floatGoal{gb})
		g.Add(1, mv, &panicGoal{gb, panic})
		g.Add(base, mv|lk, &breedGoal{goalBase: gb, speed: breed})
		g.Add(base+1, mv|lk, &temptGoal{goalBase: gb, speed: tempt})
		g.Add(base+2, 0, &followParentGoal{goalBase: gb, speed: follow})
	}
	idle := func(stroll, look int, rng float64) {
		g.Add(stroll, mv, &strollGoal{gb, 1, strollInterval, true})
		g.Add(look, lk, &lookAtGoal{goalBase: gb, rng: rng})
		g.Add(look+1, mv|lk, &randomLookGoal{goalBase: gb})
	}
	switch {
	case name == "cow" || name == "mooshroom" || name == "chicken":
		passive(2, 1, 1.25, 1.25, 2)
		idle(5, 6, 6)
	case name == "sheep":
		passive(1.25, 1, 1.1, 1.1, 2)
		g.Add(5, mv|lk|jp, &eatGrassGoal{goalBase: gb})
		idle(6, 7, 6)
	case name == "pig":
		passive(1.25, 1, 1.2, 1.1, 3)
		idle(6, 7, 6)
	case name == "rabbit":
		passive(2.2, 0.8, 1, 1.1, 2)
		g.Add(4, mv, &avoidGoal{goalBase: gb, match: func(e *aiEnt) bool { return e.isPlayer() && e.alive || e.typ == "wolf" || e.monster }, dist: 8, walk: 2.2, sprint: 2.2})
		g.Add(6, mv, &strollGoal{gb, 0.6, strollInterval, true})
		g.Add(11, lk, &lookAtGoal{goalBase: gb, rng: 10})
	case zombieTypes[name]:
		b.burnsInSun = name != "husk" && name != "drowned"
		if name == "drowned" {
			mal := ai.DefaultMalus
			mal[ai.PathWater] = 0
			b.nav.Malus = &mal
		}
		zm := &meleeGoal{goalBase: gb, speed: 1, breakDoors: name != "drowned"}
		if zm.breakDoors {
			mal := *b.nav.Malus
			mal[ai.PathDoorWoodClosed] = 0
			b.nav.Malus = &mal
		}
		g.Add(3, mv|lk, zm)
		idle(7, 8, 8)
		t.Add(1, tg, &hurtByGoal{goalBase: gb})
		t.Add(2, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("villager", "wandering_trader", "iron_golem"), mustSee: true, interval: 10})
		t.Add(5, tg, &nearestTargetGoal{goalBase: gb, match: func(e *aiEnt) bool { return e.typ == "turtle" && e.baby }, mustSee: true, interval: 10})
	case skeletonTypes[name]:
		b.burnsInSun = name != "wither_skeleton"
		g.Add(3, mv, &avoidGoal{goalBase: gb, match: typeIs("wolf"), dist: 6, walk: 1, sprint: 1.2})
		g.Add(4, mv|lk, &bowGoal{goalBase: gb, speed: 1, radius: 15, interval: 40})
		g.Add(4, mv|lk, &meleeGoal{goalBase: gb, speed: 1.2})
		idle(5, 6, 8)
		t.Add(1, tg, &hurtByGoal{goalBase: gb})
		t.Add(2, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("iron_golem"), mustSee: true, interval: 10})
	case name == "creeper":
		g.Add(1, jp, &floatGoal{gb})
		g.Add(2, mv, &swellGoal{gb})
		g.Add(3, mv, &avoidGoal{goalBase: gb, match: typeIs("ocelot", "cat"), dist: 6, walk: 1, sprint: 1.2})
		g.Add(4, mv|lk, &meleeGoal{goalBase: gb, speed: 1})
		idle(5, 6, 8)
		t.Add(1, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
		t.Add(2, tg, &hurtByGoal{goalBase: gb})
	case name == "spider" || name == "cave_spider":
		b.climber = true
		dark := func() bool { return !b.bright() }
		g.Add(1, jp, &floatGoal{gb})
		g.Add(3, mv|jp, &leapGoal{gb, 0.4})
		g.Add(4, mv|lk, &meleeGoal{goalBase: gb, speed: 1, always: true})
		idle(5, 6, 8)
		t.Add(1, tg, &hurtByGoal{goalBase: gb})
		t.Add(2, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10, when: dark})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("iron_golem"), mustSee: true, interval: 10, when: dark})
	case name == "enderman":
		g.Add(0, jp, &floatGoal{gb})
		g.Add(1, mv|jp, &freezeGoal{gb})
		g.Add(2, mv|lk, &meleeGoal{goalBase: gb, speed: 1, always: true})
		g.Add(7, mv, &strollGoal{gb, 1, strollInterval, true})
		g.Add(8, lk, &lookAtGoal{goalBase: gb, rng: 8})
		g.Add(8, mv|lk, &randomLookGoal{goalBase: gb})
		g.Add(10, 0, &leaveBlockGoal{gb})
		g.Add(11, 0, &takeBlockGoal{gb})
		t.Add(1, tg, &stareGoal{goalBase: gb})
		t.Add(2, tg, &hurtByGoal{goalBase: gb})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("endermite"), mustSee: true, interval: 10})
	case name == "slime" || name == "magma_cube":
		if b.slimeSize == 0 {
			b.slimeSize = int32(1 << b.rnd.IntN(3))
		}
		g.Add(1, mv|jp, &floatGoal{gb})
		g.Add(5, mv|jp, &slimeGoal{goalBase: gb})
		t.Add(1, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("iron_golem"), mustSee: true, interval: 10})
	case name == "witch":
		g.Add(1, jp, &floatGoal{gb})
		g.Add(2, mv|lk, &potionGoal{goalBase: gb})
		g.Add(2, mv, &strollGoal{gb, 1, strollInterval, true})
		g.Add(3, lk, &lookAtGoal{goalBase: gb, rng: 8})
		g.Add(3, mv|lk, &randomLookGoal{goalBase: gb})
		t.Add(1, tg, &hurtByGoal{goalBase: gb})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
	case name == "wolf":
		tame := func() bool { return b.owner != (api.UUID{}) }
		wild := func() bool { return !tame() }
		g.Add(1, jp, &floatGoal{gb})
		g.Add(1, mv, &panicGoal{gb, 1.5})
		g.Add(2, mv|jp, &sitGoal{gb})
		g.Add(4, mv|jp, &leapGoal{gb, 0.4})
		g.Add(5, mv|lk, &meleeGoal{goalBase: gb, speed: 1, always: true})
		g.Add(6, mv|lk, &followOwnerGoal{goalBase: gb, speed: 1})
		g.Add(7, mv|lk, &breedGoal{goalBase: gb, speed: 1})
		g.Add(8, mv, &strollGoal{gb, 1, strollInterval, true})
		g.Add(10, lk, &lookAtGoal{goalBase: gb, rng: 8})
		g.Add(10, mv|lk, &randomLookGoal{goalBase: gb})
		t.Add(1, tg, &ownerTargetGoal{goalBase: gb, hurtBy: true})
		t.Add(2, tg, &ownerTargetGoal{goalBase: gb})
		t.Add(3, tg, &hurtByGoal{goalBase: gb})
		t.Add(5, tg, &nearestTargetGoal{goalBase: gb, match: typeIs("sheep", "rabbit", "fox", "baby_turtle"), mustSee: false, interval: 10, when: wild})
		t.Add(7, tg, &nearestTargetGoal{goalBase: gb, match: func(e *aiEnt) bool { return skeletonTypes[e.typ] }, mustSee: false, interval: 10})
	case name == "iron_golem":
		g.Add(1, mv|lk, &meleeGoal{goalBase: gb, speed: 1, always: true})
		g.Add(4, mv, &strollGoal{gb, 0.6, strollInterval, true})
		g.Add(7, lk, &lookAtGoal{goalBase: gb, rng: 6})
		g.Add(8, mv|lk, &randomLookGoal{goalBase: gb})
		t.Add(2, tg, &hurtByGoal{goalBase: gb})
		t.Add(3, tg, &nearestTargetGoal{goalBase: gb, match: func(e *aiEnt) bool { return e.monster && e.typ != "creeper" }, mustSee: false, interval: 10})
	case name == "villager" || name == "wandering_trader":
		g.Add(0, jp, &floatGoal{gb})
		g.Add(1, mv, &panicGoal{gb, 0.6})
		g.Add(1, mv, &avoidGoal{goalBase: gb, match: func(e *aiEnt) bool {
			return zombieTypes[e.typ] || e.typ == "vindicator" || e.typ == "pillager" || e.typ == "evoker" || e.typ == "vex" || e.typ == "ravager" || e.typ == "zoglin"
		}, dist: 8, walk: 0.5, sprint: 0.6})
		g.Add(5, mv, &strollGoal{gb, 0.5, strollInterval, true})
		g.Add(6, lk, &lookAtGoal{goalBase: gb, rng: 8})
		g.Add(7, mv|lk, &randomLookGoal{goalBase: gb})
	case flyingTypes[name]:
		b.body.Flying = true
		m.e.NoGravity = true
		g.Add(5, mv, &flyGoal{gb})
		g.Add(6, lk, &lookAtGoal{goalBase: gb, rng: 8})
		if m.typ.Category == "monster" {
			g.Add(2, mv|lk, &meleeGoal{goalBase: gb, speed: 1, always: true})
			t.Add(1, tg, &hurtByGoal{goalBase: gb})
			t.Add(2, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
		}
	case swimTypes[name]:
		b.body.Swimming = true
		g.Add(1, mv, &panicGoal{gb, 1.25})
		g.Add(4, mv, &swimGoal{gb})
	case m.typ.Category == "monster":
		g.Add(1, jp, &floatGoal{gb})
		g.Add(2, mv|lk, &meleeGoal{goalBase: gb, speed: 1})
		idle(7, 8, 8)
		t.Add(1, tg, &hurtByGoal{goalBase: gb})
		t.Add(2, tg, &nearestTargetGoal{goalBase: gb, match: isPlayer, mustSee: true, interval: 10})
	case m.typ.Category == "creature" || m.typ.Category == "ambient" || m.typ.Category == "axolotls" || m.typ.Category == "underground_water_creature" || m.typ.Category == "water_creature" || m.typ.Category == "water_ambient":
		g.Add(0, jp, &floatGoal{gb})
		g.Add(1, mv, &panicGoal{gb, 1.25})
		idle(5, 6, 6)
	default:
		return false
	}
	return true
}

// --- enderman ---

// staring reports whether player e looks at the enderman's eyes: the angle
// test of minecraft.wiki ("looks at the upper body"), with line of sight.
func (b *mobBrain) staring(e *aiEnt) bool {
	if !e.isPlayer() || e.pumpkin || !e.alive {
		return false
	}
	yaw, pitch := e.yaw*math.Pi/180, e.pitch*math.Pi/180
	lx, ly, lz := -math.Sin(yaw)*math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw)*math.Cos(pitch)
	eye := b.eyePos()
	dx, dy, dz := eye.X-e.pos.X, eye.Y-(e.pos.Y+e.eye), eye.Z-e.pos.Z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-4 {
		return false
	}
	dot := (lx*dx + ly*dy + lz*dz) / d
	return dot > 1-0.025/d && b.canSee(e)
}

type stareGoal struct {
	goalBase
	player int32
	aggro  int
}

func (g *stareGoal) CanUse() bool {
	b := g.b
	g.player = b.nearest(64, func(e *aiEnt) bool { return e.targetOK && b.staring(e) })
	return g.player != 0
}
func (g *stareGoal) Start() { g.aggro = 5 }
func (g *stareGoal) CanContinue() bool {
	b := g.b
	if g.aggro > 0 {
		t := b.cx.a.byID(g.player)
		return t != nil && b.staring(t)
	}
	return b.keepTarget(false)
}
func (g *stareGoal) Tick() {
	b := g.b
	if g.aggro > 0 {
		if g.aggro--; g.aggro == 0 {
			b.setTarget(b.cx.a.byID(g.player))
			b.angryAt = g.player
			m := b.m
			b.act(func() { m.w.playSoundAt(m.feet(), "minecraft:entity.enderman.stare", 5, 2.5, 1) })
		}
		return
	}
	if t := b.targetEnt(); t != nil {
		d := dist2(t.pos, b.pos())
		if d > 256 && b.chance(30) {
			b.teleportToward(t.pos)
		} else if d < 16 && b.staring(t) && b.chance(4) {
			b.teleportRandom()
		}
	}
}

type freezeGoal struct{ goalBase }

func (g *freezeGoal) CanUse() bool {
	b := g.b
	t := b.targetEnt()
	return t != nil && t.isPlayer() && dist2(t.pos, b.pos()) < 16 && b.staring(t)
}
func (g *freezeGoal) CanContinue() bool { return g.CanUse() }
func (g *freezeGoal) Start()            { g.b.nav.Stop() }
func (g *freezeGoal) Tick() {
	if t := g.b.targetEnt(); t != nil {
		g.b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 10, 40)
	}
}

// teleportRandom picks a random spot within 32 blocks horizontally and 32
// vertically with a solid floor (minecraft.wiki: Enderman#Teleportation).
func (b *mobBrain) teleportRandom() {
	p := b.pos()
	b.teleportTo(api.Vec3{X: p.X + (b.rnd.Float64()-0.5)*64, Y: p.Y + float64(b.rnd.IntN(64)-32), Z: p.Z + (b.rnd.Float64()-0.5)*64})
}

func (b *mobBrain) teleportToward(t api.Vec3) {
	p := b.pos()
	dx, dy, dz := p.X-t.X, p.Y-t.Y, p.Z-t.Z
	l := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if l < 1e-4 {
		return
	}
	dx, dy, dz = dx/l, dy/l, dz/l
	b.teleportTo(api.Vec3{X: p.X + (b.rnd.Float64()-0.5)*8 - dx*16, Y: p.Y + float64(b.rnd.IntN(16)-8) - dy*16, Z: p.Z + (b.rnd.Float64()-0.5)*8 - dz*16})
}

func (b *mobBrain) teleportTo(to api.Vec3) {
	m := b.m
	b.act(func() {
		w := m.w
		x, z := int(math.Floor(to.X)), int(math.Floor(to.Z))
		y := int(math.Floor(to.Y))
		for ; y > w.MinY(); y-- {
			if len(data.Collision(w.w.Block(x, y-1, z))) > 0 {
				break
			}
		}
		if y <= w.MinY() || !standable(w.w, x, y, z, true) || !standable(w.w, x, y+2, z, true) && len(data.Collision(w.w.Block(x, y+2, z))) > 0 {
			return
		}
		from := m.feet()
		dst := api.Vec3{X: float64(x) + 0.5, Y: float64(y), Z: float64(z) + 0.5}
		m.mu.Lock()
		m.e.Pos, m.e.Prev, m.e.Vel = dst, dst, api.Vec3{}
		m.mu.Unlock()
		if mb := m.ai; mb != nil {
			mb.nav.Stop()
		}
		w.playSoundAt(from, "minecraft:entity.enderman.teleport", 5, 1, 1)
		w.playSoundAt(dst, "minecraft:entity.enderman.teleport", 5, 1, 1)
	})
}

type takeBlockGoal struct{ goalBase }

func (g *takeBlockGoal) CanUse() bool {
	b := g.b
	return b.carried == 0 && b.cx.a.grief && b.chance(10)
}
func (g *takeBlockGoal) CanContinue() bool { return false }
func (g *takeBlockGoal) Start() {
	b := g.b
	p := b.pos()
	x := int(math.Floor(p.X - 2 + b.rnd.Float64()*4))
	y := int(math.Floor(p.Y + b.rnd.Float64()*3))
	z := int(math.Floor(p.Z - 2 + b.rnd.Float64()*4))
	s := b.cx.src.Block(x, y, z)
	if !inTag("block", "minecraft:enderman_holdable", "minecraft:"+data.Canonical().BlockOf(s).Name) {
		return
	}
	b.carried = s
	m := b.m
	b.act(func() {
		if m.w.w.Block(x, y, z) != s {
			if mb := m.ai; mb != nil {
				mb.carried = 0
			}
			return
		}
		m.w.w.SetBlock(x, y, z, data.Air)
	})
}

type leaveBlockGoal struct{ goalBase }

func (g *leaveBlockGoal) CanUse() bool {
	b := g.b
	return b.carried != 0 && b.cx.a.grief && b.chance(2000)
}
func (g *leaveBlockGoal) CanContinue() bool { return false }
func (g *leaveBlockGoal) Start() {
	b := g.b
	p := b.pos()
	x := int(math.Floor(p.X - 1 + b.rnd.Float64()*2))
	y := int(math.Floor(p.Y + b.rnd.Float64()*2))
	z := int(math.Floor(p.Z - 1 + b.rnd.Float64()*2))
	src := b.cx.src
	if !data.IsAir(src.Block(x, y, z)) || !data.Canonical().IsFullCube(src.Block(x, y-1, z)) {
		return
	}
	s := b.carried
	b.carried = 0
	m := b.m
	b.act(func() {
		if data.IsAir(m.w.w.Block(x, y, z)) {
			m.w.w.SetBlock(x, y, z, s)
		}
	})
}

// endermanUpkeep: water and rain hurt and make it teleport away.
func (b *mobBrain) endermanUpkeep() {
	e := b.m.e
	wet := e.InWater
	if !wet && b.cx.a.raining && b.ticks%10 == 0 {
		p := b.eyePos()
		wet = b.cx.w.skyExposed(int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Floor(p.Z)))
	}
	if wet && b.chance(10) {
		b.teleportRandom()
	}
}

// --- slime ---

// slimeGoal hops: a jump every 10–29 ticks (a third when chasing, per
// minecraft.wiki), turning toward the target or a random direction.
type slimeGoal struct {
	goalBase
	yaw  float32
	turn int
}

func (g *slimeGoal) CanUse() bool      { return true }
func (g *slimeGoal) CanContinue() bool { return true }
func (g *slimeGoal) Tick() {
	b := g.b
	e := b.m.e
	t := b.targetEnt()
	if t != nil {
		g.yaw = ai.YawTo(t.pos.X-e.Pos.X, t.pos.Z-e.Pos.Z)
		b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 10, 10)
		if b.inReach(t) && b.ticks%10 == 0 {
			dmg := float32(b.slimeSize)
			if b.m.typ.Name == "magma_cube" {
				dmg += 2
			} else if b.slimeSize <= 1 {
				dmg = 0
			}
			if dmg > 0 {
				m, tc := b.m, t.combatant()
				b.act(func() {
					if hurt(tc, EntityDamage("minecraft:mob_attack", m), dmg) {
						m.w.playSoundAt(m.feet(), "minecraft:entity.slime.attack", 5, 1, 1)
					}
				})
			}
		}
	} else if g.turn--; g.turn <= 0 {
		g.turn = 40 + b.rnd.IntN(60)
		g.yaw = float32(b.rnd.IntN(360))
	}
	e.Yaw = ai.RotTowards(e.Yaw, g.yaw, 10)
	e.HeadYaw = e.Yaw
	if e.OnGround {
		if b.jumpDelay--; b.jumpDelay <= 0 {
			b.jumpDelay = int32(b.rnd.IntN(20) + 10)
			if t != nil {
				b.jumpDelay /= 3
			}
			b.body.Jump()
			b.body.Strafe(1, 0, 1)
		}
		return
	}
	b.body.Strafe(1, 0, 1)
}

// --- witch ---

// potionGoal throws a splash potion every 60 ticks within 10 blocks
// (minecraft.wiki: Witch). Potion effects on impact are applied by the
// projectile code when supported.
type potionGoal struct {
	goalBase
	cooldown int
}

func (g *potionGoal) CanUse() bool      { return g.b.targetEnt() != nil }
func (g *potionGoal) CanContinue() bool { return g.b.targetEnt() != nil }
func (g *potionGoal) Stop()             { g.b.nav.Stop() }
func (g *potionGoal) Tick() {
	b := g.b
	t := b.targetEnt()
	if t == nil {
		return
	}
	b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 30, 30)
	d := dist2(t.pos, b.pos())
	seen := b.canSee(t)
	if d > 100 || !seen {
		b.moveNear(t.pos, 1, 0)
	} else {
		b.nav.Stop()
	}
	if g.cooldown--; g.cooldown <= 0 && d <= 100 && seen {
		g.cooldown = 60
		b.throwPotion(t)
	}
}

func (b *mobBrain) throwPotion(t *aiEnt) {
	m := b.m
	eye := b.eyePos()
	dx, dz := t.pos.X-eye.X, t.pos.Z-eye.Z
	h := math.Sqrt(dx*dx + dz*dz)
	dy := t.pos.Y + t.eye - 1.1 - eye.Y + h*0.2
	yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	pitch := float32(-math.Atan2(dy, h) * 180 / math.Pi)
	b.act(func() {
		m.w.playSoundAt(eye, "minecraft:entity.witch.throw", 6, 1, 0.8+randBetween(0, 0.4))
		m.w.spawnProjectile("splash_potion", m, eye, yaw, pitch, 0.75, 8, &projectile{})
	})
}

// --- wolf / tamed ---

type sitGoal struct{ goalBase }

func (g *sitGoal) CanUse() bool      { return g.b.sitting && g.b.owner != (api.UUID{}) && g.b.m.e.OnGround }
func (g *sitGoal) CanContinue() bool { return g.b.sitting }
func (g *sitGoal) Start()            { g.b.nav.Stop(); g.b.body.Stop() }

func (b *mobBrain) ownerEnt() *aiEnt {
	if b.owner == (api.UUID{}) {
		return nil
	}
	for i := range b.cx.a.np {
		if e := &b.cx.a.ents[i]; e.isPlayer() && e.uuid == b.owner {
			return e
		}
	}
	return nil
}

type followOwnerGoal struct {
	goalBase
	speed float64
	delay int
}

func (g *followOwnerGoal) CanUse() bool {
	b := g.b
	o := b.ownerEnt()
	return o != nil && o.alive && !b.sitting && dist2(o.pos, b.pos()) >= 100
}
func (g *followOwnerGoal) CanContinue() bool {
	b := g.b
	o := b.ownerEnt()
	return o != nil && o.alive && !b.sitting && dist2(o.pos, b.pos()) > 4
}
func (g *followOwnerGoal) Stop() { g.b.nav.Stop() }
func (g *followOwnerGoal) Tick() {
	b := g.b
	o := b.ownerEnt()
	if o == nil {
		return
	}
	b.body.LookAt(o.pos.X, o.pos.Y+o.eye, o.pos.Z, 10, 40)
	if g.delay--; g.delay > 0 {
		return
	}
	g.delay = 10
	if dist2(o.pos, b.pos()) >= 144 {
		for range 10 {
			x := int(math.Floor(o.pos.X)) + b.rnd.IntN(7) - 3
			z := int(math.Floor(o.pos.Z)) + b.rnd.IntN(7) - 3
			y := int(math.Floor(o.pos.Y)) + b.rnd.IntN(3) - 1
			if (x-int(math.Floor(o.pos.X)))*(x-int(math.Floor(o.pos.X)))+(z-int(math.Floor(o.pos.Z)))*(z-int(math.Floor(o.pos.Z))) < 4 {
				continue
			}
			if standable(b.cx.src, x, y, z, true) {
				dst := api.Vec3{X: float64(x) + 0.5, Y: float64(y), Z: float64(z) + 0.5}
				b.m.e.Pos, b.m.e.Prev = dst, dst
				b.nav.Stop()
				return
			}
		}
		return
	}
	b.moveNear(o.pos, g.speed, 1)
}

type ownerTargetGoal struct {
	goalBase
	hurtBy bool
	seen   int32
}

func (g *ownerTargetGoal) CanUse() bool {
	b := g.b
	if b.owner == (api.UUID{}) || b.sitting {
		return false
	}
	id := b.ownerHit
	if g.hurtBy {
		id = b.ownerHurt
	}
	if id == 0 || id == g.seen {
		return false
	}
	t := b.cx.a.byID(id)
	return t != nil && t.alive && t.typ != "creeper" && t.typ != "ghast" && !(t.m != nil && t.m.ai != nil && t.m.ai.owner == b.owner)
}
func (g *ownerTargetGoal) Start() {
	b := g.b
	id := b.ownerHit
	if g.hurtBy {
		id = b.ownerHurt
	}
	g.seen = id
	b.setTarget(b.cx.a.byID(id))
}
func (g *ownerTargetGoal) CanContinue() bool { return g.b.keepTarget(false) }

// --- flying and swimming basics ---

type flyGoal struct{ goalBase }

func (g *flyGoal) CanUse() bool { return g.b.chance(20) }
func (g *flyGoal) Start() {
	b := g.b
	p := b.pos()
	for range 8 {
		x := p.X + (b.rnd.Float64()*2-1)*8
		y := p.Y + (b.rnd.Float64()*2-1)*4
		z := p.Z + (b.rnd.Float64()*2-1)*8
		s := b.cx.src.Block(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z)))
		if len(data.Collision(s)) == 0 && !data.Canonical().HasFluid(s) {
			b.body.MoveTo(x, y, z, 1)
			return
		}
	}
}
func (g *flyGoal) CanContinue() bool { return g.b.body.Moving() && !g.b.m.e.HorizontalCollision }
func (g *flyGoal) Stop()             { g.b.body.Stop() }

type swimGoal struct{ goalBase }

func (g *swimGoal) CanUse() bool {
	e := g.b.m.e
	if !e.InWater {
		if e.OnGround && g.b.chance(10) {
			e.Vel.Y = 0.4
			e.Vel.X += (g.b.rnd.Float64()*2 - 1) * 0.05
			e.Vel.Z += (g.b.rnd.Float64()*2 - 1) * 0.05
		}
		return false
	}
	return g.b.chance(20)
}
func (g *swimGoal) Start() {
	b := g.b
	p := b.pos()
	for range 8 {
		x := p.X + (b.rnd.Float64()*2-1)*10
		y := p.Y + (b.rnd.Float64()*2-1)*7
		z := p.Z + (b.rnd.Float64()*2-1)*10
		s := b.cx.src.Block(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z)))
		if data.Canonical().BlockOf(s).Name == "water" {
			b.body.MoveTo(x, y, z, 1)
			return
		}
	}
}
func (g *swimGoal) CanContinue() bool { return g.b.body.Moving() && g.b.m.e.InWater }
func (g *swimGoal) Stop()             { g.b.body.Stop() }
