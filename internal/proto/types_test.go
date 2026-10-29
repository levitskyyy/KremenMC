package proto

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

func TestLpVec3(t *testing.T) {
	tests := []struct {
		v    LpVec3
		want []byte
	}{
		{LpVec3{0, 0, 0}, []byte{0x00}},
		{LpVec3{1e-6, -1e-6, 0}, []byte{0x00}},
		{LpVec3{1, 0, -1}, []byte{0xF1, 0xFF, 0x00, 0x00, 0xFF, 0xFF}},
		{LpVec3{10, 0.2, -5}, []byte{0xF6, 0xFF, 0x40, 0x01, 0x05, 0x1F, 0x02}},
		{LpVec3{123457, 15.071, 0}, []byte{0xF5, 0xFF, 0x7F, 0xFF, 0x00, 0x07, 0x90, 0xF1, 0x01}},
	}
	for _, tt := range tests {
		var w Writer
		tt.v.Encode(&w)
		if !bytes.Equal(w.B, tt.want) {
			t.Errorf("Encode(%v) = % X, want % X", tt.v, w.B, tt.want)
		}
		var got LpVec3
		r := NewReader(w.B)
		if err := got.Decode(r); err != nil || r.Remaining() != 0 {
			t.Fatalf("Decode(% X): %v", w.B, err)
		}
		m := math.Max(math.Abs(tt.v.X), math.Max(math.Abs(tt.v.Y), math.Abs(tt.v.Z)))
		tol := math.Ceil(m)*2/32766 + 1e-9
		if math.Abs(got.X-tt.v.X) > tol || math.Abs(got.Y-tt.v.Y) > tol || math.Abs(got.Z-tt.v.Z) > tol {
			t.Errorf("Decode(Encode(%v)) = %v, tolerance %g", tt.v, got, tol)
		}
	}
}

func TestLpVec3NaN(t *testing.T) {
	var w Writer
	v := LpVec3{math.NaN(), math.Inf(1), 0}
	v.Encode(&w)
	var got LpVec3
	if err := got.Decode(NewReader(w.B)); err != nil {
		t.Fatal(err)
	}
	if got.X != 0 || got.Y < 1e10 {
		t.Errorf("got %v", got)
	}
}

func TestMoveDelta(t *testing.T) {
	tests := []struct {
		name string
		m    MoveDelta
		want []byte
	}{
		{"linear", MoveDelta{DX: 4096, DY: -1, DZ: 0, OnGround: true}, []byte{0x01, 0x10, 0x00, 0xFF, 0xFF, 0x00, 0x00}},
		{"linear air", MoveDelta{DX: 1}, []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}},
		{"stepped", MoveDelta{Steps: []MoveStep{{TickOffset: 1, DX: 2, DY: 3, DZ: 4}, {TickOffset: 2, DX: -1}}},
			[]byte{0x04, 0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04, 0x02, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00}},
	}
	for _, tt := range tests {
		var w Writer
		tt.m.Encode(&w)
		if !bytes.Equal(w.B, tt.want) {
			t.Errorf("%s: Encode = % X, want % X", tt.name, w.B, tt.want)
		}
		var got MoveDelta
		r := NewReader(w.B)
		if err := got.Decode(r); err != nil || r.Remaining() != 0 {
			t.Fatalf("%s: Decode: %v", tt.name, err)
		}
		if !reflect.DeepEqual(got, tt.m) {
			t.Errorf("%s: roundtrip = %+v, want %+v", tt.name, got, tt.m)
		}
	}
	var m MoveDelta
	if err := m.Decode(NewReader([]byte{0xC8, 0x01, 0x00})); !errors.Is(err, ErrCountTooBig) {
		t.Errorf("oversized step count err = %v", err)
	}
}

func TestNBTLen(t *testing.T) {
	c := nbt.Compound{
		"a": int8(1), "b": int16(2), "c": int32(3), "d": int64(4), "e": float32(5), "f": float64(6),
		"g": []byte{1, 2}, "h": "str", "i": nbt.List{Type: nbt.TagInt, Items: []any{int32(1), int32(2)}},
		"j": nbt.Compound{"k": []int32{1}, "l": []int64{2}}, "m": nbt.List{},
	}
	b, err := nbt.Marshal(nil, "", c, true)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   []byte
		want int
		err  bool
	}{
		{"compound", append(b, 0xAA, 0xBB), len(b), false},
		{"end", []byte{0x00, 0x01}, 1, false},
		{"string root", []byte{nbt.TagString, 0x00, 0x02, 'h', 'i', 0x09}, 5, false},
		{"truncated", b[:len(b)-1], 0, true},
		{"empty", nil, 0, true},
		{"bad tag", []byte{0x0D, 0x00}, 0, true},
		{"huge list", []byte{nbt.TagList, nbt.TagByte, 0x7F, 0xFF, 0xFF, 0xFF}, 0, true},
		{"negative array", []byte{nbt.TagIntArray, 0xFF, 0xFF, 0xFF, 0xFF}, 0, true},
	}
	for _, tt := range tests {
		n, err := NBTLen(tt.in)
		if (err != nil) != tt.err || (!tt.err && n != tt.want) {
			t.Errorf("%s: NBTLen = %d, %v; want %d, err=%v", tt.name, n, err, tt.want, tt.err)
		}
	}
	var raw RawNBT
	r := NewReader(append(b, 0x01))
	if err := raw.Decode(r); err != nil || !bytes.Equal(raw, b) || r.Remaining() != 1 {
		t.Fatalf("RawNBT.Decode: %v", err)
	}
	got, err := raw.Compound()
	if err != nil || !reflect.DeepEqual(got["h"], "str") {
		t.Fatalf("Compound() = %v, %v", got, err)
	}
	deep := bytes.Repeat([]byte{nbt.TagList, 0, 0, 0, 1}, 600)
	if _, err := NBTLen(append([]byte{nbt.TagCompound, nbt.TagList, 0, 0}, deep...)); err == nil {
		t.Error("deep nesting accepted")
	}
}

func TestOptHolder(t *testing.T) {
	o := Some(int32(5))
	if !o.Ok || o.V != 5 {
		t.Fatal(o)
	}
	if v := Resize([]int{1, 2, 3}, 2); len(v) != 2 || cap(v) != 3 {
		t.Fatal(v)
	}
	if v := Resize([]int{1}, 4); len(v) != 4 {
		t.Fatal(v)
	}
}
