package server

import (
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Item entity constants (ItemEntity).
const (
	itemPickupDefault = 10
	itemPickupThrown  = 40
	itemNeverPickup   = 32767
	itemNoDespawnAge  = -32768
	itemDespawnAge    = 6000
	itemGravity       = 0.04
	itemHealth        = 5
	itemHeight        = 0.25
)

// itemEnt is the ItemEntity state of a Mob of type minecraft:item. mu
// guards the item fields; the mob's mu guards its physics. Lock order is
// itemEnt.mu before Mob.mu.
type itemEnt struct {
	m         *Mob
	mu        sync.Mutex
	stack     item.Stack
	age       int32
	delay     int32
	tickCount int32
	health    float32
	target    api.UUID
	hasTarget bool
	thrower   api.UUID
}

var itemType = func() *data.EntityType {
	t, _ := data.Canonical().EntityByName("item")
	return t
}()

// newItemEntity is new ItemEntity(level, x, y, z, stack, vx, vy, vz); it is
// not yet added to the world.
func (w *World) newItemEntity(pos, vel api.Vec3, st item.Stack) *itemEnt {
	m := newMob(w, itemType, api.Location{World: w.name, Pos: pos, Yaw: rand.Float32() * 360})
	m.e.Vel = vel
	m.e.Gravity, m.e.Drag = itemGravity, 0.98
	m.trk.Vel = vel
	m.trk.Range = 96 // clientTrackingRange 6 chunks
	it := &itemEnt{m: m, stack: st, health: itemHealth}
	m.itm = it
	m.trk.SetMeta(itemMeta(st))
	return it
}

// addItemEntity is addFreshEntity for an item entity.
func (w *World) addItemEntity(it *itemEnt) {
	dropped.mu.Lock()
	dropped.items[it.m.e.ID] = it
	if len(dropped.items) > 4096 {
		for id, d := range dropped.items {
			if d.m.removed.Load() {
				delete(dropped.items, id)
			}
		}
	}
	dropped.mu.Unlock()
	w.mobsMu.Lock()
	w.addMobLocked(it.m)
	w.mobsMu.Unlock()
}

// SpawnItem spawns an item entity at pos with the reference's default
// random velocity (ItemEntity(level, x, y, z, stack)) and the given pickup
// delay; it returns nil for an empty stack.
func (w *World) SpawnItem(pos api.Vec3, st item.Stack, pickupDelay int32) api.Entity {
	if st.IsEmpty() {
		return nil
	}
	it := w.newItemEntity(pos, tossVelocity(), st)
	it.delay = pickupDelay
	w.addItemEntity(it)
	return it.m
}

// spawnItem spawns an item entity with an explicit velocity and pickup
// delay (ItemEntity(level, x, y, z, stack, vx, vy, vz) + setPickUpDelay).
func (w *World) spawnItem(pos, vel api.Vec3, st item.Stack, delay int64) *droppedItem {
	if st.IsEmpty() {
		return nil
	}
	it := w.newItemEntity(pos, vel, st)
	it.delay = int32(min(delay, itemNeverPickup))
	w.addItemEntity(it)
	return it
}

// PopResource is Block.popResource: drops st around the centre of block pos
// with the default pickup delay. Empty stacks and disabled block drops do
// nothing.
func (w *World) PopResource(pos api.BlockPos, st item.Stack) {
	if st.IsEmpty() || !w.blockDropsEnabled() {
		return
	}
	r := w.lootState()
	r.mu.Lock()
	at := popResourceOffset(r.rand, pos)
	r.mu.Unlock()
	vel := tossVelocity()
	it := w.newItemEntity(at, vel, st)
	it.delay = itemPickupDefault
	w.addItemEntity(it)
}

// SpawnAtLocation is Entity.spawnAtLocation(level, stack, yOffset) for an
// entity at at (death drops).
func (w *World) SpawnAtLocation(at api.Vec3, st item.Stack, yOffset float64) api.Entity {
	at.Y += yOffset
	return w.SpawnItem(at, st, itemPickupDefault)
}

// popResourceOffset draws popResource's spawn point around block pos from the level
// random r (three draws).
func popResourceOffset(r interface{ NextDouble() float64 }, pos api.BlockPos) api.Vec3 {
	h := itemHeight / 2.0
	x := float64(pos.X) + 0.5 + nextDoubleRange(r.NextDouble, -0.25, 0.25)
	y := float64(pos.Y) + 0.5 + nextDoubleRange(r.NextDouble, -0.25, 0.25) - h
	z := float64(pos.Z) + 0.5 + nextDoubleRange(r.NextDouble, -0.25, 0.25)
	return api.Vec3{X: x, Y: y, Z: z}
}

// tossVelocity is the default ItemEntity velocity, drawn from the item
// entity's own random (not the level random).
func tossVelocity() api.Vec3 {
	return api.Vec3{X: rand.Float64()*0.2 - 0.1, Y: 0.2, Z: rand.Float64()*0.2 - 0.1}
}

func nextDoubleRange(next func() float64, lo, hi float64) float64 {
	if lo >= hi {
		return lo
	}
	return next()*(hi-lo) + lo
}

func (w *World) blockDropsEnabled() bool {
	for _, n := range []string{"doTileDrops", "block_drops", "minecraft:block_drops"} {
		if v, ok := w.GameRule(n); ok {
			return v != "false"
		}
	}
	return true
}

// itemFriction is Block.getFriction for the blocks that differ from 0.6.
func itemFriction(st data.StateID) float32 {
	b := data.BlockOf(st)
	if b == nil {
		return 0.6
	}
	switch b.Name {
	case "ice", "packed_ice", "frosted_ice":
		return 0.98
	case "blue_ice":
		return 0.989
	case "slime_block":
		return 0.8
	}
	return 0.6
}

func speedFactor(st data.StateID) float32 {
	if b := data.BlockOf(st); b != nil {
		switch b.Name {
		case "soul_sand", "honey_block":
			return 0.4
		}
	}
	return 1
}

// fluidAt returns the fluid kind ("water", "lava" or "") and its height in
// the block (FluidState.getHeight).
func fluidAt(src entity.BlockSource, x, y, z int) (string, float64) {
	st := src.Block(x, y, z)
	if data.Flags(st)&data.FlagFluid == 0 {
		return "", 0
	}
	b := data.BlockOf(st)
	kind := "water"
	if b != nil && b.Name == "lava" {
		kind = "lava"
	}
	if k, _ := fluidAtTop(src, x, y+1, z); k == kind {
		return kind, 1
	}
	if b == nil || (b.Name != "water" && b.Name != "lava") {
		return kind, 8.0 / 9
	}
	lv, _ := data.Canonical().Value(st, "level")
	n, _ := atoiSmall(lv)
	if n >= 8 || n == 0 {
		return kind, 8.0 / 9
	}
	return kind, float64(8-n) / 9
}

func fluidAtTop(src entity.BlockSource, x, y, z int) (string, bool) {
	st := src.Block(x, y, z)
	if data.Flags(st)&data.FlagFluid == 0 {
		return "", false
	}
	if b := data.BlockOf(st); b != nil && b.Name == "lava" {
		return "lava", true
	}
	return "water", true
}

func atoiSmall(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, s != ""
}

// fluidHeight is Entity.getFluidHeight: how deep the bounding box sits in
// fluid kind (0 when not in it).
func fluidHeight(src entity.BlockSource, box entity.AABB, kind string) float64 {
	b := box.Grow(-0.001, -0.001, -0.001)
	x0, x1 := int(math.Floor(b.MinX)), int(math.Ceil(b.MaxX))
	y0, y1 := int(math.Floor(b.MinY)), int(math.Ceil(b.MaxY))
	z0, z1 := int(math.Floor(b.MinZ)), int(math.Ceil(b.MaxZ))
	best := 0.0
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			for z := z0; z < z1; z++ {
				k, h := fluidAt(src, x, y, z)
				if k != kind {
					continue
				}
				top := float64(y) + h
				if top >= b.MinY {
					best = max(best, top-b.MinY)
				}
			}
		}
	}
	return best
}

