package entity

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
)

type MoveVerdict uint8

const (
	MoveOK MoveVerdict = iota
	MoveTooFast
	MoveIntoBlock
	MoveInvalid
)

func (v MoveVerdict) String() string {
	switch v {
	case MoveOK:
		return "ok"
	case MoveTooFast:
		return "too fast"
	case MoveIntoBlock:
		return "into block"
	case MoveInvalid:
		return "invalid"
	}
	return "unknown"
}

const (
	maxHorizontal = 3e7
	maxVertical   = 2e7
	// moveDeflate shrinks player boxes so resting on or touching a block is not a collision.
	moveDeflate = 1e-5
)

// CheckPlayerMove validates a client-reported position against e's last accepted
// position. maxDistSq is vanilla's 100 (300 while elytra flying); pass +Inf to
// allow teleport-sized moves. noClip skips the block check (spectators).
func CheckPlayerMove(src BlockSource, e *Entity, to api.Vec3, maxDistSq float64, noClip bool) MoveVerdict {
	if !finite(to.X) || !finite(to.Y) || !finite(to.Z) ||
		math.Abs(to.X) > maxHorizontal || math.Abs(to.Z) > maxHorizontal || math.Abs(to.Y) > maxVertical {
		return MoveInvalid
	}
	if to.DistanceSq(e.Pos)-e.Vel.Dot(e.Vel) > maxDistSq {
		return MoveTooFast
	}
	if noClip {
		return MoveOK
	}
	prev := e.Box().Grow(-moveDeflate, -moveDeflate, -moveDeflate)
	next := prev.Offset(to.X-e.Pos.X, to.Y-e.Pos.Y, to.Z-e.Pos.Z)
	if collidesNew(src, prev, next) {
		return MoveIntoBlock
	}
	return MoveOK
}

// ApplyPlayerMove accepts a validated move, trusting the client's on-ground flag,
// and returns the fall distance at the moment of landing, or 0. Prev becomes the
// previously accepted position.
func (e *Entity) ApplyPlayerMove(to api.Vec3, onGround, flying bool) (landedFall float64) {
	dy := to.Y - e.Pos.Y
	e.Prev = e.Pos
	e.Pos = to
	e.OnGround = onGround
	switch {
	case flying:
		e.FallDistance = 0
	case dy < 0:
		e.FallDistance -= dy
	}
	if onGround {
		landedFall = e.FallDistance
		e.FallDistance = 0
	}
	return landedFall
}
