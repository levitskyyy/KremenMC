package gens

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/terrain"
)

// surfaceGen is terrain's noise fill followed by the material pass. Stage
// "noise" adds the surface extensions (what the reference noise dump, made
// with an empty material rule, contains); "surface" runs the full pass.
type surfaceGen struct {
	st  *terrain.Staged
	sys *surface.System
}

func (g surfaceGen) Generate(c *world.Chunk) { _ = g.GenerateStage(c, "surface") }

func (g surfaceGen) GenerateStage(c *world.Chunk, stage string) error {
	switch stage {
	case "biomes":
		return g.st.GenerateStage(c, stage)
	case "noise", "surface":
	default:
		return fmt.Errorf("surface: stage %q not supported", stage)
	}
	if err := g.st.GenerateStage(c, "noise"); err != nil {
		return err
	}
	b := surface.NewSourceBiomes(g.st.Biomes, int32(c.Dim.MinY), int32(c.Dim.Height))
	if stage == "noise" {
		return g.sys.Extensions(surface.NewCtx(), c, b)
	}
	return g.sys.Build(surface.NewCtx(), c, b)
}

func init() {
	Register("surface", func(seed int64, dim string) world.Generator {
		st, err := terrain.NewStaged(dim, seed)
		if err != nil {
			return nil
		}
		key, err := surface.SettingsFor(dim)
		if err != nil {
			return nil
		}
		sys, err := surface.Vanilla(key, seed)
		if err != nil {
			return nil
		}
		return surfaceGen{st, sys}
	})
}
