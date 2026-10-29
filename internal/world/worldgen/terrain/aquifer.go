package terrain

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// dryLevel is the fluid level of an aquifer cell holding no fluid.
const dryLevel = -32512

// surfaceOffsets are the chunk offsets (x, z) around an aquifer center whose
// surface levels decide the center's fluid.
var surfaceOffsets = [13][2]int32{{0, 0}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-3, 0}, {-2, 0}, {-1, 0}, {1, 0}, {-2, 1}, {-1, 1}, {0, 1}, {1, 1}}

// flowingSimilarity is the similarity above which neighbouring differing
// cells schedule a fluid update.
var flowingSimilarity = similarity(100, 144)

// AquiferConfig is the immutable, per-router part of the noise-based
// aquifer (reference Aquifer.Config bound to a RandomState).
type AquiferConfig struct {
	barrier, exclusion, flood, spread, lava, surface density.Sampler
	pos                                              rng.Positional
	global                                           GlobalFluid
	air, lavaState, water                            data.StateID
	minY, maxY                                       int32
}

func newAquiferConfig(r *density.Router, g GlobalFluid, air, lava data.StateID) *AquiferConfig {
	a := r.Aquifers
	if a["barrier"] == nil {
		return nil
	}
	s := r.Settings
	return &AquiferConfig{
		barrier: a["barrier"], exclusion: a["exclusion"], flood: a["fluid_level_floodedness"],
		spread: a["fluid_level_spread"], lava: a["lava"], surface: a["surface_level"],
		pos:    r.State.Positional().FromHashOf("minecraft:aquifer").ForkPositional(),
		global: g, air: air, lavaState: lava, water: g.Default.Fluid,
		minY: s.MinY, maxY: s.MinY + s.Height - 1,
	}
}

type aqCell struct {
	x, y, z int32
	st      FluidStatus
	located bool
	known   bool
}

// NoiseAquifer is the per-chunk noise-based aquifer. It memoizes pure
// functions of position only, so results do not depend on query order. Not
// safe for concurrent use.
type NoiseAquifer struct {
	cfg                 *AquiferConfig
	minGX, minGY, minGZ int32
	sizeX, sizeY, sizeZ int32
	cells               []aqCell
	surf                map[[2]int32]int32
	skipAbove           int32
	scheduleFluidUpdate bool
}

// NewAquifer builds the aquifer of chunk (cx, cz).
func (c *AquiferConfig) NewAquifer(ctx *density.Ctx, cx, cz int32) *NoiseAquifer {
	bx, bz := cx<<4, cz<<4
	a := &NoiseAquifer{cfg: c, surf: make(map[[2]int32]int32, 256)}
	a.minGX, a.minGZ = floorDiv(bx-5, 16), floorDiv(bz-5, 16)
	a.minGY = floorDiv(c.minY+1, 12) - 1
	a.sizeX = floorDiv(bx+15-5, 16) - a.minGX + 2
	a.sizeZ = floorDiv(bz+15-5, 16) - a.minGZ + 2
	a.sizeY = floorDiv(c.maxY+1, 12) + 1 - a.minGY + 1
	a.cells = make([]aqCell, a.sizeX*a.sizeY*a.sizeZ)
	maxS := int32(math.MinInt32)
	for x := bx - 16; x <= bx+24; x += 4 {
		for z := bz - 16; z <= bz+24; z += 4 {
			maxS = max(maxS, a.surfaceLevel(ctx, x, z))
		}
	}
	a.skipAbove = floorDiv(maxS+8, 12)*12 + 34
	return a
}

// ShouldScheduleFluidUpdate reports whether the fluid returned by the last
// ComputeSubstance call needs a scheduled fluid tick.
func (a *NoiseAquifer) ShouldScheduleFluidUpdate() bool { return a.scheduleFluidUpdate }

func floorDiv(a, b int32) int32 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func similarity(d1, d2 int32) float64 { return 1 - float64(d2-d1)/25 }

