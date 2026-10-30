package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Primed TNT (26.3). Verified against the reference (testdata/explosion_ref.txt):
// spawn at the block's bottom centre with velocity (-sin a·0.02, 0.2f,
// -cos a·0.02), a = nextDouble·2π from the level random; fuse 80, or
// nextInt(fuse/4)+fuse/8 when primed by an explosion; explodes with power 4
// at y + 0.0625·height when the fuse reaches 0. Motion is entity.KindBlock
// physics (internal/entity).

// spawnPrimedTNT is new PrimedTnt(level, x+0.5, y, z+0.5, owner) for the
// TNT block at p, added to the world. rnd is the level random.
func (w *World) spawnPrimedTNT(p api.BlockPos, owner api.Entity, rnd rng.Source) (*Mob, *primedTNT) {
	t, ok := data.Canonical().EntityByName("tnt")
	if !ok {
		return nil, nil
	}
	pos, vel := primedSpawn(p, rnd)
	m := newMob(w, t, api.Location{World: w.name, Pos: pos})
	m.e.Vel = vel
	m.trk.Range = 160
	st := &primedTNT{fuse: tntDefaultFuse, power: tntPower, owner: owner}
	m.onTick = func(m *Mob) { st.tick(m) }
	m.trk.SetMeta(tntMeta(st.fuse))
	return m, st
}

func (w *World) addPrimed(m *Mob) {
	w.mobsMu.Lock()
	w.addMobLocked(m)
	w.mobsMu.Unlock()
}

func (st *primedTNT) setFuse(m *Mob, f int32) {
	st.fuse = f
	m.trk.SetMeta(tntMeta(f))
}

// tick runs after the entity's KindBlock physics step: count down and
// explode (PrimedTnt.tick).
func (st *primedTNT) tick(m *Mob) {
	st.setFuse(m, st.fuse-1)
	if st.fuse > 0 {
		return
	}
	m.Remove()
	if !m.w.gameRule("tnt_explodes", true) {
		return
	}
	m.mu.Lock()
	pos := m.e.Pos
	h := m.e.Height
	m.mu.Unlock()
	w := m.w
	var calc *exCalc
	if st.portal {
		calc = &exCalc{keep: isPortalBlock}
	}
	w.explode(explodeOpts{
		center: api.Vec3{X: pos.X, Y: pos.Y + tntFuseOffsetDY*h, Z: pos.Z},
		power:  st.power, kind: explodeTNT, direct: m, causing: st.owner, calc: calc,
	})
}

func isPortalBlock(s data.StateID) bool {
	b := data.BlockOf(s)
	return b != nil && (b.Name == "nether_portal" || b.Name == "end_portal" || b.Name == "end_gateway")
}

// primeTNT is TntBlock.prime: with tnt_explodes on, it spawns primed TNT
// for the block at p and plays the fuse sound; the caller removes the
// block. It reports whether the TNT was primed.
func (w *World) primeTNT(p api.BlockPos, igniter api.Entity) bool {
	if !w.gameRule("tnt_explodes", true) {
		return false
	}
	m, _ := w.spawnPrimedTNT(p, igniter, w.levelRandom())
	if m == nil {
		return false
	}
	w.addPrimed(m)
	w.playSoundAt(m.e.Pos, "minecraft:entity.tnt.primed", soundBlocks, 1, 1)
	return true
}

// primeExploded is TntBlock.wasExploded: primed TNT with a short random fuse.
func (w *World) primeExploded(p api.BlockPos, ex *explosion) {
	if !w.gameRule("tnt_explodes", true) {
		return
	}
	m, st := w.spawnPrimedTNT(p, ex.causing, ex.rnd)
	if m == nil {
		return
	}
	st.setFuse(m, shortFuse(tntDefaultFuse, ex.rnd))
	w.addPrimed(m)
}

// Sound categories (SoundSource ordinal).
const (
	soundBlocks  = 4
	soundHostile = 5
)

// igniteTNTBlock primes the TNT block at p and removes it: the hook for
// burning projectiles (TntBlock.onProjectileHit), fire spread and redstone
// power once those exist. It reports whether the TNT was primed.
func (w *World) igniteTNTBlock(p api.BlockPos, igniter api.Entity) bool {
	if data.Canonical().BlockID(w.w.Block(p.X, p.Y, p.Z)) != tntBlock() || !w.primeTNT(p, igniter) {
		return false
	}
	w.w.SetBlock(p.X, p.Y, p.Z, data.Air)
	return true
}
