package entity

import (
	"crypto/rand"
	"math"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

var lastID atomic.Int32

// NextID allocates a process-wide unique entity ID, starting at 1.
func NextID() int32 { return lastID.Add(1) }

type Entity struct {
	ID   int32
	UUID [16]byte
	Type data.EntityID

	Pos  api.Vec3 // feet position
	Prev api.Vec3 // position at the start of the last tick or accepted move
	Vel  api.Vec3 // blocks per tick

	Yaw, Pitch, HeadYaw float32

	OnGround            bool
	HorizontalCollision bool
	VerticalCollision   bool
	FallDistance        float64

	Width, Height float64
	NoGravity     bool
	Kind          Kind
	Gravity       float64 // per-tick acceleration (living: the gravity attribute)
	Drag          float64 // air drag multiplier per tick
	WaterDrag     float64 // arrows: drag while in water
	StepHeight    float64
	SlowFalling   bool
	Flutter       bool  // chickens: falling speed ×0.6 per tick
	Age           int64 // ticks run by Tick

	// Updated at the start of each tick from the box.
	InWater, InLava         bool
	WaterHeight, LavaHeight float64

	InGround bool // stuck arrow

	supX, supZ int // last supporting block column while on ground
	supOK      bool
}

// New creates an entity of type t with physics defaults for its kind.
// A nil t yields a dimensionless entity with Type -1.
func New(t *data.EntityType) *Entity {
	e := &Entity{ID: NextID(), Type: -1, Gravity: 0.04, Drag: 0.98, Kind: KindGeneric}
	rand.Read(e.UUID[:])
	e.UUID[6] = e.UUID[6]&0x0f | 0x40
	e.UUID[8] = e.UUID[8]&0x3f | 0x80
	if t == nil {
		return e
	}
	e.Type = t.ID
	e.Width = float64(t.Width)
	e.Height = float64(t.Height)
	e.Kind = KindFor(t)
	if p, ok := PhysicsOf(t.Name); ok {
		e.Gravity, e.Drag = p.Gravity, p.Drag
	}
	if e.Kind == KindLiving {
		if a := NewAttrs(t.Name); a != nil {
			e.Gravity = a.Value(AttrGravity)
			e.StepHeight = a.Value(AttrStepHeight)
		}
	}
	switch t.Name {
	case "chicken":
		e.Flutter = true
	case "arrow", "spectral_arrow":
		e.WaterDrag = f32(0.6)
	case "trident":
		e.WaterDrag = f32(0.99)
	}
	return e
}

func (e *Entity) Box() AABB { return BoxAt(e.Pos.X, e.Pos.Y, e.Pos.Z, e.Width, e.Height) }

// AngleByte packs degrees into the 1/256-turn byte used by entity packets.
func AngleByte(deg float32) int8 {
	v := math.Floor(float64(deg) * 256 / 360)
	if !finite(v) {
		return 0
	}
	return int8(int64(math.Mod(v, 256)))
}

// WrapDegrees maps d into [-180, 180).
func WrapDegrees(d float32) float32 {
	w := math.Mod(float64(d), 360)
	if w >= 180 {
		w -= 360
	}
	if w < -180 {
		w += 360
	}
	return float32(w)
}
