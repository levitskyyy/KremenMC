package world

import (
	"math/rand/v2"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

var glowstone = data.MustState("glowstone")

type testGrid struct {
	dim    Dimension
	chunks map[uint64]*Chunk
	n      int32 // chunks span [0,n) on both axes
}

func newTestGrid(n int32, dim Dimension) *testGrid {
	g := &testGrid{dim: dim, chunks: map[uint64]*Chunk{}, n: n}
	for x := range n {
		for z := range n {
			g.chunks[ChunkPos{x, z}.Key()] = NewChunk(x, z, dim, 0)
		}
	}
	return g
}

func (g *testGrid) src(cx, cz int32) *Chunk { return g.chunks[ChunkPos{cx, cz}.Key()] }

func (g *testGrid) block(x, y, z int) data.StateID {
	return g.src(int32(x>>4), int32(z>>4)).Block(x&15, y, z&15)
}

func (g *testGrid) light(sky bool, x, y, z int) uint8 {
	c := g.src(int32(x>>4), int32(z>>4))
	i := (y-g.dim.MinY)>>4 + 1
	idx := (y&15)<<8 | (z&15)<<4 | x&15
	if sky {
		return c.SkyLight[i].Get(idx)
	}
	return c.BlockLight[i].Get(idx)
}

// reference computes the least fixpoint of the light equations by relaxation.
func (g *testGrid) reference(sky bool) map[[3]int]uint8 {
	w := int(g.n) * 16
	top := g.dim.MinY + g.dim.Height
	L := map[[3]int]uint8{}
	for changed := true; changed; {
		changed = false
		for y := top - 1; y >= g.dim.MinY; y-- {
			for z := range w {
				for x := range w {
					st := g.block(x, y, z)
					f := data.LightFilter(st)
					var best uint8
					if !sky {
						best = data.LightEmission(st)
					}
					for d, dv := range dirs {
						// neighbor is the source; light travels opposite to dv
						nx, ny, nz := x-int(dv[0]), y-int(dv[1]), z-int(dv[2])
						var nl uint8
						switch {
						case ny >= top:
							if !sky {
								continue
							}
							nl = 15
						case ny < g.dim.MinY || nx < 0 || nz < 0 || nx >= w || nz >= w:
							continue
						default:
							nl = L[[3]int{nx, ny, nz}]
						}
						best = max(best, step(sky, d, nl, f))
					}
					k := [3]int{x, y, z}
					if best != L[k] {
						L[k] = best
						changed = true
					}
				}
			}
		}
	}
	return L
}

func (g *testGrid) check(t *testing.T, label string) {
	t.Helper()
	for _, sky := range []bool{true, false} {
		ref := g.reference(sky)
		w := int(g.n) * 16
		bad := 0
		for y := g.dim.MinY; y < g.dim.MinY+g.dim.Height; y++ {
			for z := range w {
				for x := range w {
					if got, want := g.light(sky, x, y, z), ref[[3]int{x, y, z}]; got != want {
						if bad < 5 {
							t.Errorf("%s sky=%v at %d,%d,%d: got %d want %d (block %s)", label, sky, x, y, z, got, want, data.StateString(g.block(x, y, z)))
						}
						bad++
					}
				}
			}
		}
		if bad > 0 {
			t.Fatalf("%s sky=%v: %d mismatches", label, sky, bad)
		}
	}
}

func randomTerrain(g *testGrid, r *rand.Rand) {
	pick := []data.StateID{stone, stone, stone, glass, leaves, water, torch, glowstone}
	for _, c := range g.chunks {
		for x := range 16 {
			for z := range 16 {
				h := 8 + r.IntN(16)
				for y := g.dim.MinY; y < h; y++ {
					if r.IntN(10) < 7 {
						c.Sections[(y-g.dim.MinY)>>4].SetBlock(x, y&15, z, pick[r.IntN(len(pick))])
					}
				}
			}
		}
		c.Finish()
	}
}

func lightAll(g *testGrid) {
	e := NewLightEngine(g.dim, g.src)
	for _, c := range g.chunks {
		e.LightChunk(c)
	}
	for _, c := range g.chunks {
		e.Stitch(c.X, c.Z)
	}
}

func TestLightInitialMatchesReference(t *testing.T) {
	dim := Dimension{MinY: -16, Height: 48, HasSkylight: true}
	for seed := range uint64(3) {
		g := newTestGrid(2, dim)
		randomTerrain(g, rand.New(rand.NewPCG(seed, 9)))
		lightAll(g)
		g.check(t, "initial")
	}
}

func TestLightIncrementalMatchesReference(t *testing.T) {
	dim := Dimension{MinY: 0, Height: 32, HasSkylight: true}
	r := rand.New(rand.NewPCG(4, 4))
	g := newTestGrid(2, dim)
	randomTerrain(g, r)
	lightAll(g)
	e := NewLightEngine(dim, g.src)
	pick := []data.StateID{data.Air, data.Air, stone, glass, leaves, water, torch, glowstone}
	for i := range 60 {
		x, y, z := r.IntN(32), r.IntN(32), r.IntN(32)
		g.src(int32(x>>4), int32(z>>4)).SetBlock(x&15, y, z&15, pick[r.IntN(len(pick))])
		e.Update(x, y, z)
		if i%15 == 14 {
			g.check(t, "incremental")
		}
	}
}

func TestLightCases(t *testing.T) {
	dim := Dimension{MinY: 0, Height: 32, HasSkylight: true}
	g := newTestGrid(2, dim)
	for _, c := range g.chunks {
		for x := range 16 {
			for z := range 16 {
				for y := range 10 {
					c.Sections[0].SetBlock(x, y, z, stone)
				}
			}
		}
		c.Finish()
	}
	lightAll(g)
	e := NewLightEngine(dim, g.src)
	set := func(x, y, z int, s data.StateID) {
		g.src(int32(x>>4), int32(z>>4)).SetBlock(x&15, y, z&15, s)
		e.Update(x, y, z)
	}
	tests := []struct {
		name    string
		do      func()
		sky     bool
		x, y, z int
		want    uint8
	}{
		{"open sky", nil, true, 5, 10, 5, 15},
		{"under ground", nil, true, 5, 9, 5, 0},
		{"torch", func() { set(15, 10, 8, torch) }, false, 15, 10, 8, 14},
		{"torch crosses chunk border", nil, false, 16, 10, 8, 13},
		{"torch distance 5", nil, false, 20, 10, 8, 9},
		{"torch removed", func() { set(15, 10, 8, data.Air) }, false, 18, 10, 8, 0},
		{"roof shades below", func() { set(5, 12, 5, stone) }, true, 5, 11, 5, 14},
		{"glass keeps 15", func() { set(6, 12, 6, glass) }, true, 6, 11, 6, 15},
		{"leaves filter", func() { set(7, 12, 7, leaves) }, true, 7, 11, 7, 14},
		{"hole lets sky in", func() { set(3, 9, 3, data.Air) }, true, 3, 9, 3, 15},
		{"cave dark", func() { set(3, 9, 3, stone); set(3, 5, 3, data.Air) }, true, 3, 5, 3, 0},
	}
	for _, tt := range tests {
		if tt.do != nil {
			tt.do()
		}
		if got := g.light(tt.sky, tt.x, tt.y, tt.z); got != tt.want {
			t.Errorf("%s: light = %d want %d", tt.name, got, tt.want)
		}
	}
	g.check(t, "cases")
}

func BenchmarkLightChunk(b *testing.B) {
	g := newTestGrid(1, Overworld)
	r := rand.New(rand.NewPCG(1, 1))
	c := g.src(0, 0)
	for x := range 16 {
		for z := range 16 {
			h := 60 + r.IntN(8)
			for y := -64; y < h; y++ {
				c.Sections[(y+64)>>4].SetBlock(x, y&15, z, stone)
			}
		}
	}
	c.SetBlock(3, 70, 3, torch)
	c.Finish()
	e := NewLightEngine(Overworld, nil)
	for b.Loop() {
		e.LightChunk(c)
	}
}
