package entity

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Kind selects the per-tick motion rules of an entity. The order of
// acceleration, drag and position update differs per kind and was fitted
// tick by tick against the reference (testdata/traj.golden).
type Kind uint8

const (
	KindGeneric Kind = iota // gravity, move, drag (minecarts, boats until modelled)
	KindStatic              // no motion of its own (displays, markers, frames)
	KindLiving              // move, gravity, drag; ground friction × 0.91
	KindItem                // gravity, move, drag; sleeps on the ground
	KindBlock               // TNT and falling blocks
	KindThrown              // drag, gravity, position (snowball, egg, pearl, potions)
	KindArrow               // position, drag, gravity (arrows, tridents)
	KindOrb                 // experience orbs
)

const (
	airFriction    = 0.91
	groundFriction = 0.6
	minVelocity    = 0.003
	// minMoveSq is the smallest collided movement that changes position.
	minMoveSq = 1e-7
)

var slippery = [...]struct {
	name string
	f    float64
	id   data.BlockID
}{{"ice", 0.98, 0}, {"packed_ice", 0.98, 0}, {"frosted_ice", 0.98, 0}, {"blue_ice", 0.989, 0}, {"slime_block", 0.8, 0}}

var slimeID data.BlockID = math.MaxUint16

func init() {
	for i := range slippery {
		if b, ok := data.Canonical().BlockByName(slippery[i].name); ok {
			slippery[i].id = b.ID
			if slippery[i].name == "slime_block" {
				slimeID = b.ID
			}
		} else {
			slippery[i].f = groundFriction
		}
	}
}

const buoyancy = float64(float32(5e-4))

// f32 rounds through float32, as vanilla stores per-tick factors as floats.
func f32(v float64) float64 { return float64(float32(v)) }

// BlockFriction returns the friction of the block state s.
func BlockFriction(s data.StateID) float64 {
	id := data.Canonical().BlockID(s)
	for i := range slippery {
		if slippery[i].id == id {
			return f32(slippery[i].f)
		}
	}
	return f32(groundFriction)
}

// friction returns the friction of the block that affects movement: 0.5
// below the feet, in the column of the block the entity stands on.
func (e *Entity) friction(src BlockSource) float64 {
	x, z := int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Z))
	if sx, sz, ok := e.supporting(src); ok {
		x, z = sx, sz
		e.supX, e.supZ, e.supOK = sx, sz, true
	} else if e.supOK {
		x, z = e.supX, e.supZ
	}
	return BlockFriction(src.Block(x, int(math.Floor(e.Pos.Y-0.500001)), z))
}

// supporting finds the block column under the feet whose collision shape
// touches the box bottom, nearest to the entity's centre.
func (e *Entity) supporting(src BlockSource) (int, int, bool) {
	b := e.Box()
	probe := AABB{b.MinX, b.MinY - 1e-6, b.MinZ, b.MaxX, b.MinY, b.MaxZ}
	x0, y0, z0, x1, y1, z1 := cells(probe)
	best, bx, bz, found := math.MaxFloat64, 0, 0, false
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				shapes := data.Collision(src.Block(x, y, z))
				for i := range shapes {
					if !shapeBox(&shapes[i], x, y, z).Intersects(probe) {
						continue
					}
					cx, cy, cz := float64(x)+0.5-e.Pos.X, float64(y)+0.5-e.Pos.Y, float64(z)+0.5-e.Pos.Z
					if d := cx*cx + cy*cy + cz*cz; d < best {
						best, bx, bz, found = d, x, z, true
					}
					break
				}
			}
		}
	}
	return bx, bz, found
}

func (e *Entity) below(src BlockSource) data.StateID {
	return src.Block(int(math.Floor(e.Pos.X)), int(math.Floor(e.Pos.Y-0.2)), int(math.Floor(e.Pos.Z)))
}

// KindFor derives the motion kind of a type from its reference class.
func KindFor(t *data.EntityType) Kind {
	if t == nil {
		return KindGeneric
	}
	if HasAttributes(t.Name) {
		return KindLiving
	}
	p, ok := PhysicsOf(t.Name)
	if !ok {
		return KindGeneric
	}
	switch p.Class {
	case "ItemEntity":
		return KindItem
	case "PrimedTnt", "FallingBlockEntity":
		return KindBlock
	case "Snowball", "ThrownEgg", "ThrownEnderpearl", "ThrownSplashPotion", "ThrownLingeringPotion", "ThrownExperienceBottle":
		return KindThrown
	case "Arrow", "SpectralArrow", "ThrownTrident":
		return KindArrow
	case "ExperienceOrb":
		return KindOrb
	}
	if p.Gravity == 0 {
		return KindStatic
	}
	return KindGeneric
}

