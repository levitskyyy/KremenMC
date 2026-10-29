// Package vanilla assembles the verified vanilla 26.3 generation stages into
// world generators.
package vanilla

import (
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/carver"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/terrain"
)

// Terrain generates chunks up to the terrain status: biomes, noise fill with
// aquifers, material rules with their hardcoded extensions, and carvers.
// Terrain writes only into its own chunk, so it is order-independent and
// safe for concurrent use.
type Terrain struct {
	st      *terrain.Staged
	surface *surface.System
	carvers *carver.Set
	biomes  *biomeCache
	pool    sync.Pool
}

type terrainState struct {
	dctx density.Ctx
	sctx *surface.Ctx
	mask *carver.Mask
}

// NewTerrain builds the terrain generator of a vanilla dimension
// ("overworld", "the_nether", "the_end").
func NewTerrain(dim string, seed int64) (*Terrain, error) {
	st, err := terrain.NewStaged(dim, seed)
	if err != nil {
		return nil, err
	}
	key, err := surface.SettingsFor(dim)
	if err != nil {
		return nil, err
	}
	sys, err := surface.Vanilla(key, seed)
	if err != nil {
		return nil, err
	}
	s := st.Noise.Router.Settings
	set, err := carver.NewSet(carver.Context{MinY: int(s.MinY), Height: int(s.Height), SeaLevel: int(s.SeaLevel)}, seed)
	if err != nil {
		return nil, fmt.Errorf("vanilla: carvers: %w", err)
	}
	return &Terrain{st: st, surface: sys, carvers: set, biomes: newBiomeCache(st.Biomes, s.MinY, s.Height)}, nil
}

func (t *Terrain) Generate(c *world.Chunk) { _ = t.GenerateStage(c, "terrain") }

// GenerateStage stops after "biomes", "noise", "surface" or "terrain".
func (t *Terrain) GenerateStage(c *world.Chunk, stage string) error {
	return t.GenerateStageBeard(c, stage, nil)
}

// GenerateStageBeard is GenerateStage with the structures' terrain
// adaptation added to final_density (nil: none).
func (t *Terrain) GenerateStageBeard(c *world.Chunk, stage string, beard func(x, y, z int32) float32) error {
	s, _ := t.pool.Get().(*terrainState)
	if s == nil {
		set := t.st.Noise.Router.Settings
		s = &terrainState{sctx: surface.NewCtx(), mask: carver.NewMask(int(set.MinY), int(set.Height))}
	}
	s.dctx.Beardifier = beard
	defer func() {
		s.dctx.Beardifier = nil
		t.pool.Put(s)
	}()
	switch stage {
	case "biomes":
		return t.biomes.fill(c)
	case "noise", "surface", "terrain":
	default:
		return fmt.Errorf("vanilla: stage %q not supported", stage)
	}
	if err := t.biomes.fill(c); err != nil {
		return err
	}
	t.st.Noise.Fill(&s.dctx, c)
	var b *surface.SourceBiomes
	if int32(c.Dim.MinY)>>2 == t.biomes.minQ && int32(c.Dim.Height)>>2 == t.biomes.quarts {
		b = surface.NewSharedSourceBiomes(int32(c.Dim.MinY), t.biomes.column)
	} else {
		b = surface.NewSourceBiomes(t.st.Biomes, int32(c.Dim.MinY), int32(c.Dim.Height))
	}
	if stage == "noise" {
		err := t.surface.Extensions(s.sctx, c, b)
		c.Finish()
		return err
	}
	if err := t.surface.Build(s.sctx, c, b); err != nil {
		return err
	}
	if stage == "terrain" {
		s.mask.Reset()
		t.carvers.CarveChunk(c.X, c.Z, func(qx, qz int32) string { return t.st.Biomes.Biome(&s.dctx, nil, qx, 0, qz) }, s.mask)
		s.mask.ApplyTop(c.X, c.Z, t.aquifer(&s.dctx, c.X, c.Z), carveTarget{c}, func(x, y, z int, fluid bool) (data.StateID, bool) {
			return t.surface.TopMaterial(s.sctx, c, b, int32(x), int32(y), int32(z), fluid)
		})
	}
	c.Finish()
	return nil
}

func (t *Terrain) aquifer(ctx *density.Ctx, cx, cz int32) carver.Aquifer {
	g := t.st.Noise
	var aq terrain.Aquifer = terrain.DisabledAquifer{Global: g.Global, Air: g.Air}
	if g.NewAquifer != nil {
		aq = g.NewAquifer(cx, cz)
	}
	set := g.Router.Settings
	return aquiferAdapter{ctx, aq, int(set.MinY), int(set.MinY + set.Height)}
}

type aquiferAdapter struct {
	ctx        *density.Ctx
	aq         terrain.Aquifer
	minY, maxY int
}

// Substance leaves the bottom two and the top seven layers of the generation
// range uncarved, as observed in the reference terrain dumps.
func (a aquiferAdapter) Substance(x, y, z int) (data.StateID, bool) {
	if y <= a.minY+1 || y >= a.maxY-7 {
		return 0, false
	}
	return a.aq.ComputeSubstance(a.ctx, int32(x), int32(y), int32(z), 0)
}

// carveTarget keeps unbreakable blocks such as bedrock, which the reference
// never carves.
type carveTarget struct{ c *world.Chunk }

func (t carveTarget) SetBlock(x, y, z int, st data.StateID) data.StateID {
	old := t.c.Block(x, y, z)
	if b := data.BlockOf(old); b != nil && b.Hardness < 0 {
		return old
	}
	return t.c.SetBlock(x, y, z, st)
}

func (t carveTarget) Block(x, y, z int) data.StateID { return t.c.Block(x, y, z) }
