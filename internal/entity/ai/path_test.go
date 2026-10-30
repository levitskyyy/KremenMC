package ai

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

type grid map[[3]int]data.StateID

func (g grid) Block(x, y, z int) data.StateID {
	if s, ok := g[[3]int{x, y, z}]; ok || y >= 0 {
		return s
	}
	return st("stone")
}

func st(name string) data.StateID {
	s, ok := data.Canonical().DefaultState(name)
	if !ok {
		panic(name)
	}
	return s
}

func TestFindPath(t *testing.T) {
	wall := func(g grid, b string, x, h int) {
		for z := -10; z <= 10; z++ {
			for y := 0; y < h; y++ {
				if z != 3 {
					g[[3]int{x, y, z}] = st(b)
				}
			}
		}
	}
	tests := []struct {
		name  string
		build func(g grid)
		reach bool
		maxY  int32
		viaZ  int32 // path must pass this z at x=wall (gap), 99 = any
	}{
		{"flat", func(grid) {}, true, 0, 99},
		{"wall gap", func(g grid) { wall(g, "stone", 5, 3) }, true, 0, 3},
		{"fence gap", func(g grid) { wall(g, "oak_fence", 5, 1) }, true, 0, 3},
		{"step up", func(g grid) {
			for x := 5; x <= 12; x++ {
				for z := -10; z <= 10; z++ {
					g[[3]int{x, 0, z}] = st("stone")
				}
			}
		}, true, 1, 99},
		{"lava moat", func(g grid) {
			for z := -60; z <= 60; z++ {
				g[[3]int{5, -1, z}] = st("lava")
			}
		}, false, 0, 99},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := grid{}
			tc.build(g)
			f := GetFinder()
			defer PutFinder(f)
			to := Node{10, 0, 0}
			if tc.maxY == 1 {
				to.Y = 1
			}
			p, ok := f.Find(Request{Src: g, Width: 0.6, Height: 1.95, StepHeight: 0.6, From: Node{0, 0, 0}, To: to, MaxNodes: 2000, MaxDist: 40}, nil)
			if !ok {
				t.Fatal("no path")
			}
			last := p[len(p)-1]
			if got := last == to; got != tc.reach {
				t.Fatalf("reached=%v want %v (end %v, %v)", got, tc.reach, last, p)
			}
			if tc.viaZ != 99 {
				found := false
				for _, n := range p {
					if n.X == 5 && n.Z == tc.viaZ {
						found = true
					}
				}
				if !found {
					t.Fatalf("path does not use the gap: %v", p)
				}
			}
		})
	}
}

func BenchmarkFindPath(b *testing.B) {
	g := grid{}
	f := GetFinder()
	var out []Node
	for b.Loop() {
		out, _ = f.Find(Request{Src: g, Width: 0.6, Height: 1.95, StepHeight: 0.6, From: Node{0, 0, 0}, To: Node{20, 0, 7}, MaxNodes: 512, MaxDist: 40}, out[:0])
	}
}
