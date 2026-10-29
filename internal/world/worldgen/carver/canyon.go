package carver

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Canyon is the reference CanyonWorldCarver configuration.
type Canyon struct {
	Probability      float32     `json:"probability"`
	Y                Height      `json:"y"`
	VerticalRotation Float       `json:"vertical_rotation"`
	Shape            CanyonShape `json:"shape"`
}

// CanyonShape is the reference CanyonWorldCarver.Shape.
type CanyonShape struct {
	DistanceFactor        Float   `json:"distance_factor"`
	Thickness             Float   `json:"thickness"`
	WidthSmoothness       int32   `json:"width_smoothness"`
	HorizontalRadius      Float   `json:"horizontal_radius_factor"`
	VerticalRadiusDefault float32 `json:"vertical_radius_default_factor"`
	VerticalRadiusCenter  float32 `json:"vertical_radius_center_factor"`
	YScale                Float   `json:"y_scale"`
}

func (c *Canyon) IsStart(r rng.Source) bool { return r.NextFloat() <= c.Probability }

func (c *Canyon) Carve(ctx Context, r rng.Source, cx, cz, sx, sz int32, out Output) {
	x := float64(int(sx)<<4 + int(r.NextIntn(16)))
	y := float64(c.Y.Sample(r, ctx))
	z := float64(int(sz)<<4 + int(r.NextIntn(16)))
	yaw := float32(r.NextFloat() * twoPi)
	pitch := c.VerticalRotation.Sample(r)
	ys := float64(c.Shape.YScale.Sample(r))
	th := c.Shape.Thickness.Sample(r)
	dist := int(float32(maxDistance) * c.Shape.DistanceFactor.Sample(r))
	seed := r.NextLong()
	c.carve(ctx, cx, cz, seed, x, y, z, th, yaw, pitch, 0, dist, ys, out)
}

func (c *Canyon) carve(ctx Context, cx, cz int32, seed int64, x, y, z float64, th, yaw, pitch float32, step0, dist int, ys float64, out Output) {
	r := rng.MakeLegacy(seed)
	w := c.widthFactors(ctx, &r)
	minY := ctx.MinY
	skip := func(dx, dy, dz float64, py int) bool {
		f := float64(w[py-minY-1])
		return float64(float64(dx*dx)+float64(dz*dz))*f+float64(dy*dy)/6 >= 1
	}
	var dyaw, dpitch float32
	for step := step0; step < dist; step++ {
		base := 1.5 + float64(Sin(float64(float32(step)*piF/float32(dist)))*th)
		hr := base * float64(c.Shape.HorizontalRadius.Sample(&r))
		vr := c.verticalRadius(&r, base*ys, float32(dist), float32(step))
		cp := Cos(float64(pitch))
		sp := Sin(float64(pitch))
		x += float64(Cos(float64(yaw)) * cp)
		y += float64(sp)
		z += float64(Sin(float64(yaw)) * cp)
		pitch = float32(pitch * 0.7)
		pitch += float32(dpitch * 0.05)
		yaw += float32(dyaw * 0.05)
		dpitch = float32(dpitch * 0.8)
		dyaw = float32(dyaw * 0.5)
		dpitch += float32((r.NextFloat() - r.NextFloat()) * r.NextFloat() * 2)
		dyaw += float32((r.NextFloat() - r.NextFloat()) * r.NextFloat() * 4)
		if r.NextIntn(4) == 0 {
			continue
		}
		if !CanReach(cx, cz, x, z, step, dist, th) {
			return
		}
		CarveEllipsoid(cx, cz, x, y, z, hr, vr, out, skip)
	}
}

func (c *Canyon) widthFactors(ctx Context, r rng.Source) []float32 {
	w := make([]float32, ctx.Height)
	f := float32(1)
	for i := range w {
		if i == 0 || r.NextIntn(c.Shape.WidthSmoothness) == 0 {
			f = 1 + float32(r.NextFloat()*r.NextFloat())
		}
		w[i] = f * f
	}
	return w
}

func (c *Canyon) verticalRadius(r rng.Source, vr float64, dist, step float32) float64 {
	t := 1 - float32(abs32(0.5-step/dist)*2)
	f := c.Shape.VerticalRadiusDefault + float32(c.Shape.VerticalRadiusCenter*t)
	return float64(f) * vr * float64(randomBetween(r, 0.75, 1))
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
