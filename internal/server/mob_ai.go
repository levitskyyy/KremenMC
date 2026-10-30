package server

import (
	"math"
	"math/rand/v2"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/entity/ai"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Mob AI runs inside the world's region phases (world.Hooks.RegionTick):
// the tick goroutine buckets the mobs by region and takes an immutable
// snapshot of players and mobs (aiPrepare), each region worker then runs
// the brains of its own mobs reading blocks through its TickContext, and
// side effects that touch other entities or global state (attacks,
// explosions, projectiles, spawns, block edits outside the region) are
// queued and applied on the tick goroutine after the phases (aiApply).

// AIHooks lets the mob-data and interaction code feed the AI. All fields
// are optional; they are called from region workers unless noted.
type AIHooks struct {
	// TemptItem reports whether item (e.g. "minecraft:wheat") tempts
	// mobs of type typ ("cow"); default: item tag #minecraft:<typ>_food.
	TemptItem func(typ, item string) bool
	// Breed spawns the offspring of a and b (tick goroutine); default:
	// a baby of a's type at a's position.
	Breed func(a, b *Mob) *Mob
	// EggItem is the item a chicken lays (default minecraft:egg).
	EggItem func(m *Mob) string
	// AteGrass is called after a sheep ate grass (tick goroutine).
	AteGrass func(m *Mob)
}

var aiHooks AIHooks

// SetAIHooks installs the hooks; call before the server starts ticking.
func SetAIHooks(h AIHooks) { aiHooks = h }

// aiEnt is the per-tick snapshot of an entity other brains may perceive.
type aiEnt struct {
	m        *Mob
	p        *Player
	id       int32
	uuid     api.UUID
	pos      api.Vec3
	w, h     float64
	eye      float64
	yaw      float64
	pitch    float64
	typ      string // type name without namespace ("player" for players)
	monster  bool
	alive    bool
	targetOK bool // players: survival/adventure and alive
	baby     bool
	love     bool
	owner    api.UUID
	held     [2]string
	pumpkin  bool
	lastBy   int32 // players: the entity that last hurt them
}

// aiExtraPlayers adds fake player entries to the snapshot (tests only).
var aiExtraPlayers func(w *World) []aiEnt

func (e *aiEnt) isPlayer() bool { return e.typ == "player" }

func (e *aiEnt) combatant() combatant {
	if e.p != nil {
		return e.p
	}
	return e.m
}

type aiBucket struct {
	mobs []*Mob
	acts []func()
}

type aiWorld struct {
	ents    []aiEnt
	np      int // players (and test fakes) come first
	ids     map[int32]int32
	cells   map[uint64][]int32
	buckets map[uint64]*aiBucket
	pool    []*aiBucket
	day     bool
	diff    api.Difficulty
	grief   bool
	raining bool
	tick    int64
}

const aiCell = 32

func cellKey(x, z float64) uint64 {
	return uint64(uint32(int32(math.Floor(x))>>5))<<32 | uint64(uint32(int32(math.Floor(z))>>5))
}

// near calls fn for every snapshot entity within r (horizontal box) of p.
func (a *aiWorld) near(p api.Vec3, r float64, fn func(e *aiEnt)) {
	x0, x1 := int32(math.Floor(p.X-r))>>5, int32(math.Floor(p.X+r))>>5
	z0, z1 := int32(math.Floor(p.Z-r))>>5, int32(math.Floor(p.Z+r))>>5
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			for _, i := range a.cells[uint64(uint32(cx))<<32|uint64(uint32(cz))] {
				e := &a.ents[i]
				if math.Abs(e.pos.X-p.X) <= r && math.Abs(e.pos.Z-p.Z) <= r {
					fn(e)
				}
			}
		}
	}
}

func (a *aiWorld) byID(id int32) *aiEnt {
	if i, ok := a.ids[id]; ok {
		return &a.ents[i]
	}
	return nil
}

