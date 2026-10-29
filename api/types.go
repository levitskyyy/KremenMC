package api

import (
	"encoding/hex"
	"errors"
	"math"
	"strings"
)

// Key is a namespaced identifier such as "minecraft:stone".
type Key string

func NewKey(namespace, path string) Key { return Key(namespace + ":" + path) }

func (k Key) Namespace() string {
	if i := strings.IndexByte(string(k), ':'); i >= 0 {
		return string(k[:i])
	}
	return "minecraft"
}

func (k Key) Path() string {
	if i := strings.IndexByte(string(k), ':'); i >= 0 {
		return string(k[i+1:])
	}
	return string(k)
}

// Normalize returns the key with the implicit "minecraft" namespace made explicit.
func (k Key) Normalize() Key {
	if strings.IndexByte(string(k), ':') < 0 {
		return "minecraft:" + k
	}
	return k
}

type UUID [16]byte

var ErrBadUUID = errors.New("api: malformed uuid")

func ParseUUID(s string) (UUID, error) {
	var u UUID
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return u, ErrBadUUID
	}
	if _, err := hex.Decode(u[:], []byte(s)); err != nil {
		return u, ErrBadUUID
	}
	return u, nil
}

func (u UUID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}

func (u UUID) IsZero() bool { return u == UUID{} }

type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) Add(o Vec3) Vec3           { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3           { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Mul(f float64) Vec3        { return Vec3{v.X * f, v.Y * f, v.Z * f} }
func (v Vec3) Dot(o Vec3) float64        { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) Len() float64              { return math.Sqrt(v.Dot(v)) }
func (v Vec3) DistanceSq(o Vec3) float64 { d := v.Sub(o); return d.Dot(d) }

func (v Vec3) BlockPos() BlockPos {
	return BlockPos{int(math.Floor(v.X)), int(math.Floor(v.Y)), int(math.Floor(v.Z))}
}

type BlockPos struct{ X, Y, Z int }

func (p BlockPos) Add(dx, dy, dz int) BlockPos { return BlockPos{p.X + dx, p.Y + dy, p.Z + dz} }
func (p BlockPos) Center() Vec3 {
	return Vec3{float64(p.X) + 0.5, float64(p.Y) + 0.5, float64(p.Z) + 0.5}
}
func (p BlockPos) Chunk() ChunkPos { return ChunkPos{p.X >> 4, p.Z >> 4} }
func (p BlockPos) Relative(f BlockFace) BlockPos {
	d := f.Offset()
	return p.Add(d.X, d.Y, d.Z)
}

type ChunkPos struct{ X, Z int }

// Location is a position with rotation in a named world. It is a value type;
// resolve the world through Server.World.
type Location struct {
	World string
	Pos   Vec3
	Yaw   float32
	Pitch float32
}

func (l Location) BlockPos() BlockPos { return l.Pos.BlockPos() }

// Direction returns the unit look vector for the location's rotation.
func (l Location) Direction() Vec3 {
	yaw := float64(l.Yaw) * math.Pi / 180
	pitch := float64(l.Pitch) * math.Pi / 180
	xz := math.Cos(pitch)
	return Vec3{-xz * math.Sin(yaw), -math.Sin(pitch), xz * math.Cos(yaw)}
}

type BlockFace uint8

const (
	FaceDown BlockFace = iota
	FaceUp
	FaceNorth
	FaceSouth
	FaceWest
	FaceEast
	FaceSelf
)

func (f BlockFace) Offset() BlockPos {
	switch f {
	case FaceDown:
		return BlockPos{0, -1, 0}
	case FaceUp:
		return BlockPos{0, 1, 0}
	case FaceNorth:
		return BlockPos{0, 0, -1}
	case FaceSouth:
		return BlockPos{0, 0, 1}
	case FaceWest:
		return BlockPos{-1, 0, 0}
	case FaceEast:
		return BlockPos{1, 0, 0}
	}
	return BlockPos{}
}

type GameMode uint8

const (
	Survival GameMode = iota
	Creative
	Adventure
	Spectator
)

func (g GameMode) String() string {
	switch g {
	case Survival:
		return "survival"
	case Creative:
		return "creative"
	case Adventure:
		return "adventure"
	case Spectator:
		return "spectator"
	}
	return "unknown"
}

type Difficulty uint8

const (
	DifficultyPeaceful Difficulty = iota
	DifficultyEasy
	DifficultyNormal
	DifficultyHard
)

func (d Difficulty) String() string {
	switch d {
	case DifficultyPeaceful:
		return "peaceful"
	case DifficultyEasy:
		return "easy"
	case DifficultyNormal:
		return "normal"
	case DifficultyHard:
		return "hard"
	}
	return "unknown"
}

type Hand uint8

const (
	MainHand Hand = iota
	OffHand
)

// Ticks is a duration in server ticks (50ms at 20 TPS).
type Ticks int64
