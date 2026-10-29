package command

import (
	"math"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

type coordKind uint8

const (
	coordVec3 coordKind = iota
	coordBlock
	coordVec2
	coordColumn
	coordRotation
	coordAngle
)

type coord struct {
	v   float64
	rel bool
}

// coords is an unresolved position argument. For local (^) coordinates the
// components are left, up, forward. 2D kinds use c[0], c[1] as x, z (or yaw,
// pitch for rotation).
type coords struct {
	kind  coordKind
	c     [3]coord
	local bool
}

const (
	msgPosMixed       = "Cannot mix world & local coordinates (everything must either use ^ or not)"
	msgPosIncomplete  = "Incomplete (expected 3 coordinates)"
	msgPos2Incomplete = "Incomplete (expected 2 coordinates)"
)

func parseCoords(r *reader, kind coordKind) (*coords, error) {
	start := r.pos
	co := &coords{kind: kind}
	n := 3
	incomplete := msgPosIncomplete
	switch kind {
	case coordVec2, coordColumn, coordRotation:
		n, incomplete = 2, msgPos2Incomplete
	case coordAngle:
		n = 1
	}
	if kind == coordVec3 || kind == coordBlock {
		if r.canRead() && r.peek() == '^' {
			co.local = true
		}
	}
	for i := range n {
		if i > 0 {
			if !r.canRead() || r.peek() != ' ' {
				r.pos = start
				return nil, r.errf("%s", incomplete)
			}
			r.skip()
		}
		var err error
		if co.local {
			co.c[i], err = parseLocal(r)
		} else {
			integer := kind == coordBlock || kind == coordColumn
			center := kind == coordVec3 && i != 1 || kind == coordVec2
			co.c[i], err = parseWorld(r, integer, center)
		}
		if err != nil {
			return nil, err
		}
	}
	return co, nil
}

func parseLocal(r *reader) (coord, error) {
	if !r.canRead() || r.peek() != '^' {
		return coord{}, r.errf(msgPosMixed)
	}
	r.skip()
	if !r.canRead() || r.peek() == ' ' {
		return coord{rel: true}, nil
	}
	v, err := r.readDouble()
	return coord{v: v, rel: true}, err
}

func parseWorld(r *reader, integer, center bool) (coord, error) {
	if r.canRead() && r.peek() == '^' {
		return coord{}, r.errf(msgPosMixed)
	}
	if !r.canRead() {
		if integer {
			return coord{}, r.errf("Expected a block position")
		}
		return coord{}, r.errf("Expected a coordinate")
	}
	if r.peek() == '~' {
		r.skip()
		if !r.canRead() || r.peek() == ' ' {
			return coord{rel: true}, nil
		}
		v, err := r.readDouble()
		return coord{v: v, rel: true}, err
	}
	if integer {
		v, err := r.readInt()
		return coord{v: float64(v)}, err
	}
	start := r.pos
	v, err := r.readDouble()
	if err != nil {
		return coord{}, err
	}
	if center && !strings.Contains(r.s[start:r.pos], ".") {
		v += 0.5
	}
	return coord{v: v}, nil
}

func (co *coords) vec3(o api.Location) api.Vec3 {
	if co.local {
		return localToWorld(o, co.c[0].v, co.c[1].v, co.c[2].v)
	}
	res := func(c coord, base float64) float64 {
		if c.rel {
			return base + c.v
		}
		return c.v
	}
	return api.Vec3{X: res(co.c[0], o.Pos.X), Y: res(co.c[1], o.Pos.Y), Z: res(co.c[2], o.Pos.Z)}
}

func (co *coords) blockPos(o api.Location) api.BlockPos {
	switch co.kind {
	case coordColumn, coordVec2:
		v := co.raw(o)
		return api.BlockPos{X: int(math.Floor(v.X)), Z: int(math.Floor(v.Z))}
	}
	return co.vec3(o).BlockPos()
}

// raw resolves 2D and 1D kinds. Vec2/column map to X/Z; rotation maps yaw to
// X and pitch to Y; angle maps to X.
func (co *coords) raw(o api.Location) api.Vec3 {
	res := func(c coord, base float64) float64 {
		if c.rel {
			return base + c.v
		}
		return c.v
	}
	switch co.kind {
	case coordVec2, coordColumn:
		return api.Vec3{X: res(co.c[0], o.Pos.X), Z: res(co.c[1], o.Pos.Z)}
	case coordRotation:
		return api.Vec3{X: res(co.c[0], float64(o.Yaw)), Y: res(co.c[1], float64(o.Pitch))}
	case coordAngle:
		return api.Vec3{X: res(co.c[0], float64(o.Yaw))}
	}
	return co.vec3(o)
}

// localToWorld follows vanilla LocalCoordinates.getPosition.
func localToWorld(o api.Location, left, up, forward float64) api.Vec3 {
	const rad = math.Pi / 180
	yaw, pitch := float64(o.Yaw), float64(o.Pitch)
	f := math.Cos((yaw + 90) * rad)
	f1 := math.Sin((yaw + 90) * rad)
	f2 := math.Cos(-pitch * rad)
	f3 := math.Sin(-pitch * rad)
	f4 := math.Cos((-pitch + 90) * rad)
	f5 := math.Sin((-pitch + 90) * rad)
	fw := api.Vec3{X: f * f2, Y: f3, Z: f1 * f2}
	upv := api.Vec3{X: f * f4, Y: f5, Z: f1 * f4}
	lf := api.Vec3{
		X: -(fw.Y*upv.Z - fw.Z*upv.Y),
		Y: -(fw.Z*upv.X - fw.X*upv.Z),
		Z: -(fw.X*upv.Y - fw.Y*upv.X),
	}
	return api.Vec3{
		X: o.Pos.X + fw.X*forward + upv.X*up + lf.X*left,
		Y: o.Pos.Y + fw.Y*forward + upv.Y*up + lf.Y*left,
		Z: o.Pos.Z + fw.Z*forward + upv.Z*up + lf.Z*left,
	}
}
