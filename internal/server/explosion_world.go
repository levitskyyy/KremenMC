package server

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

type explodeOpts struct {
	center          api.Vec3
	power           float32
	fire            bool
	kind            explosionKind
	direct, causing api.Entity
	calc            *exCalc
}

// explode is ServerLevel.explode: run the explosion, then send the explode
// packet to every player within 64 blocks.
func (w *World) explode(o explodeOpts) *explosion {
	// Explosion.getIndirectSourceEntity: a living source causes its own
	// explosion; primed TNT passes its owner.
	causing := o.causing
	if m, ok := o.direct.(*Mob); ok && causing == nil && entity.HasAttributes(m.typ.Name) {
		causing = m
	}
	ex := &explosion{
		center: o.center, radius: o.power, fire: o.fire, interaction: w.blockInteraction(o.kind),
		direct: o.direct, causing: causing, rnd: w.levelRandom(), calc: o.calc,
	}
	ex.run(&worldExLevel{w: w, ex: ex})
	w.sendExplode(ex)
	return ex
}

// blockInteraction maps the explosion kind to a block interaction through
// the 26.3 game rules.
func (w *World) blockInteraction(k explosionKind) blockInteraction {
	decay := func(rule string, def bool) blockInteraction {
		if w.gameRule(rule, def) {
			return exDestroyWithDecay
		}
		return exDestroy
	}
	switch k {
	case explodeBlock:
		return decay("block_explosion_drop_decay", true)
	case explodeMob:
		if !w.gameRule("mob_griefing", true) {
			return exKeep
		}
		return decay("mob_explosion_drop_decay", true)
	case explodeTNT:
		return decay("tnt_explosion_drop_decay", false)
	case explodeTrigger:
		return exTriggerBlock
	}
	return exKeep
}

// gameRule reads a boolean game rule by its 26.3 id (or legacy camelCase).
func (w *World) gameRule(id string, def bool) bool {
	camel := id
	if i := strings.IndexByte(id, '_'); i >= 0 {
		var b strings.Builder
		up := false
		for _, r := range id {
			switch {
			case r == '_':
				up = true
			case up:
				b.WriteString(strings.ToUpper(string(r)))
				up = false
			default:
				b.WriteRune(r)
			}
		}
		camel = b.String()
	}
	w.mu.RLock()
	v, ok := w.rules[id]
	if !ok {
		v, ok = w.rules[camel]
	}
	w.mu.RUnlock()
	if !ok {
		return def
	}
	return v != "false"
}

func (w *World) sendExplode(ex *explosion) {
	tail := explodeTail(ex.small())
	for _, p := range w.playerList() {
		if p.Location().Pos.DistanceSq(ex.center) >= 4096 {
			continue
		}
		pk := &packets.ClientboundExplode{
			Center: packets.Vec3f64{X: ex.center.X, Y: ex.center.Y, Z: ex.center.Z},
			Radius: ex.radius, BlockCount: int32(ex.blocks), Raw: tail,
		}
		for t, v := range ex.hitPlayers {
			if tp, ok := t.(exPlayer); ok && tp.p == p {
				pk.PlayerKnockback = proto.Opt[packets.Vec3f64]{Ok: true, V: packets.Vec3f64{X: v.X, Y: v.Y, Z: v.Z}}
			}
		}
		p.send(pk)
	}
}

// levelRandom is the world's gameplay random (explosions, TNT priming,
// fire placement, popResource), shared with the drop code's level random.
// Each call locks, so it is safe from any goroutine.
func (w *World) levelRandom() rng.Source {
	r := w.lootState()
	return &lockedRandom{mu: &r.mu, r: r.rand}
}

type lockedRandom struct {
	mu *sync.Mutex
	r  *rng.Legacy
}

func (l *lockedRandom) Fork() rng.Source {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Fork()
}

func (l *lockedRandom) ForkPositional() rng.Positional {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.ForkPositional()
}

func (l *lockedRandom) SetSeed(s int64) { l.mu.Lock(); l.r.SetSeed(s); l.mu.Unlock() }
func (l *lockedRandom) NextInt() int32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextInt()
}

func (l *lockedRandom) NextIntn(n int32) int32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextIntn(n)
}

func (l *lockedRandom) NextIntRange(a, b int32) int32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextIntRange(a, b)
}

func (l *lockedRandom) NextIntBetweenInclusive(a, b int32) int32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextIntBetweenInclusive(a, b)
}

func (l *lockedRandom) NextLong() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextLong()
}

func (l *lockedRandom) NextBool() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextBool()
}

func (l *lockedRandom) NextFloat() float32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextFloat()
}

func (l *lockedRandom) NextDouble() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextDouble()
}

func (l *lockedRandom) NextGaussian() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.NextGaussian()
}

func (l *lockedRandom) Triangle(m, d float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Triangle(m, d)
}

func (l *lockedRandom) TriangleF(m, d float32) float32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.TriangleF(m, d)
}

func (l *lockedRandom) Consume(n int) { l.mu.Lock(); l.r.Consume(n); l.mu.Unlock() }

// worldExLevel adapts a World to exLevel. World block writes are applied by
// the owning region later, so the explosion reads its own writes through
// an overlay.
type worldExLevel struct {
	w       *World
	ex      *explosion
	written map[api.BlockPos]data.StateID
}