// tick is ItemEntity.tick, preceded by the tickNonPassenger bookkeeping.
func (it *itemEnt) tick(w *World) {
	it.step(w.w, w.MinY(), func() []*Mob {
		it.m.mu.Lock()
		box := it.m.e.Box().Grow(0.5, 0, 0.5)
		it.m.mu.Unlock()
		return w.mobsIn(box, nil)
	})
}

// step is one item entity tick against src; neighbours lists merge
// candidates.
func (it *itemEnt) step(src entity.BlockSource, minY int, neighbours func() []*Mob) {
	m := it.m
	it.mu.Lock()
	m.mu.Lock()
	e := m.e
	if it.stack.IsEmpty() {
		m.mu.Unlock()
		it.mu.Unlock()
		it.remove()
		return
	}
	it.tickCount++
	m.ticks++
	if it.delay > 0 && it.delay != itemNeverPickup {
		it.delay--
	}
	e.Prev = e.Pos
	box := e.Box()
	wh := fluidHeight(src, box, "water")
	lh := fluidHeight(src, box, "lava")
	switch {
	case wh > 0.1:
		it.waterPush(src, box)
		it.fluidMovement(float64(float32(0.99)))
	case lh > 0.1:
		it.fluidMovement(float64(float32(0.95)))
	default:
		e.Vel.Y -= itemGravity
	}
	if !e.OnGround || e.Vel.X*e.Vel.X+e.Vel.Z*e.Vel.Z > 1e-5 || (it.tickCount+e.ID)%4 == 0 {
		r := entity.Move(src, e.Box(), e.Vel.X, e.Vel.Y, e.Vel.Z, 0, e.OnGround)
		// Tiny collided moves are dropped (observed in the reference): the
		// position changes only when the collided movement is longer than
		// 1e-7 squared or nothing was cut off.
		d0 := r.DX*r.DX + r.DY*r.DY + r.DZ*r.DZ
		if d0 > 1e-7 || e.Vel.X*e.Vel.X+e.Vel.Y*e.Vel.Y+e.Vel.Z*e.Vel.Z-d0 < 1e-7 {
			e.Pos.X += r.DX
			e.Pos.Y += r.DY
			e.Pos.Z += r.DZ
		}
		e.OnGround = r.OnGround
		if r.CollidedX {
			e.Vel.X = 0
		}
		if r.CollidedZ {
			e.Vel.Z = 0
		}
		if r.CollidedY {
			below := src.Block(int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Y-0.2)), int(math.Floor(e.Pos.Z)))
			if b := data.BlockOf(below); b != nil && b.Name == "slime_block" && r.OnGround && e.Vel.Y < 0 {
				e.Vel.Y = -e.Vel.Y * 0.8
			} else {
				e.Vel.Y = 0
			}
		}
		feet := src.Block(int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Y)), int(math.Floor(e.Pos.Z)))
		sf := speedFactor(feet)
		if sf == 1 && data.Flags(feet)&data.FlagFluid == 0 {
			sf = speedFactor(src.Block(int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Y-0.999999)), int(math.Floor(e.Pos.Z))))
		}
		if sf != 1 {
			e.Vel.X *= float64(sf)
			e.Vel.Z *= float64(sf)
		}
		f := float32(0.98)
		if e.OnGround {
			f = itemFriction(src.Block(int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Y-0.999999)), int(math.Floor(e.Pos.Z)))) * 0.98
		}
		e.Vel.X *= float64(f)
		e.Vel.Y *= float64(float32(0.98))
		e.Vel.Z *= float64(f)
		if e.OnGround && e.Vel.Y < 0 {
			e.Vel.Y *= -0.5
		}
	}
	moved := math.Floor(e.Prev.X) != math.Floor(e.Pos.X) || math.Floor(e.Prev.Y) != math.Floor(e.Pos.Y) || math.Floor(e.Prev.Z) != math.Floor(e.Pos.Z)
	period := int32(40)
	if moved {
		period = 2
	}
	mergeNow := it.tickCount%period == 0 && it.mergeableLocked()
	dmg := it.hazard(src)
	fell := e.Pos.Y < float64(minY-64)
	m.mu.Unlock()
	it.mu.Unlock()
	if mergeNow {
		it.mergeWithNeighbours(neighbours)
	}
	it.mu.Lock()
	if it.age != itemNoDespawnAge {
		it.age++
	}
	if dmg > 0 {
		it.health -= dmg
	}
	gone := it.age >= itemDespawnAge || it.health <= 0 || fell
	it.mu.Unlock()
	if gone {
		it.remove()
	}
}

