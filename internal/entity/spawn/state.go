package spawn

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Entity is the view of an existing entity that natural spawning counts.
type Entity struct {
	Type    *EntityType
	X, Y, Z float64
	// Persistent is isPersistenceRequired() || requiresCustomPersistence().
	Persistent bool
}

// PotentialCalculator is the spawn-cost charge field (soul sand valley,
// warped forest).
type PotentialCalculator struct {
	charges []pointCharge
}

type pointCharge struct {
	x, y, z int
	charge  float64
}

func (p *PotentialCalculator) AddCharge(x, y, z int, charge float64) {
	if charge != 0 {
		p.charges = append(p.charges, pointCharge{x, y, z, charge})
	}
}

// EnergyChange is getPotentialEnergyChange(pos, charge).
func (p *PotentialCalculator) EnergyChange(x, y, z int, charge float64) float64 {
	if charge == 0 {
		return 0
	}
	var e float64
	for _, c := range p.charges {
		d := distSq(float64(c.x-x), float64(c.y-y), float64(c.z-z))
		if d == 0 {
			e += math.Inf(1)
		} else {
			e += c.charge / math.Sqrt(d)
		}
	}
	return e * charge
}

// PlayersNear returns the indices (into Level.Players) of players close
// enough for spawning to chunk (cx, cz): ChunkMap.getPlayersCloseForSpawning.
type PlayersNear func(cx, cz int) []int

// DefaultPlayersNear implements the documented rule: non-spectators whose
// horizontal distance to the chunk center is below 128 blocks. The
// reference additionally requires the chunk to be within the player-ticket
// spawn range (8 chunks), which a server should apply by passing its own
// PlayersNear.
func DefaultPlayersNear(players []Player) PlayersNear {
	return func(cx, cz int) []int {
		var out []int
		mx, mz := float64(cx*16+8), float64(cz*16+8)
		for i, p := range players {
			if !p.Spectator && distSq(mx-p.X, 0, mz-p.Z) < 16384 {
				out = append(out, i)
			}
		}
		return out
	}
}

// SpawnState is NaturalSpawner.SpawnState for one tick.
type SpawnState struct {
	e               *Engine
	l               Level
	SpawnableChunks int
	Counts          [NumCategories]int
	potential       PotentialCalculator
	near            PlayersNear
	playerCounts    map[int]*[NumCategories]int

	lastX, lastY, lastZ int
	lastType            *EntityType
	lastCharge          float64
}

// NewSpawnState is NaturalSpawner.createState. loaded reports whether the
// chunk holding an entity is loaded (the ChunkGetter); entities in unloaded
// chunks are not counted.
func (e *Engine) NewSpawnState(l Level, spawnableChunks int, entities []Entity, loaded func(cx, cz int) bool, near PlayersNear) *SpawnState {
	s := &SpawnState{e: e, l: l, SpawnableChunks: spawnableChunks, near: near, playerCounts: map[int]*[NumCategories]int{}}
	for _, en := range entities {
		if en.Persistent || en.Type.Category == Misc {
			continue
		}
		x, y, z := blockPos(en.X), blockPos(en.Y), blockPos(en.Z)
		if loaded != nil && !loaded(x>>4, z>>4) {
			continue
		}
		if cost, ok := e.Data.Settings(l.Biome(x, y, z)).Cost(en.Type); ok {
			s.potential.AddCharge(x, y, z, cost.Charge)
		}
		if en.Type.Mob {
			s.addLocal(x>>4, z>>4, en.Type.Category)
		}
		s.Counts[en.Type.Category]++
	}
	return s
}

func (s *SpawnState) addLocal(cx, cz int, c Category) {
	for _, p := range s.near(cx, cz) {
		pc := s.playerCounts[p]
		if pc == nil {
			pc = &[NumCategories]int{}
			s.playerCounts[p] = pc
		}
		pc[c]++
	}
}

// CanSpawnGlobal is canSpawnForCategoryGlobal.
func (s *SpawnState) CanSpawnGlobal(c Category) bool {
	return s.Counts[c] < c.Info().Max*s.SpawnableChunks/MagicNumber
}

// CanSpawnLocal is LocalMobCapCalculator.canSpawn: some nearby player still
// has room in the category.
func (s *SpawnState) CanSpawnLocal(c Category, cx, cz int) bool {
	for _, p := range s.near(cx, cz) {
		pc := s.playerCounts[p]
		if pc == nil || pc[c] < c.Info().Max {
			return true
		}
	}
	return false
}