// aiPrepare runs on the tick goroutine before the region phases.
func (w *World) aiPrepare(players []*Player) {
	a := &w.ai
	if a.cells == nil {
		a.cells, a.buckets, a.ids = map[uint64][]int32{}, map[uint64]*aiBucket{}, map[int32]int32{}
	}
	clear(a.ids)
	for k, b := range a.buckets {
		b.mobs, b.acts = b.mobs[:0], b.acts[:0]
		a.pool = append(a.pool, b)
		delete(a.buckets, k)
	}
	for k, v := range a.cells {
		if len(v) == 0 {
			delete(a.cells, k)
		} else {
			a.cells[k] = v[:0]
		}
	}
	a.ents = a.ents[:0]
	t := w.Time()
	a.raining = w.Storming()
	a.day = (t < 12542 || t > 23460) && w.dim == dimOverworld
	a.diff = w.Difficulty()
	a.grief = w.gameRule("mob_griefing", true)
	a.tick = w.s.CurrentTick()
	add := func(e aiEnt) {
		i := int32(len(a.ents))
		a.ents = append(a.ents, e)
		a.ids[e.id] = i
		k := cellKey(e.pos.X, e.pos.Z)
		a.cells[k] = append(a.cells[k], i)
	}
	for _, p := range players {
		if p.world.Load() != w {
			continue
		}
		l := p.Location()
		gm := p.GameMode()
		dead := p.IsDead()
		e := aiEnt{p: p, id: p.EntityID(), uuid: p.UUID(), pos: l.Pos, w: 0.6, h: 1.8, eye: 1.62,
			yaw: float64(l.Yaw), pitch: float64(l.Pitch), typ: "player", alive: !dead,
			targetOK: !dead && (gm == api.Survival || gm == api.Adventure)}
		e.held[0] = item.NameOf(p.Hand(api.MainHand).Item)
		e.held[1] = item.NameOf(p.Hand(api.OffHand).Item)
		e.pumpkin = item.NameOf(p.gear()[slotHead].Item) == "minecraft:carved_pumpkin"
		p.liv.mu.Lock()
		if p.liv.lastBy != nil && a.tick-p.liv.lastByAt < 100 {
			e.lastBy = p.liv.lastBy.EntityID()
		}
		p.liv.mu.Unlock()
		add(e)
	}
	if aiExtraPlayers != nil {
		for _, e := range aiExtraPlayers(w) {
			add(e)
		}
	}
	np := len(a.ents)
	a.np = np
	for _, m := range w.mobList() {
		if m.removed.Load() || m.liv.attrs == nil || m.proj != nil || m.itm != nil {
			continue
		}
		if m.ai == nil {
			m.ai = newBrain(m)
		}
		b := m.ai
		m.mu.Lock()
		pos, wd, ht := m.e.Pos, m.e.Width, m.e.Height
		m.mu.Unlock()
		alive := !m.liv.isDead()
		add(aiEnt{m: m, id: m.e.ID, uuid: m.e.UUID, pos: pos, w: wd, h: ht, eye: ht * 0.85,
			typ: m.typ.Name, monster: m.typ.Category == "monster", alive: alive,
			baby: b.age < 0, love: b.love > 0, owner: b.owner})
		if b.none || !alive || m.onTick != nil {
			continue
		}
		// vanilla stops idle wandering after 100 ticks without a player
		// within 32 blocks (reference wander scene, docs/vanilla-parity.md)
		near := false
		for i := range np {
			if a.ents[i].pos.DistanceSq(pos) < 32*32 {
				near = true
				break
			}
		}
		if near {
			b.noAction = 0
		} else {
			b.noAction++
		}
		k := world.ChunkAt(int(math.Floor(pos.X)), int(math.Floor(pos.Z))).Region().Key()
		bk := a.buckets[k]
		if bk == nil {
			if n := len(a.pool); n > 0 {
				bk, a.pool = a.pool[n-1], a.pool[:n-1]
			} else {
				bk = &aiBucket{}
			}
			a.buckets[k] = bk
		}
		bk.mobs = append(bk.mobs, m)
	}
}

// aiRegion is the RegionTick hook: run the brains of the region's mobs.
func (w *World) aiRegion(t *world.TickContext) {
	bk := w.ai.buckets[t.Region().Pos.Key()]
	if bk == nil {
		return
	}
	cx := aiCtx{w: w, a: &w.ai, src: tcSource{t}, bucket: bk}
	for _, m := range bk.mobs {
		if !m.removed.Load() {
			m.ai.tick(&cx)
		}
	}
}

