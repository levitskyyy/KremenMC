package world

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
)

var errBadPalette = errors.New("world: palette index out of range")

var (
	BlockStrategy = NewStrategy(4096, 4, 8, data.NumStates())
	BiomeStrategy = NewStrategy(64, 1, 3, len(data.Canonical().Biomes))
)

// SetBiomeRegistrySize adjusts the direct biome width to match the biome
// registry sent to clients. Call before creating chunks.
func SetBiomeRegistrySize(n int) { BiomeStrategy.GlobalBits = GlobalBits(n) }

// Dimension describes vertical bounds. MinY and Height are multiples of 16.
type Dimension struct {
	MinY        int
	Height      int
	HasSkylight bool
}

var (
	Overworld = Dimension{MinY: -64, Height: 384, HasSkylight: true}
	Nether    = Dimension{MinY: 0, Height: 256}
	End       = Dimension{MinY: 0, Height: 256}
)

func (d Dimension) Sections() int       { return d.Height >> 4 }
func (d Dimension) MinSection() int     { return d.MinY >> 4 }
func (d Dimension) MaxY() int           { return d.MinY + d.Height - 1 }
func (d Dimension) Contains(y int) bool { return y >= d.MinY && y < d.MinY+d.Height }

func (d Dimension) Validate() error {
	if d.MinY%16 != 0 || d.Height%16 != 0 || d.Height <= 0 || d.Height > 4064 {
		return fmt.Errorf("world: bad dimension %+v", d)
	}
	return nil
}

type Section struct {
	Blocks Container
	Biomes Container
	nonAir uint16
	fluids uint16
}

func newSection(biome data.BiomeID) Section {
	return Section{
		Blocks: NewContainer(BlockStrategy, uint32(data.Air)),
		Biomes: NewContainer(BiomeStrategy, uint32(biome)),
	}
}

func (s *Section) NonAir() int { return int(s.nonAir) }
func (s *Section) Fluids() int { return int(s.fluids) }
func (s *Section) Empty() bool { return s.nonAir == 0 }

func sectionIndex(x, y, z int) int { return y<<8 | z<<4 | x }

func (s *Section) Block(x, y, z int) data.StateID {
	return data.StateID(s.Blocks.Get(sectionIndex(x, y, z)))
}

// SetBlock sets a block in section-local coordinates and maintains counters.
func (s *Section) SetBlock(x, y, z int, st data.StateID) data.StateID {
	old := data.StateID(s.Blocks.Set(sectionIndex(x, y, z), uint32(st)))
	if old != st {
		s.account(old, -1)
		s.account(st, 1)
	}
	return old
}

func (s *Section) account(st data.StateID, d int) {
	f := data.Flags(st)
	if f&data.FlagAir == 0 {
		s.nonAir = uint16(int(s.nonAir) + d)
	}
	if f&data.FlagFluid != 0 {
		s.fluids = uint16(int(s.fluids) + d)
	}
}

// Recount recomputes non-air and fluid counters from the block container.
func (s *Section) Recount() {
	s.nonAir, s.fluids = 0, 0
	s.Blocks.Count(func(v uint32, n int) {
		f := data.Flags(data.StateID(v))
		if f&data.FlagAir == 0 {
			s.nonAir += uint16(n)
		}
		if f&data.FlagFluid != 0 {
			s.fluids += uint16(n)
		}
	})
}

// SetAllBlocks replaces the whole section from 4096 states (y<<8|z<<4|x order).
func (s *Section) SetAllBlocks(states []uint32) {
	s.Blocks.SetAll(states)
	s.Recount()
}

// Biome returns the biome of a 4x4x4 cell (cell coordinates 0..3).
func (s *Section) Biome(cx, cy, cz int) data.BiomeID {
	return data.BiomeID(s.Biomes.Get(cy<<4 | cz<<2 | cx))
}

func (s *Section) SetBiome(cx, cy, cz int, b data.BiomeID) {
	s.Biomes.Set(cy<<4|cz<<2|cx, uint32(b))
}

type BlockEntity struct {
	X, Z uint8 // chunk-local
	Y    int32
	// NetType is the minecraft:block_entity_type registry ID, or -1 if unknown (not sent).
	NetType int32
	ID      string
	Data    nbt.Compound
}

