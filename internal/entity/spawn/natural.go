package spawn

import (
	"math"
	"time"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Engine evaluates spawn decisions against a Level.
type Engine struct {
	Data *Data
	// Now is the wall clock used by date-dependent rules (bats and mob
	// heads around Halloween). Nil means time.Now.
	Now func() time.Time
	// Finalize overrides finalizeSpawn per entity name; nil entries fall
	// back to the built-in hooks (see finalize.go).
	Finalize map[string]FinalizeFunc
}

// NewEngine returns an engine over parsed spawn data.
func NewEngine(d *Data) *Engine { return &Engine{Data: d} }

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// halloween is SpecialDates.isHalloween; the reference reports
// SpecialDates.HALLOWEEN = --10-31 and isHalloween()=false on 10-28, so it
// is treated as a single-day check.
func (e *Engine) halloween() bool {
	t := e.now()
	return t.Month() == time.October && t.Day() == 31
}

// Mob is a spawn decision: an entity the engine asks the server to add.
type Mob struct {
	Type       *EntityType
	X, Y, Z    float64
	Yaw, Pitch float32
	Reason     Reason
	Persistent bool
	LeftHanded bool
	Passengers []*Mob
	Vehicle    *Mob
	// Attrs carries finalizeSpawn results (baby flag, variant, equipment
	// rolls, ...) keyed by hook-defined names.
	Attrs map[string]any
}

func (m *Mob) set(k string, v any) {
	if m.Attrs == nil {
		m.Attrs = map[string]any{}
	}
	m.Attrs[k] = v
}

// BoundingBox is the entity box at the mob position.
func (m *Mob) BoundingBox() AABB { return m.Type.SpawnAABB(m.X, m.Y, m.Z) }

// BlockPos is Entity.blockPosition.
func (m *Mob) BlockPos() (int, int, int) { return blockPos(m.X), blockPos(m.Y), blockPos(m.Z) }

// Sink receives spawned mobs. AddMob must make the mob (and its passengers)
// visible to later Level.IsUnobstructed queries before returning.
type Sink interface {
	AddMob(m *Mob)
}

// SpawnPredicate is NaturalSpawner.SpawnPredicate (the spawn-potential
// check); AfterSpawn is the AfterSpawnCallback.
type (
	SpawnPredicate func(t *EntityType, x, y, z int) bool
	AfterSpawn     func(m *Mob)
)

// SpawnCategoryForChunk is NaturalSpawner.spawnCategoryForChunk: picks a
// random position in chunk (cx, cz) and runs a spawn attempt there.
func (e *Engine) SpawnCategoryForChunk(c Category, l Level, r rng.Source, cx, cz int, pred SpawnPredicate, after AfterSpawn, sink Sink) {
	x := cx*16 + int(r.NextIntn(16))
	z := cz*16 + int(r.NextIntn(16))
	// chunk.getHeight (top block y) + 1 equals the level heightmap value.
	top := l.Height(HeightmapWorldSurface, x, z)
	minY := l.Dimension().MinY
	y := int(r.NextIntn(int32(top-minY+1))) + minY
	if y >= minY+1 {
		e.SpawnCategoryForPosition(c, l, r, cx, cz, x, y, z, pred, after, sink)
	}
}

// SpawnCategoryForPosition is NaturalSpawner.spawnCategoryForPosition with
// the reference draw order: three attempts, each a random pack wandering
// up to five blocks per step.
func (e *Engine) SpawnCategoryForPosition(c Category, l Level, r rng.Source, cx, cz, px, y, pz int, pred SpawnPredicate, after AfterSpawn, sink Sink) {
	if props(l.BlockState(px, y, pz))&bpConductor != 0 {
		return
	}
	count := 0
	for range 3 {
		x, z := px, pz
		var data *SpawnerData
		var group any
		packSize := int(math.Ceil(float64(r.NextFloat() * 4)))
		inPack := 0
		for i := 0; i < packSize; i++ {
			x += int(r.NextIntn(6) - r.NextIntn(6))
			z += int(r.NextIntn(6) - r.NextIntn(6))
			fx, fz := float64(x)+0.5, float64(z)+0.5
			_, dist, ok := nearestPlayer(l, fx, float64(y), fz)
			if !ok || !isRightDistanceToPlayerAndSpawnPoint(l, cx, cz, x, y, z, dist) {
				continue
			}
			if data == nil {
				data = e.randomSpawnMobAt(l, c, r, x, y, z)
				if data == nil {
					break
				}
				packSize = int(data.Count.Sample(r))
			}
			if !e.isValidSpawnPositionForType(l, c, data, x, y, z, dist, r) || (pred != nil && !pred(data.Type, x, y, z)) {
				continue
			}
			t := data.Type
			if !t.Mob {
				return
			}
			m := &Mob{Type: t, X: fx, Y: float64(y), Z: fz, Reason: ReasonNatural}
			m.Yaw = r.NextFloat() * 360
			if !e.isValidPositionForMob(l, m, dist) {
				continue
			}
			group = e.FinalizeSpawn(l, r, m, l.DifficultyAt(x, y, z), ReasonNatural, group)
			count++
			inPack++
			sink.AddMob(m)
			if after != nil {
				after(m)
			}
			if count >= t.ClusterSize {
				return
			}
			if groupLimitReached(m, inPack) {
				break
			}
		}
	}
}

func isRightDistanceToPlayerAndSpawnPoint(l Level, cx, cz, x, y, z int, dist float64) bool {
	if dist <= MinSpawnDistance*MinSpawnDistance {
		return false
	}
	sx, sy, sz := l.SharedSpawn()
	if distSq(float64(sx)+0.5-(float64(x)+0.5), float64(sy)+0.5-float64(y), float64(sz)+0.5-(float64(z)+0.5)) < MinSpawnDistance*MinSpawnDistance {
		return false
	}
	if x>>4 == cx && z>>4 == cz {
		return true
	}
	return l.CanSpawnEntitiesInChunk(x>>4, z>>4)
}

func (e *Engine) randomSpawnMobAt(l Level, c Category, r rng.Source, x, y, z int) *SpawnerData {
	if c == WaterAmbient && e.biomeTag(l, x, y, z, "reduce_water_ambient_spawns") && r.NextFloat() < 0.98 {
		return nil
	}
	return e.MobsAt(l, c, x, y, z).Random(r)
}

// MobsAt is NaturalSpawner.mobsAt: the fortress list on nether bricks inside
// a fortress, else structure overrides, else the biome's spawn list.
func (e *Engine) MobsAt(l Level, c Category, x, y, z int) *WeightedList {
	if e.inNetherFortressBounds(l, c, x, y, z) {
		return e.Data.Structures["minecraft:fortress"].Overrides[Monster].Spawns
	}
	for _, h := range l.Structures(x, y, z) {
		s := e.Data.Structures[h.Name]
		if s == nil || s.Overrides[c] == nil {
			continue
		}
		o := s.Overrides[c]
		if (o.Box == BoxPiece && h.InPiece) || (o.Box == BoxFull && h.InBox) {
			return o.Spawns
		}
	}
	return e.Data.Settings(l.Biome(x, y, z)).Mobs(c)
}

func (e *Engine) inNetherFortressBounds(l Level, c Category, x, y, z int) bool {
	if c != Monster || !e.is(l.BlockState(x, y-1, z), "minecraft:nether_bricks") {
		return false
	}
	if e.Data.Structures["minecraft:fortress"] == nil {
		return false
	}
	for _, h := range l.Structures(x, y, z) {
		if h.Name == "minecraft:fortress" && h.InBox {
			return true
		}
	}
	return false
}

func (e *Engine) isValidSpawnPositionForType(l Level, c Category, d *SpawnerData, x, y, z int, dist float64, r rng.Source) bool {
	t := d.Type
	if t.Category == Misc {
		return false
	}
	dd := float64(t.Category.Info().Despawn)
	if !t.FarFromPlayer && dist > dd*dd {
		return false
	}
	if !t.Summonable || !e.MobsAt(l, c, x, y, z).Contains(d) {
		return false
	}
	if !e.PlacementOK(l, t, x, y, z) {
		return false
	}
	if !e.CheckSpawnRules(l, t, ReasonNatural, x, y, z, r) {
		return false
	}
	return l.NoCollision(t.SpawnAABB(float64(x)+0.5, float64(y), float64(z)+0.5))
}

func (e *Engine) isValidPositionForMob(l Level, m *Mob, dist float64) bool {
	if dist > 16384 && m.Type.FarAway {
		return false
	}
	x, y, z := m.BlockPos()
	return e.mobCheckSpawnRules(l, m.Type, x, y, z) && e.checkSpawnObstruction(l, m.Type, m.BoundingBox(), x, y, z)
}