func (l *worldExLevel) Block(x, y, z int) data.StateID {
	if s, ok := l.written[api.BlockPos{X: x, Y: y, Z: z}]; ok {
		return s
	}
	return l.w.w.Block(x, y, z)
}

func (l *worldExLevel) worldBounds() (int, int) { return l.w.MinY(), l.w.MaxY() }

func (l *worldExLevel) setBlock(p api.BlockPos, s data.StateID) {
	if l.written == nil {
		l.written = map[api.BlockPos]data.StateID{}
	}
	l.written[p] = s
	l.w.w.SetBlock(p.X, p.Y, p.Z, s)
}
func (l *worldExLevel) primeExploded(p api.BlockPos, ex *explosion) { l.w.primeExploded(p, ex) }

func (l *worldExLevel) targets(box entity.AABB) []exTarget {
	var out []exTarget
	for _, p := range l.w.playerList() {
		if p.IsDead() {
			continue
		}
		mn, mx := p.BoundingBox()
		if (entity.AABB{MinX: mn.X, MinY: mn.Y, MinZ: mn.Z, MaxX: mx.X, MaxY: mx.Y, MaxZ: mx.Z}).Intersects(box) {
			out = append(out, exPlayer{p})
		}
	}
	for _, m := range l.w.mobList() {
		if m == l.ex.direct || !m.IsValid() {
			continue
		}
		m.mu.Lock()
		b := m.e.Box()
		m.mu.Unlock()
		if b.Intersects(box) {
			out = append(out, exMob{m})
		}
	}
	return out
}

// blockDrops is Block.getDrops for an explosion: no tool, the explosion
// radius only when drops decay. Loot draws come from the tables' random
// sequences, not the level random.
func (l *worldExLevel) blockDrops(p api.BlockPos, s data.StateID, ex *explosion) []item.Stack {
	var radius float32
	if ex.interaction == exDestroyWithDecay {
		radius = ex.radius
	}
	return l.w.BlockLoot(p, s, item.Stack{}, nil, radius)
}

func (l *worldExLevel) popResource(p api.BlockPos, st item.Stack) {
	popResourceAt(l.w, p, st, l.ex.rnd)
}

// popResourceAt is Block.popResource for explosion drops. Unlike
// World.PopResource it draws the offsets from the level random even when
// block_drops is off, as the reference does.
func popResourceAt(w *World, p api.BlockPos, st item.Stack, rnd rng.Source) {
	at := exPopOffset(p, rnd)
	if st.IsEmpty() || !w.gameRule("block_drops", true) {
		return
	}
	w.SpawnItem(at, st, itemPickupDefault)
}

// exMob and exPlayer adapt entities to exTarget.
type exMob struct{ m *Mob }

func (t exMob) exPos() api.Vec3 { return t.m.Location().Pos }

func (t exMob) exAim() api.Vec3 {
	p := t.m.Location().Pos
	if t.m.typ.Name != "tnt" {
		p.Y += float64(float32(t.m.eyeY())) // eye heights are floats
	}
	return p
}

func (t exMob) exBox() entity.AABB {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	return t.m.e.Box()
}

func (t exMob) exImmune() bool { return !t.m.IsValid() }

func (t exMob) exHurt(ex *explosion, amount float32) {
	t.m.Hurt(ExplosionDamage(ex.center, ex.direct, ex.causing), amount)
}

func (t exMob) exPush(v api.Vec3) {
	cur := t.m.Velocity()
	t.m.setVel(api.Vec3{X: cur.X + v.X, Y: cur.Y + v.Y, Z: cur.Z + v.Z})
}

func (t exMob) exKnockbackResistance() (float64, bool) { return knockbackRes(t.m.lv()) }
func (t exMob) exRecordsKnockback() bool               { return false }

func knockbackRes(l *living) (float64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attrs == nil {
		return 0, false
	}
	return l.attrs.Value(entity.AttrExplosionKnockbackResistance), true
}

type exPlayer struct{ p *Player }

func (t exPlayer) exPos() api.Vec3 { return t.p.Location().Pos }

func (t exPlayer) exAim() api.Vec3 {
	p := t.p.Location().Pos
	p.Y += float64(float32(t.p.eyeY()))
	return p
}

func (t exPlayer) exBox() entity.AABB {
	mn, mx := t.p.BoundingBox()
	return entity.AABB{MinX: mn.X, MinY: mn.Y, MinZ: mn.Z, MaxX: mx.X, MaxY: mx.Y, MaxZ: mx.Z}
}

func (t exPlayer) exImmune() bool { return t.p.GameMode() == api.Spectator }

func (t exPlayer) exHurt(ex *explosion, amount float32) {
	t.p.Hurt(ExplosionDamage(ex.center, ex.direct, ex.causing), amount)
}

// exPush is a no-op: player velocity is client-side, and the client
// applies the knockback carried in the explode packet.
func (t exPlayer) exPush(api.Vec3) {}

func (t exPlayer) exKnockbackResistance() (float64, bool) { return knockbackRes(t.p.lv()) }

func (t exPlayer) exRecordsKnockback() bool {
	gm := t.p.GameMode()
	return gm != api.Spectator && !(gm == api.Creative && t.p.Flying())
}
