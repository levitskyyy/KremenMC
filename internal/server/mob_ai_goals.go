package server

import (
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/entity/ai"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// Goal implementations. All run on the region worker with the mob's lock
// held; anything touching another entity goes through b.act.

type goalBase struct {
	ai.Base
	b *mobBrain
}

// --- movement / idle ---

type floatGoal struct{ goalBase }

func (g *floatGoal) CanUse() bool {
	e := g.b.m.e
	return e.InWater && e.WaterHeight > fluidThreshold(e) || e.InLava
}
func (g *floatGoal) CanContinue() bool { return g.CanUse() }
func (g *floatGoal) Tick() {
	if g.b.rnd.Float64() < 0.8 {
		g.b.body.Jump()
	}
}

func fluidThreshold(e *entity.Entity) float64 {
	if e.Height < 0.4 {
		return 0
	}
	return 0.4
}

type panicGoal struct {
	goalBase
	speed float64
}

func (g *panicGoal) CanUse() bool {
	b := g.b
	if b.hurtTime <= 0 && !b.burning {
		return false
	}
	p, ok := b.randomPos(5, 4, false)
	if !ok {
		return false
	}
	return b.moveNear(p, g.speed, 0)
}
func (g *panicGoal) CanContinue() bool { return !g.b.nav.Done() }
func (g *panicGoal) Stop()             { g.b.nav.Stop() }

type strollGoal struct {
	goalBase
	speed    float64
	interval int
	dry      bool
}

func (g *strollGoal) CanUse() bool {
	b := g.b
	if b.noAction >= 100 || !b.chance(g.interval/2) {
		return false
	}
	p, ok := b.randomPos(10, 7, g.dry)
	return ok && b.moveNear(p, g.speed, 0)
}
func (g *strollGoal) CanContinue() bool { return !g.b.nav.Done() }
func (g *strollGoal) Stop()             { g.b.nav.Stop() }

type lookAtGoal struct {
	goalBase
	rng  float64
	typ  string // "" players
	look int32
	left int
}

func (g *lookAtGoal) CanUse() bool {
	b := g.b
	if b.rnd.Float64() >= 0.02 {
		return false
	}
	if t := b.targetEnt(); t != nil {
		g.look = t.id
		return true
	}
	g.look = b.nearest(g.rng, func(e *aiEnt) bool {
		if g.typ == "" {
			return e.isPlayer() && e.alive
		}
		return e.typ == g.typ
	})
	return g.look != 0
}
func (g *lookAtGoal) Start() { g.left = 40 + g.b.rnd.IntN(40) }
func (g *lookAtGoal) CanContinue() bool {
	t := g.b.cx.a.byID(g.look)
	return g.left > 0 && t != nil && t.alive && dist2(t.pos, g.b.pos()) <= g.rng*g.rng
}
func (g *lookAtGoal) Tick() {
	g.left--
	if t := g.b.cx.a.byID(g.look); t != nil {
		g.b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 10, 40)
	}
}

type randomLookGoal struct {
	goalBase
	dx, dz float64
	left   int
}

func (g *randomLookGoal) CanUse() bool { return g.b.rnd.Float64() < 0.02 }
func (g *randomLookGoal) Start() {
	a := g.b.rnd.Float64() * 2 * math.Pi
	g.dx, g.dz = math.Cos(a), math.Sin(a)
	g.left = 20 + g.b.rnd.IntN(20)
}
func (g *randomLookGoal) CanContinue() bool { return g.left >= 0 }
func (g *randomLookGoal) Tick() {
	g.left--
	p := g.b.eyePos()
	g.b.body.LookAt(p.X+g.dx, p.Y, p.Z+g.dz, 10, 40)
}

// --- animals ---

type temptGoal struct {
	goalBase
	speed  float64
	player int32
	calm   int
}

func (b *mobBrain) tempts(name string) bool {
	if name == "" || name == "minecraft:air" {
		return false
	}
	if aiHooks.TemptItem != nil {
		return aiHooks.TemptItem(b.m.typ.Name, name)
	}
	return inTag("item", "minecraft:"+b.m.typ.Name+"_food", name)
}

