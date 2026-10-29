package entity

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

type HitKind uint8

const (
	HitNone HitKind = iota
	HitBlock
	HitEntity
)

// Hit is a projectile collision found during a step.
type Hit struct {
	Kind   HitKind
	Pos    api.Vec3 // exact point on the segment
	Block  api.BlockPos
	Face   api.BlockFace
	Target any // HitEntity: the value returned by the entity callback
}

// EntityHitFunc finds the nearest entity whose (inflated) box the segment
// from→to crosses; it returns that entity and the crossing point.
type EntityHitFunc func(from, to api.Vec3) (target any, at api.Vec3, ok bool)

// Raycast clips the segment from→to against block collision shapes, visiting
// the blocks the segment passes through in order.
func Raycast(src BlockSource, from, to api.Vec3) (Hit, bool) {
	if from == to {
		return Hit{}, false
	}
	bx, by, bz := from.X, from.Y, from.Z
	x, y, z := int(math.Floor(bx)), int(math.Floor(by)), int(math.Floor(bz))
	if h, ok := clipBlock(src, x, y, z, from, to); ok {
		return h, true
	}
	dx, dy, dz := to.X-bx, to.Y-by, to.Z-bz
	sx, sy, sz := sign(dx), sign(dy), sign(dz)
	tdx, tdy, tdz := inv(sx, dx), inv(sy, dy), inv(sz, dz)
	tx := tdx * frac(sx, bx)
	ty := tdy * frac(sy, by)
	tz := tdz * frac(sz, bz)
	for tx <= 1 || ty <= 1 || tz <= 1 {
		switch {
		case tx < ty && tx < tz:
			x += sx
			tx += tdx
		case ty < tz:
			y += sy
			ty += tdy
		default:
			z += sz
			tz += tdz
		}
		if h, ok := clipBlock(src, x, y, z, from, to); ok {
			return h, true
		}
	}
	return Hit{}, false
}

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func inv(s int, d float64) float64 {
	if s == 0 {
		return math.MaxFloat64
	}
	return float64(s) / d
}

func frac(s int, v float64) float64 {
	f := v - math.Floor(v)
	if s > 0 {
		return 1 - f
	}
	return f
}

func clipBlock(src BlockSource, x, y, z int, from, to api.Vec3) (Hit, bool) {
	shapes := data.Collision(src.Block(x, y, z))
	best, found := 2.0, false
	var hit Hit
	for i := range shapes {
		b := shapeBox(&shapes[i], x, y, z)
		if t, face, ok := clipBox(b, from, to); ok && t < best {
			best, found = t, true
			hit = Hit{Kind: HitBlock, Block: api.BlockPos{X: x, Y: y, Z: z}, Face: face,
				Pos: api.Vec3{X: from.X + (to.X-from.X)*t, Y: from.Y + (to.Y-from.Y)*t, Z: from.Z + (to.Z-from.Z)*t}}
		}
	}
	return hit, found
}

// clipBox returns the entry parameter t∈[0,1] of segment from→to into b and
// the face entered.
func clipBox(b AABB, from, to api.Vec3) (float64, api.BlockFace, bool) {
	d := api.Vec3{X: to.X - from.X, Y: to.Y - from.Y, Z: to.Z - from.Z}
	t := 1.0
	face := api.BlockFace(255)
	try := func(dd, start, plane float64, o1, o2, d1, d2, lo1, hi1, lo2, hi2 float64, f api.BlockFace) {
		if dd*dd < 1e-7*1e-7 {
			return
		}
		s := (plane - start) / dd
		if s <= 0 || s >= t {
			return
		}
		p1, p2 := o1+s*d1, o2+s*d2
		if lo1-1e-7 < p1 && p1 < hi1+1e-7 && lo2-1e-7 < p2 && p2 < hi2+1e-7 {
			t, face = s, f
		}
	}
	if d.X > 0 {
		try(d.X, from.X, b.MinX, from.Y, from.Z, d.Y, d.Z, b.MinY, b.MaxY, b.MinZ, b.MaxZ, api.FaceWest)
	} else if d.X < 0 {
		try(d.X, from.X, b.MaxX, from.Y, from.Z, d.Y, d.Z, b.MinY, b.MaxY, b.MinZ, b.MaxZ, api.FaceEast)
	}
	if d.Y > 0 {
		try(d.Y, from.Y, b.MinY, from.X, from.Z, d.X, d.Z, b.MinX, b.MaxX, b.MinZ, b.MaxZ, api.FaceDown)
	} else if d.Y < 0 {
		try(d.Y, from.Y, b.MaxY, from.X, from.Z, d.X, d.Z, b.MinX, b.MaxX, b.MinZ, b.MaxZ, api.FaceUp)
	}
	if d.Z > 0 {
		try(d.Z, from.Z, b.MinZ, from.X, from.Y, d.X, d.Y, b.MinX, b.MaxX, b.MinY, b.MaxY, api.FaceNorth)
	} else if d.Z < 0 {
		try(d.Z, from.Z, b.MaxZ, from.X, from.Y, d.X, d.Y, b.MinX, b.MaxX, b.MinY, b.MaxY, api.FaceSouth)
	}
	if face == 255 {
		return 0, 0, false
	}
	return t, face, true
}

