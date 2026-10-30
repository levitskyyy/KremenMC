// Package terrain implements the noise part of the reference 26.3 terrain
// status: filling a chunk from the router's final density with the
// settings' default block, and fluids from the aquifer (or the global fluid
// picker when aquifers are disabled).
package terrain

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// Aquifer decides the substance of a position whose final density is not
// positive (reference Aquifer.computeSubstance). ok=false means "use the
// default block".
type Aquifer interface {
	ComputeSubstance(c *density.Ctx, x, y, z int32, d float32) (st data.StateID, ok bool)
}

// FluidStatus is a fluid surface: positions below Level get Fluid.
type FluidStatus struct {
	Level int32
	Fluid data.StateID
}

// At is the reference FluidStatus.at.
func (f FluidStatus) At(y int32, air data.StateID) data.StateID {
	if y < f.Level {
		return f.Fluid
	}
	return air
}

// GlobalFluid is the reference global fluid picker: lava below
// min(-54, sea level), otherwise the default fluid below the sea level.
type GlobalFluid struct {
	Lava, Default FluidStatus
	SeaLevel      int32
}

func (g GlobalFluid) Pick(y int32) FluidStatus {
	if y < min(-54, g.SeaLevel) {
		return g.Lava
	}
	return g.Default
}

// DisabledAquifer is the reference Aquifer.createDisabled.
type DisabledAquifer struct {
	Global GlobalFluid
	Air    data.StateID
}

func (a DisabledAquifer) ComputeSubstance(_ *density.Ctx, _, y, _ int32, d float32) (data.StateID, bool) {
	if d > 0 {
		return 0, false
	}
	return a.Global.Pick(y).At(y, a.Air), true
}

// Generator fills chunks for one router (settings + seed). Immutable.
type Generator struct {
	Router       *density.Router
	DefaultBlock data.StateID
	Air          data.StateID
	Global       GlobalFluid
	// NewAquifer builds the per-chunk aquifer; nil means disabled.
	NewAquifer func(cx, cz int32) Aquifer
}

// New builds a generator for noise settings and seed.
func New(settings string, seed int64) (*Generator, error) {
	r, err := density.CachedRouter(settings, seed)
	if err != nil {
		return nil, err
	}
	reg := data.Canonical()
	state := func(name string) (data.StateID, error) {
		st, ok := reg.DefaultState(name)
		if !ok {
			return 0, fmt.Errorf("terrain: unknown block %s", name)
		}
		return st, nil
	}
	g := &Generator{Router: r}
	if g.DefaultBlock, err = state(r.Settings.DefaultBlock); err != nil {
		return nil, err
	}
	if g.Air, err = state("minecraft:air"); err != nil {
		return nil, err
	}
	fluid, err := state(r.Settings.DefaultFluid)
	if err != nil {
		return nil, err
	}
	lava, err := state("minecraft:lava")
	if err != nil {
		return nil, err
	}
	g.Global = GlobalFluid{Lava: FluidStatus{-54, lava}, Default: FluidStatus{r.Settings.SeaLevel, fluid}, SeaLevel: r.Settings.SeaLevel}
	if cfg := newAquiferConfig(r, g.Global, g.Air, lava); cfg != nil {
		g.NewAquifer = func(cx, cz int32) Aquifer { return cfg.NewAquifer(&density.Ctx{}, cx, cz) }
	}
	return g, nil
}

// Fill writes the noise blocks of chunk c (blocks outside the noise height
// range are left untouched) and recomputes its heightmaps. ctx is the
// caller's per-goroutine context (nil allocates one).
func (g *Generator) Fill(ctx *density.Ctx, c *world.Chunk) { g.FillMarked(ctx, c, nil) }

// FillMarked is Fill that also reports, through mark (chunk-local x, z and
// absolute y; nil: none), each fluid block the aquifer flags for a fluid
// update: vanilla marks those positions for post-processing.
func (g *Generator) FillMarked(ctx *density.Ctx, c *world.Chunk, mark func(x, y, z int)) {
	if ctx == nil {
		ctx = &density.Ctx{}
	}
	ctx.EnableCellCache()
	defer ctx.ResetCellCache()
	var aq Aquifer = DisabledAquifer{g.Global, g.Air}
	if g.NewAquifer != nil {
		aq = g.NewAquifer(c.X, c.Z)
	}
	sched, _ := aq.(interface{ ShouldScheduleFluidUpdate() bool })
	if mark == nil {
		sched = nil
	}
	fd := g.Router.Fields["final_density"]
	s := g.Router.Settings
	lo, hi := max(s.MinY, int32(c.Dim.MinY)), min(s.MinY+s.Height, int32(c.Dim.MinY+c.Dim.Height))
	// Visit blocks in 4x4 columns, y-major inside, so interpolated
	// functions reuse their memoized cell corners instead of re-looking
	// them up every block. Results do not depend on visit order.
	for cx := int32(0); cx < 16; cx += 4 {
		for cz := int32(0); cz < 16; cz += 4 {
			for y := lo; y < hi; y++ {
				for x := cx; x < cx+4; x++ {
					for z := cz; z < cz+4; z++ {
						bx, bz := c.X<<4|x, c.Z<<4|z
						d := fd.Value(ctx, bx, y, bz)
						st, ok := aq.ComputeSubstance(ctx, bx, y, bz, d)
						if !ok {
							st = g.DefaultBlock
						}
						if st != g.Air {
							c.SetBlock(int(x), int(y), int(z), st)
						}
						if sched != nil && ok && sched.ShouldScheduleFluidUpdate() && data.Canonical().HasFluid(st) {
							mark(int(x), int(y), int(z))
						}
					}
				}
			}
		}
	}
	c.RecalcHeightmaps()
}
