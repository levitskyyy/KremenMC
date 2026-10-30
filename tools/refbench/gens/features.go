package gens

import (
	"github.com/levitskyyy/KremenMC/internal/server"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// "features" is the server's vanilla generator: biomes, terrain and the
// features status on the gen.Pipeline (in-memory proto-chunks).
func init() {
	Register("features", func(seed int64, dim string) world.Generator {
		d, err := Dimension(dim)
		if err != nil {
			return nil
		}
		t, err := vanilla.NewTerrain(dim, seed)
		if err != nil {
			return nil
		}
		stages, err := t.Stages(dim, seed)
		if err != nil {
			return nil
		}
		p, err := gen.New(gen.Config{Seed: seed, Dim: d, Stages: stages, PostProcess: server.GenPostProcess})
		if err != nil {
			return nil
		}
		return gen.WorldGenerator{P: p}
	})
}
