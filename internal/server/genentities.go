package server

import (
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// queueGenEntities remembers a chunk that went live with entities from
// generation (end crystals, structure mobs and template entities); they are
// added on the tick goroutine after the world tick. Runs in the region phase.
func (w *World) queueGenEntities(c *world.Chunk) {
	if !c.HasGenEntities() {
		return
	}
	w.genMu.Lock()
	w.genQ = append(w.genQ, c)
	w.genMu.Unlock()
}

// genEntityType is the entity type of generated entity NBT when the server
// can run it (mobs and end crystals); other kinds (chest minecarts, ...)
// stay stored with the chunk.
func genEntityType(e nbt.Compound) (*data.EntityType, bool) {
	id, _ := e["id"].(string)
	t, ok := data.Canonical().EntityByName(strings.TrimPrefix(id, "minecraft:"))
	if !ok {
		return nil, false
	}
	if t.Name == "end_crystal" {
		return t, true
	}
	st := spawn.EntityByKind(int32(t.ID))
	return t, st != nil && st.Mob
}

// spawnGenEntities adds the queued generated entities to the world, once
// per chunk: a chunk unloaded meanwhile keeps them for its next load.
func (w *World) spawnGenEntities() {
	w.genMu.Lock()
	q := w.genQ
	w.genQ = nil
	w.genMu.Unlock()
	for _, c := range q {
		if w.w.Chunk(c.Pos()) != c {
			continue
		}
		for _, e := range c.TakeGenEntities(genRunnable) {
			w.spawnGenTree(e, nil)
		}
	}
}

// genRunnable: the entity or one of its passengers can run on the server.
func genRunnable(e nbt.Compound) bool {
	if _, ok := genEntityType(e); ok {
		return true
	}
	l, _ := e["Passengers"].(nbt.List)
	for _, it := range l.Items {
		if p, ok := it.(nbt.Compound); ok && genRunnable(p) {
			return true
		}
	}
	return false
}

// spawnGenTree spawns e and its Passengers (drowned on zombie nautiluses,
// chicken jockeys) seated on it. A vehicle the server cannot run is skipped
// and its passengers spawn unseated.
func (w *World) spawnGenTree(e nbt.Compound, vehicle *Mob) {
	var m *Mob
	if _, ok := genEntityType(e); ok {
		m = w.spawnGenEntity(e)
		if m != nil && vehicle != nil {
			m.mount(vehicle)
		}
	}
	l, _ := e["Passengers"].(nbt.List)
	for _, it := range l.Items {
		if p, ok := it.(nbt.Compound); ok {
			w.spawnGenTree(p, m)
		}
	}
}

func (w *World) spawnGenEntity(e nbt.Compound) *Mob {
	t, _ := genEntityType(e)
	at := api.Location{World: w.name}
	if p := nbtDoubles(e["Pos"]); len(p) == 3 {
		at.Pos = api.Vec3{X: p[0], Y: p[1], Z: p[2]}
	}
	if r := nbtFloats(e["Rotation"]); len(r) == 2 {
		at.Yaw, at.Pitch = r[0], r[1]
	}
	m := newMob(w, t, at)
	m.e.NoGravity = nbtBool(e["NoGravity"]) || t.Name == "end_crystal"
	m.invuln = nbtBool(e["Invulnerable"])
	if st := spawn.EntityByKind(int32(t.ID)); st != nil && st.Mob {
		pr := nbtBool(e["PersistenceRequired"])
		m.sp = &mobSpawn{typ: st, reason: spawn.ReasonStructure, managed: true, persistent: pr,
			rnd: rng.NewLegacy(int64(m.e.UUID[0])<<32 ^ int64(m.e.ID))}
	}
	if w.s.bus.HasHandlers(api.KindEntitySpawn) {
		ev := &api.EntitySpawnEvent{Entity: m, Location: at, Reason: api.SpawnChunkGen}
		if w.s.bus.Dispatch(ev).(*api.EntitySpawnEvent).Cancelled() {
			return nil
		}
	}
	w.mobsMu.Lock()
	w.addMobLocked(m)
	w.mobsMu.Unlock()
	return m
}

func nbtBool(v any) bool {
	b, ok := v.(int8)
	return ok && b != 0
}

func nbtDoubles(v any) []float64 {
	l, _ := v.(nbt.List)
	out := make([]float64, 0, len(l.Items))
	for _, it := range l.Items {
		if f, ok := it.(float64); ok {
			out = append(out, f)
		}
	}
	return out
}

func nbtFloats(v any) []float32 {
	l, _ := v.(nbt.List)
	out := make([]float32, 0, len(l.Items))
	for _, it := range l.Items {
		if f, ok := it.(float32); ok {
			out = append(out, f)
		}
	}
	return out
}
