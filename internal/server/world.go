package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/generator"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

type worldTick = world.TickContext

type WorldConfig struct {
	Name      string
	Dir       string
	Seed      int64
	Flat      bool
	FlatSpec  string
	Workers   int
	TickProcs int
	// Dim is "" or "overworld", "the_nether", "the_end" (dimension.go).
	Dim string
}

type levelFile struct {
	Seed       int64      `json:"seed"`
	Flat       bool       `json:"flat"`
	Spawn      [3]float64 `json:"spawn"`
	SpawnYaw   float32    `json:"spawnYaw"`
	HasSpawn   bool       `json:"hasSpawn"`
	GameTime   int64      `json:"gameTime"`
	DayTime    int64      `json:"dayTime"`
	Difficulty string     `json:"difficulty"`
	UUID       string     `json:"uuid"`
}

type World struct {
	s     *Server
	name  string
	key   api.Key
	uuid  api.UUID
	seed  int64
	flat  bool
	dir   string
	w     *world.World
	store *anvil.Storage
	pipe  *gen.Pipeline // nil for flat worlds
	dimID int32
	dim   dimKind
	poi   portalPOI

	mu         sync.RWMutex
	spawn      api.Location
	difficulty api.Difficulty
	storming   bool
	thundering bool
	rules      map[string]string

	gameTime atomic.Int64
	dayTime  atomic.Int64

	changesMu sync.Mutex
	changes   map[uint64][]int64

	mobsMu sync.RWMutex
	mobs   map[int32]*Mob

	ticketsMu sync.Mutex
	tickets   map[string]world.TicketID
	spawnTkt  world.TicketID

	pdata *dataContainer

	// tick-owned tracking snapshot
	track    []trackEntry
	trackSet map[int32]struct{}

	// tick-owned mob spawning state (spawn_tick.go)
	spawning *worldSpawn
	spawnErr error
}

// OpenWorld loads or creates a world directory with Anvil storage.
func OpenWorld(s *Server, cfg WorldConfig) (*World, error) {
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "region"), 0o755); err != nil {
		return nil, fmt.Errorf("world %s: %w", cfg.Name, err)
	}
	lf := levelFile{Seed: cfg.Seed, Flat: cfg.Flat}
	if err := readJSON(filepath.Join(cfg.Dir, "kremenmc-level.json"), &lf); err != nil {
		return nil, err
	}
	w := &World{
		s: s, name: cfg.Name, key: api.Key("minecraft:overworld"), seed: lf.Seed, flat: lf.Flat, dir: cfg.Dir,
		difficulty: s.difficulty, rules: map[string]string{}, changes: map[uint64][]int64{},
		mobs: map[int32]*Mob{}, trackSet: map[int32]struct{}{}, tickets: map[string]world.TicketID{}, pdata: newDataContainer(),
	}
	dk := dimOf(cfg.Dim)
	w.dim = dk
	switch {
	case dk != dimOverworld:
		w.key = dk.key()
	case s.defaultWorld() != nil:
		w.key = api.Key("kremenmc:" + cfg.Name)
	}
	if u, err := api.ParseUUID(lf.UUID); err == nil {
		w.uuid = u
	} else {
		h := sha256.Sum256([]byte(cfg.Name + strconv.FormatInt(lf.Seed, 10)))
		copy(w.uuid[:], h[:16])
	}
	if lf.Difficulty != "" {
		w.difficulty = parseDifficulty(lf.Difficulty)
	}
	w.gameTime.Store(lf.GameTime)
	w.dayTime.Store(lf.DayTime)
	id, ok := regsync.Index("minecraft:dimension_type", string(dk.key()))
	if !ok {
		return nil, errors.New("world: dimension type missing from registry data: " + string(dk.key()))
	}
	w.dimID = id

	w.store = anvil.NewStorage(filepath.Join(cfg.Dir, "region"), dk.bounds())
	w.store.GameTime = w.gameTime.Load
	logf := func(f string, a ...any) { s.log.Warn(fmt.Sprintf(f, a...), "world", cfg.Name) }
	var wgen world.Generator
	if lf.Flat {
		f := generator.DefaultFlat()
		if cfg.FlatSpec != "" && cfg.FlatSpec != "{}" {
			if ff, err := generator.ParseFlat(cfg.FlatSpec, "plains"); err == nil {
				f = ff
			}
		}
		wgen = f
	} else {
		p, err := vanillaDimPipeline(dk.genName(), lf.Seed, cfg.Workers, w.store, logf)
		if err != nil {
			return nil, fmt.Errorf("world %s: %w", cfg.Name, err)
		}
		w.pipe = p
		wgen = gen.WorldGenerator{P: p}
	}
	ww, err := world.New(world.Config{
		Dim: dk.bounds(), Generator: wgen, Storage: w.store, Seed: lf.Seed,
		Workers: cfg.Workers, TickWorkers: cfg.TickProcs,
		Hooks: world.Hooks{
			BlockChanged:  w.blockChanged,
			ChunkLoaded:   w.chunkLoaded,
			ChunkUnloaded: w.chunkUnloaded,
		},
		Logf: logf,
	})
	if err != nil {
		if w.pipe != nil {
			w.pipe.Close()
		}
		return nil, err
	}
	w.w = ww
	if dk != dimOverworld {
		w.spawn = dk.defaultSpawn(cfg.Name)
	} else if lf.HasSpawn {
		w.spawn = api.Location{World: cfg.Name, Pos: api.Vec3{X: lf.Spawn[0], Y: lf.Spawn[1], Z: lf.Spawn[2]}, Yaw: lf.SpawnYaw}
		w.spawnTkt = ww.AddTicket(world.ChunkAt(int(lf.Spawn[0]), int(lf.Spawn[2])), 2, world.TicketForced)
	} else {
		if err := w.findSpawn(); err != nil {
			return nil, err
		}
	}
	return w, w.saveLevel()
}

