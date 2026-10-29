package surface

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// Biomes returns the stored biome of a quart cell, as the reference
// WorldGenRegion would (the chunk holding the cell must be at BIOMES).
// qy is already clamped to the dimension.
type Biomes interface {
	QuartBiome(qx, qy, qz int32) data.BiomeID
}

// Ctx is the per-goroutine evaluation state (the reference
// MaterialRuleContext plus scratch buffers). It is not safe for concurrent
// use; reuse one per worker.
type Ctx struct {
	s      *System
	biomes Biomes
	d      density.Ctx

	x, z           int32
	depth          int32
	minSurface     int32
	gx, gz         int32
	secOK          bool
	sec            float64
	y, above       int32
	below, water   int32
	biomeOK        bool
	biomeID        data.BiomeID
	stampXZ        uint32
	stampY         uint32
	memoStamp      []uint32
	memoVal        []bool
	buf            []uint32
	heights        [256]int32
	sectionChanged []bool
	cellCache      bool
	minY, height   int32 // chunk bounds
	topX, topZ     int32
	topOK          bool
	onEval         func(c *Ctx, r data.StateID)
}

// NewCtx returns an empty context.
func NewCtx() *Ctx { return &Ctx{} }

func (c *Ctx) reset(s *System, b Biomes) {
	c.s, c.biomes = s, b
	c.topOK = false
	if len(c.memoStamp) < s.slots {
		c.memoStamp = make([]uint32, s.slots)
		c.memoVal = make([]bool, s.slots)
	}
	if !c.cellCache {
		c.d.EnableCellCache()
		c.cellCache = true
	}
	c.d.ResetCellCache()
}

func (c *Ctx) memoXZ(slot int) (bool, bool) {
	if c.memoStamp[slot] == c.stampXZ<<1|1 {
		return c.memoVal[slot], true
	}
	return false, false
}

func (c *Ctx) setMemoXZ(slot int, v bool) {
	c.memoStamp[slot] = c.stampXZ<<1 | 1
	c.memoVal[slot] = v
}

func (c *Ctx) memoY(slot int) (bool, bool) {
	if c.memoStamp[slot] == c.stampY<<1 {
		return c.memoVal[slot], true
	}
	return false, false
}

func (c *Ctx) setMemoY(slot int, v bool) {
	c.memoStamp[slot] = c.stampY << 1
	c.memoVal[slot] = v
}

func (c *Ctx) bumpXZ() {
	c.stampXZ++
	if c.stampXZ == 1<<31 {
		c.stampXZ = 1
		clear(c.memoStamp)
	}
}

func (c *Ctx) bumpY() {
	c.stampY++
	if c.stampY == 1<<31 {
		c.stampY = 1
		clear(c.memoStamp)
	}
}

func (c *Ctx) updateXZ(x, z, gx, gz int32) {
	c.bumpXZ()
	c.x, c.z, c.gx, c.gz = x, z, gx, gz
	s := c.s
	c.depth = s.SurfaceDepth(x, z)
	c.secOK = false
	c.minSurface = int32(math.Floor(float64(s.chunkSurface.Value(&c.d, x, 0, z)))) + c.depth - 8
}

func (c *Ctx) updateY(above, below, water, y int32) {
	c.bumpY()
	c.above, c.below, c.water, c.y = above, below, water, y
	c.biomeOK = false
}

func (c *Ctx) secondary() float64 {
	if !c.secOK {
		c.sec = float64(c.s.secondaryNoise.Get(float64(c.x), 0, float64(c.z)))
		c.secOK = true
	}
	return c.sec
}

func (c *Ctx) biome() data.BiomeID {
	if !c.biomeOK {
		c.biomeID = c.biomeAt(c.x, c.y, c.z)
		c.biomeOK = true
	}
	return c.biomeID
}

// biomeAt is the reference BiomeManager.getBiome over stored biomes. The
// zoom picks one of the 8 quarts around the position; when they all hold
// the same biome the pick is skipped. Quart y is clamped to the chunk like
// ChunkAccess.getNoiseBiome.
func (c *Ctx) biomeAt(x, y, z int32) data.BiomeID {
	s, b := c.s, c.biomes
	minQ := c.minY >> 2
	maxQ := (c.minY+c.height)>>2 - 1
	l, m, n := (x-2)>>2, (y-2)>>2, (z-2)>>2
	m0, m1 := max(minQ, min(maxQ, m)), max(minQ, min(maxQ, m+1))
	first := b.QuartBiome(l, m0, n)
	same := true
	for o := 1; o < 8 && same; o++ {
		qy := m0
		if o&2 != 0 {
			qy = m1
		}
		same = b.QuartBiome(l+int32(o>>2&1), qy, n+int32(o&1)) == first
	}
	if same {
		return first
	}
	qx, qy, qz := zoomQuart(s.biomeObf, x, y, z)
	return b.QuartBiome(qx, max(minQ, min(maxQ, qy)), qz)
}
