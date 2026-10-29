package world

import (
	"bytes"
	"encoding/hex"
	"math/rand/v2"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

func TestStrategyBits(t *testing.T) {
	blocks := NewStrategy(4096, 4, 8, 35723)
	biomes := NewStrategy(64, 1, 3, 67)
	tests := []struct {
		s    *Strategy
		n    int
		want uint8
	}{
		{blocks, 1, 0}, {blocks, 2, 4}, {blocks, 16, 4}, {blocks, 17, 5}, {blocks, 32, 5},
		{blocks, 33, 6}, {blocks, 256, 8}, {blocks, 257, 16},
		{biomes, 1, 0}, {biomes, 2, 1}, {biomes, 3, 2}, {biomes, 4, 2}, {biomes, 5, 3},
		{biomes, 8, 3}, {biomes, 9, 7},
	}
	for _, tt := range tests {
		if got := tt.s.bitsFor(tt.n); got != tt.want {
			t.Errorf("size %d: bitsFor(%d) = %d want %d", tt.s.Size, tt.n, got, tt.want)
		}
	}
	if GlobalBits(32768) != 15 || GlobalBits(32769) != 16 || GlobalBits(65) != 7 || GlobalBits(64) != 6 {
		t.Error("GlobalBits")
	}
}

func TestContainerResize(t *testing.T) {
	tests := []struct {
		name     string
		s        *Strategy
		distinct int
		bits     []uint8 // expected width after inserting 1..len(bits) distinct values
	}{
		{"blocks", NewStrategy(4096, 4, 8, 35723), 300, nil},
		{"biomes", NewStrategy(64, 1, 3, 67), 20, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(1, 2))
			c := NewContainer(tt.s, 7)
			ref := make([]uint32, tt.s.Size)
			for i := range ref {
				ref[i] = 7
			}
			seen := map[uint32]bool{7: true}
			for step := 0; step < tt.s.Size*4; step++ {
				i := r.IntN(tt.s.Size)
				v := uint32(r.IntN(tt.distinct)) * 3
				old := c.Set(i, v)
				if old != ref[i] {
					t.Fatalf("step %d: Set returned %d want %d", step, old, ref[i])
				}
				ref[i] = v
				seen[v] = true
				if want := tt.s.bitsFor(len(c.palette)); c.palette != nil && c.bits != want {
					t.Fatalf("bits %d for palette %d (want %d)", c.bits, len(c.palette), want)
				}
				if c.palette == nil && c.bits != tt.s.GlobalBits {
					t.Fatalf("direct with %d bits", c.bits)
				}
			}
			for i, v := range ref {
				if got := c.Get(i); got != v {
					t.Fatalf("Get(%d) = %d want %d", i, got, v)
				}
			}
			cl := c.Clone()
			c.Compact()
			for i, v := range ref {
				if c.Get(i) != v || cl.Get(i) != v {
					t.Fatalf("after compact Get(%d)", i)
				}
			}
			var w proto.Writer
			c.Encode(&w)
			if w.Len() != c.NetSize() {
				t.Fatalf("NetSize %d, encoded %d", c.NetSize(), w.Len())
			}
			d := NewContainer(tt.s, 0)
			if err := d.Decode(proto.NewReader(w.Bytes())); err != nil {
				t.Fatal(err)
			}
			for i, v := range ref {
				if d.Get(i) != v {
					t.Fatalf("decoded Get(%d) = %d want %d", i, d.Get(i), v)
				}
			}
		})
	}
}

func TestContainerSingleAndFill(t *testing.T) {
	s := NewStrategy(4096, 4, 8, 35723)
	c := NewContainer(s, 0)
	if c.Set(5, 0) != 0 || c.bits != 0 {
		t.Fatal("setting same value must not grow")
	}
	c.Set(5, 9)
	if c.bits != 4 || c.Get(5) != 9 || c.Get(4) != 0 {
		t.Fatalf("bits=%d", c.bits)
	}
	c.Fill(3)
	if v, ok := c.Single(); !ok || v != 3 {
		t.Fatal("fill")
	}
	vals := make([]uint32, 4096)
	for i := range vals {
		vals[i] = uint32(i)
	}
	c.SetAll(vals)
	if c.palette != nil || c.bits != 16 {
		t.Fatalf("4096 distinct must be direct, got bits %d", c.bits)
	}
	for i := range vals {
		if c.Get(i) != uint32(i) {
			t.Fatal("direct get")
		}
	}
}

