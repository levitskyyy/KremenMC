package entity

import "math"

// MoveInput is what a mob's controls ask of its body for one tick.
// Forward and Strafe are in input units (the move control sets Forward to
// the speed, so ground acceleration is speed², see applyInput); Speed is
// the acceleration factor; Jump requests a jump of JumpPower (or, in a
// fluid, a 0.04 upward push); Fly makes the mob ignore gravity and follow
// FlyY as its vertical input.
type MoveInput struct {
	Forward, Strafe float64
	Speed           float64
	Jump            bool
	JumpPower       float64
	Fly             bool
	FlyY            float64
}

// applyInput adds the input's acceleration to the velocity, rotated by
// the body yaw: on ground the speed is scaled by 0.216/friction³ (block
// friction 0.6 gives exactly the speed), in air and fluids by 0.02.
// Fitted against reference chase/wander traces (docs/vanilla-parity.md).
func (e *Entity) applyInput(src BlockSource) {
	in := &e.Input
	if in.Jump {
		switch {
		case e.InWater || e.InLava:
			e.Vel.Y += 0.04
		case e.OnGround:
			e.Vel.Y = in.JumpPower
		}
	}
	if in.Fly {
		e.Vel.Y += in.FlyY
	}
	if in.Forward == 0 && in.Strafe == 0 {
		return
	}
	var amount float64
	switch {
	case e.InWater || e.InLava:
		amount = 0.02
	case e.OnGround:
		f := float32(e.friction(src))
		amount = in.Speed * float64(float32(0.21600002)/(f*f*f))
	case in.Fly:
		amount = in.Speed
	default:
		amount = 0.02
	}
	s, fw := in.Strafe, in.Forward
	l := s*s + fw*fw
	if l < 1e-7 {
		return
	}
	if l > 1 {
		l = math.Sqrt(l)
		s, fw = s/l, fw/l
	}
	s, fw = s*amount, fw*amount
	yaw := float64(e.Yaw) * math.Pi / 180
	sin, cos := math.Sin(yaw), math.Cos(yaw)
	e.Vel.X += s*cos - fw*sin
	e.Vel.Z += fw*cos + s*sin
}
