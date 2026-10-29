package translate

import "github.com/levitskyyy/KremenMC/internal/proto"

// rd reads consecutive fields, keeping the first error.
type rd struct {
	r   *proto.Reader
	err error
}

func (d *rd) bool() bool {
	if d.err != nil {
		return false
	}
	var v bool
	v, d.err = d.r.Bool()
	return v
}

func (d *rd) f32() float32 {
	if d.err != nil {
		return 0
	}
	var v float32
	v, d.err = d.r.F32()
	return v
}

func (d *rd) f64() float64 {
	if d.err != nil {
		return 0
	}
	var v float64
	v, d.err = d.r.F64()
	return v
}

func (d *rd) varint() int32 {
	if d.err != nil {
		return 0
	}
	var v int32
	v, d.err = d.r.VarInt()
	return v
}

func (d *rd) i32() int32 {
	if d.err != nil {
		return 0
	}
	var v int32
	v, d.err = d.r.I32()
	return v
}
