package bridge

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
)

type UUID [16]byte

func (u UUID) IsZero() bool { return u == UUID{} }

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

func ParseUUID(s string) (UUID, error) {
	var u UUID
	var raw []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			raw = append(raw, s[i])
		}
	}
	if len(raw) != 32 {
		return u, fmt.Errorf("uuid %q: bad length", s)
	}
	if _, err := hex.Decode(u[:], raw); err != nil {
		return u, fmt.Errorf("uuid %q: %w", s, err)
	}
	return u, nil
}

type Frame struct {
	Kind byte
	ID   uint32
	Op   uint16
	Args []any
}

var errFrameTooLarge = errors.New("frame too large")

// AppendFrame encodes f including its length prefix.
func AppendFrame(dst []byte, f Frame) ([]byte, error) {
	start := len(dst)
	dst = append(dst, 0, 0, 0, 0, f.Kind)
	dst = binary.BigEndian.AppendUint32(dst, f.ID)
	dst = binary.BigEndian.AppendUint16(dst, f.Op)
	var err error
	for _, a := range f.Args {
		if dst, err = AppendValue(dst, a); err != nil {
			return dst[:start], err
		}
	}
	n := len(dst) - start - 4
	if n > MaxFrame {
		return dst[:start], errFrameTooLarge
	}
	binary.BigEndian.PutUint32(dst[start:], uint32(n))
	return dst, nil
}

// ReadFrame reads one frame. buf is reused for the payload when large enough;
// decoded strings and byte slices never alias it.
func ReadFrame(r io.Reader, buf []byte) (Frame, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, buf, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrame {
		return Frame{}, buf, errFrameTooLarge
	}
	if n < 7 {
		return Frame{}, buf, fmt.Errorf("frame: short header (%d bytes)", n)
	}
	if cap(buf) < int(n) {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, buf, fmt.Errorf("frame: %w", err)
	}
	f := Frame{
		Kind: buf[0],
		ID:   binary.BigEndian.Uint32(buf[1:5]),
		Op:   binary.BigEndian.Uint16(buf[5:7]),
	}
	d := decoder{b: buf[7:]}
	for len(d.b) > 0 {
		v, err := d.value(0)
		if err != nil {
			return Frame{}, buf, fmt.Errorf("frame op %d: %w", f.Op, err)
		}
		f.Args = append(f.Args, v)
	}
	return f, buf, nil
}

func AppendValue(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(dst, TagNil), nil
	case bool:
		if x {
			return append(dst, TagTrue), nil
		}
		return append(dst, TagFalse), nil
	case int:
		return appendInt(dst, int64(x)), nil
	case int8:
		return appendInt(dst, int64(x)), nil
	case int16:
		return appendInt(dst, int64(x)), nil
	case int32:
		return appendInt(dst, int64(x)), nil
	case int64:
		return appendInt(dst, x), nil
	case uint8:
		return appendInt(dst, int64(x)), nil
	case uint16:
		return appendInt(dst, int64(x)), nil
	case uint32:
		return appendInt(dst, int64(x)), nil
	case float32:
		return appendFloat(dst, float64(x)), nil
	case float64:
		return appendFloat(dst, x), nil
	case string:
		dst = append(dst, TagString)
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		return append(dst, x...), nil
	case []byte:
		dst = append(dst, TagBytes)
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		return append(dst, x...), nil
	case UUID:
		dst = append(dst, TagUUID)
		return append(dst, x[:]...), nil
	case []string:
		dst = append(dst, TagList)
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		for _, s := range x {
			dst = append(dst, TagString)
			dst = binary.AppendUvarint(dst, uint64(len(s)))
			dst = append(dst, s...)
		}
		return dst, nil
	case []any:
		dst = append(dst, TagList)
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		var err error
		for _, e := range x {
			if dst, err = AppendValue(dst, e); err != nil {
				return dst, err
			}
		}
		return dst, nil
	case map[string]any:
		dst = append(dst, TagMap)
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		var err error
		for k, e := range x {
			dst = binary.AppendUvarint(dst, uint64(len(k)))
			dst = append(dst, k...)
			if dst, err = AppendValue(dst, e); err != nil {
				return dst, err
			}
		}
		return dst, nil
	case Location:
		return AppendValue(dst, x.Map())
	}
	return dst, fmt.Errorf("unsupported wire type %T", v)
}

func appendInt(dst []byte, v int64) []byte {
	dst = append(dst, TagInt)
	return binary.AppendUvarint(dst, uint64(v<<1)^uint64(v>>63))
}

