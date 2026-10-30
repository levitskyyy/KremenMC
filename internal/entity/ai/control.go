package ai

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/entity"
)

// Body is the per-mob movement state the controls drive. The caller holds
// the mob's lock while calling control methods.
type Body struct {
	E          *entity.Entity
	Speed      float64 // movement_speed attribute
	JumpPower  float64 // jump_strength attribute
	MaxHeadYaw float32 // head turn limit relative to the body
	Flying     bool
	Swimming   bool // water-only movement (fish, squid)

	Look Look
	Move Move
	jump bool
	idle int // ticks the body has been still (body follows head after a delay)
}

// Look turns the head toward a point, at most YawStep/PitchStep degrees a
// tick; without a request the head returns toward the body.
type Look struct {
	X, Y, Z            float64
	YawStep, PitchStep float32
	active             bool
}

func (b *Body) LookAt(x, y, z float64, yawStep, pitchStep float32) {
	b.Look = Look{X: x, Y: y, Z: z, YawStep: yawStep, PitchStep: pitchStep, active: true}
}

// MoveMode selects what Move does this tick.
type MoveMode uint8

const (
	MoveIdle MoveMode = iota
	MoveTo
	MoveStrafe
	MoveJumpOnly
)

type Move struct {
	Mode            MoveMode
	X, Y, Z         float64
	SpeedMod        float64
	Forward, Strafe float64
}

// MoveTo asks the body to walk toward (x, y, z) at speedMod × Speed.
func (b *Body) MoveTo(x, y, z, speedMod float64) {
	b.Move = Move{Mode: MoveTo, X: x, Y: y, Z: z, SpeedMod: speedMod}
}

// Strafe walks relative to the body yaw (forward/strafe in −1..1).
func (b *Body) Strafe(forward, strafe, speedMod float64) {
	b.Move = Move{Mode: MoveStrafe, Forward: forward, Strafe: strafe, SpeedMod: speedMod}
}

func (b *Body) Stop()        { b.Move = Move{} }
func (b *Body) Jump()        { b.jump = true }
func (b *Body) Moving() bool { return b.Move.Mode == MoveTo }

// RotTowards moves angle a toward b by at most step degrees.
func RotTowards(a, b, step float32) float32 {
	d := entity.WrapDegrees(b - a)
	d = max(-step, min(step, d))
	return entity.WrapDegrees(a + d)
}

// YawTo is the yaw facing from (0,0) along (dx, dz).
func YawTo(dx, dz float64) float32 {
	return float32(math.Atan2(dz, dx)*180/math.Pi) - 90
}

// Tick applies the look and move requests to the entity's rotation and
// movement input, then clears the per-tick requests.
func (b *Body) Tick() {
	e := b.E
	in := entity.MoveInput{JumpPower: b.JumpPower}
	m := &b.Move
	switch m.Mode {
	case MoveTo:
		dx, dy, dz := m.X-e.Pos.X, m.Y-e.Pos.Y, m.Z-e.Pos.Z
		h2 := dx*dx + dz*dz
		switch {
		case b.Flying || b.Swimming:
			d := math.Sqrt(h2 + dy*dy)
			if d < 0.05 {
				m.Mode = MoveIdle
				break
			}
			e.Yaw = RotTowards(e.Yaw, YawTo(dx, dz), 90)
			sp := b.Speed * m.SpeedMod
			in.Forward, in.Speed, in.Fly = sp, sp, b.Flying
			if b.Swimming {
				in.Forward, in.Speed = 1, sp*0.1
				e.Vel.Y += dy / d * sp * 0.1
			} else {
				in.FlyY = dy / d * sp * 0.1
				in.Speed = sp * 0.1
				in.Forward = 1
			}
		case h2+dy*dy < 2.5e-7:
			m.Mode = MoveIdle
		default:
			e.Yaw = RotTowards(e.Yaw, YawTo(dx, dz), 90)
			sp := b.Speed * m.SpeedMod
			in.Forward, in.Speed = sp, sp
			w := max(1.0, e.Width)
			if dy > e.StepHeight && h2 < w*w || e.HorizontalCollision && e.OnGround && dy > -0.5 {
				in.Jump = true
			}
		}
	case MoveStrafe:
		sp := b.Speed * m.SpeedMod
		in.Forward, in.Strafe, in.Speed = m.Forward*sp, m.Strafe*sp, sp
		m.Mode = MoveIdle
	}
	if b.jump {
		in.Jump = true
		b.jump = false
	}
	if in.Forward != 0 || in.Strafe != 0 {
		b.idle = 0
	} else {
		b.idle++
	}
	e.Input = in

	head := e.HeadYaw
	if b.Look.active {
		dx, dz := b.Look.X-e.Pos.X, b.Look.Z-e.Pos.Z
		dy := b.Look.Y - (e.Pos.Y + e.Height*0.85)
		h := math.Sqrt(dx*dx + dz*dz)
		if h > 1e-5 || math.Abs(dy) > 1e-5 {
			head = RotTowards(head, YawTo(dx, dz), b.Look.YawStep)
			pitch := float32(-math.Atan2(dy, h) * 180 / math.Pi)
			e.Pitch = RotTowards(e.Pitch, pitch, b.Look.PitchStep)
		}
		b.Look.active = false
	} else {
		head = RotTowards(head, e.Yaw, 10)
		e.Pitch = RotTowards(e.Pitch, 0, 10)
	}
	lim := b.MaxHeadYaw
	if lim == 0 {
		lim = 75
	}
	if in.Forward != 0 || in.Strafe != 0 {
		// walking: the head stays within the limit of the body
		d := entity.WrapDegrees(head - e.Yaw)
		if d > lim {
			head = e.Yaw + lim
		} else if d < -lim {
			head = e.Yaw - lim
		}
	} else if b.idle > 10 {
		e.Yaw = RotTowards(e.Yaw, head, 10)
	} else if d := entity.WrapDegrees(head - e.Yaw); d > lim || d < -lim {
		e.Yaw = RotTowards(e.Yaw, head, absf(d)-lim)
	}
	e.HeadYaw = entity.WrapDegrees(head)
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