// move applies collided movement (dx, dy, dz) and updates collision flags,
// zeroing velocity on blocked axes. It returns the collision result.
func (e *Entity) move(src BlockSource, dx, dy, dz float64) MoveResult {
	r := Move(src, e.Box(), dx, dy, dz, e.StepHeight, e.OnGround)
	if r.DX*r.DX+r.DY*r.DY+r.DZ*r.DZ > minMoveSq {
		e.Pos.X += r.DX
		e.Pos.Y += r.DY
		e.Pos.Z += r.DZ
	} else {
		r.DX, r.DY, r.DZ = 0, 0, 0
	}
	e.OnGround = r.OnGround
	if !e.OnGround {
		e.supOK = false
	}
	e.HorizontalCollision = r.CollidedX || r.CollidedZ
	e.VerticalCollision = r.CollidedY
	if r.CollidedX {
		e.Vel.X = 0
	}
	if r.CollidedZ {
		e.Vel.Z = 0
	}
	return r
}

// fall accumulates fall distance for a move and reports the distance at
// the moment of landing.
func (e *Entity) fall(dy float64) (landed float64) {
	switch {
	case e.InWater:
		e.FallDistance = 0
	case dy < 0:
		e.FallDistance -= dy
	case dy > 0:
		e.FallDistance = 0
	}
	if e.OnGround {
		landed = e.FallDistance
		e.FallDistance = 0
	}
	return landed
}

// Tick runs one tick of the entity's motion rules and returns the fall
// distance at the moment of landing, or 0. Projectile kinds move without
// hit detection here; use StepProjectile for those.
func (e *Entity) Tick(src BlockSource) (landedFall float64) {
	e.Prev = e.Pos
	e.Age++
	e.updateFluids(src)
	switch e.Kind {
	case KindStatic:
		return 0
	case KindLiving:
		return e.tickLiving(src)
	case KindItem:
		return e.tickItem(src)
	case KindBlock:
		return e.tickBlock(src)
	case KindOrb:
		return e.tickOrb(src)
	case KindThrown, KindArrow:
		e.StepProjectile(src, nil)
		return 0
	}
	return e.tickGeneric(src)
}

func (e *Entity) gravity() float64 {
	if e.NoGravity {
		return 0
	}
	return e.Gravity
}

// tickLiving is living-entity travel without input: move, then gravity and
// drag; horizontal drag is 0.91 × the friction of the block left this tick.
func (e *Entity) tickLiving(src BlockSource) float64 {
	if math.Abs(e.Vel.X) < minVelocity {
		e.Vel.X = 0
	}
	if math.Abs(e.Vel.Y) < minVelocity {
		e.Vel.Y = 0
	}
	if math.Abs(e.Vel.Z) < minVelocity {
		e.Vel.Z = 0
	}
	g := e.gravity()
	if e.SlowFalling && e.Vel.Y <= 0 {
		g = min(g, 0.01)
	}
	e.applyInput(src)
	switch {
	case e.InWater:
		dy := e.Vel.Y
		r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
		e.Vel.X *= f32(0.8)
		e.Vel.Y *= f32(0.8)
		e.Vel.Z *= f32(0.8)
		if r.CollidedY {
			e.Vel.Y = 0
		}
		e.Vel.Y -= g / 16
		e.climbOut(src, dy)
		return e.fall(r.DY)
	case e.InLava:
		dy := e.Vel.Y
		r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
		if e.LavaHeight <= e.fluidJump() {
			e.Vel.X *= 0.5
			e.Vel.Y *= f32(0.8)
			e.Vel.Z *= 0.5
		} else {
			e.Vel.X *= 0.5
			e.Vel.Y *= 0.5
			e.Vel.Z *= 0.5
		}
		if r.CollidedY {
			e.Vel.Y = 0
		}
		e.Vel.Y -= g / 4
		e.climbOut(src, dy)
		return e.fall(r.DY)
	}
	h := f32(airFriction)
	if e.OnGround {
		h = f32(float64(float32(e.friction(src)) * float32(airFriction)))
	}
	vy := e.Vel.Y
	r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
	if r.CollidedY {
		e.Vel.Y = 0
		if r.OnGround && e.belowIsSlime(src) && vy < 0 {
			e.Vel.Y = -vy
		}
	}
	e.Vel.Y -= g
	e.Vel.Y *= f32(e.Drag)
	e.Vel.X *= h
	e.Vel.Z *= h
	if e.Flutter && !e.OnGround && e.Vel.Y < 0 {
		e.Vel.Y *= 0.6
	}
	return e.fall(r.DY)
}