func (a *NoiseAquifer) surfaceLevel(ctx *density.Ctx, x, z int32) int32 {
	k := [2]int32{x &^ 3, z &^ 3}
	if v, ok := a.surf[k]; ok {
		return v
	}
	v := int32(math.Floor(float64(a.cfg.surface.Value(ctx, k[0], 0, k[1]))))
	a.surf[k] = v
	return v
}

func (a *NoiseAquifer) cell(gx, gy, gz int32) *aqCell {
	ix, iy, iz := gx-a.minGX, gy-a.minGY, gz-a.minGZ
	if ix < 0 || iy < 0 || iz < 0 || ix >= a.sizeX || iy >= a.sizeY || iz >= a.sizeZ {
		return &aqCell{}
	}
	return &a.cells[(iy*a.sizeZ+iz)*a.sizeX+ix]
}

func (a *NoiseAquifer) locate(gx, gy, gz int32) *aqCell {
	c := a.cell(gx, gy, gz)
	if !c.located {
		r := a.cfg.pos.At(gx, gy, gz)
		c.x = gx*16 + r.NextIntn(10)
		c.y = gy*12 + r.NextIntn(9)
		c.z = gz*16 + r.NextIntn(10)
		c.located = true
	}
	return c
}

func (a *NoiseAquifer) status(ctx *density.Ctx, c *aqCell) FluidStatus {
	if !c.known {
		c.st = a.computeFluid(ctx, c.x, c.y, c.z)
		c.known = true
	}
	return c.st
}

func (a *NoiseAquifer) computeFluid(ctx *density.Ctx, x, y, z int32) FluidStatus {
	cfg := a.cfg
	g := cfg.global.Pick(y)
	minS := int32(math.MaxInt32)
	below := false
	for i, o := range surfaceOffsets {
		s := a.surfaceLevel(ctx, x+16*o[0], z+16*o[1])
		adj := s + 8
		if i == 0 && y-12 > adj {
			return g
		}
		top := y+12 > adj
		if top || i == 0 {
			gs := cfg.global.Pick(adj)
			if gs.At(adj, cfg.air) != cfg.air {
				if i == 0 {
					below = true
				}
				if top {
					return gs
				}
			}
		}
		minS = min(minS, s)
	}
	lv := a.fluidLevel(ctx, x, y, z, g, minS, below)
	return FluidStatus{lv, a.fluidType(ctx, x, y, z, g, lv)}
}

func clampedLerp(d, lo, hi float64) float64 {
	t := min(max(d/64, 0), 1)
	return lo + t*(hi-lo)
}

func (a *NoiseAquifer) fluidLevel(ctx *density.Ctx, x, y, z int32, g FluidStatus, minS int32, below bool) int32 {
	cfg := a.cfg
	if cfg.exclusion.Value(ctx, x, y, z) > 0 {
		return dryLevel
	}
	f := float64(cfg.flood.Value(ctx, x, y, z))
	full, part := 0.8, 0.4
	if below {
		d := float64(minS + 8 - y)
		full, part = clampedLerp(d, -0.3, 0.8), clampedLerp(d, -0.8, 0.4)
	}
	switch {
	case f > full:
		return g.Level
	case f > part:
		gy := floorDiv(y, 40)
		s := cfg.spread.Value(ctx, floorDiv(x, 16), gy, floorDiv(z, 16))
		q := int32(math.Floor(float64(s) / 0.3))
		return min(minS, gy*40+20+3*q)
	}
	return dryLevel
}

func (a *NoiseAquifer) fluidType(ctx *density.Ctx, x, y, z int32, g FluidStatus, lv int32) data.StateID {
	cfg := a.cfg
	if lv <= -10 && lv != dryLevel && g.Fluid != cfg.lavaState {
		if math.Abs(float64(cfg.lava.Value(ctx, floorDiv(x, 64), floorDiv(y, 40), floorDiv(z, 64)))) > 0.3 {
			return cfg.lavaState
		}
	}
	return g.Fluid
}

type barrierVal struct {
	v  float64
	ok bool
}