func (it *itemEnt) remove() {
	it.m.Remove()
	dropped.mu.Lock()
	delete(dropped.items, it.m.e.ID)
	dropped.mu.Unlock()
}

// itemWaterPush is the per-tick push of flowing water on an item, measured
// on the reference (item in a level 2..5 channel: +0.02843 blocks/tick
// before drag).
const itemWaterPush = 0.02843

// waterPush adds the flow of the water blocks the item's box is in.
func (it *itemEnt) waterPush(src entity.BlockSource, box entity.AABB) {
	var fx, fz float64
	for x := floorInt(box.MinX); x <= floorInt(box.MaxX-1e-7); x++ {
		for y := floorInt(box.MinY); y <= floorInt(box.MaxY-1e-7); y++ {
			for z := floorInt(box.MinZ); z <= floorInt(box.MaxZ-1e-7); z++ {
				if k, _ := world.FluidOf(src.Block(x, y, z)); k == world.FluidWater {
					dx, dz := world.FluidFlow(src, x, y, z)
					fx, fz = fx+dx, fz+dz
				}
			}
		}
	}
	if l := math.Hypot(fx, fz); l > 0 {
		it.m.e.Vel.X += fx / l * itemWaterPush
		it.m.e.Vel.Z += fz / l * itemWaterPush
	}
}