func (e *Entity) fluidJump() float64 {
	if e.Height < 0.4 {
		return 0
	}
	return 0.4
}

// climbOut gives the vertical boost used to leave a fluid over a ledge.
func (e *Entity) climbOut(src BlockSource, prevDY float64) {
	if !e.HorizontalCollision {
		return
	}
	b := e.Box().Offset(e.Vel.X, e.Vel.Y+0.6-e.Pos.Y+e.Prev.Y, e.Vel.Z)
	if !Colliding(src, b) && !e.boxHasFluid(src, b) {
		e.Vel.Y = 0.3
	}
	_ = prevDY
}

func (e *Entity) belowIsSlime(src BlockSource) bool {
	return data.Canonical().BlockID(e.below(src)) == slimeID
}

// tickItem: gravity (or buoyancy in fluids), move unless resting, then drag.
func (e *Entity) tickItem(src BlockSource) float64 {
	switch {
	case e.InWater:
		e.Vel.X *= f32(0.99)
		e.Vel.Z *= f32(0.99)
		if e.Vel.Y < 0.06 {
			e.Vel.Y += buoyancy
		}
	case e.InLava:
		e.Vel.X *= f32(0.95)
		e.Vel.Z *= f32(0.95)
		if e.Vel.Y < 0.06 {
			e.Vel.Y += buoyancy
		}
	default:
		e.Vel.Y -= e.gravity()
	}
	if e.OnGround && e.Vel.X*e.Vel.X+e.Vel.Z*e.Vel.Z <= 1e-5 && (e.Age+int64(e.ID))%4 != 0 {
		return 0
	}
	r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
	if r.CollidedY {
		e.Vel.Y = 0
	}
	h := f32(e.Drag)
	if e.OnGround {
		h = f32(float64(float32(e.friction(src)) * float32(e.Drag)))
	}
	e.Vel.X *= h
	e.Vel.Y *= f32(e.Drag)
	e.Vel.Z *= h
	if e.OnGround && e.Vel.Y < 0 {
		e.Vel.Y *= -0.5
	}
	return e.fall(r.DY)
}

// tickBlock is primed TNT and falling blocks: gravity, move, drag, and on
// the ground ×0.7 horizontally and a -0.5 vertical bounce.
func (e *Entity) tickBlock(src BlockSource) float64 {
	e.Vel.Y -= e.gravity()
	r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
	if r.CollidedY {
		e.Vel.Y = 0
	}
	d := f32(e.Drag)
	e.Vel.X *= d
	e.Vel.Y *= d
	e.Vel.Z *= d
	if e.OnGround {
		e.Vel.X *= 0.7
		e.Vel.Z *= 0.7
		e.Vel.Y *= -0.5
	}
	return e.fall(r.DY)
}

// tickOrb is an experience orb: gravity, move, drag; a landing orb keeps
// 40% of its downward speed as an upward bounce.
func (e *Entity) tickOrb(src BlockSource) float64 {
	if e.InWater {
		if e.Vel.Y < 0.06 {
			e.Vel.Y += buoyancy
		}
		e.Vel.X *= f32(0.99)
		e.Vel.Z *= f32(0.99)
	} else {
		e.Vel.Y -= e.gravity()
	}
	vy, was := e.Vel.Y, e.OnGround
	r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
	h := f32(e.Drag)
	if e.OnGround {
		h = f32(float64(float32(e.friction(src)) * float32(e.Drag)))
	}
	e.Vel.X *= h
	e.Vel.Z *= h
	if r.CollidedY {
		e.Vel.Y = 0
		if r.OnGround && !was {
			e.Vel.Y = -vy * 0.4
		}
	} else {
		e.Vel.Y *= f32(e.Drag)
	}
	return e.fall(r.DY)
}

func (e *Entity) tickGeneric(src BlockSource) float64 {
	e.Vel.Y -= e.gravity()
	r := e.move(src, e.Vel.X, e.Vel.Y, e.Vel.Z)
	if r.CollidedY {
		e.Vel.Y = 0
	}
	d := f32(e.Drag)
	h := d
	if e.OnGround {
		h = f32(float64(float32(e.friction(src)) * float32(d)))
	}
	e.Vel.X *= h
	e.Vel.Y *= d
	e.Vel.Z *= h
	return e.fall(r.DY)
}
