package server

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Monster spawner block entities (minecraft:mob_spawner) found in loaded
// chunk NBT tick with spawn.Engine.TickSpawner. The countdown lives here
// and is not written back to the chunk, so a reloaded spawner restarts
// from its saved Delay.

const spawnerScanInterval = 20

type spawnerState struct {
	sp    *spawn.Spawner
	chunk *world.Chunk
	raw   map[*spawn.SpawnData]nbt.Compound
	data  nbt.Compound
}

// spawnerWorld is the SpawnerWorld view for one pass.
type spawnerWorld struct {
	*spawnLevel
	ws    *worldSpawn
	w     *World
	works bool
}

func (s spawnerWorld) AddMob(m *spawn.Mob)     { s.ws.add(s.w, m) }
func (s spawnerWorld) SpawnerBlocksWork() bool { return s.works }

func (s spawnerWorld) CountNearby(t *spawn.EntityType, box spawn.AABB) int {
	n := 0
	s.eachEnt(box, func(e *spawnEnt) bool {
		if e.typ == t && !e.spectator && boxOverlap(e.box, box) && (e.mob == nil || !e.mob.removed.Load()) {
			n++
		}
		return true
	})
	return n
}

var spawnerNetType = world.BlockEntityNetType("minecraft:mob_spawner")

func (ws *worldSpawn) tickSpawners(w *World) {
	l := &ws.lvl
	if ws.tick >= ws.nextScan {
		ws.nextScan = ws.tick + spawnerScanInterval
		ws.scanSpawners(w)
	}
	if len(ws.spawners) == 0 {
		return
	}
	sw := spawnerWorld{spawnLevel: l, ws: ws, w: w, works: w.ruleBool("spawner_blocks_work", "spawnerBlocksWork", true)}
	for pos, st := range ws.spawners {
		cx, cz := pos[0]>>4, pos[2]>>4
		if l.chunk(cx, cz) != st.chunk || spawn.BlockName(l.BlockState(pos[0], pos[1], pos[2])) != "minecraft:spawner" {
			delete(ws.spawners, pos)
			continue
		}
		if !l.blockTicking(cx, cz) {
			continue
		}
		d0, next0 := st.sp.Delay, st.sp.Next
		ws.eng.TickSpawner(sw, st.sp, ws.rnd)
		if st.sp.Next != next0 {
			if raw, ok := st.raw[st.sp.Next]; ok {
				st.data = cloneCompound(st.data)
				st.data["SpawnData"] = raw
				ws.sendSpawnerData(w, pos, st)
			}
		}
		if d0 <= 0 && st.sp.Delay > 0 {
			// BaseSpawner.delay broadcasts block event 1 (client resets its
			// spin timer).
			ws.broadcastChunk(w, cx, cz, encode(&packets.ClientboundBlockEvent{
				Location: proto.BlockPos{X: int32(pos[0]), Y: int32(pos[1]), Z: int32(pos[2])},
				Byte1:    1, BlockID: int32(data.Canonical().BlockID(l.BlockState(pos[0], pos[1], pos[2]))),
			}))
		}
	}
}

// scanSpawners picks up spawner block entities in block-ticking chunks.
func (ws *worldSpawn) scanSpawners(w *World) {
	l := &ws.lvl
	seen := map[uint64]bool{}
	r := int32(l.simDist)
	for _, pc := range l.pchunks {
		for dz := -r; dz <= r; dz++ {
			for dx := -r; dx <= r; dx++ {
				cp := world.ChunkPos{X: pc.X + dx, Z: pc.Z + dz}
				if seen[cp.Key()] {
					continue
				}
				seen[cp.Key()] = true
				if c := l.chunk(int(cp.X), int(cp.Z)); c != nil {
					ws.scanChunk(w, c)
				}
			}
		}
	}
}

func (ws *worldSpawn) scanChunk(w *World, c *world.Chunk) {
	type found struct {
		pos  [3]int
		data nbt.Compound
	}
	var fs []found
	needNet := false
	c.RLock()
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		if be.ID != "minecraft:mob_spawner" {
			continue
		}
		pos := [3]int{int(c.X)<<4 | int(be.X), int(be.Y), int(c.Z)<<4 | int(be.Z)}
		if st := ws.spawners[pos]; st != nil && st.chunk == c {
			continue
		}
		fs = append(fs, found{pos, be.Data})
		needNet = needNet || (be.NetType < 0 && spawnerNetType >= 0)
	}
	var sts []*spawnerState
	for _, f := range fs {
		sts = append(sts, parseSpawner(f.pos, f.data))
	}
	c.RUnlock()
	if len(fs) == 0 {
		return
	}
	if needNet {
		// Disk block entities carry no network type, so the chunk packet
		// omitted them; let later chunk packets include spawners and send
		// the data to players who already have the chunk.
		c.Lock()
		for i := range c.BlockEntities {
			if be := &c.BlockEntities[i]; be.ID == "minecraft:mob_spawner" && be.NetType < 0 {
				be.NetType = spawnerNetType
			}
		}
		c.Unlock()
	}
	for i, st := range sts {
		st.chunk = c
		ws.spawners[fs[i].pos] = st
		if needNet {
			ws.sendSpawnerData(w, fs[i].pos, st)
		}
	}
}