// protoCacheSize bounds the proto-chunks the generation pipeline keeps in
// memory; idle ones beyond it are saved to the region files and dropped.
const protoCacheSize = 2048

// vanillaPipeline runs the verified vanilla overworld statuses (biomes,
// terrain, features) on the generation pipeline. Proto-chunks share the
// world's region files; full chunks are owned by the world, so the pipeline
// never writes them.
func vanillaPipeline(seed int64, workers int, st *anvil.Storage, logf func(string, ...any)) (*gen.Pipeline, error) {
	return vanillaDimPipeline("overworld", seed, workers, st, logf)
}

func vanillaDimPipeline(dim string, seed int64, workers int, st *anvil.Storage, logf func(string, ...any)) (*gen.Pipeline, error) {
	t, err := vanilla.NewTerrain(dim, seed)
	if err != nil {
		return nil, err
	}
	stages, err := t.Stages(dim, seed)
	if err != nil {
		return nil, err
	}
	return gen.New(gen.Config{
		Seed: seed, Dim: dimOfGen(dim).bounds(), Workers: workers, Stages: stages,
		Storage: protoStorage{&gen.AnvilStorage{St: st}}, CacheSize: protoCacheSize, Logf: logf,
	})
}

// protoStorage keeps the pipeline from overwriting chunks the world has
// already taken over (and possibly changed).
type protoStorage struct{ *gen.AnvilStorage }

func (p protoStorage) SaveProto(c *gen.ProtoChunk) error {
	if c.Status() == gen.StatusFull {
		return nil
	}
	return p.AnvilStorage.SaveProto(c)
}

func (w *World) findSpawn() error {
	w.spawnTkt = w.w.AddTicket(world.ChunkPos{}, 2, world.TicketForced)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := w.w.LoadChunk(ctx, world.ChunkPos{})
	if err != nil {
		return fmt.Errorf("world %s: load spawn: %w", w.name, err)
	}
	c.RLock()
	y := c.Height(world.MotionBlocking, 8, 8)
	c.RUnlock()
	w.spawn = api.Location{World: w.name, Pos: api.Vec3{X: 8.5, Y: float64(y), Z: 8.5}}
	return nil
}

func (w *World) saveLevel() error {
	w.mu.RLock()
	sp := w.spawn
	lf := levelFile{
		Seed: w.seed, Flat: w.flat, Spawn: [3]float64{sp.Pos.X, sp.Pos.Y, sp.Pos.Z}, SpawnYaw: sp.Yaw, HasSpawn: true,
		GameTime: w.gameTime.Load(), DayTime: w.dayTime.Load(), Difficulty: w.difficulty.String(), UUID: w.uuid.String(),
	}
	w.mu.RUnlock()
	return writeJSON(filepath.Join(w.dir, "kremenmc-level.json"), lf)
}