// aiApply runs the queued side effects on the tick goroutine.
func (w *World) aiApply() {
	for _, bk := range w.ai.buckets {
		for _, f := range bk.acts {
			f()
		}
		bk.acts = bk.acts[:0]
	}
}

type tcSource struct{ t *world.TickContext }

func (s tcSource) Block(x, y, z int) data.StateID { return s.t.Block(x, y, z) }

type aiCtx struct {
	w      *World
	a      *aiWorld
	src    entity.BlockSource
	bucket *aiBucket
}

// mobBrain is the AI state of one mob. Touched only by the region worker
// running the mob's region (and by the tick goroutine between phases).
type mobBrain struct {
	m       *Mob
	none    bool
	body    ai.Body
	nav     ai.Nav
	goals   ai.Selector
	targets ai.Selector
	rnd     *rand.Rand
	cx      *aiCtx

	target    int32 // entity id of the attack target, 0 none
	targetAt  int64
	noAction  int
	health    float32
	maxHealth float32
	speed     float64
	follow    float64
	damage    float64
	lastBy    int32
	lastByAt  int64
	burning   bool
	hurtTime  int32

	// feedable state (see AIHooks and the exported Mob methods)
	age         int32 // < 0 baby
	love        int32
	breedCool   int32
	owner       api.UUID
	sitting     bool
	ownerHurt   int32 // last entity that hurt the owner
	ownerHit    int32 // last entity the owner hit
	swell       int32 // creeper
	swellDir    int32
	charged     bool
	carried     data.StateID // enderman
	angryAt     int32
	slimeSize   int32
	jumpDelay   int32
	eggTime     int32
	ticks       int64
	burnsInSun  bool
	temptRange  float64
	sentAggr    bool
	sentSwell   int32
	sentCarried data.StateID
	sentSit     bool
	mainHand    string
	climber     bool
}

func newBrain(m *Mob) *mobBrain {
	b := &mobBrain{m: m, rnd: rand.New(rand.NewPCG(rand.Uint64(), uint64(m.e.ID)))}
	b.body.E = m.e
	b.body.MaxHeadYaw = 75
	b.nav.Malus = &ai.DefaultMalus
	b.nav.MaxFall = 3
	if v, ok := m.AttributeValue("minecraft:jump_strength"); ok {
		b.body.JumpPower = v
	} else {
		b.body.JumpPower = 0.42
	}
	b.eggTime = int32(b.rnd.IntN(6000) + 6000)
	if !setupGoals(b) {
		b.none = true
	}
	return b
}

func (b *mobBrain) act(f func()) { b.cx.bucket.acts = append(b.cx.bucket.acts, f) }

func (b *mobBrain) chance(n int) bool { return b.rnd.IntN(n) == 0 }

func (b *mobBrain) pos() api.Vec3 { return b.m.e.Pos }

func (b *mobBrain) tick(cx *aiCtx) {
	m := b.m
	b.cx = cx
	m.liv.mu.Lock()
	if m.liv.dead {
		m.liv.mu.Unlock()
		return
	}
	at := m.liv.attrs
	b.health = m.liv.health
	b.maxHealth = float32(at.Value(entity.AttrMaxHealth))
	b.speed = at.Value(entity.AttrMovementSpeed)
	b.follow = at.Value(entity.AttrFollowRange)
	b.damage = at.Value(entity.AttrAttackDamage)
	b.burning = m.liv.fire > 0
	b.temptRange = at.Value(entity.AttrTemptRange)
	b.mainHand = item.NameOf(m.liv.equip[slotMain].Item)
	b.hurtTime = m.liv.hurtTime
	if m.liv.lastBy != nil {
		b.lastBy, b.lastByAt = m.liv.lastBy.EntityID(), m.liv.lastByAt
	}
	m.liv.mu.Unlock()
	m.life.mu.Lock()
	if m.life.tame {
		b.owner = m.life.owner
	}
	m.life.mu.Unlock()
	b.ticks++
	if b.age < 0 {
		b.age++
	} else if b.age > 0 {
		b.age--
	}
	if b.love > 0 {
		b.love--
	}
	if b.breedCool > 0 {
		b.breedCool--
	}
	b.body.Speed = b.speed
	b.nav.Range = b.follow
	if b.target != 0 {
		if t := cx.a.byID(b.target); t == nil || !t.alive || t.isPlayer() && !t.targetOK {
			b.target = 0
		}
	}
	b.preTick()
	m.mu.Lock()
	b.targets.Tick()
	b.goals.Tick()
	b.nav.Tick(&b.body)
	b.body.Tick()
	b.postMove()
	m.mu.Unlock()
	b.syncMeta()
}

