package biome

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// FillWorldChunk sets every biome cell of c like the reference BIOMES
// status.
func FillWorldChunk(src Source, c *world.Chunk) error {
	reg := data.Canonical()
	var err error
	FillChunk(src, c.X, c.Z, int32(c.Dim.MinSection()), int32(c.Dim.Sections()), func(qx, qy, qz int32, b string) {
		id, ok := reg.BiomeByName(b)
		if !ok {
			if err == nil {
				err = fmt.Errorf("biome: %s not in the biome registry", b)
			}
			return
		}
		c.SetBiome(int(qx)*4, c.Dim.MinY+int(qy)*4, int(qz)*4, id)
	})
	return err
}

// Generator is a world.Generator that only performs the BIOMES stage.
type Generator struct{ Source Source }

func (g Generator) Generate(c *world.Chunk) { _ = FillWorldChunk(g.Source, c) }

// GenerateStage implements the bench's staged generation for "biomes".
func (g Generator) GenerateStage(c *world.Chunk, stage string) error {
	if stage != "biomes" {
		return fmt.Errorf("biome: stage %q not supported", stage)
	}
	return FillWorldChunk(g.Source, c)
}