// ClipSegment returns the entry point of segment from→to into box b.
func ClipSegment(b AABB, from, to api.Vec3) (api.Vec3, bool) {
	if b.containsPoint(from) {
		return from, true
	}
	t, _, ok := clipBox(b, from, to)
	if !ok {
		return api.Vec3{}, false
	}
	return api.Vec3{X: from.X + (to.X-from.X)*t, Y: from.Y + (to.Y-from.Y)*t, Z: from.Z + (to.Z-from.Z)*t}, true
}

func (a AABB) containsPoint(p api.Vec3) bool {
	return p.X >= a.MinX && p.X < a.MaxX && p.Y >= a.MinY && p.Y < a.MaxY && p.Z >= a.MinZ && p.Z < a.MaxZ
}

// TickProjectile is Tick for projectile kinds: it refreshes fluid state
// and steps with entity hit detection.
func (e *Entity) TickProjectile(src BlockSource, hitEntity EntityHitFunc) Hit {
	e.Prev = e.Pos
	e.Age++
	e.updateFluids(src)
	return e.StepProjectile(src, hitEntity)
}

// StepProjectile advances a thrown item or arrow by one tick and returns
// what it hit. Thrown items apply drag and gravity before moving; arrows
// move first. A stuck arrow (InGround) does not move.
func (e *Entity) StepProjectile(src BlockSource, hitEntity EntityHitFunc) Hit {
	if e.InGround {
		return Hit{}
	}
	if e.Kind == KindThrown {
		d := f32(e.Drag)
		if e.InWater {
			d = f32(0.8)
		}
		e.Vel.Y -= e.gravity()
		e.Vel.X *= d
		e.Vel.Y *= d
		e.Vel.Z *= d
	}
	from := e.Pos
	to := api.Vec3{X: from.X + e.Vel.X, Y: from.Y + e.Vel.Y, Z: from.Z + e.Vel.Z}
	h, ok := Raycast(src, from, to)
	if ok {
		to = h.Pos
	}
	if hitEntity != nil {
		if t, at, hit := hitEntity(from, to); hit {
			h, ok = Hit{Kind: HitEntity, Pos: at, Target: t}, true
		}
	}
	if e.Kind == KindThrown {
		if ok {
			e.Pos = h.Pos
		} else {
			e.Pos = to
		}
		return h
	}
	if ok && h.Kind == HitBlock {
		const back = float64(float32(0.05))
		e.Pos = api.Vec3{X: h.Pos.X - float64(sign(e.Vel.X))*back, Y: h.Pos.Y - float64(sign(e.Vel.Y))*back, Z: h.Pos.Z - float64(sign(e.Vel.Z))*back}
		e.Vel = api.Vec3{}
		e.InGround = true
		return h
	}
	if ok {
		return h
	}
	e.Pos = to
	d := f32(e.Drag)
	if e.InWater {
		d = e.WaterDrag
	}
	e.Vel.X *= d
	e.Vel.Y *= d
	e.Vel.Z *= d
	e.Vel.Y -= e.gravity()
	return Hit{}
}