// Chunk is a 16-wide column. Mutation is reserved to the owning region worker
// and always takes mu; the owner may read without locking, other goroutines
// must hold RLock (EncodeChunkData does this itself).
type Chunk struct {
	mu   sync.RWMutex
	X, Z int32
	Dim  Dimension

	Sections []Section
	// light per section from MinSection-1 to MaxSection+1
	SkyLight   []Nibbles
	BlockLight []Nibbles
	// LightReady is set once sky/block light have been computed.
	LightReady bool

	heights [numHeightmaps][256]uint16

	BlockEntities []BlockEntity
	// Extra keeps unrecognised top-level disk NBT for lossless rewrite.
	Extra         nbt.Compound
	InhabitedTime int64

	dirty atomic.Bool
}

func NewChunk(x, z int32, dim Dimension, biome data.BiomeID) *Chunk {
	n := dim.Sections()
	c := &Chunk{X: x, Z: z, Dim: dim, Sections: make([]Section, n), SkyLight: make([]Nibbles, n+2), BlockLight: make([]Nibbles, n+2)}
	for i := range c.Sections {
		c.Sections[i] = newSection(biome)
	}
	return c
}

func (c *Chunk) Pos() ChunkPos { return ChunkPos{c.X, c.Z} }

func (c *Chunk) Lock()    { c.mu.Lock() }
func (c *Chunk) Unlock()  { c.mu.Unlock() }
func (c *Chunk) RLock()   { c.mu.RLock() }
func (c *Chunk) RUnlock() { c.mu.RUnlock() }

func (c *Chunk) Dirty() bool      { return c.dirty.Load() }
func (c *Chunk) MarkDirty()       { c.dirty.Store(true) }
func (c *Chunk) clearDirty() bool { return c.dirty.Swap(false) }

// Section returns the section containing absolute y, or nil when out of range.
func (c *Chunk) Section(y int) *Section {
	i := (y - c.Dim.MinY) >> 4
	if y < c.Dim.MinY || i >= len(c.Sections) {
		return nil
	}
	return &c.Sections[i]
}

// Block returns the state at chunk-local x,z (0..15) and absolute y.
func (c *Chunk) Block(x, y, z int) data.StateID {
	s := c.Section(y)
	if s == nil {
		return data.Air
	}
	return s.Block(x, y&15, z)
}

// SetBlock sets a block and updates counters and heightmaps. Light is not
// touched; World.SetBlock runs the light engine.
func (c *Chunk) SetBlock(x, y, z int, st data.StateID) data.StateID {
	s := c.Section(y)
	if s == nil {
		return data.Air
	}
	c.mu.Lock()
	old := s.SetBlock(x, y&15, z, st)
	if old != st {
		c.updateHeights(x, y, z, st)
		c.dirty.Store(true)
	}
	c.mu.Unlock()
	return old
}

// Biome returns the biome at chunk-local block coordinates.
func (c *Chunk) Biome(x, y, z int) data.BiomeID {
	s := c.Section(y)
	if s == nil {
		return 0
	}
	return s.Biome(x>>2, (y&15)>>2, z>>2)
}

func (c *Chunk) SetBiome(x, y, z int, b data.BiomeID) {
	s := c.Section(y)
	if s == nil {
		return
	}
	c.mu.Lock()
	s.SetBiome(x>>2, (y&15)>>2, z>>2, b)
	c.dirty.Store(true)
	c.mu.Unlock()
}

// SetBlockEntity replaces or adds a block entity at the given local position.
func (c *Chunk) SetBlockEntity(be BlockEntity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dirty.Store(true)
	for i := range c.BlockEntities {
		e := &c.BlockEntities[i]
		if e.X == be.X && e.Y == be.Y && e.Z == be.Z {
			*e = be
			return
		}
	}
	c.BlockEntities = append(c.BlockEntities, be)
}

func (c *Chunk) RemoveBlockEntity(x, y, z int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.BlockEntities {
		e := &c.BlockEntities[i]
		if int(e.X) == x && int(e.Y) == y && int(e.Z) == z {
			c.BlockEntities = append(c.BlockEntities[:i], c.BlockEntities[i+1:]...)
			c.dirty.Store(true)
			return
		}
	}
}

// Finish recounts sections and rebuilds heightmaps after bulk section edits.
func (c *Chunk) Finish() {
	for i := range c.Sections {
		c.Sections[i].Recount()
	}
	c.RecalcHeightmaps()
	c.dirty.Store(true)
}
