package vanilla

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

// structureProbe answers structure_starts' biome and height questions from
// the biome source and a scratch noise chunk (no carvers, no surface).
type structureProbe struct {
	t   *Terrain
	dim world.Dimension

	mu    sync.Mutex
	cache map[world.ChunkPos]*world.Chunk
	order []world.ChunkPos
	// ctxs reuses sampling contexts (and their corner tables); they carry
	// no hooks, so cached values stay valid across probes.
	ctxs sync.Pool
}

func newStructureProbe(t *Terrain) *structureProbe {
	s := t.st.Noise.Router.Settings
	return &structureProbe{t: t, dim: world.Dimension{MinY: int(s.MinY), Height: int(s.Height), HasSkylight: true},
		cache: map[world.ChunkPos]*world.Chunk{}}
}

func (p *structureProbe) Biome(x, y, z int32) data.BiomeID {
	var ctx density.Ctx
	name := p.t.st.Biomes.Biome(&ctx, nil, x>>2, y>>2, z>>2)
	id, _ := data.Canonical().BiomeByName(name)
	return id
}

func (p *structureProbe) chunk(cp world.ChunkPos) *world.Chunk {
	p.mu.Lock()
	c, ok := p.cache[cp]
	p.mu.Unlock()
	if ok {
		return c
	}
	plains, _ := data.Canonical().BiomeByName("plains")
	c = world.NewChunk(cp.X, cp.Z, p.dim, plains)
	ctx, _ := p.ctxs.Get().(*density.Ctx)
	if ctx == nil {
		ctx = &density.Ctx{}
	}
	err := p.t.st.GenerateStageCtx(ctx, c, "noise")
	p.ctxs.Put(ctx)
	if err != nil {
		return nil
	}
	c.Finish()
	p.mu.Lock()
	if _, ok := p.cache[cp]; !ok {
		p.cache[cp] = c
		p.order = append(p.order, cp)
		if len(p.order) > 64 {
			delete(p.cache, p.order[0])
			p.order = p.order[1:]
		}
	}
	p.mu.Unlock()
	return c
}

func (p *structureProbe) Height(x, z int32, hm string) int32 {
	c := p.chunk(world.ChunkPos{X: x >> 4, Z: z >> 4})
	if c == nil {
		return int32(p.dim.MinY)
	}
	h := world.WorldSurface
	if hm == "OCEAN_FLOOR_WG" || hm == "OCEAN_FLOOR" {
		h = world.OceanFloor
	}
	return int32(c.Height(h, int(x&15), int(z&15)))
}

// structureStages returns the structure statuses for the dimension, or nil
// outside the overworld.
func (t *Terrain) structureStages(dim string, seed int64) (*structure.Stages, error) {
	if dim != "overworld" && dim != "minecraft:overworld" {
		return nil, nil
	}
	return structure.NewStages(seed, newStructureProbe(t))
}

// StructureStagesForTest exposes the overworld structure stages.
func StructureStagesForTest(t *Terrain, seed int64) (*structure.Stages, error) {
	return t.structureStages("overworld", seed)
}