func (g *temptGoal) CanUse() bool {
	if g.calm > 0 {
		g.calm--
		return false
	}
	b := g.b
	r := 10.0
	if b.temptRange > 0 {
		r = b.temptRange
	}
	g.player = b.nearest(r, func(e *aiEnt) bool {
		return e.isPlayer() && e.alive && (b.tempts(e.held[0]) || b.tempts(e.held[1]))
	})
	return g.player != 0
}
func (g *temptGoal) CanContinue() bool {
	t := g.b.cx.a.byID(g.player)
	return t != nil && t.alive && (g.b.tempts(t.held[0]) || g.b.tempts(t.held[1])) && dist2(t.pos, g.b.pos()) < 100
}
func (g *temptGoal) Stop() { g.calm = 100; g.b.nav.Stop() }
func (g *temptGoal) Tick() {
	b := g.b
	t := b.cx.a.byID(g.player)
	if t == nil {
		return
	}
	b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 10, 40)
	if dist2(t.pos, b.pos()) < 6.25 {
		b.nav.Stop()
		b.body.Stop()
		return
	}
	b.moveNear(t.pos, g.speed, 1)
}

type breedGoal struct {
	goalBase
	speed   float64
	partner int32
	time    int
}

func (g *breedGoal) CanUse() bool {
	b := g.b
	if b.love <= 0 {
		return false
	}
	g.partner = b.nearest(8, func(e *aiEnt) bool {
		return e.m != nil && e.m != b.m && e.typ == b.m.typ.Name && e.love && e.alive
	})
	return g.partner != 0
}
func (g *breedGoal) CanContinue() bool {
	t := g.b.cx.a.byID(g.partner)
	return t != nil && t.alive && t.love && g.time < 60 && g.b.love > 0
}
func (g *breedGoal) Stop() { g.partner, g.time = 0, 0 }
func (g *breedGoal) Tick() {
	b := g.b
	t := b.cx.a.byID(g.partner)
	if t == nil {
		return
	}
	b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 10, 40)
	b.moveNear(t.pos, g.speed, 1)
	g.time++
	if g.time >= 60 && dist2(t.pos, b.pos()) < 9 && b.m.e.ID < t.id {
		a, o := b.m, t.m
		b.love, b.breedCool = 0, 6000
		b.act(func() {
			if ob := o.ai; ob != nil {
				ob.love, ob.breedCool = 0, 6000
			}
			if aiHooks.Breed != nil {
				aiHooks.Breed(a, o)
				return
			}
			e, err := a.w.SpawnEntity(api.Key("minecraft:"+a.typ.Name), api.Location{World: a.w.name, Pos: a.feet()}, nil)
			if err == nil {
				c := e.(*Mob)
				c.SetAIAge(-24000)
				c.setBaby()
			}
		})
	}
}

type followParentGoal struct {
	goalBase
	speed  float64
	parent int32
	delay  int
}

func (g *followParentGoal) CanUse() bool {
	b := g.b
	if b.age >= 0 {
		return false
	}
	g.parent = b.nearest(8, func(e *aiEnt) bool {
		return e.m != nil && e.typ == b.m.typ.Name && !e.baby && e.alive
	})
	t := b.cx.a.byID(g.parent)
	return t != nil && dist2(t.pos, b.pos()) >= 9
}
func (g *followParentGoal) CanContinue() bool {
	t := g.b.cx.a.byID(g.parent)
	if g.b.age >= 0 || t == nil || !t.alive {
		return false
	}
	d := dist2(t.pos, g.b.pos())
	return d >= 9 && d <= 256
}
func (g *followParentGoal) Start() { g.delay = 0 }
func (g *followParentGoal) Tick() {
	if g.delay--; g.delay > 0 {
		return
	}
	g.delay = 10
	if t := g.b.cx.a.byID(g.parent); t != nil {
		g.b.moveNear(t.pos, g.speed, 1)
	}
}

type eatGrassGoal struct {
	goalBase
	left int
}

func (g *eatGrassGoal) grass() (int, int, int, string) {
	p := g.b.pos()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Floor(p.Z))
	if n := data.Canonical().BlockOf(g.b.cx.src.Block(x, y, z)).Name; n == "short_grass" {
		return x, y, z, n
	}
	if n := data.Canonical().BlockOf(g.b.cx.src.Block(x, y-1, z)).Name; n == "grass_block" {
		return x, y - 1, z, n
	}
	return 0, 0, 0, ""
}

