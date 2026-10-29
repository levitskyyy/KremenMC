package translate

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

func litChunk(t *testing.T) (*packets.ClientboundLevelChunkWithLight, [4]uint64) {
	t.Helper()
	plains, _ := data.Canonical().BiomeByName("plains")
	stone, _ := data.Canonical().ParseState("stone")
	torch, _ := data.Canonical().ParseState("glowstone")
	ch := world.NewChunk(0, 0, world.Overworld, plains)
	for x := range 16 {
		for z := range 16 {
			for y := -64; y < 60; y++ {
				ch.SetBlock(x, y, z, stone)
			}
		}
	}
	ch.SetBlock(8, 20, 8, torch)
	ch.Finish()
	world.NewLightEngine(ch.Dim, nil).LightChunk(ch)
	var w proto.Writer
	w.I32(0)
	w.I32(0)
	world.EncodeChunkData(&w, ch)
	var p packets.ClientboundLevelChunkWithLight
	if err := p.Decode(&proto.Reader{B: w.B}); err != nil {
		t.Fatal(err)
	}
	var masks [4]uint64
	for i, b := range [][]byte{p.SkyLightMask, p.BlockLightMask, p.EmptySkyLightMask, p.EmptyBlockLightMask} {
		for j, x := range b {
			masks[i] |= uint64(x) << (8 * j)
		}
	}
	if masks[0] == 0 || masks[1] == 0 || masks[2] == 0 {
		t.Fatalf("test chunk masks %x", masks)
	}
	return &p, masks
}

func TestLightMasksTranslated(t *testing.T) {
	ch, want := litChunk(t)
	for _, proto_ := range Default().Supported() {
		c := lookup(t, proto_)
		bodies := encode(t, c, packets.Play, ch)
		if len(bodies) != 1 {
			t.Fatalf("%d: %d frames", proto_, len(bodies))
		}
		r := proto.Reader{B: bodies[0]}
		r.VarInt()
		r.I32()
		r.I32()
		if proto_ <= 769 {
			l, err := proto.NBTLen(r.B[r.Off:])
			if err != nil {
				t.Fatal(err)
			}
			r.Off += l
		} else {
			n, _ := r.VarInt()
			for range n {
				r.VarInt()
				k, _ := r.VarInt()
				r.Raw(int(k) * 8)
			}
		}
		if _, err := r.ByteArray(1 << 21); err != nil {
			t.Fatal(err)
		}
		if n, _ := r.VarInt(); n != 0 {
			t.Fatalf("%d: %d block entities", proto_, n)
		}
		for i := range want {
			n, err := r.VarInt()
			if err != nil || n > 1 {
				t.Fatalf("%d: mask %d has %d longs (%v)", proto_, i, n, err)
			}
			var got uint64
			if n == 1 {
				v, _ := r.I64()
				got = uint64(v)
			}
			if got != want[i] {
				t.Fatalf("%d: mask %d = %x, want %x", proto_, i, got, want[i])
			}
		}
		for k, m := range []uint64{want[0], want[1]} {
			n, _ := r.VarInt()
			if want := popcount(m); int(n) != want {
				t.Fatalf("%d: light list %d has %d arrays, want %d", proto_, k, n, want)
			}
			for range n {
				if b, err := r.ByteArray(2048); err != nil || len(b) != 2048 {
					t.Fatalf("%d: light array: %v", proto_, err)
				}
			}
		}
		if r.Remaining() != 0 {
			t.Fatalf("%d: %d trailing bytes", proto_, r.Remaining())
		}
	}
}

func popcount(v uint64) int {
	n := 0
	for ; v != 0; v &= v - 1 {
		n++
	}
	return n
}