func (a *NoiseAquifer) pressure(ctx *density.Ctx, x, y, z int32, b *barrierVal, s1, s2 FluidStatus) float64 {
	cfg := a.cfg
	f1, f2 := s1.At(y, cfg.air), s2.At(y, cfg.air)
	if (f1 == cfg.lavaState && f2 == cfg.water) || (f1 == cfg.water && f2 == cfg.lavaState) {
		return 2
	}
	if s1.Level == s2.Level {
		return 0
	}
	u, d := max(s1.Level, s2.Level), min(s1.Level, s2.Level)
	mid := float64(u+d) / 2
	yc := float64(y) + 0.5
	var p float64
	if yc > mid {
		q := float64(u) - yc
		if q > 0 {
			p = q / 0.75
		} else {
			p = q / 1.25
		}
	} else {
		q := yc - float64(d) + 3
		if q > 0 {
			p = q / 1.5
		} else {
			p = q / 5
		}
	}
	if p > -4 && p < 4 {
		if !b.ok {
			b.v, b.ok = float64(cfg.barrier.Value(ctx, x, y, z)), true
		}
		p += 2 * b.v
	}
	return p
}

type aqNear struct {
	d2 int32
	c  *aqCell
}

// ComputeSubstance is the reference NoiseBasedAquifer.computeSubstance.
func (a *NoiseAquifer) ComputeSubstance(ctx *density.Ctx, x, y, z int32, density float32) (data.StateID, bool) {
	a.scheduleFluidUpdate = false
	if density > 0 {
		return 0, false
	}
	cfg := a.cfg
	gs := cfg.global.Pick(y).At(y, cfg.air)
	if gs == cfg.lavaState || y > a.skipAbove {
		return gs, true
	}
	var n [4]aqNear
	for i := range n {
		n[i].d2 = math.MaxInt32
	}
	gx, gy, gz := floorDiv(x-5, 16), floorDiv(y+1, 12), floorDiv(z-5, 16)
	for dx := int32(0); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(0); dz <= 1; dz++ {
				c := a.locate(gx+dx, gy+dy, gz+dz)
				ex, ey, ez := c.x-x, c.y-y, c.z-z
				d := ex*ex + ey*ey + ez*ez
				switch {
				case d <= n[0].d2:
					n[3], n[2], n[1], n[0] = n[2], n[1], n[0], aqNear{d, c}
				case d <= n[1].d2:
					n[3], n[2], n[1] = n[2], n[1], aqNear{d, c}
				case d <= n[2].d2:
					n[3], n[2] = n[2], aqNear{d, c}
				case d <= n[3].d2:
					n[3] = aqNear{d, c}
				}
			}
		}
	}
	st1 := a.status(ctx, n[0].c)
	f1 := st1.At(y, cfg.air)
	s12 := similarity(n[0].d2, n[1].d2)
	if s12 <= 0 {
		a.scheduleFluidUpdate = s12 >= flowingSimilarity && st1 != a.status(ctx, n[1].c)
		return f1, true
	}
	d := float64(density)
	var b barrierVal
	st2 := a.status(ctx, n[1].c)
	if d+s12*a.pressure(ctx, x, y, z, &b, st1, st2) > 0 {
		return 0, false
	}
	st3 := a.status(ctx, n[2].c)
	s13 := similarity(n[0].d2, n[2].d2)
	if s13 > 0 && d+s12*s13*a.pressure(ctx, x, y, z, &b, st1, st3) > 0 {
		return 0, false
	}
	s23 := similarity(n[1].d2, n[2].d2)
	if s23 > 0 && d+s12*s23*a.pressure(ctx, x, y, z, &b, st2, st3) > 0 {
		return 0, false
	}
	upd := st1 != st2 ||
		(s13 >= flowingSimilarity && st1 != st3) ||
		(s23 >= flowingSimilarity && st2 != st3)
	if !upd && similarity(n[0].d2, n[3].d2) >= flowingSimilarity {
		upd = st1 != a.status(ctx, n[3].c)
	}
	a.scheduleFluidUpdate = upd
	return f1, true
}