func (g *eatGrassGoal) CanUse() bool {
	n := 1000
	if g.b.age < 0 {
		n = 50
	}
	if !g.b.chance(n / 2) {
		return false
	}
	_, _, _, name := g.grass()
	return name != ""
}
func (g *eatGrassGoal) Start() {
	g.left = 40
	g.b.nav.Stop()
	m := g.b.m
	g.b.act(func() { m.w.broadcastNear(m.feet(), entityEvent(m.e.ID, 10)) })
}
func (g *eatGrassGoal) CanContinue() bool { return g.left > 0 }
func (g *eatGrassGoal) Tick() {
	g.left--
	if g.left != 4 {
		return
	}
	x, y, z, name := g.grass()
	if name == "" {
		return
	}
	m := g.b.m
	grief := g.b.cx.a.grief
	g.b.act(func() {
		if grief {
			if name == "short_grass" {
				m.w.Block(api.BlockPos{X: x, Y: y, Z: z}).Break(false)
			} else if dirt, ok := data.Canonical().DefaultState("dirt"); ok {
				m.w.w.SetBlock(x, y, z, dirt)
			}
		}
		if aiHooks.AteGrass != nil {
			aiHooks.AteGrass(m)
		}
		if b := m.ai; b != nil && b.age < 0 {
			b.age = min(0, b.age+1200)
		}
	})
}

type avoidGoal struct {
	goalBase
	match        func(e *aiEnt) bool
	dist         float64
	walk, sprint float64
	from         int32
}

func (g *avoidGoal) CanUse() bool {
	b := g.b
	g.from = b.nearest(g.dist, g.match)
	t := b.cx.a.byID(g.from)
	if t == nil {
		return false
	}
	p := b.pos()
	for range 10 {
		q, ok := b.randomPos(16, 7, false)
		if !ok {
			continue
		}
		if dist2(q, t.pos) > dist2(p, t.pos) {
			return b.moveNear(q, g.walk, 0)
		}
	}
	return false
}
func (g *avoidGoal) CanContinue() bool { return !g.b.nav.Done() }
func (g *avoidGoal) Stop()             { g.b.nav.Stop() }
func (g *avoidGoal) Tick() {
	if t := g.b.cx.a.byID(g.from); t != nil && dist2(t.pos, g.b.pos()) < 49 {
		g.b.nav.SetSpeed(g.sprint)
	} else {
		g.b.nav.SetSpeed(g.walk)
	}
}

// --- combat ---

// attackReach is the horizontal inflation of the attacker's box used for
// melee reach (fitted: the reference zombie hits at a box gap of 0.72 and
// not at 0.84).
const attackReach = 0.8284271247461903

func (b *mobBrain) inReach(t *aiEnt) bool {
	e := b.m.e
	r := attackReach
	ax0, ax1 := e.Pos.X-e.Width/2-r, e.Pos.X+e.Width/2+r
	az0, az1 := e.Pos.Z-e.Width/2-r, e.Pos.Z+e.Width/2+r
	tx0, tx1 := t.pos.X-t.w/2, t.pos.X+t.w/2
	tz0, tz1 := t.pos.Z-t.w/2, t.pos.Z+t.w/2
	return ax0 < tx1 && ax1 > tx0 && az0 < tz1 && az1 > tz0 && e.Pos.Y < t.pos.Y+t.h && e.Pos.Y+e.Height > t.pos.Y
}

type meleeGoal struct {
	goalBase
	speed    float64
	always   bool // keep chasing an unseen target
	cooldown int
	repath   int
	lastPos  api.Vec3
	// door breaking (zombies on hard)
	breakDoors bool
	door       [3]int
	doorTime   int
}

func (g *meleeGoal) CanUse() bool {
	b := g.b
	t := b.targetEnt()
	if t == nil || !t.alive {
		return false
	}
	if b.inReach(t) {
		return true
	}
	return b.moveNear(t.pos, g.speed, 1)
}
func (g *meleeGoal) CanContinue() bool {
	b := g.b
	t := b.targetEnt()
	if t == nil || !t.alive {
		return false
	}
	if !g.always && b.nav.Done() && !b.inReach(t) {
		return false
	}
	return dist2(t.pos, b.pos()) <= b.follow*b.follow
}
func (g *meleeGoal) Start() { g.repath, g.cooldown = 0, 0 }
func (g *meleeGoal) Stop()  { g.b.nav.Stop(); g.doorTime = 0 }
func (g *meleeGoal) Tick() {
	b := g.b
	t := b.targetEnt()
	if t == nil {
		return
	}
	b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 30, 30)
	if g.breakDoors && g.tickDoor() {
		return
	}
	g.repath--
	if g.repath <= 0 || dist2(t.pos, g.lastPos) > 1 && g.repath <= 2 {
		d := dist2(t.pos, b.pos())
		g.repath = 4 + b.rnd.IntN(7)
		if d > 1024 {
			g.repath += 10
		} else if d > 256 {
			g.repath += 5
		}
		g.lastPos = t.pos
		if !b.moveNear(t.pos, g.speed, 0) {
			g.repath += 15
		}
	}
	if g.cooldown > 0 {
		g.cooldown--
	}
	if g.cooldown <= 0 && b.inReach(t) && b.canSee(t) {
		g.cooldown = 20
		b.meleeHit(t)
	}
}

