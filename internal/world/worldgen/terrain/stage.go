package terrain

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// Staged runs the biomes and noise stages for one dimension. Immutable and
// safe for concurrent use.
type Staged struct {
	Biomes biome.Source
	Noise  *Generator
}

// NewStaged builds the stage generator of a vanilla dimension.
func NewStaged(dim string, seed int64) (*Staged, error) {
	settings := map[string]string{"overworld": "minecraft:overworld", "the_nether": "minecraft:nether", "the_end": "minecraft:end"}[dim]
	if settings == "" {
		return nil, fmt.Errorf("terrain: unknown dimension %q", dim)
	}
	src, err := biome.ForDimension(dim, seed)
	if err != nil {
		return nil, err
	}
	g, err := New(settings, seed)
	if err != nil {
		return nil, err
	}
	return &Staged{src, g}, nil
}

func (s *Staged) Generate(c *world.Chunk) { _ = s.GenerateStage(c, "noise") }

// GenerateStage stops after "biomes" or "noise".
func (s *Staged) GenerateStage(c *world.Chunk, stage string) error {
	return s.GenerateStageCtx(&density.Ctx{}, c, stage)
}

// GenerateStageCtx is GenerateStage with the caller's per-goroutine context.
func (s *Staged) GenerateStageCtx(ctx *density.Ctx, c *world.Chunk, stage string) error {
	if stage != "biomes" && stage != "noise" {
		return fmt.Errorf("terrain: stage %q not supported", stage)
	}
	if err := biome.FillWorldChunk(s.Biomes, c); err != nil {
		return err
	}
	if stage == "noise" {
		s.Noise.Fill(ctx, c)
	}
	return nil
}
