package gens

import (
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
)

func init() {
	Register("biomes", func(seed int64, dim string) world.Generator {
		src, err := biome.ForDimension(dim, seed)
		if err != nil {
			return nil
		}
		return biome.Generator{Source: src}
	})
}
