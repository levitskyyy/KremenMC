package gens

import (
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

func init() {
	Register("vanilla", func(seed int64, dim string) world.Generator {
		t, err := vanilla.NewTerrain(dim, seed)
		if err != nil {
			return nil
		}
		return t
	})
}
