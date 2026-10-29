package server

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// Level events and ranges used while digging.
const (
	levelDestroyProgress      = 2019
	levelDestroyProgressSound = 2020
	destroyProgressRangeSq    = 32 * 32
	levelEventRangeSq         = 64 * 64
	finishTolerance           = 0.7
	blockInteractRange        = 4.5
	creativeInteractBonus     = 0.5
)

// digMachine is the per-player block breaking state of ServerPlayerGameMode.
// It is owned by the player phase of the tick goroutine.
type digMachine struct {
	ticks      int32
	destroying bool
	pos        api.BlockPos
	face       int32
	start      int32
	delayed    bool
	delayedPos api.BlockPos
	lastSent   int32
	init       bool
}

func (p *Player) digInputs() digInputs {
	p.inv.mu.Lock()
	in := digInputs{main: p.inv.hand(api.MainHand)}
	in.equip[0] = in.main
	in.equip[1] = p.inv.get(invOffhand)
	for i := range 4 {
		in.equip[2+i] = p.inv.get(invArmorFeet + i)
	}
	p.inv.mu.Unlock()
	if p.use.effects != nil {
		for _, e := range []struct {
			name string
			dst  *int32
		}{{"haste", &in.haste}, {"conduit_power", &in.conduit}, {"mining_fatigue", &in.fatigue}} {
			if id, ok := item.BuiltinID("mob_effect", e.name); ok {
				if a := p.use.effects[id]; a != nil {
					*e.dst = a.amplifier + 1
				}
			}
		}
	}
	p.mu.Lock()
	in.ground = p.e.OnGround
	pos := p.e.Pos
	eye := 1.62
	if p.sneaking {
		eye = 1.27
	}
	p.mu.Unlock()
	if w := p.world.Load(); w != nil {
		in.water = eyeInWater(w.w, pos.X, pos.Y+eye, pos.Z)
	}
	return in
}

func (p *Player) progressAt(w *World, pos api.BlockPos) (float32, data.StateID) {
	st := w.w.Block(pos.X, pos.Y, pos.Z)
	in := p.digInputs()
	return destroyProgress(st, &in), st
}

// blockReach is Player.isWithinBlockInteractionRange(pos, 1.0).
func (p *Player) blockReach(pos api.BlockPos) bool {
	r := blockInteractRange + 1.0
	if p.GameMode() == api.Creative {
		r += creativeInteractBonus
	}
	eye := p.EyeLocation().Pos
	dx := math.Max(math.Max(float64(pos.X)-eye.X, eye.X-float64(pos.X+1)), 0)
	dy := math.Max(math.Max(float64(pos.Y)-eye.Y, eye.Y-float64(pos.Y+1)), 0)
	dz := math.Max(math.Max(float64(pos.Z)-eye.Z, eye.Z-float64(pos.Z+1)), 0)
	return dx*dx+dy*dy+dz*dz < r*r
}

// actionRestricted is Player.blockActionRestricted.
func (p *Player) actionRestricted(w *World, pos api.BlockPos, gm api.GameMode) bool {
	switch gm {
	case api.Spectator:
		return true
	case api.Adventure:
		main := p.Hand(api.MainHand)
		if main.IsEmpty() {
			return true
		}
		return !canBreakAdventure(main, w.w.Block(pos.X, pos.Y, pos.Z))
	}
	return false
}

// digEnv is what the breaking state machine reads and does; the player
// implements it for the live server, tests with a model.
type digEnv interface {
	air(pos api.BlockPos) bool
	progress(pos api.BlockPos) float32
	creative() bool
	restricted(pos api.BlockPos) bool
	mayInteract(pos api.BlockPos) bool
	// interactDenied fires the plugin left-click event.
	interactDenied(pos api.BlockPos, face int32) bool
	resync(pos api.BlockPos)
	sendProgress(pos api.BlockPos, stage int32)
	hitEvent(id int32, pos api.BlockPos, face int32)
	destroyAndAck(pos api.BlockPos)
	destroy(pos api.BlockPos) bool
}