// AI-driven synched data (indices from kremenmc.probe.ai.Meta): the mob
// flags byte 15 (aggressive 4, left-handed 2), creeper swell direction 16,
// enderman carried block 16, tamable flags 18 (sitting 1, tame 4). Sent
// as deltas when they change.
const (
	metaCreeperSwell = 16
	metaCarried      = 16
	metaTameFlags    = 18
	serOptBlock      = 15
)

func (b *mobBrain) syncMeta() {
	m := b.m
	var raw []byte
	aggr := b.target != 0
	if aggr != b.sentAggr {
		b.sentAggr = aggr
		m.life.mu.Lock()
		f := int32(0)
		if m.life.leftHanded {
			f |= 2
		}
		m.life.mu.Unlock()
		if aggr {
			f |= 4
		}
		raw = appendMeta(raw, metaMobFlags, serByte, f)
	}
	switch m.typ.Name {
	case "creeper":
		if d := b.swellDir; d != b.sentSwell {
			b.sentSwell = d
			if d == 0 {
				d = -1
			}
			raw = appendMeta(raw, metaCreeperSwell, serVarInt, d)
		}
	case "enderman":
		if b.carried != b.sentCarried {
			b.sentCarried = b.carried
			raw = appendMeta(raw, metaCarried, serOptBlock, int32(b.carried))
		}
	case "wolf", "cat", "parrot":
		if b.sitting != b.sentSit {
			b.sentSit = b.sitting
			f := int32(0)
			if b.sitting {
				f |= 1
			}
			if b.owner != (api.UUID{}) {
				f |= 4
			}
			raw = appendMeta(raw, metaTameFlags, serByte, f)
		}
	}
	if len(raw) == 0 {
		return
	}
	pk := encode(&packets.ClientboundSetEntityData{EntityID: m.e.ID, Raw: entity.EndMeta(raw)})
	b.act(func() { m.trk.Push(pk) })
}

// targetEnt returns the snapshot of the attack target.
func (b *mobBrain) targetEnt() *aiEnt {
	if b.target == 0 {
		return nil
	}
	return b.cx.a.byID(b.target)
}

func (b *mobBrain) setTarget(e *aiEnt) {
	if e == nil {
		b.target = 0
		return
	}
	b.target, b.targetAt = e.id, b.ticks
}

func (b *mobBrain) eyePos() api.Vec3 {
	p := b.m.e.Pos
	p.Y += b.m.e.Height * 0.85
	return p
}

// canSee is a block raycast between the eyes.
func (b *mobBrain) canSee(e *aiEnt) bool {
	to := e.pos
	to.Y += e.eye
	_, hit := entity.Raycast(b.cx.src, b.eyePos(), to)
	return !hit
}

func dist2(a, b api.Vec3) float64 { return a.DistanceSq(b) }

// moveNear paths toward p (the nav keeps the path while p stays in the
// same block).
func (b *mobBrain) moveNear(p api.Vec3, speed, reach float64) bool {
	return b.nav.MoveTo(b.cx.src, &b.body, p.X, p.Y, p.Z, speed, reach)
}

// randomPos picks a random standable position within h horizontally and
// v vertically (land mobs), avoiding water when dry is set.
func (b *mobBrain) randomPos(h, v int, dry bool) (api.Vec3, bool) {
	p := b.pos()
	src := b.cx.src
	for range 10 {
		x := int(math.Floor(p.X)) + b.rnd.IntN(2*h+1) - h
		z := int(math.Floor(p.Z)) + b.rnd.IntN(2*h+1) - h
		y0 := int(math.Floor(p.Y)) + b.rnd.IntN(2*v+1) - v
		for dy := 0; dy <= v; dy++ {
			for _, y := range [2]int{y0 - dy, y0 + dy} {
				if standable(src, x, y, z, dry) {
					return api.Vec3{X: float64(x) + 0.5, Y: float64(y), Z: float64(z) + 0.5}, true
				}
			}
		}
	}
	return api.Vec3{}, false
}

