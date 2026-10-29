package spawn

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// CustomSpawnRules is SpawnData.CustomSpawnRules: inclusive light ranges
// that replace the type's spawn predicate.
type CustomSpawnRules struct {
	BlockLightMin, BlockLightMax int
	SkyLightMin, SkyLightMax     int
}

// Valid is CustomSpawnRules.isValidPosition.
func (c CustomSpawnRules) Valid(l Level, x, y, z int) bool {
	b, s := l.BlockLight(x, y, z), l.SkyLight(x, y, z)
	return b >= c.BlockLightMin && b <= c.BlockLightMax && s >= c.SkyLightMin && s <= c.SkyLightMax
}

// SpawnData is one spawner potential.
type SpawnData struct {
	Type *EntityType
	// OnlyID is true when the entity NBT holds nothing but "id"; only then
	// does the spawner call finalizeSpawn.
	OnlyID bool
	// Pos is an explicit spawn position from the entity NBT (0-3 coords).
	Pos   []float64
	Rules *CustomSpawnRules
	// Weight is the weight in the spawn potentials list.
	Weight int32
}

// Spawner is BaseSpawner state (monster spawner block entity).
type Spawner struct {
	X, Y, Z            int
	Delay              int // spawnDelay, -1 forces a reroll
	MinDelay, MaxDelay int
	Count              int
	MaxNearby          int
	PlayerRange        int
	Range              int
	Potentials         []*SpawnData
	Next               *SpawnData
}

// NewSpawner returns a spawner with the vanilla defaults (delay 20,
// 200-800, count 4, max nearby 6, player range 16, spawn range 4).
func NewSpawner(x, y, z int, t *EntityType) *Spawner {
	return &Spawner{X: x, Y: y, Z: z, Delay: 20, MinDelay: 200, MaxDelay: 800, Count: 4, MaxNearby: 6, PlayerRange: 16, Range: 4,
		Next: &SpawnData{Type: t, OnlyID: true}}
}

// SpawnerWorld is what a spawner tick needs beyond Level.
type SpawnerWorld interface {
	Level
	Sink
	// SpawnerBlocksWork is the spawner_blocks_work game rule.
	SpawnerBlocksWork() bool
	// CountNearby counts live non-spectator entities of exactly this type
	// whose boxes intersect box.
	CountNearby(t *EntityType, box AABB) int
}

func (s *Spawner) nearPlayer(l Level) bool {
	cx, cy, cz := float64(s.X)+0.5, float64(s.Y)+0.5, float64(s.Z)+0.5
	r := float64(s.PlayerRange)
	for _, p := range l.Players() {
		if p.Spectator {
			continue
		}
		if s.PlayerRange < 0 || distSq(p.X-cx, p.Y-cy, p.Z-cz) < r*r {
			return true
		}
	}
	return false
}

func (s *Spawner) delay(r rng.Source) {
	if s.MaxDelay <= s.MinDelay {
		s.Delay = s.MinDelay
	} else {
		s.Delay = s.MinDelay + int(r.NextIntn(int32(s.MaxDelay-s.MinDelay)))
	}
	if d := pickSpawnData(s.Potentials, r); d != nil {
		s.Next = d
	}
}

func pickSpawnData(list []*SpawnData, r rng.Source) *SpawnData {
	var total int32
	for _, d := range list {
		total += d.Weight
	}
	if total == 0 {
		return nil
	}
	n := r.NextIntn(total)
	for _, d := range list {
		n -= d.Weight
		if n < 0 {
			return d
		}
	}
	return nil
}

// TickSpawner is BaseSpawner.serverTick. r is the level random.
func (e *Engine) TickSpawner(w SpawnerWorld, s *Spawner, r rng.Source) {
	if !s.nearPlayer(w) || !w.SpawnerBlocksWork() {
		return
	}
	if s.Delay == -1 {
		s.delay(r)
	}
	if s.Delay > 0 {
		s.Delay--
		return
	}
	if s.Next == nil {
		s.Next = pickSpawnData(s.Potentials, r)
		if s.Next == nil {
			s.Next = &SpawnData{}
		}
	}
	d := s.Next
	spawned := false
	sr := float64(s.Range)
	for range s.Count {
		t := d.Type
		if t == nil {
			s.delay(r)
			return
		}
		var x, y, z float64
		if len(d.Pos) >= 1 {
			x = d.Pos[0]
		} else {
			x = float64(float64(s.X)+float64((r.NextDouble()-r.NextDouble())*sr)) + 0.5
		}
		if len(d.Pos) >= 2 {
			y = d.Pos[1]
		} else {
			y = float64(s.Y + int(r.NextIntn(3)) - 1)
		}
		if len(d.Pos) >= 3 {
			z = d.Pos[2]
		} else {
			z = float64(float64(s.Z)+float64((r.NextDouble()-r.NextDouble())*sr)) + 0.5
		}
		if !w.NoCollision(t.SpawnAABB(x, y, z)) {
			continue
		}
		bx, by, bz := blockPos(x), blockPos(y), blockPos(z)
		if d.Rules != nil {
			if !t.Category.Info().Friendly && w.Difficulty() == Peaceful {
				continue
			}
			if !d.Rules.Valid(w, bx, by, bz) {
				continue
			}
		} else if !e.CheckSpawnRules(w, t, ReasonSpawner, bx, by, bz, r) {
			continue
		}
		box := AABB{float64(s.X), float64(s.Y), float64(s.Z), float64(s.X + 1), float64(s.Y + 1), float64(s.Z + 1)}
		box = AABB{box.MinX - sr, box.MinY - sr, box.MinZ - sr, box.MaxX + sr, box.MaxY + sr, box.MaxZ + sr}
		if w.CountNearby(t, box) >= s.MaxNearby {
			s.delay(r)
			return
		}
		m := &Mob{Type: t, X: x, Y: y, Z: z, Reason: ReasonSpawner}
		m.Yaw = r.NextFloat() * 360
		if t.Mob {
			mx, my, mz := m.BlockPos()
			if d.Rules == nil && !e.mobCheckSpawnRules(w, t, mx, my, mz) || !e.checkSpawnObstruction(w, t, m.BoundingBox(), mx, my, mz) {
				continue
			}
			if d.OnlyID {
				e.FinalizeSpawn(w, r, m, w.DifficultyAt(mx, my, mz), ReasonSpawner, nil)
			}
		}
		w.AddMob(m)
		spawned = true
	}
	if spawned {
		s.delay(r)
	}
}