// action is ServerPlayerGameMode.handleBlockBreakAction after the reach
// and build height checks.
func (d *digMachine) action(env digEnv, pos api.BlockPos, action, face int32) {
	if !d.init {
		d.lastSent, d.init = -1, true
	}
	switch action {
	case digStart:
		if !env.mayInteract(pos) || env.interactDenied(pos, face) {
			env.resync(pos)
			return
		}
		if env.creative() {
			env.destroyAndAck(pos)
			return
		}
		if env.restricted(pos) {
			env.resync(pos)
			return
		}
		d.start = d.ticks
		prog := float32(1)
		air := env.air(pos)
		if !air {
			prog = env.progress(pos)
		}
		if !air && prog >= 1 {
			env.destroyAndAck(pos)
			return
		}
		if d.destroying {
			env.resync(d.pos)
		}
		d.destroying, d.pos, d.face = true, pos, face
		j := int32(prog * 10)
		env.sendProgress(pos, j)
		d.lastSent = j
	case digFinish:
		if pos != d.pos || env.air(pos) {
			return
		}
		i := d.ticks - d.start
		if env.progress(pos)*float32(i+1) >= finishTolerance {
			d.destroying = false
			env.sendProgress(pos, -1)
			env.destroyAndAck(pos)
			return
		}
		if !d.delayed {
			d.destroying = false
			d.delayed, d.delayedPos = true, pos
		}
	case digChangeDirection:
		// Only retargets the face of the hit events (observed black-box).
		d.face = face
	case digAbort:
		d.destroying = false
		if pos != d.pos {
			env.resync(d.pos)
		}
		env.sendProgress(pos, -1)
	}
}

// tick is ServerPlayerGameMode.tick.
func (d *digMachine) tick(env digEnv) {
	if !d.init {
		d.lastSent, d.init = -1, true
	}
	d.ticks++
	switch {
	case d.delayed:
		if env.air(d.delayedPos) {
			d.delayed = false
			return
		}
		// A delayed break is re-evaluated at the progress of a single tick,
		// so it only completes for blocks broken within one tick (observed
		// black-box; see dig_ref_test.go).
		f := env.progress(d.delayedPos)
		if j := int32(f * 10); j != d.lastSent {
			env.sendProgress(d.delayedPos, j)
			d.lastSent = j
		}
		if f >= 1 {
			d.delayed = false
			env.destroy(d.delayedPos)
		}
	case d.destroying:
		if env.air(d.pos) {
			env.sendProgress(d.pos, -1)
			d.lastSent, d.destroying = -1, false
			return
		}
		i := d.ticks - d.start
		f := env.progress(d.pos) * float32(i+1)
		if j := int32(f * 10); j != d.lastSent {
			env.sendProgress(d.pos, j)
			d.lastSent = j
		}
		ev := int32(levelDestroyProgress)
		if i%4 == 0 {
			ev = levelDestroyProgressSound
		}
		env.hitEvent(ev, d.pos, d.face)
	}
}

// playerDig adapts a Player in world w to digEnv.
type playerDig struct {
	p *Player
	w *World
}

func (e playerDig) air(pos api.BlockPos) bool { return data.IsAir(e.w.w.Block(pos.X, pos.Y, pos.Z)) }
func (e playerDig) progress(pos api.BlockPos) float32 {
	f, _ := e.p.progressAt(e.w, pos)
	return f
}
func (e playerDig) creative() bool { return e.p.GameMode() == api.Creative }
func (e playerDig) restricted(pos api.BlockPos) bool {
	return e.p.actionRestricted(e.w, pos, e.p.GameMode())
}
func (e playerDig) mayInteract(pos api.BlockPos) bool      { return e.p.mayInteract(e.w, pos) }
func (e playerDig) resync(pos api.BlockPos)                { e.p.resync(e.w, pos) }
func (e playerDig) sendProgress(pos api.BlockPos, j int32) { e.w.destroyProgress(e.p, pos, j) }
func (e playerDig) hitEvent(id int32, pos api.BlockPos, face int32) {
	e.w.levelEvent(nil, id, pos, face)
}
func (e playerDig) destroyAndAck(pos api.BlockPos) { e.p.destroyAndAck(e.w, pos) }
func (e playerDig) destroy(pos api.BlockPos) bool  { return e.p.destroyBlock(e.w, pos) }
func (e playerDig) interactDenied(pos api.BlockPos, face int32) bool {
	return e.p.interact(api.LeftClickBlock, &block{w: e.w, pos: pos}, api.BlockFace(face), api.MainHand, api.Vec3{})
}

// digAction handles a START/ABORT/STOP_DESTROY_BLOCK action.
func (p *Player) digAction(w *World, pos api.BlockPos, action, face int32) {
	if !p.blockReach(pos) {
		return
	}
	if !w.w.Dim().Contains(pos.Y) {
		p.resync(w, pos)
		return
	}
	p.dg.action(playerDig{p, w}, pos, action, face)
}

// digTick advances the player's breaking state by one tick.
func (p *Player) digTick() {
	if w := p.world.Load(); w != nil {
		p.dg.tick(playerDig{p, w})
	}
}