func standable(src entity.BlockSource, x, y, z int, dry bool) bool {
	feet, head, below := src.Block(x, y, z), src.Block(x, y+1, z), src.Block(x, y-1, z)
	if len(data.Collision(feet)) > 0 || len(data.Collision(head)) > 0 {
		return false
	}
	if dry && (data.Canonical().HasFluid(feet) || data.Canonical().HasFluid(below)) {
		return false
	}
	if len(data.Collision(below)) == 0 {
		return data.Canonical().HasFluid(feet) && !dry
	}
	bn := data.Canonical().BlockOf(below).Name
	return bn != "lava" && bn != "magma_block" && bn != "cactus" && !ai.IsWoodDoor(below)
}

// skyExposed reports whether block (x, y, z) sees the sky (sky light 15).
func (w *World) skyExposed(x, y, z int) bool {
	c := w.w.Chunk(world.ChunkAt(x, z))
	if c == nil || !c.Dim.Contains(y) {
		return y >= w.MaxY()
	}
	c.RLock()
	defer c.RUnlock()
	si := (y-c.Dim.MinY)>>4 + 1
	return c.SkyLight[si].Get((y&15)<<8|(z&15)<<4|x&15) >= 15
}

// blockLight is the block light at (x, y, z).
func (w *World) lightAt(x, y, z int) (sky, blk uint8) {
	c := w.w.Chunk(world.ChunkAt(x, z))
	if c == nil || !c.Dim.Contains(y) {
		return 15, 0
	}
	c.RLock()
	defer c.RUnlock()
	si := (y-c.Dim.MinY)>>4 + 1
	i := (y&15)<<8 | (z&15)<<4 | x&15
	return c.SkyLight[si].Get(i), c.BlockLight[si].Get(i)
}

// bright approximates "brightness > 0.5": daylight sky access or a block
// light of 8 or more.
func (b *mobBrain) bright() bool {
	p := b.eyePos()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Floor(p.Z))
	sky, blk := b.cx.w.lightAt(x, y, z)
	if blk >= 8 {
		return true
	}
	return b.cx.a.day && !b.cx.a.raining && sky >= 12
}

// preTick runs per-type upkeep before the goals (no mob lock held).
func (b *mobBrain) preTick() {
	m := b.m
	a := b.cx.a
	if b.burnsInSun && a.day && !a.raining && b.ticks%4 == 0 && !b.burning {
		p := b.eyePos()
		m.mu.Lock()
		wet := m.e.InWater
		m.mu.Unlock()
		if !wet && b.cx.w.skyExposed(int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Floor(p.Z))) {
			m.liv.mu.Lock()
			helmet := !m.liv.equip[slotHead].IsEmpty()
			m.liv.mu.Unlock()
			if !helmet {
				m.liv.ignite(160)
			}
		}
	}
	if b.owner != (api.UUID{}) {
		if o := b.ownerEnt(); o != nil && o.lastBy != 0 && o.lastBy != b.m.e.ID {
			b.ownerHurt = o.lastBy
		}
	}
	switch m.typ.Name {
	case "creeper":
		b.tickCreeper()
	case "enderman":
		b.endermanUpkeep()
	case "chicken":
		if b.age >= 0 {
			if b.eggTime--; b.eggTime <= 0 {
				b.eggTime = int32(b.rnd.IntN(6000) + 6000)
				p := b.pos()
				b.act(func() {
					name := "minecraft:egg"
					if aiHooks.EggItem != nil {
						name = aiHooks.EggItem(m)
					}
					if id, ok := item.IDOf(name); ok {
						m.w.spawnAtLocation(p, item.Stack{Item: id, Count: 1})
						m.w.playSoundAt(p, "minecraft:entity.chicken.egg", 7, 1, (b.rnd.Float32()-b.rnd.Float32())*0.2+1)
					}
				})
			}
		}
	}
}

