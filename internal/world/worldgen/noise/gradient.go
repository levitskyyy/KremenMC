// Package noise implements the reference 26.3 noise primitives
// (net.minecraft.world.level.levelgen.synth): gradient (Perlin "improved")
// noise and its smeared variant, simplex noise, noise stacks (octave sums),
// normal noise and the blended terrain noise. Sampling is bit-exact with
// the reference: values are float32, lattice math follows its evaluation
// order, and products are explicitly rounded so Go never fuses them into
// FMA instructions.
//
// Every noise value is immutable once constructed; all sampling methods are
// pure and safe for concurrent use by any number of goroutines.
package noise

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// gradients is the reference 16-entry gradient table (12 cube edges plus 4
// repeats). Only the low 4 bits of a permuted hash select an entry.
var gradients = [16][3]int8{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

// gradientsF is gradients as float32 (int8 -> float32 is exact), sparing
// the conversions on the hot path.
var gradientsF = func() (t [16][3]float32) {
	for i, g := range gradients {
		t[i] = [3]float32{float32(g[0]), float32(g[1]), float32(g[2])}
	}
	return
}()

const roundOff = 33554432.0

// gradientBase is the reference GradientNoise state: a 256-entry
// permutation and a random lattice offset.
type gradientBase struct {
	perm             [256]uint8
	offX, offY, offZ float64
}

func (g *gradientBase) init(r rng.Source) {
	g.offX = float64(r.NextDouble() * 256)
	g.offY = float64(r.NextDouble() * 256)
	g.offZ = float64(r.NextDouble() * 256)
	for i := range g.perm {
		g.perm[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(r.NextIntn(int32(256-i)))
		g.perm[i], g.perm[j] = g.perm[j], g.perm[i]
	}
}

// Offsets returns the lattice offset added to sample coordinates.
func (g *gradientBase) Offsets() (x, y, z float64) { return g.offX, g.offY, g.offZ }

// Perm returns the permutation table.
func (g *gradientBase) Perm() [256]uint8 { return g.perm }

func (g *gradientBase) p(i int32) int32 { return int32(g.perm[uint8(i)]) }

func gradDot(h int32, x, y, z float32) float32 {
	g := &gradientsF[h&15]
	return float32(float32(g[0]*x)+float32(g[1]*y)) + float32(g[2]*z)
}

// Wrap folds a coordinate into [-2^24, 2^24] to keep precision, like the
// reference GradientNoise.wrap.
func Wrap(d float64) float64 {
	return d - float64(float64(mth.LFloor(d/roundOff+0.5))*roundOff)
}