func (it *itemEnt) fluidMovement(mul float64) {
	v := &it.m.e.Vel
	add := 0.0
	if v.Y < 0.06 {
		add = float64(float32(5.0e-4))
	}
	v.X, v.Y, v.Z = v.X*mul, v.Y+add, v.Z*mul
}

// hazard returns this tick's damage from lava, fire or cactus contact.
func (it *itemEnt) hazard(src entity.BlockSource) float32 {
	b := it.m.e.Box()
	x0, y0, z0 := int(math.Floor(b.MinX)), int(math.Floor(b.MinY)), int(math.Floor(b.MinZ))
	x1, y1, z1 := int(math.Floor(b.MaxX)), int(math.Floor(b.MaxY)), int(math.Floor(b.MaxZ))
	var dmg float32
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				bl := data.BlockOf(src.Block(x, y, z))
				if bl == nil {
					continue
				}
				switch bl.Name {
				case "lava":
					dmg = max(dmg, 4)
				case "fire", "soul_fire":
					dmg = max(dmg, 1)
				case "cactus":
					dmg = max(dmg, 1)
				}
			}
		}
	}
	if dmg > 0 && it.fireproof() {
		if bl := data.BlockOf(src.Block(int(math.Floor(it.m.e.Pos.X)), int(math.Floor(it.m.e.Pos.Y)), int(math.Floor(it.m.e.Pos.Z)))); bl == nil || bl.Name != "cactus" {
			return 0
		}
	}
	return dmg
}

func (it *itemEnt) fireproof() bool {
	v, ok := it.stack.Component(item.DamageResistant)
	return ok && strings.Contains(string(v), "is_fire")
}

func (it *itemEnt) mergeable() bool {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.mergeableLocked()
}

func (it *itemEnt) mergeableLocked() bool {
	return !it.m.removed.Load() && it.delay != itemNeverPickup && it.age != itemNoDespawnAge && it.age < itemDespawnAge && it.stack.Count < it.stack.MaxStackSize()
}