func (ws *worldSpawn) sendSpawnerData(w *World, pos [3]int, st *spawnerState) {
	if spawnerNetType < 0 {
		return
	}
	b, err := nbt.Marshal(nil, "", st.data, true)
	if err != nil {
		return
	}
	ws.broadcastChunk(w, pos[0]>>4, pos[2]>>4, encode(&packets.ClientboundBlockEntityData{
		Location: proto.BlockPos{X: int32(pos[0]), Y: int32(pos[1]), Z: int32(pos[2])},
		Action:   spawnerNetType, NBTData: proto.RawNBT(b),
	}))
}

func (ws *worldSpawn) broadcastChunk(w *World, cx, cz int, raw []byte) {
	k := world.ChunkPos{X: int32(cx), Z: int32(cz)}.Key()
	for _, p := range w.s.list {
		if p.world.Load() == w && p.hasChunk(k) {
			p.c.SendRaw(raw)
		}
	}
}

func cloneCompound(c nbt.Compound) nbt.Compound {
	out := make(nbt.Compound, len(c)+1)
	for k, v := range c {
		out[k] = v
	}
	return out
}

// parseSpawner reads BaseSpawner.load fields from block entity NBT.
func parseSpawner(pos [3]int, d nbt.Compound) *spawnerState {
	s := spawn.NewSpawner(pos[0], pos[1], pos[2], nil)
	s.Next = nil
	st := &spawnerState{sp: s, raw: map[*spawn.SpawnData]nbt.Compound{}, data: d}
	geti := func(k string, def int) int {
		if v, ok := nbtInt(d[k]); ok {
			return v
		}
		return def
	}
	s.Delay = geti("Delay", 20)
	if _, ok := d["MinSpawnDelay"]; ok {
		s.MinDelay = geti("MinSpawnDelay", s.MinDelay)
		s.MaxDelay = geti("MaxSpawnDelay", s.MaxDelay)
		s.Count = geti("SpawnCount", s.Count)
	}
	if _, ok := d["MaxNearbyEntities"]; ok {
		s.MaxNearby = geti("MaxNearbyEntities", s.MaxNearby)
		s.PlayerRange = geti("RequiredPlayerRange", s.PlayerRange)
	}
	if _, ok := d["SpawnRange"]; ok {
		s.Range = geti("SpawnRange", s.Range)
	}
	if sd, ok := d["SpawnData"].(nbt.Compound); ok {
		s.Next = parseSpawnData(sd, 1)
		st.raw[s.Next] = sd
	}
	if l, ok := d["SpawnPotentials"]; ok {
		for _, it := range nbtList(l) {
			e, ok := it.(nbt.Compound)
			if !ok {
				continue
			}
			sd, _ := e["data"].(nbt.Compound)
			w, ok := nbtInt(e["weight"])
			if !ok {
				w = 1
			}
			p := parseSpawnData(sd, int32(w))
			s.Potentials = append(s.Potentials, p)
			st.raw[p] = sd
		}
	} else {
		if s.Next == nil {
			s.Next = &spawn.SpawnData{}
		}
		s.Potentials = []*spawn.SpawnData{s.Next}
		s.Next.Weight = 1
	}
	return st
}

func parseSpawnData(sd nbt.Compound, weight int32) *spawn.SpawnData {
	out := &spawn.SpawnData{Weight: weight}
	ent, _ := sd["entity"].(nbt.Compound)
	if id, ok := ent["id"].(string); ok {
		if t, ok := spawn.EntityByName(id); ok {
			out.Type = t
		}
	}
	out.OnlyID = len(ent) == 1 && ent["id"] != nil
	for _, v := range nbtList(ent["Pos"]) {
		if f, ok := v.(float64); ok && len(out.Pos) < 3 {
			out.Pos = append(out.Pos, f)
		}
	}
	if r, ok := sd["custom_spawn_rules"].(nbt.Compound); ok {
		rules := &spawn.CustomSpawnRules{BlockLightMax: 15, SkyLightMax: 15}
		rules.BlockLightMin, rules.BlockLightMax = lightRange(r["block_light_limit"])
		rules.SkyLightMin, rules.SkyLightMax = lightRange(r["sky_light_limit"])
		out.Rules = rules
	}
	return out
}

// lightRange decodes an InclusiveRange<Integer>: a bare int or
// {min_inclusive, max_inclusive}; absent means 0..15.
func lightRange(v any) (int, int) {
	if n, ok := nbtInt(v); ok {
		return n, n
	}
	if c, ok := v.(nbt.Compound); ok {
		lo, ok1 := nbtInt(c["min_inclusive"])
		hi, ok2 := nbtInt(c["max_inclusive"])
		if ok1 && ok2 {
			return lo, hi
		}
	}
	return 0, 15
}

func nbtInt(v any) (int, bool) {
	switch n := v.(type) {
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	}
	return 0, false
}

func nbtList(v any) []any {
	switch l := v.(type) {
	case nbt.List:
		return l.Items
	case []nbt.Compound:
		out := make([]any, len(l))
		for i := range l {
			out[i] = l[i]
		}
		return out
	}
	return nil
}
