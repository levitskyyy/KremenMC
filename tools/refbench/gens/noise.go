package gens

import (
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/terrain"
)

func init() {
	Register("noise", func(seed int64, dim string) world.Generator {
		s, err := terrain.NewStaged(dim, seed)
		if err != nil {
			return nil
		}
		return s
	})
}
