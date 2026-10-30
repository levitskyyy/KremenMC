package gen

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Stage produces one status for one chunk. Run is called at most once per
// chunk and status (unless a previous run failed), from any worker
// goroutine; implementations must be safe for concurrent calls on different
// chunks and must derive every random value from the world seed and
// positions only (never from shared or per-worker RNG state).
type Stage interface {
	Status() Status
	Run(ctx *StageContext) error
}

// StageFunc adapts a function to Stage.
type StageFunc struct {
	S Status
	F func(*StageContext) error
}

func (s StageFunc) Status() Status              { return s.S }
func (s StageFunc) Run(ctx *StageContext) error { return s.F(ctx) }

// StageContext is the WorldGenRegion-like view a stage runs in. It covers
// the step's direct dependency radius: chunk at distance d is guaranteed to
// have reached Step.Direct[d]. Block writes are allowed within the step's
// write radius; writes outside it are dropped and reported, as vanilla does.
type StageContext struct {
	p      *Pipeline
	step   *Step
	pos    world.ChunkPos
	r      int
	grid   []*ProtoChunk
	worker int
	rr     rng.Source // RegionRandom, created on first use
}

func (c *StageContext) reset(p *Pipeline, step *Step, pos world.ChunkPos, worker int) {
	c.p, c.step, c.pos, c.worker = p, step, pos, worker
	c.rr = nil
	c.r = max(step.Radius(), 0)
	n := 2*c.r + 1
	if cap(c.grid) < n*n {
		c.grid = make([]*ProtoChunk, n*n)
	}
	c.grid = c.grid[:n*n]
}

func (c *StageContext) clear() {
	clear(c.grid)
	c.p, c.step = nil, nil
}