// tickDoor breaks a closed wooden door in the way (hard difficulty):
// 240 ticks of hitting, then the door is removed (minecraft.wiki).
func (g *meleeGoal) tickDoor() bool {
	b := g.b
	if b.cx.a.diff != api.DifficultyHard || !b.cx.a.grief {
		g.doorTime = 0
		return false
	}
	n, ok := b.nav.Next()
	if !ok {
		return false
	}
	x, y, z := int(n.X), int(n.Y), int(n.Z)
	if !ai.IsWoodDoor(b.cx.src.Block(x, y, z)) {
		g.doorTime = 0
		return false
	}
	cx, _, cz := n.Center()
	if dist2(api.Vec3{X: cx, Y: b.pos().Y, Z: cz}, b.pos()) > 2.25 {
		return false
	}
	if g.door != [3]int{x, y, z} {
		g.door, g.doorTime = [3]int{x, y, z}, 0
	}
	b.body.Stop()
	g.doorTime++
	m := b.m
	if g.doorTime%20 == 0 {
		b.act(func() {
			m.w.playSoundAt(api.Vec3{X: cx, Y: float64(y), Z: cz}, "minecraft:entity.zombie.attack_wooden_door", 5, 1, 1)
		})
	}
	if g.doorTime >= 240 {
		g.doorTime = 0
		b.act(func() {
			pos := api.BlockPos{X: x, Y: y, Z: z}
			m.w.Block(pos).Break(true)
			m.w.playSoundAt(api.Vec3{X: cx, Y: float64(y), Z: cz}, "minecraft:entity.zombie.break_wooden_door", 5, 1, 1)
		})
	}
	return true
}

// meleeHit queues a melee hit through the vanilla damage pipeline.
func (b *mobBrain) meleeHit(t *aiEnt) {
	m, tc := b.m, t.combatant()
	yaw := ai.YawTo(t.pos.X-m.e.Pos.X, t.pos.Z-m.e.Pos.Z)
	m.e.Yaw, m.e.HeadYaw = yaw, yaw
	b.act(func() {
		if tc.IsValid() && !tc.lv().isDead() {
			m.w.broadcastNear(m.feet(), &packets.ClientboundSwingAnimation{EntityID: m.e.ID, Hand: 0, Animation: swingWhack, Duration: swingTicks})
			m.mobAttack(tc)
		}
	})
}

type leapGoal struct {
	goalBase
	y float64
}

func (g *leapGoal) CanUse() bool {
	b := g.b
	t := b.targetEnt()
	if t == nil || !b.m.e.OnGround {
		return false
	}
	d := dist2(t.pos, b.pos())
	return d >= 4 && d <= 16 && b.chance(5)
}
func (g *leapGoal) CanContinue() bool { return !g.b.m.e.OnGround }
func (g *leapGoal) Start() {
	b := g.b
	t := b.targetEnt()
	e := b.m.e
	dx, dz := t.pos.X-e.Pos.X, t.pos.Z-e.Pos.Z
	l := math.Sqrt(dx*dx + dz*dz)
	if l > 1e-4 {
		e.Vel.X += dx/l*0.4 + e.Vel.X*0.2
		e.Vel.Z += dz/l*0.4 + e.Vel.Z*0.2
	}
	e.Vel.Y = g.y
	b.m.liv.motion = true
}

type bowGoal struct {
	goalBase
	speed      float64
	radius     float64
	interval   int
	cooldown   int
	seen       int
	draw       int
	strafeTime int
	strafeLeft bool
	strafeBack bool
}