// postMove runs with the mob lock held after the controls.
func (b *mobBrain) postMove() {
	e := b.m.e
	if b.climber && e.HorizontalCollision {
		e.Vel.Y = 0.2
		e.FallDistance = 0
	}
}

// --- exported hooks for the mob-data / interaction code ---

// InLove reports whether the mob is in love mode.
func (m *Mob) InLove() bool { return m.ai != nil && m.ai.love > 0 }

// SetInLove starts love mode for ticks (600 when fed); tick goroutine.
func (m *Mob) SetInLove(ticks int) {
	if b := m.brain(); b != nil && b.age == 0 && b.breedCool == 0 {
		b.love = int32(ticks)
	}
}

// CanBreed reports whether feeding the mob now would start love mode.
func (m *Mob) CanBreed() bool {
	b := m.brain()
	return b != nil && b.age == 0 && b.breedCool == 0 && b.love == 0
}

// AIAge is the ageable age: negative for babies (ticks until adult).
func (m *Mob) AIAge() int32 {
	if b := m.brain(); b != nil {
		return b.age
	}
	return 0
}

// SetAIAge sets the age (e.g. -24000 for a baby).
func (m *Mob) SetAIAge(age int32) {
	if b := m.brain(); b != nil {
		b.age = age
	}
}

// SetOwner tames the mob to owner (zero UUID untames).
func (m *Mob) SetOwner(owner api.UUID) {
	if b := m.brain(); b != nil {
		b.owner = owner
	}
}

func (m *Mob) Owner() api.UUID {
	if b := m.brain(); b != nil {
		return b.owner
	}
	return api.UUID{}
}

// SetSitting orders a tamed mob to sit or stand.
func (m *Mob) SetSitting(v bool) {
	if b := m.brain(); b != nil {
		b.sitting = v
	}
}

func (m *Mob) Sitting() bool { b := m.brain(); return b != nil && b.sitting }

// SetSlimeSize sets a slime or magma cube size (1, 2, 4).
func (m *Mob) SetSlimeSize(n int) {
	if b := m.brain(); b != nil {
		b.slimeSize = int32(n)
	}
}

func (m *Mob) brain() *mobBrain {
	if m.ai == nil && m.liv.attrs != nil && m.proj == nil && m.itm == nil {
		m.ai = newBrain(m)
	}
	return m.ai
}

// aiOwnerEvents feeds owner-defence goals; called from the damage pipeline
// on the tick goroutine when a player hurts or is hurt.
func (w *World) aiOwnerEvent(owner *Player, other api.Entity, ownerWasHurt bool) {
	if other == nil {
		return
	}
	id, u := other.EntityID(), owner.UUID()
	for _, m := range w.mobList() {
		if b := m.ai; b != nil && b.owner == u {
			if ownerWasHurt {
				b.ownerHurt = id
			} else {
				b.ownerHit = id
			}
		}
	}
}

// mobDied runs AI death effects (slime splitting) on the tick goroutine.
func mobDied(m *Mob) {
	b := m.ai
	if b == nil || b.slimeSize <= 1 || (m.typ.Name != "slime" && m.typ.Name != "magma_cube") {
		return
	}
	n := b.slimeSize / 2
	cnt := 2 + rand.IntN(3)
	p := m.feet()
	for i := range cnt {
		ox := (float64(i%2) - 0.5) * float64(b.slimeSize) / 4
		oz := (float64(i/2) - 0.5) * float64(b.slimeSize) / 4
		e, err := m.w.SpawnEntity(api.Key("minecraft:"+m.typ.Name), api.Location{World: m.w.name, Pos: api.Vec3{X: p.X + ox, Y: p.Y + 0.5, Z: p.Z + oz}, Yaw: rand.Float32() * 360}, nil)
		if err == nil {
			e.(*Mob).SetSlimeSize(int(n))
		}
	}
}

func entityEvent(id int32, status int8) *packets.ClientboundEntityEvent {
	return &packets.ClientboundEntityEvent{EntityID: id, EntityStatus: status}
}