// destroyProgress is ServerLevel.destroyBlockProgress: every other player
// within 32 blocks sees the crack stage of breaker's block.
func (w *World) destroyProgress(breaker *Player, pos api.BlockPos, stage int32) {
	raw := encode(&packets.ClientboundBlockDestruction{EntityID: breaker.e.ID, Location: protoPos(pos), DestroyStage: int8(stage)})
	for _, o := range w.playerList() {
		if o == breaker {
			continue
		}
		l := o.Location().Pos
		dx, dy, dz := float64(pos.X)-l.X, float64(pos.Y)-l.Y, float64(pos.Z)-l.Z
		if dx*dx+dy*dy+dz*dz < destroyProgressRangeSq {
			o.c.SendRaw(raw)
		}
	}
}

// levelEvent is Level.levelEvent(except, ...): players within 64 blocks
// other than except.
func (w *World) levelEvent(except *Player, id int32, pos api.BlockPos, d int32) {
	raw := encode(&packets.ClientboundLevelEvent{EffectID: id, Location: protoPos(pos), Data: d})
	c := pos.Center()
	for _, o := range w.playerList() {
		if o == except {
			continue
		}
		if o.Location().Pos.DistanceSq(c) < levelEventRangeSq {
			o.c.SendRaw(raw)
		}
	}
}

func protoPos(p api.BlockPos) proto.BlockPos {
	return proto.BlockPos{X: int32(p.X), Y: int32(p.Y), Z: int32(p.Z)}
}

// mayInteract is ServerLevel.mayInteract: spawn protection for non-ops.
func (p *Player) mayInteract(w *World, pos api.BlockPos) bool {
	r := p.s.props.SpawnProtection
	if r <= 0 || w != p.s.defaultWorld() || p.IsOp() || p.s.ops.empty() {
		return true
	}
	sp := w.Spawn().Pos
	sx, sz := int(math.Floor(sp.X)), int(math.Floor(sp.Z))
	return max(abs(pos.X-sx), abs(pos.Z-sz)) > r
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// canBreakAdventure evaluates the can_break component (block and state
// parts of each block predicate; NBT and component matchers never match
// since block entities are not modelled).
func canBreakAdventure(st item.Stack, s data.StateID) bool {
	v, ok := st.Component(item.CanBreak)
	if !ok {
		return false
	}
	b := data.BlockOf(s)
	if b == nil {
		return false
	}
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	if err != nil {
		return false
	}
	for range n {
		match := true
		if ok, _ := r.Bool(); ok {
			var h item.HolderSet
			k, err := r.VarInt()
			if err != nil {
				return false
			}
			if k == 0 {
				if h.Tag, err = r.String(32767); err != nil {
					return false
				}
			} else {
				for range k - 1 {
					id, _ := r.VarInt()
					h.IDs = append(h.IDs, id)
				}
			}
			match = match && blockInSet(h, b.ID)
		}
		if ok, _ := r.Bool(); ok {
			np, err := r.VarInt()
			if err != nil {
				return false
			}
			for range np {
				name, _ := r.String(32767)
				exact, _ := r.Bool()
				val, have := data.Canonical().Value(s, name)
				if exact {
					want, _ := r.String(32767)
					match = match && have && val == want
					continue
				}
				var lo, hi string
				var hasLo, hasHi bool
				if hasLo, _ = r.Bool(); hasLo {
					lo, _ = r.String(32767)
				}
				if hasHi, _ = r.Bool(); hasHi {
					hi, _ = r.String(32767)
				}
				match = match && have && propInRange(b, name, val, lo, hasLo, hi, hasHi)
			}
		}
		if ok, _ := r.Bool(); ok {
			return false
		}
		ne, err := r.VarInt()
		if err != nil || ne != 0 {
			return false
		}
		np, err := r.VarInt()
		if err != nil || np != 0 {
			return false
		}
		if match {
			return true
		}
	}
	return false
}

// propInRange compares by the property's value order, as
// StatePropertiesPredicate does for ranged matchers.
func propInRange(b *data.Block, prop, val, lo string, hasLo bool, hi string, hasHi bool) bool {
	pi := b.PropIndex(prop)
	if pi < 0 {
		return false
	}
	pr := &b.Props[pi]
	v := pr.ValueIndex(val)
	if hasLo {
		if l := pr.ValueIndex(lo); l < 0 || v < l {
			return false
		}
	}
	if hasHi {
		if h := pr.ValueIndex(hi); h < 0 || v > h {
			return false
		}
	}
	return true
}

func (l *opList) empty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.ops) == 0
}
