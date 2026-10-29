package surface

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// column is the reference BlockColumn: one block column of the chunk being
// built, addressed by absolute y.
type column interface {
	get(y int32) data.StateID
	set(y int32, st data.StateID)
}

// erodedBadlandsExtension is the reference MaterialSystem
// erodedBadlandsExtension: stone pillars (hoodoos) raised from the surface.
func (s *System) erodedBadlandsExtension(col column, x, z, height, minY int32) {
	fx, fz := float64(x), float64(z)
	pillar := min(math.Abs(float64(s.badlandsSurf.Get(fx, 0, fz))*8.25), float64(s.badlandsPillar.Get(fx*0.2, 0, fz*0.2))*15)
	if pillar <= 0 {
		return
	}
	roof := math.Abs(float64(s.badlandsPillarRoof.Get(fx*0.75, 0, fz*0.75)) * 1.5)
	top := 64 + min(pillar*pillar*2.5, math.Ceil(roof*50)+24)
	ft := int32(math.Floor(top))
	if height > ft {
		return
	}
	for y := ft; y >= minY; y-- {
		st := col.get(y)
		if st == s.DefaultBlock {
			break
		}
		if st == s.sWater {
			return
		}
	}
	for y := ft; y >= minY && s.reg.IsAir(col.get(y)); y-- {
		col.set(y, s.DefaultBlock)
	}
}

// frozenOceanExtension is the reference MaterialSystem frozenOceanExtension:
// icebergs of packed ice capped with snow.
func (s *System) frozenOceanExtension(minSurface int32, biome data.BiomeID, col column, x, z, height int32) {
	fx, fz := float64(x), float64(z)
	pillar := min(math.Abs(float64(s.icebergSurf.Get(fx, 0, fz))*8.25), float64(s.icebergPillar.Get(fx*1.28, 0, fz*1.28))*15)
	if pillar <= 1.8 {
		return
	}
	roof := math.Abs(float64(s.icebergPillarRoof.Get(fx*1.17, 0, fz*1.17)) * 1.5)
	top := min(pillar*pillar*1.2, math.Ceil(roof*40)+14)
	if int(biome) < len(s.climates) && s.climates[biome].heightAdjustedTemperature(x, 63, z, s.SeaLevel) > 0.1 {
		top -= 2
	}
	if top <= 2 {
		return
	}
	sea := float64(s.SeaLevel)
	bottom := sea - top - 7
	top += sea
	r := s.pr.at(x, 0, z)
	snowDepth := 2 + r.nextIntn(4)
	snowFrom := s.SeaLevel + 18 + r.nextIntn(10)
	snow := int32(0)
	itop, ibottom := int32(top), int32(bottom)
	for y := max(height, itop+1); y >= minSurface; y-- {
		if s.reg.IsAir(col.get(y)) && y < itop && r.nextDouble() > 0.01 ||
			col.get(y) == s.sWater && y > ibottom && y < s.SeaLevel && r.nextDouble() > 0.15 {
			if snow <= snowDepth && y > snowFrom {
				col.set(y, s.sSnowBlock)
				snow++
			} else {
				col.set(y, s.sPackedIce)
			}
		}
	}
}
