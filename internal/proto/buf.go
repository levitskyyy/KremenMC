package proto

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"unicode/utf8"
)

var (
	ErrVarIntTooBig = errors.New("proto: varint too big")
	ErrShortBuffer  = errors.New("proto: short buffer")
	ErrStringTooBig = errors.New("proto: string too long")
)

type Writer struct {
	B []byte
}

func (w *Writer) Reset()          { w.B = w.B[:0] }
func (w *Writer) Len() int        { return len(w.B) }
func (w *Writer) Bytes() []byte   { return w.B }
func (w *Writer) Raw(p []byte)    { w.B = append(w.B, p...) }
func (w *Writer) U8(v uint8)      { w.B = append(w.B, v) }
func (w *Writer) I8(v int8)       { w.B = append(w.B, byte(v)) }
func (w *Writer) U16(v uint16)    { w.B = binary.BigEndian.AppendUint16(w.B, v) }
func (w *Writer) I16(v int16)     { w.U16(uint16(v)) }
func (w *Writer) I32(v int32)     { w.B = binary.BigEndian.AppendUint32(w.B, uint32(v)) }
func (w *Writer) I64(v int64)     { w.B = binary.BigEndian.AppendUint64(w.B, uint64(v)) }
func (w *Writer) U64(v uint64)    { w.B = binary.BigEndian.AppendUint64(w.B, v) }
func (w *Writer) F32(v float32)   { w.I32(int32(math.Float32bits(v))) }
func (w *Writer) F64(v float64)   { w.I64(int64(math.Float64bits(v))) }
func (w *Writer) VarInt(v int32)  { w.B = AppendVarInt(w.B, v) }
func (w *Writer) VarLong(v int64) { w.B = AppendVarLong(w.B, v) }

func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}

func (w *Writer) String(s string) {
	w.VarInt(int32(len(s)))
	w.B = append(w.B, s...)
}

func (w *Writer) ByteArray(p []byte) {
	w.VarInt(int32(len(p)))
	w.B = append(w.B, p...)
}

func (w *Writer) UUID(u [16]byte) { w.B = append(w.B, u[:]...) }

func (w *Writer) Position(x, y, z int32) {
	w.I64(int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF))
}

func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func AppendVarLong(b []byte, v int64) []byte {
	u := uint64(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func VarIntSize(v int32) int {
	u := uint32(v)
	n := 1
	for u >= 0x80 {
		u >>= 7
		n++
	}
	return n
}

type Reader struct {
	B   []byte
	Off int
}

func NewReader(b []byte) *Reader { return &Reader{B: b} }

func (r *Reader) Remaining() int { return len(r.B) - r.Off }

func (r *Reader) take(n int) ([]byte, error) {
	if n < 0 || r.Off+n > len(r.B) {
		return nil, ErrShortBuffer
	}
	p := r.B[r.Off : r.Off+n]
	r.Off += n
	return p, nil
}

func (r *Reader) Rest() []byte {
	p := r.B[r.Off:]
	r.Off = len(r.B)
	return p
}

func (r *Reader) Raw(n int) ([]byte, error) { return r.take(n) }

func (r *Reader) U8() (uint8, error) {
	if r.Off >= len(r.B) {
		return 0, ErrShortBuffer
	}
	v := r.B[r.Off]
	r.Off++
	return v, nil
}

func (r *Reader) I8() (int8, error) {
	v, err := r.U8()
	return int8(v), err
}

func (r *Reader) Bool() (bool, error) {
	v, err := r.U8()
	return v != 0, err
}

func (r *Reader) U16() (uint16, error) {
	p, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(p), nil
}

func (r *Reader) I16() (int16, error) {
	v, err := r.U16()
	return int16(v), err
}

func (r *Reader) I32() (int32, error) {
	p, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(p)), nil
}

func (r *Reader) I64() (int64, error) {
	p, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(p)), nil
}

func (r *Reader) F32() (float32, error) {
	v, err := r.I32()
	return math.Float32frombits(uint32(v)), err
}

func (r *Reader) F64() (float64, error) {
	v, err := r.I64()
	return math.Float64frombits(uint64(v)), err
}

func (r *Reader) VarInt() (int32, error) {
	var u uint32
	for i := 0; i < 5; i++ {
		b, err := r.U8()
		if err != nil {
			return 0, err
		}
		u |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, ErrVarIntTooBig
}

func (r *Reader) VarLong() (int64, error) {
	var u uint64
	for i := 0; i < 10; i++ {
		b, err := r.U8()
		if err != nil {
			return 0, err
		}
		u |= uint64(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int64(u), nil
		}
	}
	return 0, ErrVarIntTooBig
}

func (r *Reader) String(max int) (string, error) {
	n, err := r.VarInt()
	if err != nil {
		return "", err
	}
	if n < 0 || int(n) > max*3 {
		return "", ErrStringTooBig
	}
	p, err := r.take(int(n))
	if err != nil {
		return "", err
	}
	if utf8.RuneCount(p) > max {
		return "", ErrStringTooBig
	}
	return string(p), nil
}

func (r *Reader) ByteArray(max int) ([]byte, error) {
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n < 0 || int(n) > max {
		return nil, ErrShortBuffer
	}
	return r.take(int(n))
}

func (r *Reader) UUID() (u [16]byte, err error) {
	p, err := r.take(16)
	if err != nil {
		return u, err
	}
	copy(u[:], p)
	return u, nil
}

func (r *Reader) Position() (x, y, z int32, err error) {
	v, err := r.I64()
	if err != nil {
		return
	}
	x = int32(v >> 38)
	y = int32(v << 52 >> 52)
	z = int32(v << 26 >> 38)
	return
}

func ReadVarInt(r io.ByteReader) (int32, error) {
	var u uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		u |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, ErrVarIntTooBig
}
