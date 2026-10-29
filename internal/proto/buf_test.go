package proto

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestVarInt(t *testing.T) {
	tests := []struct {
		v    int32
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{2, []byte{0x02}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{255, []byte{0xff, 0x01}},
		{25565, []byte{0xdd, 0xc7, 0x01}},
		{2097151, []byte{0xff, 0xff, 0x7f}},
		{2097152, []byte{0x80, 0x80, 0x80, 0x01}},
		{math.MaxInt32, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{math.MinInt32, []byte{0x80, 0x80, 0x80, 0x80, 0x08}},
	}
	for _, tt := range tests {
		got := AppendVarInt(nil, tt.v)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("AppendVarInt(%d) = %x, want %x", tt.v, got, tt.want)
		}
		if n := VarIntSize(tt.v); n != len(tt.want) {
			t.Errorf("VarIntSize(%d) = %d, want %d", tt.v, n, len(tt.want))
		}
		r := NewReader(tt.want)
		v, err := r.VarInt()
		if err != nil || v != tt.v || r.Remaining() != 0 {
			t.Errorf("VarInt(%x) = %d, %v (rem %d), want %d", tt.want, v, err, r.Remaining(), tt.v)
		}
		v, err = ReadVarInt(bytes.NewReader(tt.want))
		if err != nil || v != tt.v {
			t.Errorf("ReadVarInt(%x) = %d, %v", tt.want, v, err)
		}
	}
}

func TestVarIntErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrShortBuffer},
		{"truncated", []byte{0x80}, ErrShortBuffer},
		{"truncated4", []byte{0xff, 0xff, 0xff, 0xff}, ErrShortBuffer},
		{"six bytes", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, ErrVarIntTooBig},
	}
	for _, tt := range tests {
		if _, err := NewReader(tt.in).VarInt(); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestVarLong(t *testing.T) {
	tests := []struct {
		v    int64
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{2147483647, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{math.MaxInt64, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
		{-2147483648, []byte{0x80, 0x80, 0x80, 0x80, 0xf8, 0xff, 0xff, 0xff, 0xff, 0x01}},
		{math.MinInt64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}},
	}
	for _, tt := range tests {
		got := AppendVarLong(nil, tt.v)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("AppendVarLong(%d) = %x, want %x", tt.v, got, tt.want)
		}
		v, err := NewReader(tt.want).VarLong()
		if err != nil || v != tt.v {
			t.Errorf("VarLong(%x) = %d, %v", tt.want, v, err)
		}
	}
	if _, err := NewReader(bytes.Repeat([]byte{0x80}, 11)).VarLong(); !errors.Is(err, ErrVarIntTooBig) {
		t.Errorf("11-byte varlong: err = %v", err)
	}
}

func TestPosition(t *testing.T) {
	tests := []struct {
		x, y, z int32
		raw     uint64
	}{
		{18357644, 831, -20882616, 0b0100011000000111011000110010110000010101101101001000001100111111},
		{0, 0, 0, 0},
		{-1, -1, -1, math.MaxUint64},
		{33554431, 2047, 33554431, 0},
		{-33554432, -2048, -33554432, 0},
		{1, -64, -1, 0},
	}
	for _, tt := range tests {
		var w Writer
		w.BlockPos(BlockPos{tt.x, tt.y, tt.z})
		if tt.raw != 0 {
			got := uint64(w.B[0])<<56 | uint64(w.B[1])<<48 | uint64(w.B[2])<<40 | uint64(w.B[3])<<32 |
				uint64(w.B[4])<<24 | uint64(w.B[5])<<16 | uint64(w.B[6])<<8 | uint64(w.B[7])
			if got != tt.raw {
				t.Errorf("Position(%d,%d,%d) = %064b, want %064b", tt.x, tt.y, tt.z, got, tt.raw)
			}
		}
		p, err := NewReader(w.B).BlockPos()
		if err != nil || p != (BlockPos{tt.x, tt.y, tt.z}) {
			t.Errorf("roundtrip (%d,%d,%d) = %+v, %v", tt.x, tt.y, tt.z, p, err)
		}
	}
}

func TestStringLimits(t *testing.T) {
	var w Writer
	w.String("héllo")
	if s, err := NewReader(w.B).String(5); err != nil || s != "héllo" {
		t.Fatalf("String = %q, %v", s, err)
	}
	if _, err := NewReader(w.B).String(4); !errors.Is(err, ErrStringTooBig) {
		t.Fatalf("String(4) err = %v", err)
	}
	if _, err := NewReader([]byte{0x05, 'a'}).String(10); !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("short string err = %v", err)
	}
}

func TestCount(t *testing.T) {
	r := NewReader([]byte{0x03, 1, 2, 3})
	if n, err := r.Count(1); err != nil || n != 3 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if _, err := NewReader([]byte{0x03, 1, 2, 3}).Count(2); !errors.Is(err, ErrCountTooBig) {
		t.Fatalf("Count(2) err = %v", err)
	}
	if _, err := NewReader([]byte{0xff, 0xff, 0xff, 0xff, 0x0f}).Count(1); !errors.Is(err, ErrCountTooBig) {
		t.Fatalf("negative count err = %v", err)
	}
}

func BenchmarkVarIntAppend(b *testing.B) {
	buf := make([]byte, 0, 64)
	for i := 0; i < b.N; i++ {
		buf = AppendVarInt(buf[:0], int32(i))
	}
}

func BenchmarkVarIntRead(b *testing.B) {
	data := AppendVarInt(nil, 2097151)
	r := Reader{}
	for i := 0; i < b.N; i++ {
		r.B, r.Off = data, 0
		if _, err := r.VarInt(); err != nil {
			b.Fatal(err)
		}
	}
}
