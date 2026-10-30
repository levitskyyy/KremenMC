package gen

import (
	"slices"
	"sort"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// StructureStart is an opaque structure start owned by the structure stage.
// Encode must be deterministic in content; it is persisted with the chunk.
type StructureStart interface {
	Encode() nbt.Compound
}

// RawStart keeps a structure start as NBT; it is what storage decodes into
// unless Config.DecodeStart is set.
type RawStart nbt.Compound

func (r RawStart) Encode() nbt.Compound { return nbt.Compound(r) }

// Tick is a block or fluid tick scheduled during generation.
type Tick struct {
	X, Y, Z  int32 // absolute
	Type     string
	Delay    int32
	Priority int32
}

// ProtoChunk is a chunk in generation. The embedded world.Chunk holds
// blocks, biomes, final heightmaps, light and pending block entities.
//
// Concurrency: the pipeline hands a ProtoChunk to stages only under its
// scheduling rules, so stages never lock. Data of a chunk below
// StatusInitializeLight can still be changed by neighbouring feature steps;
// from StatusInitializeLight on blocks are sealed, and once at StatusFull the
// pipeline never modifies the chunk again.
type ProtoChunk struct {
	*world.Chunk

	status atomic.Uint32

	// Starts are structure starts by structure id; Refs are packed
	// ChunkPos (world.ChunkPos.Key) of chunks holding a start of that
	// structure, kept sorted. Iterate maps via SortedStarts/SortedRefs.
	Starts map[string]StructureStart
	Refs   map[string][]int64

	// Entities are entities created by generation, as entity NBT.
	Entities []nbt.Compound
	// PostProcessing holds packed section-relative positions per section
	// (vanilla ProtoChunk.packOffsetCoordinates) to update on promotion.
	PostProcessing [][]int16
	BlockTicks     []Tick
	FluidTicks     []Tick
	tickIdx        map[tickKey]struct{} // BlockTicks and FluidTicks, built on first schedule (ticks.go)
	// Data is free-form persisted state owned by stages (e.g. blending,
	// below-zero retrogen). Stages must namespace their keys.
	Data nbt.Compound
}

// NewProtoChunk returns an empty chunk at StatusEmpty.
func NewProtoChunk(pos world.ChunkPos, dim world.Dimension) *ProtoChunk {
	plains, _ := data.Canonical().BiomeByName("plains")
	return &ProtoChunk{
		Chunk:          world.NewChunk(pos.X, pos.Z, dim, plains),
		Starts:         map[string]StructureStart{},
		Refs:           map[string][]int64{},
		PostProcessing: make([][]int16, dim.Sections()),
	}
}

// Status is the highest completed generation status.
func (c *ProtoChunk) Status() Status { return Status(c.status.Load()) }

// SetStatus is used by storage implementations when decoding.
func (c *ProtoChunk) SetStatus(s Status) { c.status.Store(uint32(s)) }

// Height reads a heightmap at local x,z. The *_WG maps use the same
// predicates as WORLD_SURFACE and OCEAN_FLOOR.
func (c *ProtoChunk) Height(h world.Heightmap, x, z int) int {
	switch h {
	case world.WorldSurfaceWG:
		h = world.WorldSurface
	case world.OceanFloorWG:
		h = world.OceanFloor
	}
	return c.Chunk.Height(h, x, z)
}

// AddRef records a reference to a start of structure id in chunk src.
func (c *ProtoChunk) AddRef(id string, src world.ChunkPos) {
	k := int64(src.Key())
	l := c.Refs[id]
	i, found := slices.BinarySearch(l, k)
	if !found {
		c.Refs[id] = slices.Insert(l, i, k)
	}
}

// SortedStarts returns structure ids with starts in this chunk, sorted.
func (c *ProtoChunk) SortedStarts() []string { return sortedKeys(c.Starts) }

// SortedRefs returns structure ids referenced from this chunk, sorted.
func (c *ProtoChunk) SortedRefs() []string { return sortedKeys(c.Refs) }

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// MarkPostProcess queues an absolute position for post-processing.
func (c *ProtoChunk) MarkPostProcess(x, y, z int) {
	si := (y - c.Dim.MinY) >> 4
	if si < 0 || si >= len(c.PostProcessing) {
		return
	}
	c.PostProcessing[si] = append(c.PostProcessing[si], int16(x&15|(y&15)<<4|(z&15)<<8))
}

// Promote copies the generated content into dst, a fresh chunk at the same
// position (as world.Generator receives it). dst does not share memory
// with c afterwards.
func (c *ProtoChunk) Promote(dst *world.Chunk) { c.promote(dst, nil) }

// promote is Promote that, with p set, also applies p's post-processing.
func (c *ProtoChunk) promote(dst *world.Chunk, p *Pipeline) {
	for i := range c.Sections {
		dst.Sections[i].Blocks = c.Sections[i].Blocks.Clone()
		dst.Sections[i].Biomes = c.Sections[i].Biomes.Clone()
	}
	for i := range c.SkyLight {
		dst.SkyLight[i] = c.SkyLight[i].Clone()
		dst.BlockLight[i] = c.BlockLight[i].Clone()
	}
	dst.LightReady = c.LightReady
	dst.BlockEntities = slices.Clone(c.BlockEntities)
	for i := range dst.BlockEntities {
		dst.BlockEntities[i].Data = cloneCompound(dst.BlockEntities[i].Data)
	}
	dst.InhabitedTime = c.InhabitedTime
	dst.PendingTicks = c.pendingTicks()
	dst.GenEntities = nil
	for _, e := range c.Entities {
		dst.GenEntities = append(dst.GenEntities, cloneCompound(e))
	}
	if p != nil {
		p.postProcess(c, dst)
	}
	dst.Finish()
}

func cloneCompound(c nbt.Compound) nbt.Compound {
	if c == nil {
		return nil
	}
	o := make(nbt.Compound, len(c))
	for k, v := range c {
		o[k] = cloneValue(v)
	}
	return o
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case nbt.Compound:
		return cloneCompound(x)
	case []nbt.Compound:
		o := make([]nbt.Compound, len(x))
		for i := range x {
			o[i] = cloneCompound(x[i])
		}
		return o
	case nbt.List:
		o := nbt.List{Type: x.Type, Items: make([]any, len(x.Items))}
		for i := range x.Items {
			o.Items[i] = cloneValue(x.Items[i])
		}
		return o
	case []byte:
		return slices.Clone(x)
	case []int32:
		return slices.Clone(x)
	case []int64:
		return slices.Clone(x)
	}
	return v
}