// mergeWithNeighbours merges with item entities within 0.5 blocks
// horizontally.
func (it *itemEnt) mergeWithNeighbours(neighbours func() []*Mob) {
	it.m.mu.Lock()
	box := it.m.e.Box().Grow(0.5, 0, 0.5)
	it.m.mu.Unlock()
	for _, o := range neighbours() {
		ot := o.itm
		if ot == nil || ot == it || o.removed.Load() {
			continue
		}
		if !ot.mergeable() {
			continue
		}
		o.mu.Lock()
		hit := o.e.Box().Intersects(box)
		o.mu.Unlock()
		if !hit {
			continue
		}
		it.tryToMerge(ot)
		if it.m.removed.Load() {
			return
		}
	}
}

var mergeMu sync.Mutex

func (it *itemEnt) snapshot() (item.Stack, bool, api.UUID) {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.stack, it.hasTarget, it.target
}

func (it *itemEnt) tryToMerge(o *itemEnt) {
	mergeMu.Lock()
	defer mergeMu.Unlock()
	a, ha, ta := it.snapshot()
	b, hb, tb := o.snapshot()
	if ha != hb || ta != tb || !item.SameItemSameComponents(a, b) || a.Count+b.Count > a.MaxStackSize() {
		return
	}
	if b.Count < a.Count {
		mergeInto(it, o)
	} else {
		mergeInto(o, it)
	}
}

// mergeInto moves src's items into dst (ItemEntity.merge).
func mergeInto(dst, src *itemEnt) {
	src.mu.Lock()
	ss, sd, sa := src.stack, src.delay, src.age
	src.mu.Unlock()
	dst.mu.Lock()
	k := min(dst.stack.MaxStackSize()-dst.stack.Count, ss.Count, 64)
	dst.setStackLocked(dst.stack.WithCount(dst.stack.Count + k))
	dst.delay = max(dst.delay, sd)
	dst.age = min(dst.age, sa)
	dst.mu.Unlock()
	src.mu.Lock()
	rest := ss.WithCount(ss.Count - k)
	src.setStackLocked(rest)
	src.mu.Unlock()
	if rest.IsEmpty() {
		src.remove()
	}
}

func (it *itemEnt) setStackLocked(st item.Stack) {
	it.stack = st
	if !st.IsEmpty() {
		it.m.trk.SetMeta(itemMeta(st))
	}
}

// touch is ItemEntity.playerTouch.
func (it *itemEnt) touch(p *Player) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.delay != 0 || it.hasTarget && it.target != p.prof.UUID || it.stack.IsEmpty() || it.m.removed.Load() {
		return
	}
	st := it.stack
	n := st.Count
	p.inv.mu.Lock()
	rest := p.inv.m.AddToInventory(st, menuPlayer{p})
	p.inv.mu.Unlock()
	if rest.Count == n && !rest.IsEmpty() {
		return
	}
	raw := encode(&packets.ClientboundTakeItemEntity{CollectedEntityID: it.m.e.ID, CollectorEntityID: p.e.ID, PickupItemCount: n})
	w := p.world.Load()
	for _, o := range w.playerList() {
		if o == p || o.isTracking(it.m.e.ID) {
			o.c.SendRaw(raw)
		}
	}
	it.setStackLocked(rest)
	if rest.IsEmpty() {
		it.m.Remove()
		dropped.mu.Lock()
		delete(dropped.items, it.m.e.ID)
		dropped.mu.Unlock()
	}
}

// pickupNearby is the item part of Player.aiStep: touch item entities in
// the bounding box grown by (1, 0.5, 1).
func (p *Player) pickupNearby() {
	if p.GameMode() == api.Spectator || p.IsDead() {
		return
	}
	w := p.world.Load()
	if w == nil {
		return
	}
	mn, mx := p.BoundingBox()
	box := entity.AABB{MinX: mn.X - 1, MinY: mn.Y - 0.5, MinZ: mn.Z - 1, MaxX: mx.X + 1, MaxY: mx.Y + 0.5, MaxZ: mx.Z + 1}
	for _, m := range w.mobList() {
		if m.itm == nil || m.removed.Load() {
			continue
		}
		m.mu.Lock()
		hit := m.e.Box().Intersects(box)
		m.mu.Unlock()
		if hit {
			m.itm.touch(p)
		}
	}
}
