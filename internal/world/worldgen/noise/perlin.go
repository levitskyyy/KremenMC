package noise

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// PerlinRange is the reference PerlinNoise.RANGE bound.
var PerlinRange = Interval{-2, 2}

// PerlinStandardDeviation is the reference estimate of single-octave
// Perlin noise deviation, used by normal-noise normalization.
const PerlinStandardDeviation = 0.2702247831245211

// Perlin is the reference PerlinNoise: one octave of improved gradient noise.
type Perlin struct{ gradientBase }

// NewPerlin consumes 3 doubles and 256 bounded ints from r.
func NewPerlin(r rng.Source) *Perlin {
	p := &Perlin{}
	p.init(r)
	return p
}

func (p *Perlin) Range() Interval { return PerlinRange }

// Get samples the noise at (x, y, z).
func (p *Perlin) Get(x, y, z float64) float32 {
	d := Wrap(x) + p.offX
	e := Wrap(y) + p.offY
	f := Wrap(z) + p.offZ
	i, j, k := mth.Floor(d), mth.Floor(e), mth.Floor(f)
	fy := float32(e - float64(j))
	return p.SampleAndLerp(i, j, k, float32(d-float64(i)), fy, float32(f-float64(k)), fy)
}

// Get2 is the reference two-argument get(x, z) = get(x, 0, z).
func (p *Perlin) Get2(x, z float64) float32 { return p.Get(x, 0, z) }

// SampleAndLerp blends the 8 corner gradients of lattice cell (gx, gy, gz).
// dy is the y fraction used for gradients and fadeY the one used for the
// fade weight; they differ only for smeared sampling.
func (p *Perlin) SampleAndLerp(gx, gy, gz int32, dx, dy, dz, fadeY float32) float32 {
	i := p.p(gx)
	j := p.p(gx + 1)
	k := p.p(i + gy)
	l := p.p(i + gy + 1)
	m := p.p(j + gy)
	n := p.p(j + gy + 1)
	dx1, dy1, dz1 := dx-1, dy-1, dz-1
	v000 := gradDot(p.p(k+gz), dx, dy, dz)
	v100 := gradDot(p.p(m+gz), dx1, dy, dz)
	v010 := gradDot(p.p(l+gz), dx, dy1, dz)
	v110 := gradDot(p.p(n+gz), dx1, dy1, dz)
	v001 := gradDot(p.p(k+gz+1), dx, dy, dz1)
	v101 := gradDot(p.p(m+gz+1), dx1, dy, dz1)
	v011 := gradDot(p.p(l+gz+1), dx, dy1, dz1)
	v111 := gradDot(p.p(n+gz+1), dx1, dy1, dz1)
	return mth.Lerp3F(mth.SmoothStep(dx), mth.SmoothStep(fadeY), mth.SmoothStep(dz),
		v000, v100, v010, v110, v001, v101, v011, v111)
}

// NoiseWithDerivative samples like Get and adds the analytic gradient of
// the noise to d (the reference accumulates into its output array).
func (p *Perlin) NoiseWithDerivative(x, y, z float64, d *[3]float32) float32 {
	dd := Wrap(x) + p.offX
	e := Wrap(y) + p.offY
	f := Wrap(z) + p.offZ
	i, j, k := mth.Floor(dd), mth.Floor(e), mth.Floor(f)
	return p.sampleWithDerivative(i, j, k, float32(dd-float64(i)), float32(e-float64(j)), float32(f-float64(k)), d)
}

func (p *Perlin) sampleWithDerivative(gx, gy, gz int32, dx, dy, dz float32, out *[3]float32) float32 {
	i := p.p(gx)
	j := p.p(gx + 1)
	k := p.p(i + gy)
	l := p.p(i + gy + 1)
	m := p.p(j + gy)
	n := p.p(j + gy + 1)
	g := [8]*[3]int8{
		&gradients[p.p(k+gz)&15], &gradients[p.p(m+gz)&15], &gradients[p.p(l+gz)&15], &gradients[p.p(n+gz)&15],
		&gradients[p.p(k+gz+1)&15], &gradients[p.p(m+gz+1)&15], &gradients[p.p(l+gz+1)&15], &gradients[p.p(n+gz+1)&15],
	}
	dot := func(g *[3]int8, x, y, z float32) float32 {
		return float32(float32(float32(g[0])*x)+float32(float32(g[1])*y)) + float32(float32(g[2])*z)
	}
	dx1, dy1, dz1 := dx-1, dy-1, dz-1
	v0 := dot(g[0], dx, dy, dz)
	v1 := dot(g[1], dx1, dy, dz)
	v2 := dot(g[2], dx, dy1, dz)
	v3 := dot(g[3], dx1, dy1, dz)
	v4 := dot(g[4], dx, dy, dz1)
	v5 := dot(g[5], dx1, dy, dz1)
	v6 := dot(g[6], dx, dy1, dz1)
	v7 := dot(g[7], dx1, dy1, dz1)
	sx, sy, sz := mth.SmoothStep(dx), mth.SmoothStep(dy), mth.SmoothStep(dz)
	var c [3]float32
	for a := range 3 {
		c[a] = mth.Lerp3F(sx, sy, sz, float32(g[0][a]), float32(g[1][a]), float32(g[2][a]), float32(g[3][a]),
			float32(g[4][a]), float32(g[5][a]), float32(g[6][a]), float32(g[7][a]))
	}
	ex := mth.Lerp2F(sy, sz, v1-v0, v3-v2, v5-v4, v7-v6)
	ey := mth.Lerp2F(sz, sx, v2-v0, v6-v4, v3-v1, v7-v5)
	ez := mth.Lerp2F(sx, sy, v4-v0, v5-v1, v6-v2, v7-v3)
	out[0] += c[0] + float32(mth.SmoothStepDerivative(dx)*ex)
	out[1] += c[1] + float32(mth.SmoothStepDerivative(dy)*ey)
	out[2] += c[2] + float32(mth.SmoothStepDerivative(dz)*ez)
	return mth.Lerp3F(sx, sy, sz, v0, v1, v2, v3, v4, v5, v6, v7)
}

func (p *Perlin) AddTo(acc float32, bx, by, bz, xzScale, yScale float64, amp float32) float32 {
	return acc + float32(amp*p.Get(float64(bx*xzScale), float64(by*yScale), float64(bz*xzScale)))
}
