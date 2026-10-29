package gen

import "github.com/levitskyyy/KremenMC/internal/world"

// Decoration order. Two feature steps whose 3x3 write zones overlap
// (Chebyshev distance <= 2) run in a fixed order: the chunk with the lower
// rank first. Ranks repeat with period 3 on both axes, so every 3x3 block
// holds 9 distinct ranks and no two conflicting chunks share one. The rank
// table minimises the dependency closure (see TestDecorationClosure):
// before a chunk decorates, chunks up to 7 away may need to decorate first.

// orderPeriod is 2*WriteRadius(features)+1.
const orderPeriod = 3

// conflictRadius is the distance within which two feature steps touch a
// common chunk.
const conflictRadius = orderPeriod - 1

// decorationRank[(x mod 3)*3 + (z mod 3)]
var decorationRank = [orderPeriod * orderPeriod]int8{3, 5, 4, 2, 6, 1, 8, 7, 0}

func mod3(v int32) int32 { return (v%3 + 3) % 3 }

// DecorationRank is the position of p in the fixed feature order (0..8).
func DecorationRank(p world.ChunkPos) int {
	return int(decorationRank[mod3(p.X)*3+mod3(p.Z)])
}

// decoratesBefore reports whether a must finish its feature step before b
// starts its own; both must be distinct and within conflictRadius.
func decoratesBefore(a, b world.ChunkPos) bool { return DecorationRank(a) < DecorationRank(b) }

func cheb(a, b world.ChunkPos) int32 {
	return max(abs32(a.X-b.X), abs32(a.Z-b.Z))
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
