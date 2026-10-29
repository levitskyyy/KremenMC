package entity

import (
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestCheckPlayerMove(t *testing.T) {
	src := floor().
		fill(stone, 10, 64, -8, 10, 66, 16).
		fill(fence, 0, 64, 5, 0, 64, 5)
	tests := []struct {
		name      string
		from, to  api.Vec3
		vel       api.Vec3
		maxDistSq float64
		noClip    bool
		want      MoveVerdict
	}{
		{"walk", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.7, Y: 64, Z: 0.6}, api.Vec3{}, 100, false, MoveOK},
		{"stand still", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveOK},
		{"jump", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: 64.42, Z: 0.5}, api.Vec3{}, 100, false, MoveOK},
		{"touch wall face", api.Vec3{X: 9.5, Y: 64, Z: 0.5}, api.Vec3{X: 10 - pw/2, Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveOK},
		{"too fast", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 10.6, Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveTooFast},
		{"too fast diagonal", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 6.5, Y: 70, Z: 6.5}, api.Vec3{}, 100, false, MoveTooFast},
		{"fast with velocity", api.Vec3{X: 0.5, Y: 80, Z: 0.5}, api.Vec3{X: 0.5, Y: 70, Z: 11.5}, api.Vec3{Y: -10, Z: 10}, 100, false, MoveOK},
		{"elytra limit", api.Vec3{X: 0.5, Y: 80, Z: 0.5}, api.Vec3{X: 0.5, Y: 80, Z: 15.5}, api.Vec3{}, 300, false, MoveOK},
		{"teleport", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 1000, Y: 64, Z: 0.5}, api.Vec3{}, math.Inf(1), false, MoveOK},
		{"nan", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: math.NaN(), Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveInvalid},
		{"inf", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: math.Inf(-1), Z: 0.5}, api.Vec3{}, math.Inf(1), false, MoveInvalid},
		{"beyond horizontal limit", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: 64, Z: 3.1e7}, api.Vec3{}, math.Inf(1), false, MoveInvalid},
		{"beyond vertical limit", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: -2.1e7, Z: 0.5}, api.Vec3{}, math.Inf(1), false, MoveInvalid},
		{"into wall", api.Vec3{X: 9.5, Y: 64, Z: 0.5}, api.Vec3{X: 10, Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveIntoBlock},
		{"into floor", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, api.Vec3{X: 0.5, Y: 63.5, Z: 0.5}, api.Vec3{}, 100, false, MoveIntoBlock},
		{"into fence", api.Vec3{X: -0.5, Y: 64, Z: 5.5}, api.Vec3{X: 0.2, Y: 64, Z: 5.5}, api.Vec3{}, 100, false, MoveIntoBlock},
		{"spectator into wall", api.Vec3{X: 9.5, Y: 64, Z: 0.5}, api.Vec3{X: 10.5, Y: 64, Z: 0.5}, api.Vec3{}, 100, true, MoveOK},
		{"leave block already inside", api.Vec3{X: 10.5, Y: 64, Z: 0.5}, api.Vec3{X: 10.3, Y: 64, Z: 0.5}, api.Vec3{}, 100, false, MoveOK},
		{"stuck in wall then into floor", api.Vec3{X: 10.5, Y: 64, Z: 0.5}, api.Vec3{X: 10.5, Y: 63.5, Z: 0.5}, api.Vec3{}, 100, false, MoveIntoBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := living(tt.from)
			e.Vel = tt.vel
			if got := CheckPlayerMove(src, e, tt.to, tt.maxDistSq, tt.noClip); got != tt.want {
				t.Fatalf("CheckPlayerMove = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyPlayerMove(t *testing.T) {
	type step struct {
		y        float64
		onGround bool
		flying   bool
		fall     float64 // FallDistance after the move
		landed   float64
	}
	tests := []struct {
		name  string
		start float64
		steps []step
	}{
		{"walk on ground", 64, []step{{64, true, false, 0, 0}, {64, true, false, 0, 0}}},
		{"jump and land", 64, []step{{64.42, false, false, 0, 0}, {64.75, false, false, 0, 0}, {64.3, false, false, 0.45, 0}, {64, true, false, 0, 0.75}}},
		{"long fall", 80, []step{{78, false, false, 2, 0}, {73, false, false, 7, 0}, {64, true, false, 0, 16}}},
		{"rising keeps fall distance", 80, []step{{78, false, false, 2, 0}, {79, false, false, 2, 0}, {75, false, false, 6, 0}}},
		{"flying resets", 80, []step{{70, false, false, 10, 0}, {72, false, true, 0, 0}, {65, false, true, 0, 0}, {64, true, false, 0, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := living(api.Vec3{X: 0.5, Y: tt.start, Z: 0.5})
			for i, s := range tt.steps {
				prev := e.Pos
				to := api.Vec3{X: 0.5, Y: s.y, Z: 0.5}
				landed := e.ApplyPlayerMove(to, s.onGround, s.flying)
				if e.Pos != to || e.Prev != prev || e.OnGround != s.onGround {
					t.Fatalf("step %d: pos %+v prev %+v onGround %v", i, e.Pos, e.Prev, e.OnGround)
				}
				if !near(landed, s.landed) || !near(e.FallDistance, s.fall) {
					t.Fatalf("step %d: landed %v fall %v, want %v %v", i, landed, e.FallDistance, s.landed, s.fall)
				}
			}
		})
	}
}

func BenchmarkCheckPlayerMove(b *testing.B) {
	src := floor()
	e := living(api.Vec3{X: 0.5, Y: 64, Z: 0.5})
	to := api.Vec3{X: 0.7, Y: 64, Z: 0.6}
	for b.Loop() {
		CheckPlayerMove(src, e, to, 100, false)
	}
}
