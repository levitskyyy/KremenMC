package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// lakeFeature is lake: a union of random ellipsoids in a 16x8x16 box from (-8,-4,-8),
// blocks below the origin, fluid in the lower half and cave air above,
// refused when the shell would leak, then a barrier shell.
type lakeFeature struct {
	fluid, barrier                data.StateID
	canPlace, airFluid, barrierOK Predicate
}

func lakeIdx(x, y, z int) int { return (x*16+z)*8 + y }

func (f *lakeFeature) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if p.Y <= lv.MinY()+4 {
		return false
	}
	p = p.Add(Pos{-8, -4, -8})
	if !f.canPlace.Test(lv, p) {
		return false
	}
	var grid [2048]bool
	n := int(r.NextIntn(4)) + 4
	for range n {
		sx := r.NextDouble()*6 + 3
		sy := r.NextDouble()*4 + 2
		sz := r.NextDouble()*6 + 3
		cx := r.NextDouble()*(16-sx-2) + 1 + sx/2
		cy := r.NextDouble()*(8-sy-4) + 2 + sy/2
		cz := r.NextDouble()*(16-sz-2) + 1 + sz/2
		for x := 1; x < 15; x++ {
			for z := 1; z < 15; z++ {
				for y := 1; y < 7; y++ {
					dx := (float64(x) - cx) / (sx / 2)
					dy := (float64(y) - cy) / (sy / 2)
					dz := (float64(z) - cz) / (sz / 2)
					if dx*dx+dy*dy+dz*dz < 1 {
						grid[lakeIdx(x, y, z)] = true
					}
				}
			}
		}
	}
	edge := func(x, y, z int) bool {
		if grid[lakeIdx(x, y, z)] {
			return false
		}
		return x < 15 && grid[lakeIdx(x+1, y, z)] || x > 0 && grid[lakeIdx(x-1, y, z)] ||
			z < 15 && grid[lakeIdx(x, y, z+1)] || z > 0 && grid[lakeIdx(x, y, z-1)] ||
			y < 7 && grid[lakeIdx(x, y+1, z)] || y > 0 && grid[lakeIdx(x, y-1, z)]
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if !edge(x, y, z) {
					continue
				}
				st := lv.Block(p.Add(Pos{x, y, z}))
				if y >= 4 && data.Flags(st)&data.FlagFluid != 0 {
					return false
				}
				if y < 4 && !isSolid(st) && st != f.fluid {
					return false
				}
			}
		}
	}
	caveAir, _ := data.DefaultState("cave_air")
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				q := p.Add(Pos{x, y, z})
				if grid[lakeIdx(x, y, z)] && f.airFluid.Test(lv, q) {
					if y >= 4 {
						lv.SetBlock(q, caveAir)
					} else {
						lv.SetBlock(q, f.fluid)
					}
				}
			}
		}
	}
	if f.barrier != data.Air {
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if edge(x, y, z) && (y < 4 || r.NextIntn(2) != 0) {
						q := p.Add(Pos{x, y, z})
						if isSolid(lv.Block(q)) && f.barrierOK.Test(lv, q) {
							lv.SetBlock(q, f.barrier)
						}
					}
				}
			}
		}
	}
	return true
}

func init() {
	featureTypes["lake"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Fluid     json.RawMessage `json:"fluid"`
			Barrier   json.RawMessage `json:"barrier"`
			CanPlace  json.RawMessage `json:"can_place_feature"`
			AirFluid  json.RawMessage `json:"can_replace_with_air_or_fluid"`
			BarrierOK json.RawMessage `json:"can_replace_with_barrier"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("lake: %w", err)
		}
		f := &lakeFeature{}
		var err error
		if f.fluid, err = parseBlockState(raw.Fluid); err != nil {
			return nil, fmt.Errorf("lake: %w", err)
		}
		if f.barrier, err = parseBlockState(raw.Barrier); err != nil {
			return nil, fmt.Errorf("lake: %w", err)
		}
		for _, pr := range []struct {
			dst *Predicate
			raw json.RawMessage
		}{{&f.canPlace, raw.CanPlace}, {&f.airFluid, raw.AirFluid}, {&f.barrierOK, raw.BarrierOK}} {
			if *pr.dst, err = rg.parsePredicate(pr.raw); err != nil {
				return nil, fmt.Errorf("lake: %w", err)
			}
		}
		return f, nil
	}
}
