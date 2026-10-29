package carver

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Cave is the reference CaveWorldCarver configuration.
type Cave struct {
	Probability         float32 `json:"probability"`
	Y                   Height  `json:"y"`
	Count               Int     `json:"count"`
	Thickness           Float   `json:"thickness"`
	WeirdThicknessBias  bool    `json:"weird_thickness_bias"`
	RoomVerticalRadius  Float   `json:"room_vertical_radius_multiplier"`
	HorizontalRadius    Float   `json:"horizontal_radius_multiplier"`
	VerticalRadius      Float   `json:"vertical_radius_multiplier"`
	StartVerticalRadius Float   `json:"start_vertical_radius_multiplier"`
	FloorLevel          Float   `json:"floor_level"`
}

const (
	piF   = float32(math.Pi)
	twoPi = piF * 2
	halfP = piF / 2
)

func (c *Cave) IsStart(r rng.Source) bool { return r.NextFloat() <= c.Probability }

func (c *Cave) Carve(ctx Context, r rng.Source, cx, cz, sx, sz int32, out Output) {
	count := c.Count.Sample(r)
	for range count {
		x := float64(int(sx)<<4 + int(r.NextIntn(16)))
		y := float64(c.Y.Sample(r, ctx))
		z := float64(int(sz)<<4 + int(r.NextIntn(16)))
		hm := float64(c.HorizontalRadius.Sample(r))
		vm := float64(c.VerticalRadius.Sample(r))
		svm := float64(c.StartVerticalRadius.Sample(r))
		floor := float64(c.FloorLevel.Sample(r))
		skip := func(dx, dy, dz float64, _ int) bool { return caveSkip(dx, dy, dz, floor) }
		tunnels := 1
		if r.NextIntn(4) == 0 {
			rvm := float64(c.RoomVerticalRadius.Sample(r))
			f := 1 + float32(r.NextFloat()*6)
			c.room(cx, cz, x, y, z, f, rvm, out, skip)
			tunnels += int(r.NextIntn(4))
		}
		for range tunnels {
			yaw := float32(r.NextFloat() * twoPi)
			pitch := (r.NextFloat() - 0.5) / 4
			th := c.thickness(r)
			dist := maxDistance - int(r.NextIntn(maxDistance/4))
			seed := r.NextLong()
			c.tunnel(cx, cz, seed, x, y, z, hm, vm, th, yaw, pitch, 0, dist, svm, out, skip)
		}
	}
}

// maxDistance is (2*range-1)*16 blocks for the carver range of 4 chunks.
const maxDistance = (2*Range - 1) * 16

// Range is the reference WorldCarver.getRange: source chunks up to this
// distance can carve into a chunk.
const Range = 4

func (c *Cave) thickness(r rng.Source) float32 {
	t := c.Thickness.Sample(r)
	if c.WeirdThicknessBias && r.NextIntn(10) == 0 {
		t = float32(t * (float32(r.NextFloat()*r.NextFloat()*3) + 1))
	}
	return t
}

func (c *Cave) room(cx, cz int32, x, y, z float64, f float32, rvm float64, out Output, skip SkipFunc) {
	hr := 1.5 + float64(Sin(float64(halfP))*f)
	vr := hr * rvm
	CarveEllipsoid(cx, cz, x+1, y, z, hr, vr, out, skip)
}

func (c *Cave) tunnel(cx, cz int32, seed int64, x, y, z, hm, vm float64, th, yaw, pitch float32, step0, dist int, svm float64, out Output, skip SkipFunc) {
	r := rng.MakeLegacy(seed)
	branch := int(r.NextIntn(int32(dist/2))) + dist/4
	steep := r.NextIntn(6) == 0
	var dyaw, dpitch float32
	for step := step0; step < dist; step++ {
		base := 1.5 + float64(Sin(float64(float32(step)*piF/float32(dist)))*th)
		hr := base * hm
		vr := base * vm * svm
		cp := Cos(float64(pitch))
		x += float64(Cos(float64(yaw)) * cp)
		y += float64(Sin(float64(pitch)))
		z += float64(Sin(float64(yaw)) * cp)
		if steep {
			pitch = float32(pitch * 0.92)
		} else {
			pitch = float32(pitch * 0.7)
		}
		pitch += float32(dpitch * 0.1)
		yaw += float32(dyaw * 0.1)
		dpitch = float32(dpitch * 0.9)
		dyaw = float32(dyaw * 0.75)
		dpitch += float32((r.NextFloat() - r.NextFloat()) * r.NextFloat() * 2)
		dyaw += float32((r.NextFloat() - r.NextFloat()) * r.NextFloat() * 4)
		if step == branch && th > 1 {
			c.tunnel(cx, cz, r.NextLong(), x, y, z, hm, vm, float32(r.NextFloat()*0.5)+0.5, yaw-halfP, pitch/3, step, dist, 1, out, skip)
			c.tunnel(cx, cz, r.NextLong(), x, y, z, hm, vm, float32(r.NextFloat()*0.5)+0.5, yaw+halfP, pitch/3, step, dist, 1, out, skip)
			return
		}
		if r.NextIntn(4) == 0 {
			continue
		}
		if !CanReach(cx, cz, x, z, step, dist, th) {
			return
		}
		CarveEllipsoid(cx, cz, x, y, z, hr, vr, out, skip)
	}
}