func (w *World) close() error {
	errs := []error{w.saveLevel(), w.savePortals(), w.w.Close()}
	if w.pipe != nil {
		errs = append(errs, w.pipe.Close())
	}
	return errors.Join(append(errs, w.store.Close())...)
}

func sectionKey(cx, sy, cz int32) uint64 {
	return uint64(cx)&0x3fffff<<42 | uint64(cz)&0x3fffff<<20 | uint64(sy)&0xfffff
}

func sectionFromKey(k uint64) (cx, sy, cz int32) {
	return int32(int64(k) >> 42), int32(int64(k<<44) >> 44), int32(int64(k<<22) >> 42)
}

func (w *World) blockChanged(x, y, z int, old, s data.StateID) {
	w.portalBlockChanged(x, y, z, old, s)
	k := sectionKey(int32(x>>4), int32(y>>4), int32(z>>4))
	rec := int64(s)<<12 | int64((x&15)<<8|(z&15)<<4|(y&15))
	w.changesMu.Lock()
	w.changes[k] = append(w.changes[k], rec)
	w.changesMu.Unlock()
}

func (w *World) chunkLoaded(c *world.Chunk) {
	if w.s.bus.HasHandlers(api.KindChunkLoad) {
		w.s.bus.Dispatch(&api.ChunkLoadEvent{World: w, X: int(c.X), Z: int(c.Z)})
	}
}

func (w *World) chunkUnloaded(c *world.Chunk) {
	if w.s.bus.HasHandlers(api.KindChunkUnload) {
		w.s.bus.Dispatch(&api.ChunkUnloadEvent{World: w, X: int(c.X), Z: int(c.Z), Save: true})
	}
}

// flushChanges broadcasts this tick's block changes to players that have the
// chunk. Runs on the tick goroutine after the world ticked.
func (w *World) flushChanges(players []*Player) {
	w.changesMu.Lock()
	if len(w.changes) == 0 {
		w.changesMu.Unlock()
		return
	}
	ch := w.changes
	w.changes = make(map[uint64][]int64, len(ch))
	w.changesMu.Unlock()
	for k, recs := range ch {
		cx, sy, cz := sectionFromKey(k)
		var raw []byte
		if len(recs) == 1 {
			r := recs[0]
			l := int32(r & 0xfff)
			raw = encode(&packets.ClientboundBlockUpdate{
				Location: proto.BlockPos{X: cx<<4 | l>>8&15, Y: sy<<4 | l&15, Z: cz<<4 | l>>4&15},
				Type:     int32(r >> 12),
			})
		} else {
			raw = encode(&packets.ClientboundSectionBlocksUpdate{
				ChunkCoordinates: packets.ClientboundSectionBlocksUpdateChunkCoordinates{X: cx, Y: sy, Z: cz},
				Records:          recs,
			})
		}
		ck := world.ChunkPos{X: cx, Z: cz}.Key()
		for _, p := range players {
			if p.hasChunk(ck) {
				p.c.SendRaw(raw)
			}
		}
	}
}

// hashedSeed is what the Login packet carries for biome noise.
func (w *World) hashedSeed() int64 {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(w.seed))
	h := sha256.Sum256(b[:])
	return int64(binary.BigEndian.Uint64(h[:8]))
}

func (w *World) Name() string                      { return w.name }
func (w *World) Key() api.Key                      { return w.key }
func (w *World) UUID() api.UUID                    { return w.uuid }
func (w *World) Seed() int64                       { return w.seed }
func (w *World) Dimension() api.Dimension          { return w.dim.api() }
func (w *World) DimensionType() api.Key            { return w.dim.key() }
func (w *World) MinY() int                         { return w.w.Dim().MinY }
func (w *World) MaxY() int                         { return w.w.Dim().MinY + w.w.Dim().Height }
func (w *World) Time() int64                       { return w.dayTime.Load() % 24000 }
func (w *World) FullTime() int64                   { return w.dayTime.Load() }
func (w *World) PersistentData() api.DataContainer { return w.pdata }

func (w *World) Spawn() api.Location {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.spawn
}

