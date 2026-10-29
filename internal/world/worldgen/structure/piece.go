package structure

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// This file is the contract between the structure framework and the code
// that builds individual structures (template structures, jigsaw, and the
// hardcoded legacy piece structures: mineshaft, stronghold, fortress,
// monument, mansion, temples, ...). It is not wired into the generation
// pipeline yet.
//
// Lifecycle, mirroring vanilla's structure_starts / structure_references /
// features statuses:
//
//  1. Starts (status structure_starts, chunk C only). For each structure set
//     whose placement accepts C (Placement.IsSpreadChunk, concentric rings,
//     exclusion zones), the set's structures are tried; a Type's Generate
//     receives a GenContext for C and returns the pieces of the start, or
//     none. The result depends only on the world seed and C: Generate must
//     not read other chunks and draws randomness only from GenContext.Random.
//  2. References (status structure_references). Every chunk within 8 records
//     the starts whose BoundingBox (union of piece boxes) intersects it.
//  3. Placement (status features, per decoration step, before that step's
//     features). For each referenced start whose Step equals the current
//     step, each piece whose box intersects the decorated chunk's box gets
//     Place with the chunk's box as clip. Pieces must write only inside the
//     clip box, so that every chunk writes its own part of the structure
//     and the result does not depend on decoration order.
//
// Persistence: Start.Encode/DecodeStart produce the vanilla
// structures.starts entry ({id, ChunkX, ChunkZ, references, Children}); each
// child is a piece compound with "id" (the structure_piece type) and the
// piece-specific fields written by Piece.Save. Pieces must round-trip
// exactly, so that save -> restart -> continue places the same blocks.

// Box is a vanilla BoundingBox: inclusive block coordinates.
type Box struct {
	MinX, MinY, MinZ int32
	MaxX, MaxY, MaxZ int32
}

// Intersects reports whether b and o share at least one block.
func (b Box) Intersects(o Box) bool {
	return b.MaxX >= o.MinX && b.MinX <= o.MaxX && b.MaxZ >= o.MinZ && b.MinZ <= o.MaxZ &&
		b.MaxY >= o.MinY && b.MinY <= o.MaxY
}

// Encapsulate returns the smallest box holding b and o.
func (b Box) Encapsulate(o Box) Box {
	return Box{min(b.MinX, o.MinX), min(b.MinY, o.MinY), min(b.MinZ, o.MinZ),
		max(b.MaxX, o.MaxX), max(b.MaxY, o.MaxY), max(b.MaxZ, o.MaxZ)}
}

// Contains reports whether x,y,z lies in b.
func (b Box) Contains(x, y, z int32) bool {
	return x >= b.MinX && x <= b.MaxX && y >= b.MinY && y <= b.MaxY && z >= b.MinZ && z <= b.MaxZ
}

// IntArray is the BB int array vanilla saves: minX minY minZ maxX maxY maxZ.
func (b Box) IntArray() []int32 { return []int32{b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ} }

// ChunkBox is the full-height box of chunk cx,cz for a dimension.
func ChunkBox(cx, cz int32, minY, height int32) Box {
	return Box{cx << 4, minY, cz << 4, cx<<4 + 15, minY + height - 1, cz<<4 + 15}
}

// Rotation and Mirror are vanilla Rotation and Mirror.
type Rotation uint8

const (
	RotNone Rotation = iota
	Rot90            // clockwise_90
	Rot180
	Rot270 // counterclockwise_90
)

type Mirror uint8

const (
	MirrorNone Mirror = iota
	MirrorLeftRight
	MirrorFrontBack
)

// Level is what a piece writes to while placing: the WorldGenRegion of the
// decorated chunk. Positions are absolute. Writes outside the clip box
// passed to Place are a contract violation.
type Level interface {
	MinY() int32
	MaxY() int32 // exclusive
	Block(x, y, z int32) data.StateID
	SetBlock(x, y, z int32, s data.StateID) bool
	// Height is the heightmap value at x,z ("WORLD_SURFACE_WG", ...).
	Height(heightmap string, x, z int32) int32
	Biome(x, y, z int32) data.BiomeID
	// SetBlockEntity stores block entity NBT (with "id") at x,y,z.
	SetBlockEntity(x, y, z int32, id string, nbt nbt.Compound)
	// AddEntity stores an entity created by the structure (entity NBT with
	// "id" and "Pos") as a pending chunk entity.
	AddEntity(e nbt.Compound)
	// MarkPostProcess queues x,y,z for shape updates on chunk promotion.
	MarkPostProcess(x, y, z int32)
}

