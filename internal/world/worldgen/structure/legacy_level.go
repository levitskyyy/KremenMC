package structure

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"

// Seed is the world seed (legacy pieces' positional randoms).
func (l *stageLevel) Seed() int64 { return l.sc.Seed() }

// RegionRandom is the decorated chunk's WorldGenRegion.getRandom.
func (l *stageLevel) RegionRandom() rng.Source { return l.sc.RegionRandom() }