func (w *World) SetSpawn(l api.Location) {
	l.World = w.name
	w.mu.Lock()
	w.spawn = l
	w.mu.Unlock()
	raw := encode(w.spawnPacket())
	for _, p := range w.playerList() {
		p.c.SendRaw(raw)
	}
}

func (w *World) spawnPacket() *packets.ClientboundSetDefaultSpawnPosition {
	sp := w.Spawn()
	b := sp.BlockPos()
	return &packets.ClientboundSetDefaultSpawnPosition{
		GlobalPos: packets.GlobalPos{DimensionName: string(w.key), Location: proto.BlockPos{X: int32(b.X), Y: int32(b.Y), Z: int32(b.Z)}},
		Yaw:       sp.Yaw, Pitch: sp.Pitch,
	}
}

func (w *World) SetTime(t int64) {
	full := w.dayTime.Load()
	w.dayTime.Store(full - full%24000 + t)
	w.broadcastTime()
}

func (w *World) timePacket() *packets.ClientboundSetTime {
	p := &packets.ClientboundSetTime{Age: w.gameTime.Load()}
	if id, ok := regsync.Index("minecraft:world_clock", "minecraft:overworld"); ok {
		rate := float32(1)
		if v, ok := w.GameRule("doDaylightCycle"); ok && v == "false" {
			rate = 0
		}
		p.ClockUpdates = []packets.ClientboundSetTimeClockUpdate{{ID: id, TotalTicks: w.dayTime.Load(), Rate: rate}}
	}
	return p
}

func (w *World) broadcastTime() {
	raw := encode(w.timePacket())
	for _, p := range w.playerList() {
		p.c.SendRaw(raw)
	}
}

func (w *World) Difficulty() api.Difficulty {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.difficulty
}

func (w *World) SetDifficulty(d api.Difficulty) {
	w.mu.Lock()
	w.difficulty = d
	w.mu.Unlock()
	raw := encode(&packets.ClientboundChangeDifficulty{Difficulty: int32(d), DifficultyLocked: false})
	for _, p := range w.playerList() {
		p.c.SendRaw(raw)
	}
}

func (w *World) Storming() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.storming
}

func (w *World) SetStorming(v bool) {
	w.mu.Lock()
	w.storming = v
	w.mu.Unlock()
	ev := uint8(1)
	lvl := float32(0)
	if v {
		ev, lvl = 2, 1
	}
	raws := [][]byte{
		encode(&packets.ClientboundGameEvent{Reason: ev}),
		encode(&packets.ClientboundGameEvent{Reason: 7, GameMode: lvl}),
	}
	for _, p := range w.playerList() {
		for _, r := range raws {
			p.c.SendRaw(r)
		}
	}
}

func (w *World) Thundering() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.thundering
}

func (w *World) SetThundering(v bool) {
	w.mu.Lock()
	w.thundering = v
	w.mu.Unlock()
	lvl := float32(0)
	if v {
		lvl = 1
	}
	raw := encode(&packets.ClientboundGameEvent{Reason: 8, GameMode: lvl})
	for _, p := range w.playerList() {
		p.c.SendRaw(raw)
	}
}

func (w *World) GameRule(name string) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	v, ok := w.rules[name]
	return v, ok
}

func (w *World) SetGameRule(name, value string) bool {
	w.mu.Lock()
	w.rules[name] = value
	w.mu.Unlock()
	return true
}

func (w *World) Block(pos api.BlockPos) api.Block { return &block{w: w, pos: pos} }

func (w *World) BlockState(pos api.BlockPos) api.BlockState {
	return blockState(w.w.Block(pos.X, pos.Y, pos.Z))
}

func (w *World) SetBlockState(pos api.BlockPos, st api.BlockState, _ api.BlockUpdateFlags) bool {
	if st == nil || !w.w.Dim().Contains(pos.Y) {
		return false
	}
	w.w.SetBlock(pos.X, pos.Y, pos.Z, data.StateID(st.ID()))
	return true
}

func (w *World) HighestBlockY(x, z int) int {
	c := w.w.Chunk(world.ChunkAt(x, z))
	if c == nil {
		return w.MinY()
	}
	c.RLock()
	defer c.RUnlock()
	return c.Height(world.MotionBlocking, x&15, z&15) - 1
}