// Example from minecraft.wiki "Chunk format": single-valued air blocks,
// indirect 1-bit biomes with palette [0x27, 0x03] and one long.
func TestWikiSectionExample(t *testing.T) {
	want, _ := hex.DecodeString("0000" + "0000" + "00" + "00" + "01" + "02" + "2703" + "CCFFCCFFCCFFCCFF")
	sec := newSection(0)
	r := proto.NewReader(want[4:])
	if err := sec.Blocks.Decode(r); err != nil {
		t.Fatal(err)
	}
	if err := sec.Biomes.Decode(r); err != nil {
		t.Fatal(err)
	}
	if r.Remaining() != 0 {
		t.Fatalf("%d bytes left", r.Remaining())
	}
	if sec.Biomes.Get(0) != 0x03 || sec.Biomes.Get(8) != 0x27 || sec.Biomes.Get(63) != 0x03 {
		t.Fatalf("biome values %d %d", sec.Biomes.Get(0), sec.Biomes.Get(8))
	}
	var w proto.Writer
	w.I16(int16(sec.nonAir))
	w.I16(int16(sec.fluids))
	sec.Blocks.Encode(&w)
	sec.Biomes.Encode(&w)
	if !bytes.Equal(w.Bytes(), want) {
		t.Fatalf("got  %x\nwant %x", w.Bytes(), want)
	}
}

// One stone block at index 0 in an air section: indirect 4 bits, palette
// [air, stone], first long == 1, no data-array length prefix.
func TestSectionEncodeLayout(t *testing.T) {
	sec := newSection(5)
	sec.SetBlock(0, 0, 0, 1)
	var w proto.Writer
	w.I16(int16(sec.nonAir))
	w.I16(int16(sec.fluids))
	sec.Blocks.Encode(&w)
	sec.Biomes.Encode(&w)
	var want []byte
	want = append(want, 0x00, 0x01, 0x00, 0x00, 0x04, 0x02, 0x00, 0x01)
	want = append(want, 0, 0, 0, 0, 0, 0, 0, 1)
	want = append(want, make([]byte, 255*8)...)
	want = append(want, 0x00, 0x05)
	if !bytes.Equal(w.Bytes(), want) {
		t.Fatalf("layout mismatch: len %d want %d, head %x", w.Len(), len(want), w.Bytes()[:16])
	}
}

func TestPackIndices(t *testing.T) {
	for _, b := range []uint8{1, 2, 4, 5, 9, 12, 15, 16} {
		vals := make([]uint32, 4096)
		for i := range vals {
			vals[i] = uint32(i*7) & (1<<b - 1)
		}
		packed := PackIndices(nil, vals, b)
		if len(packed) != longsFor(4096, b) {
			t.Fatalf("bits %d: %d longs", b, len(packed))
		}
		out := make([]uint32, 4096)
		if !UnpackIndices(out, packed, b) {
			t.Fatal("unpack")
		}
		for i := range vals {
			if out[i] != vals[i] {
				t.Fatalf("bits %d idx %d", b, i)
			}
		}
	}
	// 5 bits: 12 entries per long, 4 bits padding at the top
	if longsFor(4096, 5) != 342 || longsFor(256, 9) != 37 {
		t.Fatal("longsFor")
	}
}

func BenchmarkContainerGet(b *testing.B) {
	c := NewContainer(BlockStrategy, 0)
	for i := range 4096 {
		c.Set(i, uint32(i%40))
	}
	var s uint32
	for i := 0; b.Loop(); i++ {
		s += c.Get(i & 4095)
	}
	_ = s
}

func BenchmarkContainerSet(b *testing.B) {
	c := NewContainer(BlockStrategy, 0)
	for i := 0; b.Loop(); i++ {
		c.Set(i&4095, uint32(i%40))
	}
}
