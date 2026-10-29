package entity

import (
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

type blocks map[[3]int]data.StateID

func (b blocks) Block(x, y, z int) data.StateID { return b[[3]int{x, y, z}] }

func (b blocks) fill(s data.StateID, x0, y0, z0, x1, y1, z1 int) blocks {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				b[[3]int{x, y, z}] = s
			}
		}
	}
	return b
}

func mustState(name string) data.StateID {
	s, ok := data.DefaultState(name)
	if !ok {
		panic("unknown block " + name)
	}
	return s
}

var (
	stone = mustState("stone")
	slab  = mustState("oak_slab")
	fence = mustState("oak_fence")
	ice   = mustState("ice")
)

// floor returns a world with a stone floor whose top is at y=64.
func floor() blocks { return blocks{}.fill(stone, -8, 63, -8, 16, 63, 16) }

const (
	pw = 0.6
	ph = 1.8
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMove(t *testing.T) {
	type want struct {
		dx, dy, dz float64
		cx, cy, cz bool
		ground     bool
	}
	tests := []struct {
		name       string
		src        blocks
		pos        [3]float64
		dx, dy, dz float64
		step       float64
		onGround   bool
		want       want
	}{
		{
			name: "free fall in air",
			src:  blocks{},
			pos:  [3]float64{0.5, 70, 0.5}, dy: -1,
			want: want{dy: -1},
		},
		{
			name: "land on floor",
			src:  floor(),
			pos:  [3]float64{0.5, 65, 0.5}, dy: -2,
			want: want{dy: -1, cy: true, ground: true},
		},
		{
			name: "no tunnelling at high speed",
			src:  floor(),
			pos:  [3]float64{0.5, 70, 0.5}, dy: -10,
			want: want{dy: -6, cy: true, ground: true},
		},
		{
			name: "resting on floor",
			src:  floor(),
			pos:  [3]float64{0.5, 64, 0.5}, dy: -0.08, onGround: true,
			want: want{dy: 0, cy: true, ground: true},
		},
		{
			name: "walk into wall",
			src:  floor().fill(stone, 10, 64, -8, 10, 66, 16),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 0.5,
			want: want{dx: 10 - (9.5 + pw/2), cx: true},
		},
		{
			name: "walk into wall towards negative x",
			src:  floor().fill(stone, 0, 64, -8, 0, 66, 16),
			pos:  [3]float64{1.5, 64, 0.5}, dx: -0.5,
			want: want{dx: 1 - (1.5 - pw/2), cx: true},
		},
		{
			name: "slide along wall diagonally",
			src:  floor().fill(stone, 10, 64, -8, 10, 66, 16),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 0.4, dz: 0.3,
			want: want{dx: 10 - (9.5 + pw/2), dz: 0.3, cx: true},
		},
		{
			name: "step onto slab",
			src:  floor().fill(slab, 10, 64, -8, 10, 64, 16),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 0.3, dy: -0.08, step: 0.6, onGround: true,
			want: want{dx: 0.3, dy: 0.5, cy: true, ground: true},
		},
		{
			name: "slab without step height",
			src:  floor().fill(slab, 10, 64, -8, 10, 64, 16),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 0.3, dy: -0.08, onGround: true,
			want: want{dx: 10 - (9.5 + pw/2), cx: true, cy: true, ground: true},
		},
		{
			name: "no step while airborne",
			src:  floor().fill(slab, 10, 64, -8, 10, 64, 16),
			pos:  [3]float64{9.5, 64.2, 0.5}, dx: 0.3, dy: 0.1, step: 0.6,
			want: want{dx: 10 - (9.5 + pw/2), dy: 0.1, cx: true},
		},
		{
			name: "full block is too high to step",
			src:  floor().fill(stone, 10, 64, -8, 10, 64, 16),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 0.3, dy: -0.08, step: 0.6, onGround: true,
			want: want{dx: 10 - (9.5 + pw/2), cx: true, cy: true, ground: true},
		},
		{
			name: "jump under ceiling",
			src:  floor().fill(stone, -8, 66, -8, 16, 66, 16),
			pos:  [3]float64{0.5, 64, 0.5}, dy: 0.42,
			want: want{dy: 66 - (64 + ph), cy: true},
		},
		{
			name: "fence blocks walking",
			src:  floor().fill(fence, 10, 64, 0, 10, 64, 0),
			pos:  [3]float64{9.5, 64, 0.5}, dx: 1, dy: -0.08, step: 0.6, onGround: true,
			want: want{dx: 10.375 - (9.5 + pw/2), cx: true, cy: true, ground: true},
		},
		{
			name: "fence top is checked from the block above",
			src:  blocks{}.fill(fence, 10, 64, 0, 10, 64, 0),
			pos:  [3]float64{9.5, 65.2, 0.5}, dx: 1,
			want: want{dx: 10.375 - (9.5 + pw/2), cx: true},
		},
		{
			name: "land on fence",
			src:  blocks{}.fill(fence, 0, 64, 0, 0, 64, 0),
			pos:  [3]float64{0.5, 66, 0.5}, dy: -1,
			want: want{dy: -0.5, cy: true, ground: true},
		},
		{
			name: "non-finite movement",
			src:  floor(),
			pos:  [3]float64{0.5, 64, 0.5}, dx: math.NaN(), dy: -1,
			want: want{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := BoxAt(tt.pos[0], tt.pos[1], tt.pos[2], pw, ph)
			r := Move(tt.src, box, tt.dx, tt.dy, tt.dz, tt.step, tt.onGround)
			w := tt.want
			if !near(r.DX, w.dx) || !near(r.DY, w.dy) || !near(r.DZ, w.dz) {
				t.Errorf("movement = (%v, %v, %v), want (%v, %v, %v)", r.DX, r.DY, r.DZ, w.dx, w.dy, w.dz)
			}
			if r.CollidedX != w.cx || r.CollidedY != w.cy || r.CollidedZ != w.cz || r.OnGround != w.ground {
				t.Errorf("flags = x%v y%v z%v ground%v, want x%v y%v z%v ground%v",
					r.CollidedX, r.CollidedY, r.CollidedZ, r.OnGround, w.cx, w.cy, w.cz, w.ground)
			}
			if after := box.Offset(r.DX, r.DY, r.DZ); Colliding(tt.src, after) {
				t.Errorf("moved box %+v overlaps blocks", after)
			}
		})
	}
}

func TestMoveLandsExactly(t *testing.T) {
	box := BoxAt(0.5, 65.3, 0.5, pw, ph)
	r := Move(floor(), box, 0, -3, 0, 0, false)
	if got := 65.3 + r.DY; got != 64 {
		t.Fatalf("landed at y=%v, want exactly 64", got)
	}
}

func TestColliding(t *testing.T) {
	src := floor().fill(fence, 3, 64, 3, 3, 64, 3)
	tests := []struct {
		name string
		box  AABB
		want bool
	}{
		{"standing on floor", BoxAt(0.5, 64, 0.5, pw, ph), false},
		{"sunk into floor", BoxAt(0.5, 63.9, 0.5, pw, ph), true},
		{"in the air", BoxAt(0.5, 80, 0.5, pw, ph), false},
		{"inside fence post", BoxAt(3.5, 64.5, 3.5, 0.2, 0.2), true},
		{"above fence post top", BoxAt(3.5, 65.5, 3.5, 0.2, 0.2), false},
		{"in fence post upper half", BoxAt(3.5, 65.1, 3.5, 0.2, 0.2), true},
		{"beside fence post", BoxAt(3.1, 64.5, 3.5, 0.2, 0.2), false},
	}
	for _, tt := range tests {
		if got := Colliding(src, tt.box); got != tt.want {
			t.Errorf("%s: Colliding = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMoveNoAlloc(t *testing.T) {
	src := floor().fill(slab, 10, 64, -8, 10, 64, 16)
	box := BoxAt(9.5, 64, 0.5, pw, ph)
	if n := testing.AllocsPerRun(100, func() { Move(src, box, 0.3, -0.08, 0.1, 0.6, true) }); n != 0 {
		t.Fatalf("Move allocates %v times per call", n)
	}
}

func BenchmarkMove(b *testing.B) {
	src := floor().fill(slab, 10, 64, -8, 10, 64, 16)
	box := BoxAt(9.5, 64, 0.5, pw, ph)
	b.Run("walk", func(b *testing.B) {
		for b.Loop() {
			Move(src, box.Offset(-3, 0, 0), 0.2, -0.08, 0.1, 0.6, true)
		}
	})
	b.Run("step", func(b *testing.B) {
		for b.Loop() {
			Move(src, box, 0.3, -0.08, 0.1, 0.6, true)
		}
	})
	b.Run("fall", func(b *testing.B) {
		for b.Loop() {
			Move(src, box.Offset(0, 10, 0), 0, -3.9, 0, 0.6, false)
		}
	})
}
