package gen

import (
	"fmt"
	"strings"
)

// Status is a vanilla 26.3 chunk status. In 26.3 noise, surface and carvers
// are a single terrain status.
type Status uint8

const (
	StatusEmpty Status = iota
	StatusStructureStarts
	StatusStructureReferences
	StatusBiomes
	StatusTerrain
	StatusFeatures
	StatusInitializeLight
	StatusLight
	StatusSpawn
	StatusFull
	NumStatuses = int(StatusFull) + 1
)

var statusNames = [NumStatuses]string{
	"empty", "structure_starts", "structure_references", "biomes", "terrain",
	"features", "initialize_light", "light", "spawn", "full",
}

func (s Status) String() string {
	if int(s) < NumStatuses {
		return statusNames[s]
	}
	return fmt.Sprintf("status(%d)", s)
}

// ID is the namespaced registry name ("minecraft:terrain").
func (s Status) ID() string { return "minecraft:" + s.String() }

// ParseStatus accepts "terrain" or "minecraft:terrain".
func ParseStatus(name string) (Status, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	for i, n := range statusNames {
		if n == name {
			return Status(i), true
		}
	}
	return 0, false
}

// Parent is the status a chunk must have before this step runs; vanilla
// makes empty its own parent.
func (s Status) Parent() Status {
	if s == StatusEmpty {
		return StatusEmpty
	}
	return s - 1
}

// FinalHeightmaps reports whether the chunk carries the final heightmaps
// (WORLD_SURFACE, OCEAN_FLOOR, MOTION_BLOCKING*) rather than the *_WG ones.
func (s Status) FinalHeightmaps() bool { return s >= StatusTerrain }

// Step mirrors vanilla ChunkStep in ChunkPyramid.GENERATION_PYRAMID.
type Step struct {
	Target Status
	// Direct[d] is the minimum status every chunk at Chebyshev distance d
	// must have; Direct[0] is the parent. A stage may read chunks only
	// within len(Direct)-1, and only data present at Direct[d].
	Direct []Status
	// WriteRadius is vanilla blockStateWriteRadius: block writes through
	// the stage view are allowed within this distance; -1 allows none.
	WriteRadius int
}

// Radius is the direct dependency radius.
func (s *Step) Radius() int { return len(s.Direct) - 1 }

// RequiredAt returns the status required at distance d, ok=false beyond the radius.
func (s *Step) RequiredAt(d int) (Status, bool) {
	if d < 0 || d >= len(s.Direct) {
		return 0, false
	}
	return s.Direct[d], true
}

func ring(n int, first Status, rest Status) []Status {
	r := make([]Status, n+1)
	for i := range r {
		r[i] = rest
	}
	r[0] = first
	return r
}

func withRing(r []Status, d int, s Status) []Status { r[d] = s; return r }

// Pyramid is vanilla 26.3 ChunkPyramid.GENERATION_PYRAMID, verified against
// the reference by TestPyramidMatchesReference.
var Pyramid = [NumStatuses]Step{
	{Target: StatusEmpty, Direct: nil, WriteRadius: -1},
	{Target: StatusStructureStarts, Direct: []Status{StatusEmpty}, WriteRadius: -1},
	{Target: StatusStructureReferences, Direct: ring(8, StatusStructureStarts, StatusStructureStarts), WriteRadius: -1},
	{Target: StatusBiomes, Direct: ring(8, StatusStructureReferences, StatusStructureStarts), WriteRadius: -1},
	{Target: StatusTerrain, Direct: withRing(ring(8, StatusBiomes, StatusStructureStarts), 1, StatusBiomes), WriteRadius: 0},
	{Target: StatusFeatures, Direct: withRing(ring(8, StatusTerrain, StatusStructureStarts), 1, StatusTerrain), WriteRadius: 1},
	{Target: StatusInitializeLight, Direct: []Status{StatusFeatures}, WriteRadius: -1},
	{Target: StatusLight, Direct: []Status{StatusInitializeLight, StatusInitializeLight}, WriteRadius: -1},
	{Target: StatusSpawn, Direct: []Status{StatusLight, StatusBiomes}, WriteRadius: -1},
	{Target: StatusFull, Direct: []Status{StatusSpawn}, WriteRadius: -1},
}

// Accumulated returns vanilla's accumulatedDependencies for the step to s:
// the minimum status every chunk at distance d must reach before s can be
// produced at the centre, following direct dependencies transitively.
func Accumulated(s Status) []Status {
	st := &Pyramid[s]
	if len(st.Direct) == 0 {
		return nil
	}
	var acc []Status
	put := func(d int, v Status) {
		for len(acc) <= d {
			acc = append(acc, StatusEmpty)
		}
		acc[d] = max(acc[d], v)
	}
	for i, req := range st.Direct {
		put(i, req)
		if req == s {
			continue
		}
		for j, v := range Accumulated(req) {
			put(i+j, v)
		}
	}
	return acc
}
