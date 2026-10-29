package carver

import "github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"

// SkipFunc reports whether a position inside an ellipsoid's bounding
// cylinder is left uncarved; dx, dy, dz are the offsets from the centre
// divided by the radii, y is absolute.
type SkipFunc func(dx, dy, dz float64, y int) bool

// CarveEllipsoid is the reference WorldCarver.carveEllipsoid: it carves
// the part of the ellipsoid at x,y,z with horizontal radius hr and
// vertical radius vr that lies in chunk cx,cz.
func CarveEllipsoid(cx, cz int32, x, y, z, hr, vr float64, out Output, skip SkipFunc) {
	bx, bz := int(cx)<<4, int(cz)<<4
	x0 := max(int(mth.Floor(x-hr))-bx-1, 0)
	x1 := min(int(mth.Floor(x+hr))-bx, 15)
	y0 := max(int(mth.Floor(y-vr))-1, out.MinY())
	y1 := min(int(mth.Floor(y+vr))+1, out.MaxY())
	z0 := max(int(mth.Floor(z-hr))-bz-1, 0)
	z1 := min(int(mth.Floor(z+hr))-bz, 15)
	for lx := x0; lx <= x1; lx++ {
		dx := (float64(bx+lx) + 0.5 - x) / hr
		for lz := z0; lz <= z1; lz++ {
			dz := (float64(bz+lz) + 0.5 - z) / hr
			if float64(dx*dx)+float64(dz*dz) >= 1 {
				continue
			}
			for py := y1; py > y0; py-- {
				dy := (float64(py) - 0.5 - y) / vr
				if !skip(dx, dy, dz, py) {
					out.Carve(lx, py, lz)
				}
			}
		}
	}
}

// CanReach is the reference WorldCarver.canReach: whether a tunnel at
// x,z with steps left can still reach chunk cx,cz.
func CanReach(cx, cz int32, x, z float64, step, distance int, thickness float32) bool {
	dx := x - float64(int(cx)<<4+8)
	dz := z - float64(int(cz)<<4+8)
	rem := float64(distance - step)
	r := float64(thickness) + 2 + 16
	return float64(dx*dx)+float64(dz*dz)-float64(rem*rem) <= r*r
}

func caveSkip(dx, dy, dz, floor float64) bool {
	if dy <= floor {
		return true
	}
	return float64(dx*dx)+float64(dy*dy)+float64(dz*dz) >= 1
}