func (w *World) Biome(pos api.BlockPos) api.Key {
	c := w.w.Chunk(world.ChunkAt(pos.X, pos.Z))
	if c == nil {
		return "minecraft:plains"
	}
	c.RLock()
	b := c.Biome(pos.X&15, pos.Y, pos.Z&15)
	c.RUnlock()
	return api.Key("minecraft:" + data.Canonical().BiomeName(b))
}

func (w *World) IsChunkLoaded(x, z int) bool {
	return w.w.Chunk(world.ChunkPos{X: int32(x), Z: int32(z)}) != nil
}

func (w *World) LoadChunk(x, z int, _ bool, done func(bool)) {
	p := world.ChunkPos{X: int32(x), Z: int32(z)}
	go func() {
		_, err := w.w.LoadChunk(context.Background(), p)
		if done != nil {
			w.w.Submit(x<<4, z<<4, func(*worldTick) { done(err == nil) })
		}
	}()
}

func (w *World) UnloadChunk(x, z int, _ bool) bool {
	return !w.w.Ticketed(world.ChunkPos{X: int32(x), Z: int32(z)})
}

func (w *World) LoadedChunks() []api.ChunkPos {
	seen := map[uint64]bool{}
	var out []api.ChunkPos
	for _, p := range w.playerList() {
		for _, k := range p.sentChunks() {
			if !seen[k] && w.w.Chunk(world.PosFromKey(k)) != nil {
				seen[k] = true
				cp := world.PosFromKey(k)
				out = append(out, api.ChunkPos{X: int(cp.X), Z: int(cp.Z)})
			}
		}
	}
	return out
}

func ticketKey(owner string, x, z int) string {
	return owner + "|" + strconv.Itoa(x) + "|" + strconv.Itoa(z)
}

func (w *World) AddChunkTicket(owner string, x, z int) {
	k := ticketKey(owner, x, z)
	w.ticketsMu.Lock()
	defer w.ticketsMu.Unlock()
	if _, ok := w.tickets[k]; ok {
		return
	}
	w.tickets[k] = w.w.AddTicket(world.ChunkPos{X: int32(x), Z: int32(z)}, 0, world.TicketPlugin)
}

func (w *World) RemoveChunkTicket(owner string, x, z int) {
	k := ticketKey(owner, x, z)
	w.ticketsMu.Lock()
	id, ok := w.tickets[k]
	delete(w.tickets, k)
	w.ticketsMu.Unlock()
	if ok {
		w.w.RemoveTicket(id)
	}
}

func (w *World) playerList() []*Player {
	var out []*Player
	for _, p := range w.s.onlinePlayers() {
		if p.World() == api.World(w) {
			out = append(out, p)
		}
	}
	return out
}

func (w *World) Players() []api.Player {
	var out []api.Player
	for _, p := range w.playerList() {
		out = append(out, p)
	}
	return out
}

func (w *World) mobList() []*Mob {
	w.mobsMu.RLock()
	defer w.mobsMu.RUnlock()
	return slices.Collect(maps.Values(w.mobs))
}

func (w *World) Entities() []api.Entity {
	var out []api.Entity
	for _, p := range w.playerList() {
		out = append(out, p)
	}
	for _, m := range w.mobList() {
		out = append(out, m)
	}
	return out
}

func (w *World) NearbyEntities(center api.Vec3, radius float64) []api.Entity {
	var out []api.Entity
	r2 := radius * radius
	for _, e := range w.Entities() {
		if e.Location().Pos.DistanceSq(center) <= r2 {
			out = append(out, e)
		}
	}
	return out
}

func (w *World) Entity(id api.UUID) (api.Entity, bool) {
	for _, e := range w.Entities() {
		if e.UUID() == id {
			return e, true
		}
	}
	return nil, false
}

func (w *World) SpawnEntity(kind api.Key, at api.Location, configure func(api.Entity)) (api.Entity, error) {
	t, ok := data.Canonical().EntityByName(string(kind))
	if !ok {
		return nil, fmt.Errorf("unknown entity type %s", kind)
	}
	if t.Name == "player" {
		return nil, errors.New("cannot spawn players")
	}
	m := newMob(w, t, at)
	if configure != nil {
		configure(m)
	}
	if w.s.bus.HasHandlers(api.KindEntitySpawn) {
		ev := &api.EntitySpawnEvent{Entity: m, Location: at, Reason: api.SpawnPlugin}
		if w.s.bus.Dispatch(ev).(*api.EntitySpawnEvent).Cancelled() {
			return nil, errors.New("spawn cancelled")
		}
	}
	w.mobsMu.Lock()
	w.mobs[m.e.ID] = m
	w.mobsMu.Unlock()
	return m, nil
}