func appendFloat(dst []byte, v float64) []byte {
	dst = append(dst, TagFloat)
	return binary.BigEndian.AppendUint64(dst, math.Float64bits(v))
}

const maxDepth = 64

type decoder struct{ b []byte }

var errTruncated = errors.New("truncated value")

func (d *decoder) uvarint() (uint64, error) {
	v, n := binary.Uvarint(d.b)
	if n <= 0 {
		return 0, errTruncated
	}
	d.b = d.b[n:]
	return v, nil
}

func (d *decoder) bytes() ([]byte, error) {
	n, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(d.b)) {
		return nil, errTruncated
	}
	out := d.b[:n:n]
	d.b = d.b[n:]
	return out, nil
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("value nesting too deep")
	}
	if len(d.b) == 0 {
		return nil, errTruncated
	}
	tag := d.b[0]
	d.b = d.b[1:]
	switch tag {
	case TagNil:
		return nil, nil
	case TagFalse:
		return false, nil
	case TagTrue:
		return true, nil
	case TagInt:
		u, err := d.uvarint()
		if err != nil {
			return nil, err
		}
		return int64(u>>1) ^ -int64(u&1), nil
	case TagFloat:
		if len(d.b) < 8 {
			return nil, errTruncated
		}
		v := math.Float64frombits(binary.BigEndian.Uint64(d.b))
		d.b = d.b[8:]
		return v, nil
	case TagString:
		b, err := d.bytes()
		return string(b), err
	case TagBytes:
		b, err := d.bytes()
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), b...), nil
	case TagUUID:
		if len(d.b) < 16 {
			return nil, errTruncated
		}
		var u UUID
		copy(u[:], d.b)
		d.b = d.b[16:]
		return u, nil
	case TagList:
		n, err := d.uvarint()
		if err != nil {
			return nil, err
		}
		if n > uint64(len(d.b)) {
			return nil, errTruncated
		}
		out := make([]any, 0, n)
		for range n {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case TagMap:
		n, err := d.uvarint()
		if err != nil {
			return nil, err
		}
		if n > uint64(len(d.b)) {
			return nil, errTruncated
		}
		out := make(map[string]any, n)
		for range n {
			k, err := d.bytes()
			if err != nil {
				return nil, err
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out[string(k)] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown tag %d", tag)
}

// Accessors for decoded arguments; they return zero values on type mismatch.

func ArgString(args []any, i int) string {
	if i < len(args) {
		s, _ := args[i].(string)
		return s
	}
	return ""
}

func ArgInt(args []any, i int) int64 {
	if i < len(args) {
		switch v := args[i].(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		}
	}
	return 0
}

func ArgFloat(args []any, i int) float64 {
	if i < len(args) {
		switch v := args[i].(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		}
	}
	return 0
}

func ArgBool(args []any, i int) bool {
	if i < len(args) {
		b, _ := args[i].(bool)
		return b
	}
	return false
}

func ArgUUID(args []any, i int) (UUID, bool) {
	if i < len(args) {
		u, ok := args[i].(UUID)
		return u, ok
	}
	return UUID{}, false
}

func ArgMap(args []any, i int) map[string]any {
	if i < len(args) {
		m, _ := args[i].(map[string]any)
		return m
	}
	return nil
}

func ArgList(args []any, i int) []any {
	if i < len(args) {
		l, _ := args[i].([]any)
		return l
	}
	return nil
}

func ArgStrings(args []any, i int) []string {
	l := ArgList(args, i)
	out := make([]string, 0, len(l))
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Fields is a decoded map value with the same zero-on-mismatch accessors.
type Fields map[string]any

func (f Fields) one(k string) []any { return []any{f[k]} }

func (f Fields) Has(k string) bool          { return f[k] != nil }
func (f Fields) String(k string) string     { return ArgString(f.one(k), 0) }
func (f Fields) Int(k string) int64         { return ArgInt(f.one(k), 0) }
func (f Fields) Bool(k string) bool         { return ArgBool(f.one(k), 0) }
func (f Fields) Float(k string) float64     { return ArgFloat(f.one(k), 0) }
func (f Fields) List(k string) []any        { return ArgList(f.one(k), 0) }
func (f Fields) Map(k string) Fields        { return Fields(ArgMap(f.one(k), 0)) }
func (f Fields) UUID(k string) (UUID, bool) { return ArgUUID(f.one(k), 0) }