func (c *StageContext) idx(cx, cz int32) (int, int, bool) {
	dx, dz := int(cx-c.pos.X), int(cz-c.pos.Z)
	if dx < -c.r || dx > c.r || dz < -c.r || dz > c.r {
		return 0, 0, false
	}
	return (dz+c.r)*(2*c.r+1) + dx + c.r, max(abs(dx), abs(dz)), true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Chunk is the chunk being generated (writable).
func (c *StageContext) Chunk() *ProtoChunk { return c.grid[len(c.grid)/2] }

func (c *StageContext) Pos() world.ChunkPos     { return c.pos }
func (c *StageContext) Target() Status          { return c.step.Target }
func (c *StageContext) Step() *Step             { return c.step }
func (c *StageContext) Seed() int64             { return c.p.cfg.Seed }
func (c *StageContext) Dim() world.Dimension    { return c.p.cfg.Dim }
func (c *StageContext) Worker() int             { return c.worker }
func (c *StageContext) Pipeline() *Pipeline     { return c.p }
func (c *StageContext) MinBlockX() int          { return int(c.pos.X) << 4 }
func (c *StageContext) MinBlockZ() int          { return int(c.pos.Z) << 4 }
func (c *StageContext) WriteRadius() int        { return c.step.WriteRadius }
func (c *StageContext) Logf(f string, a ...any) { c.p.cfg.Logf(f, a...) }

// Neighbour returns the chunk at chunk coordinates cx,cz and the status it
// is guaranteed to have reached. ok is false outside the dependency radius
// (vanilla raises "Requested chunk unavailable during world generation").
// The centre is reported at its parent status. Reading data that appears
// only after the guaranteed status is order-dependent and must be avoided.
func (c *StageContext) Neighbour(cx, cz int32) (*ProtoChunk, Status, bool) {
	i, d, ok := c.idx(cx, cz)
	if !ok {
		c.p.stats.farReads.Add(1)
		return nil, 0, false
	}
	return c.grid[i], c.step.Direct[d], true
}

// blocksAt returns the chunk holding block column x,z if its blocks are
// visible to this step: the centre always, neighbours from terrain on.
func (c *StageContext) blocksAt(x, z int) (*ProtoChunk, int, bool) {
	cx, cz := int32(x>>4), int32(z>>4)
	i, d, ok := c.idx(cx, cz)
	if !ok {
		c.p.stats.farReads.Add(1)
		return nil, d, false
	}
	if d > 0 && c.step.Direct[d] < StatusTerrain {
		return nil, d, false
	}
	return c.grid[i], d, true
}

// Block reads a block at absolute coordinates. Columns whose guaranteed
// status predates terrain read as air, independent of how far they have
// actually progressed; columns outside the radius read as air and count as
// far reads.
func (c *StageContext) Block(x, y, z int) data.StateID {
	pc, _, ok := c.blocksAt(x, z)
	if !ok {
		return data.Air
	}
	return pc.Chunk.Block(x&15, y, z&15)
}

// Height reads a heightmap with the same visibility rules as Block.
func (c *StageContext) Height(h world.Heightmap, x, z int) int {
	pc, _, ok := c.blocksAt(x, z)
	if !ok {
		return c.p.cfg.Dim.MinY
	}
	return pc.Height(h, x&15, z&15)
}

// Biome reads the biome at absolute block coordinates. Biomes are visible
// in chunks guaranteed at biomes or later (and in the centre).
func (c *StageContext) Biome(x, y, z int) (data.BiomeID, bool) {
	cx, cz := int32(x>>4), int32(z>>4)
	i, d, ok := c.idx(cx, cz)
	if !ok {
		c.p.stats.farReads.Add(1)
		return 0, false
	}
	if d > 0 && c.step.Direct[d] < StatusBiomes {
		return 0, false
	}
	return c.grid[i].Chunk.Biome(x&15, y, z&15), true
}

// InWriteZone mirrors WorldGenRegion.isWithinWriteZone for block x,z.
func (c *StageContext) InWriteZone(x, z int) bool {
	d := max(abs(x>>4-int(c.pos.X)), abs(z>>4-int(c.pos.Z)))
	return d <= c.step.WriteRadius
}

// SetBlock writes a block at absolute coordinates. Outside the write zone
// or the dimension's height the write is dropped and false returned;
// out-of-zone writes are reported like vanilla's "Detected setBlock in a
// far chunk".
func (c *StageContext) SetBlock(x, y, z int, s data.StateID) bool {
	if !c.InWriteZone(x, z) {
		c.p.stats.farWrites.Add(1)
		c.p.cfg.Logf("gen: detected setBlock in a far chunk [%d, %d], pos: %d %d %d, status: %s",
			x>>4, z>>4, x, y, z, c.step.Target.ID())
		return false
	}
	if !c.p.cfg.Dim.Contains(y) {
		return false
	}
	i, _, _ := c.idx(int32(x>>4), int32(z>>4))
	c.grid[i].Chunk.SetBlock(x&15, y, z&15, s)
	return true
}

// Positional is the world-level positional random factory for alg
// (vanilla RandomState.random). It is immutable and safe to share.
func (c *StageContext) Positional(alg rng.Algorithm) rng.Positional {
	return alg.NewInstance(c.p.cfg.Seed).ForkPositional()
}

// Decoration returns a fresh xoroshiro-backed WorldgenRandom seeded with
// setDecorationSeed(seed, minBlockX, minBlockZ) and that decoration seed,
// as vanilla's applyBiomeDecoration does.
func (c *StageContext) Decoration() (*rng.WorldgenRandom, int64) {
	r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
	s := r.SetDecorationSeed(c.p.cfg.Seed, int32(c.MinBlockX()), int32(c.MinBlockZ()))
	return r, s
}

// LargeFeature returns a fresh legacy-backed WorldgenRandom seeded with
// setLargeFeatureSeed(seed, cx, cz) (structure starts, carvers).
func (c *StageContext) LargeFeature(seed int64, cx, cz int32) *rng.WorldgenRandom {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	return r
}

func runStage(s Stage, ctx *StageContext) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gen: %s at %d,%d: panic: %v", ctx.step.Target, ctx.pos.X, ctx.pos.Z, r)
		}
	}()
	if err := s.Run(ctx); err != nil {
		return fmt.Errorf("gen: %s at %d,%d: %w", ctx.step.Target, ctx.pos.X, ctx.pos.Z, err)
	}
	return nil
}