func (g *bowGoal) CanUse() bool      { t := g.b.targetEnt(); return t != nil && t.alive && g.hasBow() }
func (g *bowGoal) hasBow() bool      { return g.b.mainHand == "minecraft:bow" }
func (g *bowGoal) CanContinue() bool { return g.CanUse() || !g.b.nav.Done() }
func (g *bowGoal) Start()            { g.cooldown, g.seen, g.draw = -1, 0, -1 }
func (g *bowGoal) Stop()             { g.b.nav.Stop(); g.draw = -1 }

func (g *bowGoal) Tick() {
	b := g.b
	t := b.targetEnt()
	if t == nil {
		return
	}
	seen := b.canSee(t)
	if seen != (g.seen > 0) {
		g.seen = 0
	}
	if seen {
		g.seen++
	} else {
		g.seen--
	}
	d := dist2(t.pos, b.pos())
	r2 := g.radius * g.radius
	if d <= r2 && g.seen >= 20 {
		b.nav.Stop()
		g.strafeTime++
	} else {
		b.moveNear(t.pos, g.speed, 0)
		g.strafeTime = -1
	}
	if g.strafeTime >= 20 {
		if b.rnd.Float64() < 0.3 {
			g.strafeLeft = !g.strafeLeft
		}
		if b.rnd.Float64() < 0.3 {
			g.strafeBack = !g.strafeBack
		}
		g.strafeTime = 0
	}
	if g.strafeTime > -1 {
		if d > r2*0.75 {
			g.strafeBack = false
		} else if d < r2*0.25 {
			g.strafeBack = true
		}
		fw, st := 0.5, 0.5
		if g.strafeBack {
			fw = -0.5
		}
		if g.strafeLeft {
			st = -0.5
		}
		b.body.Strafe(fw, st, g.speed)
		b.m.e.Yaw = ai.RotTowards(b.m.e.Yaw, ai.YawTo(t.pos.X-b.pos().X, t.pos.Z-b.pos().Z), 30)
	}
	b.body.LookAt(t.pos.X, t.pos.Y+t.eye, t.pos.Z, 30, 30)
	if g.draw >= 0 {
		if !seen && g.seen < -60 {
			g.draw = -1
			return
		}
		g.draw++
		if seen && g.draw >= 20 {
			g.draw = -1
			g.cooldown = g.interval
			b.shootArrow(t)
		}
		return
	}
	if g.cooldown--; g.cooldown <= 0 && g.seen >= -60 {
		g.draw = 0
	}
}

// shootArrow fires at t like a skeleton: speed 1.6, inaccuracy
// 14 − 4·difficulty, aimed at a third of the target's height with an
// arc of 0.2 per block of horizontal distance.
func (b *mobBrain) shootArrow(t *aiEnt) {
	m := b.m
	eye := b.eyePos()
	eye.Y -= 0.1
	dx, dz := t.pos.X-eye.X, t.pos.Z-eye.Z
	h := math.Sqrt(dx*dx + dz*dz)
	dy := t.pos.Y + t.h/3 - eye.Y + h*0.2
	yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	pitch := float32(-math.Atan2(dy, h) * 180 / math.Pi)
	inacc := float64(14 - 4*int(b.cx.a.diff))
	stray := m.typ.Name == "stray"
	b.act(func() {
		m.liv.mu.Lock()
		bow := m.liv.equip[slotMain]
		m.liv.mu.Unlock()
		ammo := item.Stack{}
		if id, ok := item.IDOf("minecraft:arrow"); ok {
			ammo = item.Stack{Item: id, Count: 1}
		}
		pr := &projectile{baseDmg: 2, pickup: pickupDisallowed, weapon: bow, stack: ammo,
			fire: bow.EnchantmentLevel("minecraft:flame") > 0}
		_ = stray
		m.w.spawnProjectile("arrow", m, eye, yaw, pitch, 1.6, inacc, pr)
		m.w.playSoundAt(eye, "minecraft:entity.skeleton.shoot", 5, 1, 1/(randBetween(0, 0.4)+0.8))
	})
}

type swellGoal struct{ goalBase }

func (g *swellGoal) CanUse() bool {
	b := g.b
	t := b.targetEnt()
	return b.swellDir > 0 || t != nil && dist2(t.pos, b.pos()) < 9
}
func (g *swellGoal) CanContinue() bool { return g.CanUse() }
func (g *swellGoal) Start()            { g.b.nav.Stop() }
func (g *swellGoal) Stop()             { g.b.swellDir = -1 }
func (g *swellGoal) Tick() {
	b := g.b
	t := b.targetEnt()
	switch {
	case t == nil, dist2(t.pos, b.pos()) > 49, !b.canSee(t):
		b.swellDir = -1
	default:
		if b.swellDir <= 0 {
			m := b.m
			b.act(func() { m.w.playSoundAt(m.feet(), "minecraft:entity.creeper.primed", 5, 1, 0.5) })
		}
		b.swellDir = 1
	}
}
func (g *swellGoal) Uninterruptible() bool { return true }

