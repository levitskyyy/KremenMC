package spawn

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// AABB is an axis-aligned box in block coordinates.
type AABB struct{ MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64 }

// Player is the spawn-relevant view of a player.
type Player struct {
	X, Y, Z   float64
	Spectator bool
	Creative  bool
}

// Difficulty is the world difficulty.
type Difficulty uint8

const (
	Peaceful Difficulty = iota
	Easy
	Normal
	Hard
)

// DifficultyInstance is the regional difficulty at a position.
type DifficultyInstance struct {
	Base      Difficulty
	Effective float32
	Special   float32 // getSpecialMultiplier
}

// IsHarderThan is DifficultyInstance.isHarderThan.
func (d DifficultyInstance) IsHarderThan(v float32) bool { return d.Effective > v }

// NewDifficultyInstance computes vanilla regional difficulty from the base
// difficulty, the level day time, the chunk inhabited time and the moon
// brightness (documented formula; see finalize.go for verification status).
func NewDifficultyInstance(base Difficulty, dayTime, inhabited int64, moon float32) DifficultyInstance {
	eff := calculateDifficulty(base, dayTime, inhabited, moon)
	var special float32
	switch {
	case eff < 2:
		special = 0
	case eff > 4:
		special = 1
	default:
		special = (eff - 2) / 2
	}
	return DifficultyInstance{base, eff, special}
}

func calculateDifficulty(base Difficulty, dayTime, inhabited int64, moon float32) float32 {
	if base == Peaceful {
		return 0
	}
	f := float32(0.75)
	g := clampf((float32(dayTime)+-72000)/1440000, 0, 1) * 0.25
	f += g
	scale := float32(0.75)
	if base == Hard {
		scale = 1
	}
	h := clampf(float32(inhabited)/3600000, 0, 1) * scale
	h += clampf(moon*0.25, 0, g)
	if base == Easy {
		h *= 0.5
	}
	f += h
	return float32(base) * f
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// StructureHit reports one structure start referencing a position's chunk:
// whether the position lies in its overall bounding box and in any piece.
type StructureHit struct {
	Name    string
	InBox   bool
	InPiece bool
}

// Level is the world view the engine reads. Implementations must answer
// with the same data the reference ServerLevel would (light after updates,
// heightmaps of the loaded chunk, biome after zoom, ...).
type Level interface {
	Dimension() *Dimension
	SeaLevel() int
	BlockState(x, y, z int) data.StateID
	// SkyLight and BlockLight are the raw light-engine values
	// (getBrightness(LightLayer.SKY/BLOCK, pos)).
	SkyLight(x, y, z int) int
	BlockLight(x, y, z int) int
	// SkyDarken is the level's current sky darkening (0..11 overworld).
	SkyDarken() int
	// Height is getHeight(heightmap, x, z): one above the top matching block.
	Height(h Heightmap, x, z int) int
	// Biome is the biome key at a position after biome zoom.
	Biome(x, y, z int) string
	Players() []Player
	// SharedSpawn is the world spawn position.
	SharedSpawn() (x, y, z int)
	// CanSpawnEntitiesInChunk reports whether a chunk is entity-ticking.
	CanSpawnEntitiesInChunk(cx, cz int) bool
	// NoCollision is CollisionGetter.noCollision(box): blocks, world border
	// and hard-collision entities.
	NoCollision(box AABB) bool
	// IsUnobstructed reports that no live entity which blocks building
	// intersects the box.
	IsUnobstructed(box AABB) bool
	// Structures lists structure starts whose references cover the chunk of
	// the position.
	Structures(x, y, z int) []StructureHit
	Difficulty() Difficulty
	DifficultyAt(x, y, z int) DifficultyInstance
	Thundering() bool
	Seed() int64
	MoonBrightness() float32
	// SurfaceSlimeChance is the gameplay/surface_slime_spawn_chance
	// environment attribute at a position.
	SurfaceSlimeChance(x, y, z int) float32
}

func blockPos(v float64) int { return int(math.Floor(v)) }

// rawBrightness is getRawBrightness(pos, darken).
func rawBrightness(l Level, x, y, z, darken int) int {
	return max(l.SkyLight(x, y, z)-darken, l.BlockLight(x, y, z))
}

// MaxLocalRawBrightness is LevelReader.getMaxLocalRawBrightness(pos).
func MaxLocalRawBrightness(l Level, x, y, z int) int {
	return maxLocalRawBrightness(l, x, y, z, l.SkyDarken())
}

func maxLocalRawBrightness(l Level, x, y, z, darken int) int {
	if x < -30000000 || z < -30000000 || x >= 30000000 || z >= 30000000 {
		return 15
	}
	return rawBrightness(l, x, y, z, darken)
}

// lightMagic is getLightLevelDependentMagicValue(pos).
func lightMagic(l Level, x, y, z int) float32 {
	// Explicit conversions keep Go from fusing multiply-adds; the reference
	// rounds every float operation.
	f := float32(MaxLocalRawBrightness(l, x, y, z)) / 15
	g := f / float32(4-float32(3*f))
	a := l.Dimension().AmbientLight
	return g + float32(a*float32(1-g))
}

// pathfindingCostFromLight is getPathfindingCostFromLightLevels(pos).
func pathfindingCostFromLight(l Level, x, y, z int) float32 {
	return lightMagic(l, x, y, z) - 0.5
}

func containsAnyLiquid(l Level, b AABB) bool {
	x0, y0, z0 := blockPos(b.MinX), blockPos(b.MinY), blockPos(b.MinZ)
	x1, y1, z1 := int(math.Ceil(b.MaxX)), int(math.Ceil(b.MaxY)), int(math.Ceil(b.MaxZ))
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			for z := z0; z < z1; z++ {
				if props(l.BlockState(x, y, z)).fluid() != FluidNone {
					return true
				}
			}
		}
	}
	return false
}

func nearestPlayer(l Level, x, y, z float64) (Player, float64, bool) {
	best, bestD, found := Player{}, -1.0, false
	for _, p := range l.Players() {
		if p.Spectator {
			continue
		}
		d := distSq(p.X-x, p.Y-y, p.Z-z)
		if !found || d < bestD {
			best, bestD, found = p, d, true
		}
	}
	return best, bestD, found
}

// distSq is dx*dx+dy*dy+dz*dz rounded per operation (no fused multiply-add).
func distSq(dx, dy, dz float64) float64 {
	return float64(dx*dx) + float64(dy*dy) + float64(dz*dz)
}
