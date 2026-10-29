// Package wire is the binary encoding shared by the kremenmc WASM host and guest
// SDKs. Integers are varints (zigzag for signed), floats are little-endian
// IEEE 754, strings and byte slices are length-prefixed with a uvarint.
package wire

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/levitskyyy/KremenMC/api"
)

var ErrShort = errors.New("wire: truncated payload")

type Writer struct{ B []byte }

func (w *Writer) Reset()           { w.B = w.B[:0] }
func (w *Writer) U8(v uint8)       { w.B = append(w.B, v) }
func (w *Writer) Uvarint(v uint64) { w.B = binary.AppendUvarint(w.B, v) }
func (w *Writer) Varint(v int64)   { w.B = binary.AppendVarint(w.B, v) }
func (w *Writer) F32(v float32)    { w.B = binary.LittleEndian.AppendUint32(w.B, math.Float32bits(v)) }
func (w *Writer) F64(v float64)    { w.B = binary.LittleEndian.AppendUint64(w.B, math.Float64bits(v)) }
func (w *Writer) UUID(u api.UUID)  { w.B = append(w.B, u[:]...) }
func (w *Writer) Raw(b []byte)     { w.B = append(w.B, b...) }

func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}

func (w *Writer) String(s string) {
	w.Uvarint(uint64(len(s)))
	w.B = append(w.B, s...)
}

func (w *Writer) Bytes(b []byte) {
	w.Uvarint(uint64(len(b)))
	w.B = append(w.B, b...)
}

func (w *Writer) Strings(ss []string) {
	w.Uvarint(uint64(len(ss)))
	for _, s := range ss {
		w.String(s)
	}
}

func (w *Writer) Vec3(v api.Vec3) { w.F64(v.X); w.F64(v.Y); w.F64(v.Z) }

func (w *Writer) BlockPos(p api.BlockPos) {
	w.Varint(int64(p.X))
	w.Varint(int64(p.Y))
	w.Varint(int64(p.Z))
}

func (w *Writer) Location(l api.Location) {
	w.String(l.World)
	w.Vec3(l.Pos)
	w.F32(l.Yaw)
	w.F32(l.Pitch)
}

// Reader decodes sequentially. The first error is sticky: subsequent reads
// return zero values and Err reports it.
type Reader struct {
	B   []byte
	Err error
}

func NewReader(b []byte) *Reader { return &Reader{B: b} }

func (r *Reader) fail() { r.Err = ErrShort; r.B = nil }

func (r *Reader) Len() int { return len(r.B) }

func (r *Reader) U8() uint8 {
	if len(r.B) < 1 {
		r.fail()
		return 0
	}
	v := r.B[0]
	r.B = r.B[1:]
	return v
}

func (r *Reader) Bool() bool { return r.U8() != 0 }

func (r *Reader) Uvarint() uint64 {
	v, n := binary.Uvarint(r.B)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.B = r.B[n:]
	return v
}

func (r *Reader) Varint() int64 {
	v, n := binary.Varint(r.B)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.B = r.B[n:]
	return v
}

func (r *Reader) F32() float32 {
	if len(r.B) < 4 {
		r.fail()
		return 0
	}
	v := math.Float32frombits(binary.LittleEndian.Uint32(r.B))
	r.B = r.B[4:]
	return v
}

func (r *Reader) F64() float64 {
	if len(r.B) < 8 {
		r.fail()
		return 0
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(r.B))
	r.B = r.B[8:]
	return v
}

func (r *Reader) UUID() (u api.UUID) {
	if len(r.B) < 16 {
		r.fail()
		return u
	}
	copy(u[:], r.B)
	r.B = r.B[16:]
	return u
}

// BytesRef returns a slice aliasing the input buffer.
func (r *Reader) BytesRef() []byte {
	n := r.Uvarint()
	if r.Err != nil || uint64(len(r.B)) < n {
		r.fail()
		return nil
	}
	b := r.B[:n:n]
	r.B = r.B[n:]
	return b
}

func (r *Reader) Bytes() []byte { return append([]byte(nil), r.BytesRef()...) }

func (r *Reader) String() string { return string(r.BytesRef()) }

// Count reads a collection length and rejects values that cannot fit in the
// remaining input, so hostile lengths cannot force large allocations.
func (r *Reader) Count() int {
	n := r.Uvarint()
	if n > uint64(len(r.B)) {
		r.fail()
		return 0
	}
	return int(n)
}

func (r *Reader) Strings() []string {
	n := r.Count()
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		out = append(out, r.String())
	}
	return out
}

func (r *Reader) Vec3() api.Vec3 { return api.Vec3{X: r.F64(), Y: r.F64(), Z: r.F64()} }

func (r *Reader) BlockPos() api.BlockPos {
	return api.BlockPos{X: int(r.Varint()), Y: int(r.Varint()), Z: int(r.Varint())}
}

func (r *Reader) Location() api.Location {
	return api.Location{World: r.String(), Pos: r.Vec3(), Yaw: r.F32(), Pitch: r.F32()}
}

func binaryAppendUvarint(b []byte, v uint64) []byte { return binary.AppendUvarint(b, v) }