// creeperFuse is the 30-tick fuse (reference: swell reaches 30 and the
// creeper explodes 30 ticks after the goal starts).
const creeperFuse = 30

func (b *mobBrain) tickCreeper() {
	b.swell += b.swellDir
	if b.swell < 0 {
		b.swell = 0
	}
	if b.swell >= creeperFuse {
		b.swell = creeperFuse
		m := b.m
		power := float32(3)
		if b.charged {
			power = 6
		}
		b.act(func() {
			if m.removed.Load() || m.liv.isDead() {
				return
			}
			m.Remove()
			m.w.explode(explodeOpts{center: m.feet(), power: power, kind: explodeMob, direct: m, causing: m})
		})
		b.swellDir = 0
	}
}

// --- target selection ---

type hurtByGoal struct {
	goalBase
	seenAt int64
}

func (g *hurtByGoal) CanUse() bool {
	b := g.b
	if b.lastBy == 0 || b.lastByAt <= g.seenAt {
		return false
	}
	t := b.cx.a.byID(b.lastBy)
	return t != nil && t.alive && (!t.isPlayer() || t.targetOK) && t.m != b.m
}
func (g *hurtByGoal) Start() {
	g.seenAt = g.b.lastByAt
	g.b.setTarget(g.b.cx.a.byID(g.b.lastBy))
}
func (g *hurtByGoal) CanContinue() bool { return g.b.keepTarget(false) }

type nearestTargetGoal struct {
	goalBase
	match    func(e *aiEnt) bool
	mustSee  bool
	interval int
	unseen   int
	when     func() bool
}

func (g *nearestTargetGoal) CanUse() bool {
	b := g.b
	if g.interval > 0 && !b.chance(g.interval/2) {
		return false
	}
	if g.when != nil && !g.when() {
		return false
	}
	if b.m.typ.Category == "monster" && b.cx.a.diff == api.DifficultyPeaceful {
		return false
	}
	r := b.follow
	best, bd := (*aiEnt)(nil), math.MaxFloat64
	p := b.pos()
	b.cx.a.near(p, r, func(e *aiEnt) {
		if e.m == b.m || !e.alive || math.Abs(e.pos.Y-p.Y) > 4+r/4 || !g.match(e) {
			return
		}
		if e.isPlayer() && !e.targetOK {
			return
		}
		if d := dist2(e.pos, p); d < bd && d <= r*r {
			best, bd = e, d
		}
	})
	if best == nil || g.mustSee && !b.canSee(best) {
		return false
	}
	g.b.setTarget(best)
	return true
}
func (g *nearestTargetGoal) CanContinue() bool {
	b := g.b
	if !b.keepTarget(g.mustSee) {
		return false
	}
	if g.when != nil && !g.when() {
		return false
	}
	return true
}
func (g *nearestTargetGoal) Stop() { g.b.target = 0 }

// keepTarget checks a current target is still valid: alive, within follow
// range and (if mustSee) seen within the last 60 ticks.
func (b *mobBrain) keepTarget(mustSee bool) bool {
	t := b.targetEnt()
	if t == nil || !t.alive || t.isPlayer() && !t.targetOK {
		b.target = 0
		return false
	}
	if dist2(t.pos, b.pos()) > b.follow*b.follow {
		b.target = 0
		return false
	}
	if mustSee && b.ticks%10 == 0 && !b.canSee(t) {
		if b.ticks-b.targetAt > 60 {
			b.target = 0
			return false
		}
	} else if mustSee && b.ticks%10 == 0 {
		b.targetAt = b.ticks
	}
	return true
}

// nearest returns the id of the nearest snapshot entity within r matching fn.
func (b *mobBrain) nearest(r float64, fn func(e *aiEnt) bool) int32 {
	p := b.pos()
	var id int32
	bd := r * r
	b.cx.a.near(p, r, func(e *aiEnt) {
		if e.m == b.m || !fn(e) {
			return
		}
		if d := dist2(e.pos, p); d <= bd {
			id, bd = e.id, d
		}
	})
	return id
}
