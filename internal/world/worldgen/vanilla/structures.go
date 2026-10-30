package vanilla

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
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

// SearchBiome returns a biome lookup that keeps one climate search cursor
// across calls, as a reference thread scanning findBiomeHorizontal does
// (its parameter-list search starts from the previous result, which
// decides ties: ring snaps of seed 8266389061079214669, probe
// kremenmc.probe.oruin.BiomeQuarts).
func (p *structureProbe) SearchBiome() func(x, y, z int32) data.BiomeID {
	var ctx density.Ctx
	cur := &climate.Cursor{}
	return func(x, y, z int32) data.BiomeID {
		id, _ := data.Canonical().BiomeByName(p.t.st.Biomes.Biome(&ctx, cur, x>>2, y>>2, z>>2))
		return id
	}
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

// Bounds is the dimension's build range [min, max).
func (p *structureProbe) Bounds() (int32, int32) {
	return int32(p.dim.MinY), int32(p.dim.MinY + p.dim.Height)
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

// structureStages returns the structure statuses for the dimension.
func (t *Terrain) structureStages(dim string, seed int64) (*structure.Stages, error) {
	switch strings.TrimPrefix(dim, "minecraft:") {
	case "overworld":
		return structure.NewStages(seed, newStructureProbe(t))
	case "the_nether":
		return structure.NewDimStages(seed, newStructureProbe(t), "is_nether")
	case "the_end":
		return structure.NewDimStages(seed, newStructureProbe(t), "is_end")
	}
	return nil, nil
}

// DimStructureStagesForTest exposes a dimension's structure stages.
func DimStructureStagesForTest(t *Terrain, dim string, seed int64) (*structure.Stages, error) {
	return t.structureStages(dim, seed)
}

// StructureStagesForTest exposes the overworld structure stages.
func StructureStagesForTest(t *Terrain, seed int64) (*structure.Stages, error) {
	return t.structureStages("overworld", seed)
}

// StructureProbeForTest exposes the overworld structure_starts probe.
func StructureProbeForTest(t *Terrain) structure.Probe { return newStructureProbe(t) }
