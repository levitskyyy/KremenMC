package gen

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// RegionRandom is WorldGenRegion.getRandom of this stage run: the level
// seed's xoroshiro positional factory, hash
// "minecraft:worldgen_region_random", at (chunk min x, 0, chunk min z)
// (kremenmc.probe.temple.RegionRand: real WorldGenRegions, 3/3 chunks). One
// instance per run, shared by everything that draws from it.
func (c *StageContext) RegionRandom() rng.Source {
	if c.rr == nil {
		f := rng.NewXoroshiro(c.p.cfg.Seed).ForkPositional().FromHashOf("minecraft:worldgen_region_random").ForkPositional()
		c.rr = f.At(int32(c.MinBlockX()), 0, int32(c.MinBlockZ()))
	}
	return c.rr
}
