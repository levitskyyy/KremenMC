// Package gen is the chunk generation pipeline: vanilla 26.3 chunk
// statuses, proto-chunks, a stage interface and a parallel, deterministic
// scheduler. Stages (biome source, density functions, surface, carvers,
// features, structures, light, spawning) plug in per status.
//
// # Statuses and dependencies
//
// Status and Pyramid mirror ChunkStatus and ChunkPyramid.GENERATION_PYRAMID
// of the reference (checked by TestPyramidMatchesReference). In 26.3 noise,
// surface and carvers form one terrain status. A step to status S for chunk
// C runs once C is at S-1 and every chunk at Chebyshev distance d is at
// Pyramid[S].Direct[d]. The step sees exactly that neighbourhood through
// StageContext and may write blocks within Pyramid[S].WriteRadius.
//
// # Determinism
//
// The result is a function of the seed and the stages only: independent of
// worker count, request order, cancellation, eviction and restarts. Vanilla
// is not: a feature step writes into its 8 neighbours and reads what they
// contain, so its result depends on which neighbouring feature steps ran
// before it, which in vanilla follows thread timing and player movement.
// The pipeline fixes that order and adds two dependencies vanilla lacks:
//
//  1. Decoration order. Two feature steps whose 3x3 zones overlap (chunks at
//     distance <= 2) never run concurrently; the chunk with the lower
//     DecorationRank runs first. Ranks depend on (x mod 3, z mod 3) only, so
//     every 3x3 neighbourhood decorates in ascending rank (pinned by
//     TestDecorationOrderPinned). Before C decorates, all lower-ranked
//     chunks within 2 must have decorated, transitively: up to 7 chunks
//     away (TestDecorationClosure).
//  2. Sealing. Initialize_light of C waits until every chunk within 1 has
//     finished features, so from initialize_light on a chunk's blocks never
//     change. Get at a lower status returns a chunk that later neighbour
//     feature steps may still modify.
//
// Reads follow the guaranteed status, not the actual one: a stage reading
// blocks of a neighbour guaranteed only structure_starts sees air even if
// that chunk is further along (vanilla would see whatever happens to be
// there). Reads beyond the dependency radius return air and are counted;
// writes outside the write radius are dropped and logged, as vanilla does.
//
// Where this differs from vanilla: whenever vanilla happened to decorate two
// overlapping chunks in the other order, or a feature reads blocks two or
// more chunks away, or a feature writes outside its 3x3. Vanilla's own
// order is not reproducible (it depends on timing and player paths), so no
// fixed order can match it in general. A single-threaded vanilla server
// decorating a fresh area spiralling out from spawn mostly decorates in
// increasing distance from the player; the fixed rank order matches that
// for pairs of chunks only by coincidence. Cost: a full chunk needs
// features up to 9 chunks away and terrain up to 10 (vanilla: 1 and 2),
// which only matters at the edge of the generated area.
//
// Structure starts shared by several chunks must not hold mutable state
// changed during placement unless every chunk touching it lies within
// distance 2 of each other (then the decoration order serialises them).
//
// Stages must draw randomness only from the seed and positions
// (StageContext.Positional, Decoration, LargeFeature), never from shared or
// per-worker RNG, and must iterate maps in sorted order.
//
// # Scheduling
//
// Requests create jobs (chunk, status) with reference counts; concurrent
// requests for the same chunk and status share one job. A job becomes
// ready when its dependencies are done and runs on the worker pool. When
// the last request for a job is cancelled, unstarted jobs nobody else needs
// are abandoned recursively; running steps finish and are kept. Chunks
// that are not loaded first load from Storage, so a chunk persisted at a
// status resumes there. With Storage, idle chunks beyond CacheSize are
// saved and evicted; Close saves the rest.
//
// A chunk that reached full is sealed: the world owns it and the storage
// may not keep the pipeline's copy. A full chunk stays cached while a
// neighbour within 1 is not full (their light step reads it), up to twice
// CacheSize. If a sealed chunk is evicted and a neighbour's step needs it
// again (decoration order, the features radius), it is rebuilt from storage
// up to terrain only; its features and later steps only advance the status
// and it is never saved, so feature writes are not replayed into
// neighbours and the world's stored chunk is not overwritten. A new
// request for the full chunk itself (the world lost it) unseals it.
package gen
