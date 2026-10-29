package proto

import (
	"encoding/binary"
	"errors"
	"math"
)

var (
	ErrCountTooBig = errors.New("proto: element count too large")
	ErrBadLength   = errors.New("proto: bad length")
)

// Opt is a value preceded on the wire by a presence boolean.
type Opt[T any] struct {
	V  T
	Ok bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{V: v, Ok: true} }

// Holder is a registry reference: either a registry ID or an inline value.
type Holder[T any] struct {
	ID    int32
	Value *T
}

// IDSet is a registry holder set: either a tag name or a list of registry IDs.
type IDSet struct {
	Tag   string
	IDs   []int32
	IsTag bool
}

type BlockPos struct {
	X, Y, Z int32
}

func (w *Writer) U32(v uint32) { w.B = binary.BigEndian.AppendUint32(w.B, v) }

func (w *Writer) BlockPos(p BlockPos) { w.Position(p.X, p.Y, p.Z) }

// Grow ensures capacity for n more bytes.
func (w *Writer) Grow(n int) {
	if cap(w.B)-len(w.B) < n {
		nb := make([]byte, len(w.B), 2*cap(w.B)+n)
		copy(nb, w.B)
		w.B = nb
	}
}

func (r *Reader) U32() (uint32, error) {
	p, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(p), nil
}

func (r *Reader) U64() (uint64, error) {
	p, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(p), nil
}

func (r *Reader) BlockPos() (BlockPos, error) {
	x, y, z, err := r.Position()
	return BlockPos{x, y, z}, err
}

// Count reads a VarInt element count and rejects counts that cannot fit in the
// remaining input, assuming each element occupies at least minSize bytes.
func (r *Reader) Count(minSize int) (int, error) {
	n, err := r.VarInt()
	if err != nil {
		return 0, err
	}
	if minSize < 1 {
		minSize = 1
	}
	if n < 0 || int64(n)*int64(minSize) > int64(r.Remaining()) {
		return 0, ErrCountTooBig
	}
	return int(n), nil
}

// BytesCopy reads a VarInt-prefixed byte array into dst's storage.
func (r *Reader) BytesCopy(dst []byte, max int) ([]byte, error) {
	p, err := r.ByteArray(max)
	if err != nil {
		return nil, err
	}
	return append(dst[:0], p...), nil
}

// RestCopy copies all remaining bytes into dst's storage.
func (r *Reader) RestCopy(dst []byte) []byte {
	return append(dst[:0], r.Rest()...)
}

// Resize returns s with length n, reusing capacity when possible.
func Resize[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}

// LpVec3 is a low-precision velocity vector (usually 6 bytes on the wire).
type LpVec3 struct {
	X, Y, Z float64
}

const (
	lpMaxQuantized = 32766.0
	lpAbsMax       = 1.7179869183e10
	lpAbsMin       = 1.0 / lpMaxQuantized
)

func lpSanitize(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-lpAbsMax, math.Min(v, lpAbsMax))
}

func lpPack(v float64) uint64 { return uint64(math.Round((v*0.5 + 0.5) * lpMaxQuantized)) }

func lpUnpack(v uint64) float64 {
	return math.Min(float64(v&0x7FFF), lpMaxQuantized)*2.0/lpMaxQuantized - 1.0
}

func (v *LpVec3) Encode(w *Writer) {
	x, y, z := lpSanitize(v.X), lpSanitize(v.Y), lpSanitize(v.Z)
	m := math.Max(math.Abs(x), math.Max(math.Abs(y), math.Abs(z)))
	if m < lpAbsMin {
		w.U8(0)
		return
	}
	scale := uint64(math.Ceil(m))
	cont := scale&3 != scale
	markers := scale
	if cont {
		markers = scale&3 | 4
	}
	s := float64(scale)
	packed := markers | lpPack(x/s)<<3 | lpPack(y/s)<<18 | lpPack(z/s)<<33
	w.U8(byte(packed))
	w.U8(byte(packed >> 8))
	w.U32(uint32(packed >> 16))
	if cont {
		w.VarInt(int32(scale >> 2))
	}
}

func (v *LpVec3) Decode(r *Reader) error {
	a, err := r.U8()
	if err != nil {
		return err
	}
	if a == 0 {
		*v = LpVec3{}
		return nil
	}
	b, err := r.U8()
	if err != nil {
		return err
	}
	c, err := r.U32()
	if err != nil {
		return err
	}
	packed := uint64(c)<<16 | uint64(b)<<8 | uint64(a)
	scale := uint64(a & 3)
	if a&4 != 0 {
		s, err := r.VarInt()
		if err != nil {
			return err
		}
		scale |= uint64(uint32(s)) << 2
	}
	s := float64(scale)
	v.X = lpUnpack(packed>>3) * s
	v.Y = lpUnpack(packed>>18) * s
	v.Z = lpUnpack(packed>>33) * s
	return nil
}

// MoveStep is one step of a stepped relative entity movement.
type MoveStep struct {
	TickOffset int32
	DX, DY, DZ int16
}

// MoveDelta is the relative position payload of the entity move packets
// (protocol 777+): a properties VarInt (bit 0 on-ground, rest step count)
// followed by either a single delta or Steps.
type MoveDelta struct {
	DX, DY, DZ int16
	OnGround   bool
	Steps      []MoveStep
}

func (m *MoveDelta) Encode(w *Writer) {
	props := int32(len(m.Steps)) << 1
	if m.OnGround {
		props |= 1
	}
	w.VarInt(props)
	if len(m.Steps) == 0 {
		w.I16(m.DX)
		w.I16(m.DY)
		w.I16(m.DZ)
		return
	}
	for i := range m.Steps {
		s := &m.Steps[i]
		w.VarInt(s.TickOffset)
		w.I16(s.DX)
		w.I16(s.DY)
		w.I16(s.DZ)
	}
}

func (m *MoveDelta) Decode(r *Reader) error {
	props, err := r.VarInt()
	if err != nil {
		return err
	}
	m.OnGround = props&1 != 0
	n := int(uint32(props) >> 1)
	if n == 0 {
		m.Steps = m.Steps[:0]
		if m.DX, err = r.I16(); err != nil {
			return err
		}
		if m.DY, err = r.I16(); err != nil {
			return err
		}
		m.DZ, err = r.I16()
		return err
	}
	if n*7 > r.Remaining() {
		return ErrCountTooBig
	}
	m.DX, m.DY, m.DZ = 0, 0, 0
	m.Steps = Resize(m.Steps, n)
	for i := range m.Steps {
		s := &m.Steps[i]
		if s.TickOffset, err = r.VarInt(); err != nil {
			return err
		}
		if s.DX, err = r.I16(); err != nil {
			return err
		}
		if s.DY, err = r.I16(); err != nil {
			return err
		}
		if s.DZ, err = r.I16(); err != nil {
			return err
		}
	}
	return nil
}
