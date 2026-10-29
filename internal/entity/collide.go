// Package entity implements entities, AABB block collision and generic physics.
package entity

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// BlockSource is satisfied by *world.World and *world.TickContext. Unloaded or
// out-of-world positions must report air.
type BlockSource interface {
	Block(x, y, z int) data.StateID
}

const eps = 1e-7

const (
	axisX = iota
	axisY
	axisZ
)

// cells returns the inclusive block range touched by box, extended one block
// down for shapes taller than a block (fences, walls).
func cells(box AABB) (x0, y0, z0, x1, y1, z1 int) {
	return int(math.Floor(box.MinX - eps)), int(math.Floor(box.MinY-eps)) - 1, int(math.Floor(box.MinZ - eps)),
		int(math.Floor(box.MaxX + eps)), int(math.Floor(box.MaxY + eps)), int(math.Floor(box.MaxZ + eps))
}

func shapeBox(s *data.Box, x, y, z int) AABB {
	fx, fy, fz := float64(x), float64(y), float64(z)
	return AABB{s.MinX + fx, s.MinY + fy, s.MinZ + fz, s.MaxX + fx, s.MaxY + fy, s.MaxZ + fz}
}

// Colliding reports whether any block collision shape intersects box.
func Colliding(src BlockSource, box AABB) bool {
	x0, y0, z0, x1, y1, z1 := cells(box)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				shapes := data.Collision(src.Block(x, y, z))
				for i := range shapes {
					if shapeBox(&shapes[i], x, y, z).Intersects(box) {
						return true
					}
				}
			}
		}
	}
	return false
}

// collidesNew reports whether next intersects a block shape that prev does not,
// comparing whole block shapes like vanilla isPlayerCollidingWithAnythingNew.
func collidesNew(src BlockSource, prev, next AABB) bool {
	x0, y0, z0, x1, y1, z1 := cells(next)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				shapes := data.Collision(src.Block(x, y, z))
				hitNext, hitPrev := false, false
				for i := range shapes {
					b := shapeBox(&shapes[i], x, y, z)
					hitNext = hitNext || b.Intersects(next)
					hitPrev = hitPrev || b.Intersects(prev)
				}
				if hitNext && !hitPrev {
					return true
				}
			}
		}
	}
	return false
}

// clipX limits movement d along X of box a against obstacle b (both rotated so
// the movement axis is X), like vanilla VoxelShape.collideX.
func clipX(b, a AABB, d float64) float64 {
	if b.MaxY <= a.MinY+eps || b.MinY >= a.MaxY-eps || b.MaxZ <= a.MinZ+eps || b.MinZ >= a.MaxZ-eps {
		return d
	}
	if d > 0 {
		if b.MinX >= a.MaxX-eps {
			d = min(d, b.MinX-a.MaxX)
		}
	} else if b.MaxX <= a.MinX+eps {
		d = max(d, b.MaxX-a.MinX)
	}
	return d
}

func collideAxis(src BlockSource, box AABB, ax int, d float64) float64 {
	if math.Abs(d) < eps {
		return 0
	}
	sweep := box
	switch ax {
	case axisX:
		sweep = box.Stretch(d, 0, 0)
	case axisY:
		sweep = box.Stretch(0, d, 0)
	case axisZ:
		sweep = box.Stretch(0, 0, d)
	}
	rbox := box.rot(ax)
	x0, y0, z0, x1, y1, z1 := cells(sweep)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				shapes := data.Collision(src.Block(x, y, z))
				for i := range shapes {
					d = clipX(shapeBox(&shapes[i], x, y, z).rot(ax), rbox, d)
					if math.Abs(d) < eps {
						return 0
					}
				}
			}
		}
	}
	return d
}

// collideBox resolves Y first, then the larger horizontal component first.
func collideBox(src BlockSource, box AABB, dx, dy, dz float64) (float64, float64, float64) {
	if dy != 0 {
		dy = collideAxis(src, box, axisY, dy)
		if dy != 0 {
			box = box.Offset(0, dy, 0)
		}
	}
	zFirst := math.Abs(dx) < math.Abs(dz)
	if zFirst && dz != 0 {
		dz = collideAxis(src, box, axisZ, dz)
		if dz != 0 {
			box = box.Offset(0, 0, dz)
		}
	}
	if dx != 0 {
		dx = collideAxis(src, box, axisX, dx)
		if !zFirst && dx != 0 {
			box = box.Offset(dx, 0, 0)
		}
	}
	if !zFirst && dz != 0 {
		dz = collideAxis(src, box, axisZ, dz)
	}
	return dx, dy, dz
}

type MoveResult struct {
	DX, DY, DZ                      float64
	CollidedX, CollidedY, CollidedZ bool
	OnGround                        bool // moved down and was stopped vertically
}

// Move sweeps box by (dx, dy, dz) against block collision shapes. stepHeight > 0
// enables vanilla step-up; wasOnGround is the on-ground flag before the move.
// Non-finite movement yields no movement.
func Move(src BlockSource, box AABB, dx, dy, dz, stepHeight float64, wasOnGround bool) MoveResult {
	if !finite(dx) || !finite(dy) || !finite(dz) {
		return MoveResult{}
	}
	rx, ry, rz := collideBox(src, box, dx, dy, dz)
	landed := ry != dy && dy < 0
	if stepHeight > 0 && (landed || wasOnGround) && (rx != dx || rz != dz) {
		sx, sy, sz := collideBox(src, box, dx, stepHeight, dz)
		_, up, _ := collideBox(src, box.Stretch(dx, 0, dz), 0, stepHeight, 0)
		if up < stepHeight {
			hx, _, hz := collideBox(src, box.Offset(0, up, 0), dx, 0, dz)
			if hx*hx+hz*hz > sx*sx+sz*sz {
				sx, sy, sz = hx, up, hz
			}
		}
		if sx*sx+sz*sz > rx*rx+rz*rz {
			_, down, _ := collideBox(src, box.Offset(sx, sy, sz), 0, dy-sy, 0)
			rx, ry, rz = sx, sy+down, sz
		}
	}
	return MoveResult{
		DX: rx, DY: ry, DZ: rz,
		CollidedX: math.Abs(rx-dx) > eps, CollidedY: ry != dy, CollidedZ: math.Abs(rz-dz) > eps,
		OnGround: ry != dy && dy < 0,
	}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
