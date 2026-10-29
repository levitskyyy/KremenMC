package entity

import (
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func living(pos api.Vec3) *Entity {
	return &Entity{Pos: pos, Width: pw, Height: ph, Gravity: 0.08, Drag: 0.98, StepHeight: 0.6, Kind: KindLiving}
}

func TestTickFallAndLand(t *testing.T) {
	tests := []struct {
		name  string
		start float64
	}{
		{"short drop", 65.25},
		{"long drop", 90},
		{"from rest on floor", 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := living(api.Vec3{X: 0.5, Y: tt.start, Z: 0.5})
			var landed float64
			for i := 0; i < 200 && !e.OnGround; i++ {
				landed = e.Tick(floor())
				if !e.OnGround && landed != 0 {
					t.Fatalf("tick %d: landedFall %v while airborne", i, landed)
				}
				if !e.OnGround && !near(e.FallDistance, tt.start-e.Pos.Y) {
					t.Fatalf("tick %d: FallDistance %v, fell %v", i, e.FallDistance, tt.start-e.Pos.Y)
				}
			}
			if !e.OnGround || e.Pos.Y != 64 {
				t.Fatalf("pos y=%v onGround=%v, want exactly 64 on ground", e.Pos.Y, e.OnGround)
			}
			if !near(landed, tt.start-64) || e.FallDistance != 0 || math.Abs(e.Vel.Y+0.0784) > 1e-6 {
				t.Fatalf("landed=%v fall=%v vy=%v, want landed %v and reset", landed, e.FallDistance, e.Vel.Y, tt.start-64)
			}
			if got := e.Tick(floor()); got != 0 || e.Pos.Y != 64 || !e.OnGround {
				t.Fatalf("resting tick: landed=%v y=%v onGround=%v", got, e.Pos.Y, e.OnGround)
			}
		})
	}
}

func TestTickVelocity(t *testing.T) {
	iceFloor := blocks{}.fill(ice, -8, 63, -8, 16, 63, 16)
	tests := []struct {
		name      string
		src       blocks
		pos, vel  api.Vec3
		noGravity bool
		onGround  bool
		wantPos   api.Vec3
		wantVel   api.Vec3
	}{
		{
			name: "ground friction",
			src:  floor(),
			pos:  api.Vec3{X: 0.5, Y: 64, Z: 0.5}, vel: api.Vec3{X: 0.5}, onGround: true,
			wantPos: api.Vec3{X: 1, Y: 64, Z: 0.5}, wantVel: api.Vec3{X: 0.5 * 0.6 * 0.91, Y: -0.08 * 0.98},
		},
		{
			name: "ice friction",
			src:  iceFloor,
			pos:  api.Vec3{X: 0.5, Y: 64, Z: 0.5}, vel: api.Vec3{Z: 0.5}, onGround: true,
			wantPos: api.Vec3{X: 0.5, Y: 64, Z: 1}, wantVel: api.Vec3{Z: 0.5 * 0.98 * 0.91, Y: -0.08 * 0.98},
		},
		{
			name: "air drag without gravity",
			src:  blocks{},
			pos:  api.Vec3{X: 0.5, Y: 80, Z: 0.5}, vel: api.Vec3{X: 1, Y: 1}, noGravity: true,
			wantPos: api.Vec3{X: 1.5, Y: 81, Z: 0.5}, wantVel: api.Vec3{X: 0.91, Y: 0.98},
		},
		{
			name:    "living moves before gravity",
			src:     blocks{},
			pos:     api.Vec3{X: 0.5, Y: 80, Z: 0.5},
			wantPos: api.Vec3{X: 0.5, Y: 80, Z: 0.5}, wantVel: api.Vec3{Y: -0.08 * 0.98},
		},
		{
			name: "jump into ceiling",
			src:  floor().fill(stone, -8, 66, -8, 16, 66, 16),
			pos:  api.Vec3{X: 0.5, Y: 64, Z: 0.5}, vel: api.Vec3{Y: 0.5},
			wantPos: api.Vec3{X: 0.5, Y: 66 - ph, Z: 0.5}, wantVel: api.Vec3{Y: -0.08 * 0.98},
		},
		{
			name: "wall zeroes horizontal velocity",
			src:  floor().fill(stone, 10, 64, -8, 10, 66, 16),
			pos:  api.Vec3{X: 9.5, Y: 64, Z: 0.5}, vel: api.Vec3{X: 1}, onGround: true,
			wantPos: api.Vec3{X: 10 - pw/2, Y: 64, Z: 0.5}, wantVel: api.Vec3{Y: -0.08 * 0.98},
		},
		{
			name: "tiny velocity snaps to zero",
			src:  floor(),
			pos:  api.Vec3{X: 0.5, Y: 64, Z: 0.5}, vel: api.Vec3{X: 0.002, Z: -0.004}, onGround: true,
			wantPos: api.Vec3{X: 0.5, Y: 64, Z: 0.496}, wantVel: api.Vec3{Y: -0.08 * 0.98, Z: -0.004 * 0.6 * 0.91},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := living(tt.pos)
			e.Vel, e.NoGravity, e.OnGround = tt.vel, tt.noGravity, tt.onGround
			e.Tick(tt.src)
			if e.Prev != tt.pos {
				t.Errorf("Prev = %+v, want %+v", e.Prev, tt.pos)
			}
			if !nearVec(e.Pos, tt.wantPos) {
				t.Errorf("Pos = %+v, want %+v", e.Pos, tt.wantPos)
			}
			if !nearVec(e.Vel, tt.wantVel) {
				t.Errorf("Vel = %+v, want %+v", e.Vel, tt.wantVel)
			}
		})
	}
}

func TestTickFallDistanceResetsGoingUp(t *testing.T) {
	e := living(api.Vec3{X: 0.5, Y: 80, Z: 0.5})
	e.FallDistance = 5
	e.Vel.Y = 1
	e.Tick(blocks{})
	if e.FallDistance != 0 {
		t.Fatalf("FallDistance = %v after moving up, want 0", e.FallDistance)
	}
}

func nearVec(a, b api.Vec3) bool {
	n := func(x, y float64) bool { return math.Abs(x-y) < 1e-6 }
	return n(a.X, b.X) && n(a.Y, b.Y) && n(a.Z, b.Z)
}

func TestTickNoAlloc(t *testing.T) {
	src := floor()
	e := living(api.Vec3{X: 0.5, Y: 64, Z: 0.5})
	if n := testing.AllocsPerRun(100, func() { e.Vel.X = 0.2; e.Tick(src) }); n != 0 {
		t.Fatalf("Tick allocates %v times per call", n)
	}
}

func BenchmarkTick(b *testing.B) {
	src := floor()
	b.Run("walking", func(b *testing.B) {
		e := living(api.Vec3{X: 0.5, Y: 64, Z: 0.5})
		for b.Loop() {
			e.Pos.X, e.Vel.X, e.Vel.Z = 0.5, 0.2, 0.1
			e.Tick(src)
		}
	})
	b.Run("falling", func(b *testing.B) {
		e := living(api.Vec3{X: 0.5, Y: 100, Z: 0.5})
		for b.Loop() {
			e.Pos.Y, e.Vel.Y, e.OnGround = 100, -1, false
			e.Tick(src)
		}
	})
}
