package entity

import (
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// syncInterval forces an absolute position sync so relative-move rounding
// never accumulates on clients.
const syncInterval = 400

// Tracked holds what viewers last received about one entity and turns state
// changes into shared packet bodies. It is owned by the tick goroutine.
type Tracked struct {
	ID    int32
	UUID  [16]byte
	Type  int32
	Data  int32 // Add Entity object data
	Range float64

	sent                         api.Vec3
	sentYaw, sentPitch, sentHead int8
	sinceSync                    int
	ready                        bool

	meta      []byte
	metaDirty bool

	// Vel is the velocity sent in Add Entity (projectiles).
	Vel api.Vec3

	mu      sync.Mutex
	pending [][]byte
	spawnX  func() [][]byte
}

// Push queues a packet body for existing viewers with the next Update. Safe
// for concurrent use.
func (t *Tracked) Push(b []byte) {
	t.mu.Lock()
	t.pending = append(t.pending, b)
	t.mu.Unlock()
}

// SetSpawnExtra sets a function producing extra bodies for new viewers
// (attributes, equipment) after Add Entity and metadata.
func (t *Tracked) SetSpawnExtra(f func() [][]byte) { t.spawnX = f }

// AddSpawnExtra appends f's bodies after the current spawn extras.
func (t *Tracked) AddSpawnExtra(f func() [][]byte) {
	prev := t.spawnX
	if prev == nil {
		t.spawnX = f
		return
	}
	t.spawnX = func() [][]byte { return append(prev(), f()...) }
}

// SetMeta replaces the metadata (entries followed by the 0xFF terminator).
func (t *Tracked) SetMeta(raw []byte) {
	t.meta = raw
	t.metaDirty = true
}

func quant(v float64) int64 { return int64(math.Round(v * 4096)) }

func fitsShort(d int64) bool { return d >= math.MinInt16 && d <= math.MaxInt16 }

func body(p packets.Packet) []byte {
	var w proto.Writer
	packets.Marshal(&w, p)
	return w.B
}

// Update records this tick's state and returns the packet bodies existing
// viewers need, or nil when nothing visible changed. The slices are fresh and
// may be shared between viewers.
func (t *Tracked) Update(pos api.Vec3, yaw, pitch, head float32, onGround bool) [][]byte {
	by, bp, bh := AngleByte(yaw), AngleByte(pitch), AngleByte(head)
	if !t.ready {
		t.sent, t.sentYaw, t.sentPitch, t.sentHead, t.ready = pos, by, bp, bh, true
		t.metaDirty = false
		t.mu.Lock()
		t.pending = nil
		t.mu.Unlock()
		return nil
	}
	var out [][]byte
	dx := quant(pos.X) - quant(t.sent.X)
	dy := quant(pos.Y) - quant(t.sent.Y)
	dz := quant(pos.Z) - quant(t.sent.Z)
	moved := dx != 0 || dy != 0 || dz != 0
	rotated := by != t.sentYaw || bp != t.sentPitch
	t.sinceSync++
	switch {
	case moved && fitsShort(dx) && fitsShort(dy) && fitsShort(dz) && t.sinceSync < syncInterval:
		d := proto.MoveDelta{DX: int16(dx), DY: int16(dy), DZ: int16(dz), OnGround: onGround}
		if rotated {
			out = append(out, body(&packets.ClientboundMoveEntityPosRot{EntityID: t.ID, Delta: d, Yaw: by, Pitch: bp}))
		} else {
			out = append(out, body(&packets.ClientboundMoveEntityPos{EntityID: t.ID, Delta: d}))
		}
		t.sent.X = float64(quant(t.sent.X)+dx) / 4096
		t.sent.Y = float64(quant(t.sent.Y)+dy) / 4096
		t.sent.Z = float64(quant(t.sent.Z)+dz) / 4096
	case moved || t.sinceSync >= syncInterval:
		out = append(out, body(&packets.ClientboundEntityPositionSync{
			EntityID: t.ID, Position: packets.Vec3f64{X: pos.X, Y: pos.Y, Z: pos.Z},
			Yaw: yaw, Pitch: pitch, OnGround: onGround,
		}))
		t.sent = pos
		t.sinceSync = 0
	case rotated:
		out = append(out, body(&packets.ClientboundMoveEntityRot{EntityID: t.ID, OnGround: onGround, Yaw: by, Pitch: bp}))
	}
	t.sentYaw, t.sentPitch = by, bp
	if bh != t.sentHead {
		out = append(out, body(&packets.ClientboundRotateHead{EntityID: t.ID, HeadYaw: bh}))
		t.sentHead = bh
	}
	if t.metaDirty {
		out = append(out, t.metaBody())
		t.metaDirty = false
	}
	t.mu.Lock()
	out = append(out, t.pending...)
	t.pending = nil
	t.mu.Unlock()
	return out
}

func (t *Tracked) metaBody() []byte {
	return body(&packets.ClientboundSetEntityData{EntityID: t.ID, Raw: t.meta})
}

// Spawn returns the bodies that make the entity appear for a new viewer in
// the state existing viewers have.
func (t *Tracked) Spawn() [][]byte {
	out := [][]byte{body(&packets.ClientboundAddEntity{
		EntityID: t.ID, ObjectUUID: t.UUID, Type: t.Type,
		X: t.sent.X, Y: t.sent.Y, Z: t.sent.Z,
		Pitch: t.sentPitch, Yaw: t.sentYaw, HeadPitch: t.sentHead, ObjectData: t.Data,
		Velocity: proto.LpVec3{X: t.Vel.X, Y: t.Vel.Y, Z: t.Vel.Z},
	})}
	if len(t.meta) > 1 {
		out = append(out, t.metaBody())
	}
	if t.spawnX != nil {
		out = append(out, t.spawnX()...)
	}
	return out
}

// Despawn encodes Remove Entities for ids.
func Despawn(ids []int32) []byte {
	return body(&packets.ClientboundRemoveEntities{EntityIDs: ids})
}

// Metadata serializer IDs and indices used by the engine (26.3).
const (
	MetaByte = 0
	MetaPose = 20

	IndexFlags     = 0
	IndexPose      = 6
	IndexSkinParts = 16

	FlagCrouching = 0x02
	FlagSprinting = 0x08

	PoseStanding  = 0
	PoseCrouching = 5
)

// AppendMetaByte appends one byte-typed metadata entry.
func AppendMetaByte(b []byte, index uint8, v byte) []byte {
	return append(b, index, MetaByte, v)
}

// AppendMetaPose appends a pose entry.
func AppendMetaPose(b []byte, pose int32) []byte {
	b = append(b, IndexPose, MetaPose)
	return proto.AppendVarInt(b, pose)
}

// EndMeta terminates a metadata list.
func EndMeta(b []byte) []byte { return append(b, 0xFF) }