func (w *World) DropItem(at api.Vec3, st api.ItemStack) (api.Entity, error) {
	e := w.SpawnItem(at, stackFromAPI(st), itemPickupDefault)
	if e == nil {
		return nil, errors.New("empty item stack")
	}
	return e, nil
}

func (w *World) PlaySound(snd api.Sound) {
	for _, p := range w.playerList() {
		p.PlaySound(snd)
	}
}

func (w *World) SpawnParticle(api.Key, api.Vec3, int, api.Vec3, float64) {}

// Explode is a block explosion (ServerLevel.explode with interaction
// BLOCK, or NONE when breakBlocks is false); see explosion*.go.
func (w *World) Explode(at api.Vec3, power float32, fire, breakBlocks bool) {
	k := explodeBlock
	if !breakBlocks {
		k = explodeNone
	}
	w.explode(explodeOpts{center: at, power: power, fire: fire, kind: k})
}

func (w *World) Save() error {
	if w == w.s.defaultWorld() {
		w.s.requestPlayerSaves()
	}
	return errors.Join(w.saveLevel(), w.w.SaveAll())
}

// tickEntities runs physics for non-player entities.
func (w *World) tickEntities() {
	for _, m := range w.mobList() {
		if m.removed.Load() {
			w.mobsMu.Lock()
			delete(w.mobs, m.e.ID)
			w.mobsMu.Unlock()
			continue
		}
		m.tasks.run(w.s.CurrentTick())
		if m.itm != nil {
			m.itm.tick(w)
			continue
		}
		m.tickMob()
		if m.liv.attrs == nil && m.e.Pos.Y < float64(w.MinY()-64) {
			m.Remove()
		}
	}
}

type block struct {
	w   *World
	pos api.BlockPos
}

func (b *block) World() api.World                  { return b.w }
func (b *block) Pos() api.BlockPos                 { return b.pos }
func (b *block) State() api.BlockState             { return b.w.BlockState(b.pos) }
func (b *block) Type() api.Key                     { return b.State().Type() }
func (b *block) Biome() api.Key                    { return b.w.Biome(b.pos) }
func (b *block) PersistentData() api.DataContainer { return newDataContainer() }
func (b *block) SetState(s api.BlockState, f api.BlockUpdateFlags) bool {
	return b.w.SetBlockState(b.pos, s, f)
}
func (b *block) Relative(f api.BlockFace) api.Block { return &block{w: b.w, pos: b.pos.Relative(f)} }
func (b *block) LightLevel() uint8                  { return max(b.SkyLight(), b.BlockLight()) }
func (b *block) SkyLight() uint8                    { return b.light(true) }
func (b *block) BlockLight() uint8                  { return b.light(false) }

func (b *block) light(sky bool) uint8 {
	c := b.w.w.Chunk(world.ChunkAt(b.pos.X, b.pos.Z))
	if c == nil || !c.Dim.Contains(b.pos.Y) {
		return 0
	}
	c.RLock()
	defer c.RUnlock()
	si := (b.pos.Y-c.Dim.MinY)>>4 + 1
	li := (b.pos.Y&15)<<8 | (b.pos.Z&15)<<4 | b.pos.X&15
	if sky {
		return c.SkyLight[si].Get(li)
	}
	return c.BlockLight[si].Get(li)
}

func (b *block) BlockEntity() ([]byte, bool) { return nil, false }
func (b *block) SetBlockEntity([]byte) bool  { return false }

func (b *block) Break(drop bool) bool {
	st := b.w.w.Block(b.pos.X, b.pos.Y, b.pos.Z)
	if data.IsAir(st) {
		return false
	}
	b.w.w.SetBlock(b.pos.X, b.pos.Y, b.pos.Z, data.Air)
	if drop {
		b.w.dropBlockNoTool(b.pos, st)
	}
	return true
}

var _ api.World = (*World)(nil)
var _ entity.BlockSource = (*world.World)(nil)
