package server

import (
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// exTarget is an entity an explosion can hit.
type exTarget interface {
	exPos() api.Vec3
	// exAim is the point knockback is aimed at: the eyes, or the feet for
	// primed TNT.
	exAim() api.Vec3
	exBox() entity.AABB
	// exImmune is Entity.ignoreExplosion (spectators, creative players
	// are handled in exHurt / exPlayerKnockback).
	exImmune() bool
	// exHurt applies explosion damage from ex.
	exHurt(ex *explosion, amount float32)
	exPush(v api.Vec3)
	// exKnockbackResistance is the explosion_knockback_resistance
	// attribute (blast protection adds to it); living entities only.
	exKnockbackResistance() (float64, bool)
	// exRecordsKnockback reports players whose knockback the explode
	// packet carries (not spectators or flying creative players).
	exRecordsKnockback() bool
}

// exLevel is the world an explosion runs in.
type exLevel interface {
	exBlocks
	worldBounds() (minY, maxY int)
	setBlock(p api.BlockPos, s data.StateID)
	targets(box entity.AABB) []exTarget
	// primeExploded primes a TNT block hit by ex (fuse from shortFuse).
	primeExploded(p api.BlockPos, ex *explosion)
	// blockDrops returns the loot of s at p for an explosion (radius set
	// when drops decay); popResource spawns one collected stack.
	blockDrops(p api.BlockPos, s data.StateID, ex *explosion) []item.Stack
	popResource(p api.BlockPos, st item.Stack)
}

type explosion struct {
	center      api.Vec3
	radius      float32
	fire        bool
	interaction blockInteraction
	direct      api.Entity // e.g. the primed TNT or creeper (already removed)
	causing     api.Entity // indirect source: TNT owner, the creeper itself
	rnd         rng.Source
	calc        *exCalc

	hitPlayers map[exTarget]api.Vec3
	blocks     int
}

func (e *explosion) interactsWithBlocks() bool { return e.interaction != exKeep }

// small selects the plain explosion particle instead of the emitter.
func (e *explosion) small() bool { return e.radius < 2 || !e.interactsWithBlocks() }

// run is ServerExplosion.explode: rays, entities, blocks, fire. It returns
// the number of affected block positions.
func (e *explosion) run(lv exLevel) int {
	minY, maxY := lv.worldBounds()
	list := explodedPositions(lv, minY, maxY, e.center, e.radius, e.rnd, e.calc)
	e.hurtEntities(lv)
	if e.interactsWithBlocks() {
		shufflePositions(list, e.rnd)
		e.interactWithBlocks(lv, list)
	}
	if e.fire {
		e.createFire(lv, list)
	}
	e.blocks = len(list)
	return len(list)
}

func (e *explosion) hurtEntities(lv exLevel) {
	q := float64(e.radius * 2)
	c := e.center
	box := entity.AABB{
		MinX: float64(exFloor(c.X - q - 1)), MinY: float64(exFloor(c.Y - q - 1)), MinZ: float64(exFloor(c.Z - q - 1)),
		MaxX: float64(exFloor(c.X + q + 1)), MaxY: float64(exFloor(c.Y + q + 1)), MaxZ: float64(exFloor(c.Z + q + 1)),
	}
	for _, t := range lv.targets(box) {
		if t.exImmune() {
			continue
		}
		pos := t.exPos()
		dist := math.Sqrt(exDistSq(pos, c)) / q
		if dist > 1 {
			continue
		}
		aim := t.exAim()
		d := api.Vec3{X: aim.X - c.X, Y: aim.Y - c.Y, Z: aim.Z - c.Z}
		l := math.Sqrt(exDot(d.X, d.Y, d.Z, d.X, d.Y, d.Z))
		if l == 0 {
			continue
		}
		d = api.Vec3{X: d.X / l, Y: d.Y / l, Z: d.Z / l}
		seen := seenPercent(lv, c, t.exBox())
		t.exHurt(e, explosionDamage(c, e.radius, pos, seen))
		f := (1 - dist) * float64(seen)
		if res, living := t.exKnockbackResistance(); living {
			f *= 1 - res
		}
		v := api.Vec3{X: d.X * f, Y: d.Y * f, Z: d.Z * f}
		t.exPush(v)
		if t.exRecordsKnockback() {
			if e.hitPlayers == nil {
				e.hitPlayers = map[exTarget]api.Vec3{}
			}
			e.hitPlayers[t] = v
		}
	}
}

// maxCombinedDrops caps one merged drop stack (ServerExplosion constant).
const maxCombinedDrops = 16

type dropCollector struct {
	pos api.BlockPos
	st  item.Stack
}

func (e *explosion) interactWithBlocks(lv exLevel, list []api.BlockPos) {
	var drops []dropCollector
	tnt := tntBlock()
	for _, p := range list {
		s := lv.Block(p.X, p.Y, p.Z)
		if data.IsAir(s) {
			continue
		}
		if e.interaction == exTriggerBlock {
			continue
		}
		b := data.Canonical().BlockID(s)
		if b != tnt {
			for _, st := range lv.blockDrops(p, s, e) {
				drops = addDrop(drops, st, p)
			}
		}
		lv.setBlock(p, data.Air)
		if b == tnt {
			lv.primeExploded(p, e)
		}
	}
	for _, d := range drops {
		lv.popResource(d.pos, d.st)
	}
}

// addDrop merges st into the first collector holding a mergeable stack
// with room under the cap, else starts a collector at p.
func addDrop(l []dropCollector, st item.Stack, p api.BlockPos) []dropCollector {
	for i := range l {
		c := &l[i]
		if item.SameItemSameComponents(c.st, st) {
			room := min(maxCombinedDrops, c.st.MaxStackSize()) - c.st.Count
			if room <= 0 {
				continue
			}
			n := min(room, st.Count)
			c.st.Count += n
			st.Count -= n
			if st.Count <= 0 {
				return l
			}
		}
	}
	return append(l, dropCollector{pos: p, st: st})
}

func (e *explosion) createFire(lv exLevel, list []api.BlockPos) {
	for _, p := range list {
		if e.rnd.NextIntn(3) != 0 {
			continue
		}
		if !data.IsAir(lv.Block(p.X, p.Y, p.Z)) {
			continue
		}
		if exFlags(lv.Block(p.X, p.Y-1, p.Z))&exSolidRender == 0 {
			continue
		}
		s := fireStateAt(lv, p)
		lv.setBlock(p, s)
		fireOnPlace(s, e.rnd)
	}
}

var tntBlock = sync.OnceValue(func() data.BlockID {
	b, _ := data.Canonical().BlockByName("tnt")
	return b.ID
})

// exPopOffset is where Block.popResource places an item for block p.
func exPopOffset(p api.BlockPos, rnd rng.Source) api.Vec3 {
	const halfItem = 0.125
	x := float64(p.X) + 0.5 + (float64(rnd.NextDouble()*0.5) - 0.25)
	y := float64(p.Y) + 0.5 + (float64(rnd.NextDouble()*0.5) - 0.25) - halfItem
	z := float64(p.Z) + 0.5 + (float64(rnd.NextDouble()*0.5) - 0.25)
	return api.Vec3{X: x, Y: y, Z: z}
}
