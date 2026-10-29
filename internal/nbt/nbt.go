package nbt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	TagEnd byte = iota
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

const maxDepth = 512

var ErrDepth = errors.New("nbt: max depth exceeded")

type Compound map[string]any

type List struct {
	Type  byte
	Items []any
}

func tagOf(v any) (byte, error) {
	switch v.(type) {
	case int8, bool:
		return TagByte, nil
	case int16:
		return TagShort, nil
	case int32:
		return TagInt, nil
	case int64:
		return TagLong, nil
	case float32:
		return TagFloat, nil
	case float64:
		return TagDouble, nil
	case []byte:
		return TagByteArray, nil
	case string:
		return TagString, nil
	case List, []Compound:
		return TagList, nil
	case Compound:
		return TagCompound, nil
	case []int32:
		return TagIntArray, nil
	case []int64:
		return TagLongArray, nil
	}
	return 0, fmt.Errorf("nbt: unsupported type %T", v)
}

// Marshal writes a root compound. Network NBT (1.20.2+) omits the root name.
func Marshal(b []byte, name string, c Compound, network bool) ([]byte, error) {
	b = append(b, TagCompound)
	if !network {
		b = appendString(b, name)
	}
	return appendCompound(b, c)
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func appendCompound(b []byte, c Compound) ([]byte, error) {
	var err error
	for k, v := range c {
		t, e := tagOf(v)
		if e != nil {
			return b, e
		}
		b = append(b, t)
		b = appendString(b, k)
		if b, err = appendPayload(b, v); err != nil {
			return b, err
		}
	}
	return append(b, TagEnd), nil
}

func appendPayload(b []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case int8:
		return append(b, byte(x)), nil
	case bool:
		if x {
			return append(b, 1), nil
		}
		return append(b, 0), nil
	case int16:
		return binary.BigEndian.AppendUint16(b, uint16(x)), nil
	case int32:
		return binary.BigEndian.AppendUint32(b, uint32(x)), nil
	case int64:
		return binary.BigEndian.AppendUint64(b, uint64(x)), nil
	case float32:
		return binary.BigEndian.AppendUint32(b, math.Float32bits(x)), nil
	case float64:
		return binary.BigEndian.AppendUint64(b, math.Float64bits(x)), nil
	case []byte:
		b = binary.BigEndian.AppendUint32(b, uint32(len(x)))
		return append(b, x...), nil
	case string:
		return appendString(b, x), nil
	case Compound:
		return appendCompound(b, x)
	case []Compound:
		items := make([]any, len(x))
		for i := range x {
			items[i] = x[i]
		}
		return appendPayload(b, List{Type: TagCompound, Items: items})
	case List:
		t := x.Type
		if len(x.Items) == 0 {
			t = TagEnd
		}
		b = append(b, t)
		b = binary.BigEndian.AppendUint32(b, uint32(len(x.Items)))
		var err error
		for _, it := range x.Items {
			if b, err = appendPayload(b, it); err != nil {
				return b, err
			}
		}
		return b, nil
	case []int32:
		b = binary.BigEndian.AppendUint32(b, uint32(len(x)))
		for _, n := range x {
			b = binary.BigEndian.AppendUint32(b, uint32(n))
		}
		return b, nil
	case []int64:
		b = binary.BigEndian.AppendUint32(b, uint32(len(x)))
		for _, n := range x {
			b = binary.BigEndian.AppendUint64(b, uint64(n))
		}
		return b, nil
	}
	return b, fmt.Errorf("nbt: unsupported type %T", v)
}

type decoder struct {
	b   []byte
	off int
}

var errShort = errors.New("nbt: unexpected end")

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || d.off+n > len(d.b) {
		return nil, errShort
	}
	p := d.b[d.off : d.off+n]
	d.off += n
	return p, nil
}

func (d *decoder) u8() (byte, error) {
	p, err := d.take(1)
	if err != nil {
		return 0, err
	}
	return p[0], nil
}

func (d *decoder) u16() (uint16, error) {
	p, err := d.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(p), nil
}

func (d *decoder) u32() (uint32, error) {
	p, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(p), nil
}

func (d *decoder) u64() (uint64, error) {
	p, err := d.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(p), nil
}

func (d *decoder) str() (string, error) {
	n, err := d.u16()
	if err != nil {
		return "", err
	}
	p, err := d.take(int(n))
	return string(p), err
}

func (d *decoder) arrLen(elem int) (int, error) {
	n, err := d.u32()
	if err != nil {
		return 0, err
	}
	if int64(n)*int64(elem) > int64(len(d.b)-d.off) {
		return 0, errShort
	}
	return int(n), nil
}

// Unmarshal reads a root compound and returns it with the number of bytes consumed.
func Unmarshal(b []byte, network bool) (name string, c Compound, n int, err error) {
	d := &decoder{b: b}
	t, err := d.u8()
	if err != nil {
		return
	}
	if t != TagCompound {
		if t == TagEnd && network {
			return "", nil, d.off, nil
		}
		return "", nil, 0, fmt.Errorf("nbt: root tag %d is not compound", t)
	}
	if !network {
		if name, err = d.str(); err != nil {
			return
		}
	}
	v, err := d.payload(TagCompound, 0)
	if err != nil {
		return
	}
	return name, v.(Compound), d.off, nil
}

func (d *decoder) payload(t byte, depth int) (any, error) {
	if depth > maxDepth {
		return nil, ErrDepth
	}
	switch t {
	case TagByte:
		v, err := d.u8()
		return int8(v), err
	case TagShort:
		v, err := d.u16()
		return int16(v), err
	case TagInt:
		v, err := d.u32()
		return int32(v), err
	case TagLong:
		v, err := d.u64()
		return int64(v), err
	case TagFloat:
		v, err := d.u32()
		return math.Float32frombits(v), err
	case TagDouble:
		v, err := d.u64()
		return math.Float64frombits(v), err
	case TagByteArray:
		n, err := d.arrLen(1)
		if err != nil {
			return nil, err
		}
		p, err := d.take(n)
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), p...), nil
	case TagString:
		return d.str()
	case TagList:
		et, err := d.u8()
		if err != nil {
			return nil, err
		}
		n, err := d.arrLen(1)
		if err != nil {
			return nil, err
		}
		l := List{Type: et, Items: make([]any, 0, n)}
		for i := 0; i < n; i++ {
			v, err := d.payload(et, depth+1)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, v)
		}
		return l, nil
	case TagCompound:
		c := Compound{}
		for {
			ct, err := d.u8()
			if err != nil {
				return nil, err
			}
			if ct == TagEnd {
				return c, nil
			}
			k, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.payload(ct, depth+1)
			if err != nil {
				return nil, err
			}
			c[k] = v
		}
	case TagIntArray:
		n, err := d.arrLen(4)
		if err != nil {
			return nil, err
		}
		a := make([]int32, n)
		for i := range a {
			v, _ := d.u32()
			a[i] = int32(v)
		}
		return a, nil
	case TagLongArray:
		n, err := d.arrLen(8)
		if err != nil {
			return nil, err
		}
		a := make([]int64, n)
		for i := range a {
			v, _ := d.u64()
			a[i] = int64(v)
		}
		return a, nil
	}
	return nil, fmt.Errorf("nbt: unknown tag %d", t)
}
