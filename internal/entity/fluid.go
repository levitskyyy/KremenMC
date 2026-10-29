package entity

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

type Fluid uint8

const (
	FluidNone Fluid = iota
	FluidWater
	FluidLava
)

var waterPlants = map[string]bool{
	"water": true, "bubble_column": true, "kelp": true, "kelp_plant": true, "seagrass": true, "tall_seagrass": true,
}

// FluidAt returns the fluid in state s and its surface height within the
// block (source 8/9, flowing (8-level)/9).
func FluidAt(s data.StateID) (Fluid, float64) {
	if !data.Canonical().HasFluid(s) {
		return FluidNone, 0
	}
	b := data.BlockOf(s)
	if b == nil {
		return FluidNone, 0
	}
	f := FluidNone
	switch {
	case b.Name == "lava":
		f = FluidLava
	case waterPlants[b.Name]:
		f = FluidWater
	default:
		if v, ok := data.Canonical().Value(s, "waterlogged"); ok && v == "true" {
			f = FluidWater
		}
	}
	if f == FluidNone {
		return FluidNone, 0
	}
	amount := 8
	if b.Name == "water" || b.Name == "lava" {
		if v, ok := data.Canonical().Value(s, "level"); ok {
			l := int(v[0] - '0')
			if len(v) > 1 {
				l = 10 + int(v[1]-'0')
			}
			if l > 0 && l < 8 {
				amount = 8 - l
			}
		}
	}
	return f, float64(amount) / 9
}

// fluidHeight returns the fluid kind and its absolute surface height in
// block (x, y, z); a block with the same fluid above is full.
func fluidHeight(src BlockSource, x, y, z int) (Fluid, float64) {
	f, h := FluidAt(src.Block(x, y, z))
	if f == FluidNone {
		return f, 0
	}
	if up, _ := FluidAt(src.Block(x, y+1, z)); up == f {
		h = 1
	}
	return f, float64(y) + h
}

// updateFluids sets InWater/InLava and the submersion heights from the box
// at the start of the tick.
func (e *Entity) updateFluids(src BlockSource) {
	e.InWater, e.InLava = false, false
	e.WaterHeight, e.LavaHeight = 0, 0
	b := e.Box().Grow(-0.001, -0.001, -0.001)
	x0, y0, z0 := int(math.Floor(b.MinX)), int(math.Floor(b.MinY)), int(math.Floor(b.MinZ))
	x1, y1, z1 := int(math.Ceil(b.MaxX)), int(math.Ceil(b.MaxY)), int(math.Ceil(b.MaxZ))
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			for z := z0; z < z1; z++ {
				f, top := fluidHeight(src, x, y, z)
				if f == FluidNone || top < b.MinY {
					continue
				}
				d := top - b.MinY
				switch f {
				case FluidWater:
					e.InWater = true
					e.WaterHeight = max(e.WaterHeight, d)
				case FluidLava:
					e.InLava = true
					e.LavaHeight = max(e.LavaHeight, d)
				}
			}
		}
	}
}

func (e *Entity) boxHasFluid(src BlockSource, b AABB) bool {
	x0, y0, z0 := int(math.Floor(b.MinX)), int(math.Floor(b.MinY)), int(math.Floor(b.MinZ))
	x1, y1, z1 := int(math.Ceil(b.MaxX)), int(math.Ceil(b.MaxY)), int(math.Ceil(b.MaxZ))
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			for z := z0; z < z1; z++ {
				if f, top := fluidHeight(src, x, y, z); f != FluidNone && top >= b.MinY {
					return true
				}
			}
		}
	}
	return false
}
