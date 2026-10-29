package server

import (
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Natural mob spawning, despawning and monster spawners.
//
// Everything here runs on the server tick goroutine, before the world's
// entity physics and region phases (vanilla order: chunk source tick with
// natural spawning, then entities with checkDespawn, then block entities).
// No region phase runs concurrently; chunk reads still take the chunk read
// lock because IO and plugin goroutines may touch chunks.
//
// Spawned mobs have no AI: they keep generic physics, so they stand, fall
// and sink. finalizeSpawn results (baby, variants, colors, handedness) are
// kept in Mob.sp.attrs but not sent as entity metadata yet.

// mobSpawn is the spawn-engine state of a Mob; touched only by the tick
// goroutine after the mob is published.
type mobSpawn struct {
	typ        *spawn.EntityType
	reason     spawn.Reason
	managed    bool // spawned by the engine: counts toward caps, may despawn
	persistent bool // PersistenceRequired
	noAction   int
	rnd        *rng.Legacy // Entity.random (the despawn roll)
	attrs      map[string]any
	leftHanded bool
}

var spawnData struct {
	once sync.Once
	d    atomic.Pointer[spawn.Data]
	err  atomic.Pointer[error]
}

// loadedSpawnData starts loading the vanilla spawn tables in the
// background and returns them once ready (nil until then).
func loadedSpawnData() (*spawn.Data, error) {
	spawnData.once.Do(func() {
		go func() {
			p, err := wgdata.Load()
			var d *spawn.Data
			if err == nil {
				d, err = spawn.FromPack(p)
			}
			if err != nil {
				spawnData.err.Store(&err)
				return
			}
			spawnData.d.Store(d)
		}()
	})
	if e := spawnData.err.Load(); e != nil {
		return nil, *e
	}
	return spawnData.d.Load(), nil
}

// worldSpawn is the per-world spawning state, owned by the tick goroutine.
type worldSpawn struct {
	eng   *spawn.Engine
	lvl   spawnLevel
	rnd   *rng.Legacy // the level random
	seeds *rand.Rand  // seeds for per-mob randoms
	tick  int64

	chunks   [][2]int
	nearSet  map[uint64]struct{}
	nearList []world.ChunkPos
	mobs     []*Mob
	entities []spawn.Entity

	spawners map[[3]int]*spawnerState
	nextScan int64

	stats spawnStats
}

// spawnStats are read by tests and diagnostics from other goroutines.
type spawnStats struct {
	ticks, nanos, maxNanos atomic.Int64
	spawned, despawned     atomic.Int64
	chunks, cap            atomic.Int64
}

func (w *World) spawnState() *worldSpawn {
	if w.spawning != nil {
		return w.spawning
	}
	d, err := loadedSpawnData()
	if err != nil {
		if w.spawnErr == nil {
			w.spawnErr = err
			w.s.log.Error("mob spawning disabled", "world", w.name, "err", err)
		}
		return nil
	}
	if d == nil {
		return nil
	}
	dim := d.Dimensions["minecraft:overworld"]
	if dim == nil {
		return nil
	}
	seed := uint64(w.seed) ^ uint64(time.Now().UnixNano())
	ws := &worldSpawn{
		eng:      spawn.NewEngine(d),
		rnd:      rng.NewLegacy(int64(seed)),
		seeds:    rand.New(rand.NewPCG(seed, seed>>17|1)),
		nearSet:  map[uint64]struct{}{},
		spawners: map[[3]int]*spawnerState{},
	}
	ws.lvl = spawnLevel{w: w, dim: dim, seaLevel: 63, zoomSeed: obfuscateSeed(w.seed), ents: map[uint64][]*spawnEnt{}}
	if w.flat {
		ws.lvl.seaLevel = -63
	}
	w.spawning = ws
	return ws
}

// ruleBool reads a boolean game rule by its 26.3 id, accepting the legacy
// camelCase name too.
func (w *World) ruleBool(id, legacy string, def bool) bool {
	w.mu.RLock()
	v, ok := w.rules[id]
	if !ok && legacy != "" {
		v, ok = w.rules[legacy]
	}
	w.mu.RUnlock()
	if !ok {
		return def
	}
	return v != "false"
}

// tickSpawn runs natural spawning, despawn checks and spawner block
// entities for one tick.
func (w *World) tickSpawn(all []*Player) {
	ws := w.spawnState()
	if ws == nil {
		return
	}
	start := time.Now()
	ws.tick++
	ws.begin(w, all)
	ws.natural(w)
	ws.despawn(w)
	ws.tickSpawners(w)
	el := int64(time.Since(start))
	ws.stats.ticks.Add(1)
	ws.stats.nanos.Add(el)
	if el > ws.stats.maxNanos.Load() {
		ws.stats.maxNanos.Store(el)
	}
}

func (ws *worldSpawn) begin(w *World, all []*Player) {
	l := &ws.lvl
	l.haveLast = false
	l.players = l.players[:0]
	l.pchunks = l.pchunks[:0]
	clear(l.ents)
	for _, p := range all {
		if p.world.Load() != w || !p.joined.Load() {
			continue
		}
		p.mu.Lock()
		pos, gm, box := p.e.Pos, p.gm, p.e.Box()
		p.mu.Unlock()
		sp := spawn.Player{X: pos.X, Y: pos.Y, Z: pos.Z, Spectator: gm == api.Spectator, Creative: gm == api.Creative}
		l.players = append(l.players, sp)
		l.pchunks = append(l.pchunks, world.ChunkAt(int(math.Floor(pos.X)), int(math.Floor(pos.Z))))
		l.index(&spawnEnt{box: fromEntityBox(box), blocks: true, spectator: sp.Spectator})
	}
	l.simDist = max(w.s.simDist, 2)
	l.dayTime = w.dayTime.Load()
	w.mu.RLock()
	l.diff = spawn.Difficulty(w.difficulty)
	storm, thunder := w.storming, w.thundering
	sx, sy, sz := w.spawn.Pos.X, w.spawn.Pos.Y, w.spawn.Pos.Z
	w.mu.RUnlock()
	l.spawnPos = [3]int{int(math.Floor(sx)), int(math.Floor(sy)), int(math.Floor(sz))}
	l.thunder = thunder && storm
	var rain, th float32
	if storm {
		rain = 1
		if thunder {
			th = 1
		}
	}
	l.darken = skyDarken(l.dayTime, rain, th)

	ws.mobs = ws.mobs[:0]
	ws.entities = ws.entities[:0]
	for _, m := range w.mobList() {
		if m.removed.Load() {
			continue
		}
		if m.sp == nil {
			m.sp = &mobSpawn{typ: spawn.EntityByKind(int32(m.typ.ID)), persistent: true}
		}
		if m.sp.typ == nil {
			continue
		}
		m.mu.Lock()
		pos, box := m.e.Pos, m.e.Box()
		m.mu.Unlock()
		ws.mobs = append(ws.mobs, m)
		l.index(&spawnEnt{box: fromEntityBox(box), typ: m.sp.typ, mob: m,
			blocks: blocksBuilding(m.typ), hard: hardCollision(m.typ)})
		if m.sp.managed {
			ws.entities = append(ws.entities, spawn.Entity{Type: m.sp.typ, X: pos.X, Y: pos.Y, Z: pos.Z,
				Persistent: m.sp.persistent || m.sp.typ.CustomPersist})
		}
	}
}

func (l *spawnLevel) index(e *spawnEnt) {
	cx := int(math.Floor((e.box.MinX+e.box.MaxX)/2)) >> 4
	cz := int(math.Floor((e.box.MinZ+e.box.MaxZ)/2)) >> 4
	k := world.ChunkPos{X: int32(cx), Z: int32(cz)}.Key()
	l.ents[k] = append(l.ents[k], e)
}

func (l *spawnLevel) unindex(m *Mob) {
	for k, es := range l.ents {
		for i, e := range es {
			if e.mob == m {
				l.ents[k] = append(es[:i], es[i+1:]...)
				return
			}
		}
	}
}

func blocksBuilding(t *data.EntityType) bool {
	switch t.Kind {
	case "", "projectile":
		return false
	case "other":
		n := t.Name
		return strings.HasSuffix(n, "boat") || strings.HasSuffix(n, "raft") || strings.HasSuffix(n, "minecart") ||
			n == "falling_block" || n == "tnt" || n == "end_crystal"
	}
	return true
}

func hardCollision(t *data.EntityType) bool {
	n := t.Name
	return n == "shulker" || strings.HasSuffix(n, "boat") || strings.HasSuffix(n, "raft") || strings.HasSuffix(n, "minecart")
}

// spawnCandidates collects the chunks within 8 chunks (chessboard) of a
// player: their count scales the global caps (DistanceManager
// naturalSpawnChunkCounter), and the ones that are block-ticking with a
// non-spectator player within 128 blocks form the spawning list
// (ChunkMap.collectSpawningChunks), in a deterministic order.
func (ws *worldSpawn) spawnCandidates() {
	l := &ws.lvl
	clear(ws.nearSet)
	ws.nearList = ws.nearList[:0]
	ws.chunks = ws.chunks[:0]
	for _, pc := range l.pchunks {
		for dz := int32(-spawn.SpawnDistanceChunk); dz <= spawn.SpawnDistanceChunk; dz++ {
			for dx := int32(-spawn.SpawnDistanceChunk); dx <= spawn.SpawnDistanceChunk; dx++ {
				c := world.ChunkPos{X: pc.X + dx, Z: pc.Z + dz}
				if _, ok := ws.nearSet[c.Key()]; ok {
					continue
				}
				ws.nearSet[c.Key()] = struct{}{}
				ws.nearList = append(ws.nearList, c)
			}
		}
	}
	for _, c := range ws.nearList {
		cx, cz := int(c.X), int(c.Z)
		if !l.blockTicking(cx, cz) || !l.playerCloseForSpawning(cx, cz) {
			continue
		}
		ws.chunks = append(ws.chunks, [2]int{cx, cz})
	}
}

func (l *spawnLevel) playerCloseForSpawning(cx, cz int) bool {
	mx, mz := float64(cx*16+8), float64(cz*16+8)
	for _, p := range l.players {
		if !p.Spectator && (mx-p.X)*(mx-p.X)+(mz-p.Z)*(mz-p.Z) < spawn.SpawnDistanceBlock*spawn.SpawnDistanceBlock {
			return true
		}
	}
	return false
}

// playersNear is ChunkMap.getPlayersCloseForSpawning (LocalMobCapCalculator).
func (ws *worldSpawn) playersNear(cx, cz int) []int {
	if _, ok := ws.nearSet[world.ChunkPos{X: int32(cx), Z: int32(cz)}.Key()]; !ok {
		return nil
	}
	var out []int
	mx, mz := float64(cx*16+8), float64(cz*16+8)
	for i, p := range ws.lvl.players {
		if !p.Spectator && (mx-p.X)*(mx-p.X)+(mz-p.Z)*(mz-p.Z) < spawn.SpawnDistanceBlock*spawn.SpawnDistanceBlock {
			out = append(out, i)
		}
	}
	return out
}

func (ws *worldSpawn) natural(w *World) {
	l := &ws.lvl
	ws.spawnCandidates()
	ws.stats.chunks.Store(int64(len(ws.nearList)))
	for _, c := range ws.chunks {
		if ch := l.chunk(c[0], c[1]); ch != nil {
			ch.Lock()
			ch.InhabitedTime++
			ch.Unlock()
		}
	}
	// ServerChunkCache.tickChunks: the global caps scale with the chunks
	// near players (not the ticked list), then the list is shuffled with
	// the level random and each chunk runs spawnForChunk.
	st := ws.eng.NewSpawnState(l, len(ws.nearList), ws.entities, l.loaded, ws.playersNear)
	ws.stats.cap.Store(int64(spawn.Monster.Info().Max * st.SpawnableChunks / spawn.MagicNumber))
	fr, en, pe := spawn.TickGates(w.ruleBool("spawn_mobs", "doMobSpawning", true),
		w.ruleBool("spawn_monsters", "spawnMonsters", true), l.diff, w.gameTime.Load())
	cats := st.FilteredSpawningCategories(fr, en, pe)
	spawn.ShuffleChunks(ws.rnd, ws.chunks)
	if len(cats) == 0 {
		return
	}
	sink := sinkFunc(func(m *spawn.Mob) { ws.add(w, m) })
	for _, c := range ws.chunks {
		ws.eng.SpawnForChunk(l, ws.rnd, c[0], c[1], st, cats, sink)
	}
}

type sinkFunc func(*spawn.Mob)

func (f sinkFunc) AddMob(m *spawn.Mob) { f(m) }

var spawnReasons = map[spawn.Reason]api.SpawnReason{
	spawn.ReasonNatural:         api.SpawnNatural,
	spawn.ReasonChunkGeneration: api.SpawnChunkGen,
	spawn.ReasonSpawner:         api.SpawnSpawner,
}

// add turns a spawn decision into a server Mob and makes it visible to
// later obstruction checks of this pass.
func (ws *worldSpawn) add(w *World, sm *spawn.Mob) {
	dt := data.Canonical().Entity(data.EntityID(sm.Type.Kind))
	if dt == nil {
		return
	}
	m := newMob(w, dt, api.Location{World: w.name, Pos: api.Vec3{X: sm.X, Y: sm.Y, Z: sm.Z}, Yaw: sm.Yaw, Pitch: sm.Pitch})
	m.sp = &mobSpawn{typ: sm.Type, reason: sm.Reason, managed: true, persistent: sm.Persistent,
		rnd: rng.NewLegacy(int64(ws.seeds.Uint64())), attrs: sm.Attrs, leftHanded: sm.LeftHanded}
	if w.s.bus.HasHandlers(api.KindEntitySpawn) {
		r, ok := spawnReasons[sm.Reason]
		if !ok {
			r = api.SpawnDefault
		}
		ev := &api.EntitySpawnEvent{Entity: m, Location: m.Location(), Reason: r}
		if w.s.bus.Dispatch(ev).(*api.EntitySpawnEvent).Cancelled() {
			return
		}
	}
	w.mobsMu.Lock()
	w.mobs[m.e.ID] = m
	w.mobsMu.Unlock()
	ws.mobs = append(ws.mobs, m)
	box := m.e.Box()
	ws.lvl.index(&spawnEnt{box: fromEntityBox(box), typ: sm.Type, mob: m,
		blocks: blocksBuilding(dt), hard: hardCollision(dt)})
	ws.stats.spawned.Add(1)
	for _, p := range sm.Passengers {
		ws.add(w, p)
	}
}

// despawn is Mob.checkDespawn for every mob in an entity-ticking chunk,
// followed by the serverAiStep idle counter. Engine-spawned mobs in
// unloaded chunks are dropped: entities are not stored with chunks yet.
func (ws *worldSpawn) despawn(w *World) {
	l := &ws.lvl
	for _, m := range ws.mobs {
		sp := m.sp
		if m.removed.Load() || !sp.typ.Mob {
			continue
		}
		m.mu.Lock()
		pos := m.e.Pos
		m.mu.Unlock()
		cx, cz := int(math.Floor(pos.X))>>4, int(math.Floor(pos.Z))>>4
		if !l.loaded(cx, cz) {
			if sp.managed && !sp.persistent {
				ws.remove(m)
			}
			continue
		}
		if !l.entityTicking(cx, cz) {
			continue
		}
		if sp.rnd == nil {
			sp.rnd = rng.NewLegacy(int64(ws.seeds.Uint64()))
		}
		res := spawn.CheckDespawn(l, spawn.DespawnState{
			Type: sp.typ, X: pos.X, Y: pos.Y, Z: pos.Z,
			PersistenceRequired: sp.persistent, CustomPersistence: sp.typ.CustomPersist, FarAway: sp.typ.FarAway,
			NoActionTime: sp.noAction,
		}, sp.rnd)
		sp.noAction = res.NoActionTime
		if res.Remove {
			ws.remove(m)
			continue
		}
		sp.noAction++
	}
}

func (ws *worldSpawn) remove(m *Mob) {
	m.Remove()
	ws.lvl.unindex(m)
	ws.stats.despawned.Add(1)
}

// spawnMobsForChunkGeneration is the hook for the chunk-generation spawn
// step (creature packs placed when a chunk reaches the spawn status). It
// is not called yet: the vanilla generator stages are still being built.
// It must run on the tick goroutine with the chunk and its neighbours
// loaded; the accessor random is approximated by the level random.
func (w *World) spawnMobsForChunkGeneration(cx, cz int) {
	ws := w.spawnState()
	if ws == nil || !w.ruleBool("spawn_mobs", "doMobSpawning", true) {
		return
	}
	l := &ws.lvl
	l.haveLast = false
	bx, bz := cx*16+8, cz*16+8
	by := l.Height(spawn.HeightmapWorldSurface, bx, bz)
	ws.eng.SpawnMobsForChunkGeneration(l, spawn.ChunkGenSeed(w.seed, cx, cz), ws.rnd, bx, by, bz, cx, cz,
		sinkFunc(func(m *spawn.Mob) { ws.add(w, m) }))
}