// Piece is one part of a structure start. Implementations are immutable
// after generation except through Place (see below).
type Piece interface {
	// Type is the structure_piece registry id ("minecraft:igloo", ...).
	Type() string
	// Box is the piece's bounding box; it must cover every block Place can
	// write.
	Box() Box
	// Place writes the piece's blocks inside clip. r is the per-chunk
	// random vanilla hands to postProcess: seeded by the decoration seed of
	// the placing chunk with the structure's feature index and step.
	// pivot is the start's first piece origin (some pieces use it).
	// Place may run once per intersecting chunk, from different workers
	// (never concurrently for overlapping 3x3 neighbourhoods); it must not
	// mutate the piece unless the mutation is a pure function of the piece
	// and identical for every chunk (vanilla adjusts some piece heights on
	// first placement: such pieces must compute the adjustment
	// deterministically, independent of which chunk places first).
	Place(lv Level, r *rng.WorldgenRandom, clip Box, chunkX, chunkZ int32) error
	// Save writes the piece-specific fields (vanilla addAdditionalSaveData
	// plus the common id, GD, O, BB). Load must invert it.
	Save() nbt.Compound
}

// PieceLoader rebuilds a piece from its saved compound; registered per
// structure_piece id with RegisterPiece.
type PieceLoader func(c nbt.Compound) (Piece, error)

var pieceLoaders = map[string]PieceLoader{}

// RegisterPiece registers a loader for a structure_piece id. Call from
// init() in the file implementing the piece.
func RegisterPiece(id string, l PieceLoader) { pieceLoaders[id] = l }

// GenContext is what a structure Type sees while generating a start: the
// chunk, the seeded random and the terrain/biome probes. Everything must be
// a pure function of the seed and the chunk.
type GenContext struct {
	Seed           int64
	ChunkX, ChunkZ int32
	// Random is vanilla's structure-start random: a legacy WorldgenRandom
	// seeded with setLargeFeatureSeed(seed, chunkX, chunkZ).
	Random *rng.WorldgenRandom
	// Height returns the terrain-only height at block x,z for a heightmap
	// ("WORLD_SURFACE_WG", "OCEAN_FLOOR_WG"); nil until the terrain height
	// API is wired.
	Height func(x, z int32, heightmap string) int32
	// Biome returns the biome source's biome at quart coordinates.
	Biome func(qx, qy, qz int32) data.BiomeID
	// BiomeOK reports whether the biome at block x,y,z is in the
	// structure's biomes list.
	BiomeOK func(x, y, z int32) bool
	// MinY, MaxY bound the dimension (both 0: the overworld, -64..320).
	MinY, MaxY int32
}

// Type generates the starts of one worldgen/structure entry.
type Type interface {
	// Step is the decoration step the structure's pieces are placed in.
	Step() int
	// Generate returns the start's pieces at the chunk, or nil when the
	// structure does not generate there (biome or terrain checks failed).
	Generate(ctx *GenContext) ([]Piece, error)
}

// Start is a generated structure start, persisted as vanilla
// structures.starts[id].
type Start struct {
	ID             string
	ChunkX, ChunkZ int32
	References     int32
	Pieces         []Piece
}

// Box is the union of the piece boxes.
func (s *Start) Box() Box {
	b := s.Pieces[0].Box()
	for _, p := range s.Pieces[1:] {
		b = b.Encapsulate(p.Box())
	}
	return b
}

// Encode is the vanilla structure start compound; it implements
// gen.StructureStart.
func (s *Start) Encode() nbt.Compound {
	ch := make([]any, len(s.Pieces))
	for i, p := range s.Pieces {
		ch[i] = p.Save()
	}
	return nbt.Compound{
		"id": s.ID, "ChunkX": s.ChunkX, "ChunkZ": s.ChunkZ, "references": s.References,
		"Children": nbt.List{Type: nbt.TagCompound, Items: ch},
	}
}