// CanSpawn is the SpawnState spawn-potential predicate.
func (s *SpawnState) CanSpawn(t *EntityType, x, y, z int) bool {
	s.lastX, s.lastY, s.lastZ, s.lastType = x, y, z, t
	cost, ok := s.e.Data.Settings(s.l.Biome(x, y, z)).Cost(t)
	if !ok {
		s.lastCharge = 0
		return true
	}
	s.lastCharge = cost.Charge
	return s.potential.EnergyChange(x, y, z, cost.Charge) <= cost.EnergyBudget
}

// AfterSpawn records a spawned mob in counts, potential and local caps.
func (s *SpawnState) AfterSpawn(m *Mob) {
	x, y, z := m.BlockPos()
	charge := 0.0
	if x == s.lastX && y == s.lastY && z == s.lastZ && m.Type == s.lastType {
		charge = s.lastCharge
	} else if cost, ok := s.e.Data.Settings(s.l.Biome(x, y, z)).Cost(m.Type); ok {
		charge = cost.Charge
	}
	s.potential.AddCharge(x, y, z, charge)
	s.Counts[m.Type.Category]++
	s.addLocal(x>>4, z>>4, m.Type.Category)
}

// FilteredSpawningCategories is getFilteredSpawningCategories(state,
// friendlies, enemies) plus the caller-side persistent-category gate
// (creature category only every 400 game ticks; see TickGates).
func (s *SpawnState) FilteredSpawningCategories(friendlies, enemies, persistent bool) []Category {
	var out []Category
	for _, c := range SpawningCategories {
		i := c.Info()
		if (friendlies || !i.Friendly) && (enemies || i.Friendly) && (persistent || !i.Persistent) && s.CanSpawnGlobal(c) {
			out = append(out, c)
		}
	}
	return out
}

// SpawnForChunk is NaturalSpawner.spawnForChunk.
func (e *Engine) SpawnForChunk(l Level, r rng.Source, cx, cz int, s *SpawnState, cats []Category, sink Sink) {
	for _, c := range cats {
		if s.CanSpawnLocal(c, cx, cz) {
			e.SpawnCategoryForChunk(c, l, r, cx, cz, s.CanSpawn, s.AfterSpawn, sink)
		}
	}
}

// TickGates derives the friendly/enemy/persistent flags ServerChunkCache
// passes to category filtering (documented behaviour, see doc.go).
func TickGates(spawnMobs, spawnMonsters bool, d Difficulty, gameTime int64) (friendlies, enemies, persistent bool) {
	return spawnMobs, spawnMobs && spawnMonsters && d != Peaceful, gameTime%400 == 0
}

// ShuffleChunks is Util.shuffle over the spawning chunk list, drawn from
// the level random before chunks are ticked.
func ShuffleChunks[T any](r rng.Source, list []T) {
	for i := len(list); i > 1; i-- {
		j := r.NextIntn(int32(i))
		list[i-1], list[j] = list[j], list[i-1]
	}
}

// TickInput is what one natural-spawning pass over a level needs.
type TickInput struct {
	// Chunks are the chunks eligible for spawning this tick (loaded,
	// entity-ticking, with a non-spectator player close enough); their
	// count is the spawnable chunk count for the global caps.
	Chunks   [][2]int
	Entities []Entity
	// Loaded reports whether an entity's chunk counts (ChunkGetter).
	Loaded        func(cx, cz int) bool
	Near          PlayersNear
	SpawnMobs     bool // game rule spawn_mobs
	SpawnMonsters bool // game rule spawn_monsters
	GameTime      int64
}

// TickNatural runs one natural-spawning pass the way ServerChunkCache
// does: build the SpawnState, filter categories, shuffle the chunk list
// with the level random, then spawnForChunk each chunk. The orchestration
// order follows documented behaviour and is not oracle-verified; the
// per-chunk step is.
func (e *Engine) TickNatural(l Level, r rng.Source, in TickInput, sink Sink) *SpawnState {
	st := e.NewSpawnState(l, len(in.Chunks), in.Entities, in.Loaded, in.Near)
	fr, en, pe := TickGates(in.SpawnMobs, in.SpawnMonsters, l.Difficulty(), in.GameTime)
	cats := st.FilteredSpawningCategories(fr, en, pe)
	chunks := append([][2]int(nil), in.Chunks...)
	ShuffleChunks(r, chunks)
	for _, c := range chunks {
		e.SpawnForChunk(l, r, c[0], c[1], st, cats, sink)
	}
	return st
}
