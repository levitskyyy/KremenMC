package anvil

import (
	"bytes"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/generator"
)

func TestXXH32Vectors(t *testing.T) {
	tests := []struct {
		in   string
		seed uint32
		want uint32
	}{
		{"", 0, 0x02CC5D05},
		{"a", 0, 0x550D7456},
		{"abc", 0, 0x32D153FF},
		{"Nobody inspects the spammish repetition", 0, 0xE2293B2F},
	}
	for _, tt := range tests {
		if got := xxh32([]byte(tt.in), tt.seed); got != tt.want {
			t.Errorf("xxh32(%q) = %08x want %08x", tt.in, got, tt.want)
		}
	}
}

func TestLZ4RoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	random := make([]byte, 200_000)
	for i := range random {
		random[i] = byte(r.Uint32())
	}
	text := bytes.Repeat([]byte("minecraft:stone minecraft:dirt "), 9000)
	for _, in := range [][]byte{nil, []byte("x"), []byte("0123456789abcdef"), random, text, make([]byte, 100_000)} {
		enc := lz4Compress(nil, in)
		out, err := lz4Decompress(enc)
		if err != nil {
			t.Fatalf("len %d: %v", len(in), err)
		}
		if !bytes.Equal(out, in) {
			t.Fatalf("len %d: roundtrip mismatch", len(in))
		}
	}
	if enc := lz4Compress(nil, text); len(enc) > len(text)/10 {
		t.Errorf("poor compression: %d -> %d", len(text), len(enc))
	}
	enc := lz4Compress(nil, text)
	enc[30] ^= 0xFF
	if _, err := lz4Decompress(enc); err == nil {
		t.Error("corruption not detected")
	}
}

func TestRegionReadWrite(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenRegion(dir, -1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	payload := func(n int, seed uint64) []byte {
		rr := rand.New(rand.NewPCG(seed, 0))
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rr.IntN(4))
		}
		return b
	}
	tests := []struct {
		x, z int
		n    int
		typ  byte
	}{
		{0, 0, 100, CompressionZlib},
		{31, 31, 9000, CompressionGzip},
		{5, 7, 20000, CompressionNone},
		{6, 7, 50000, CompressionLZ4},
		{0, 0, 30000, CompressionNone},     // grow in place
		{0, 0, 10, CompressionZlib},        // shrink
		{9, 9, 2_000_000, CompressionNone}, // > 255 sectors -> external .mcc
	}
	want := map[[2]int][]byte{}
	for i, tt := range tests {
		p := payload(tt.n, uint64(i))
		if err := r.Write(tt.x, tt.z, p, tt.typ); err != nil {
			t.Fatal(err)
		}
		want[[2]int{tt.x, tt.z}] = p
	}
	if _, err := os.Stat(filepath.Join(dir, "c.-23.73.mcc")); err != nil {
		t.Fatalf("external chunk file: %v", err)
	}
	check := func(r *Region) {
		for k, p := range want {
			got, err := r.Read(k[0], k[1])
			if err != nil || !bytes.Equal(got, p) {
				t.Fatalf("chunk %v: err=%v equal=%v", k, err, bytes.Equal(got, p))
			}
		}
		if got, err := r.Read(1, 1); got != nil || err != nil {
			t.Fatal("absent chunk returned data")
		}
	}
	check(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = OpenRegion(dir, -1, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	check(r)
	// sectors must never overlap
	owner := map[int][2]int{}
	for i, loc := range r.locs {
		for s := int(loc >> 8); s < int(loc>>8)+int(loc&0xFF); s++ {
			if o, ok := owner[s]; ok {
				t.Fatalf("sector %d shared by %v and %d", s, o, i)
			}
			owner[s] = [2]int{i & 31, i >> 5}
		}
	}
	if err := r.Delete(9, 9); err != nil {
		t.Fatal(err)
	}
	if r.Has(9, 9) {
		t.Fatal("delete")
	}
	r.Close()
}

func TestChunkNBTRoundTrip(t *testing.T) {
	g := generator.NewTerrain(99)
	c := world.NewChunk(-3, 17, world.Overworld, 0)
	g.Generate(c)
	world.NewLightEngine(c.Dim, nil).LightChunk(c)
	c.BlockEntities = append(c.BlockEntities, world.BlockEntity{X: 3, Y: 70, Z: 9, NetType: -1, ID: "minecraft:chest", Data: nbt.Compound{"Lock": "x"}})
	c.Extra = nbt.Compound{"structures": nbt.Compound{"references": nbt.Compound{}}}

	dir := t.TempDir()
	for _, comp := range []byte{CompressionZlib, CompressionLZ4, CompressionGzip} {
		s := NewStorage(dir, world.Overworld)
		s.Compression = comp
		if err := s.SaveChunk(c); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s = NewStorage(dir, world.Overworld)
		got, err := s.LoadChunk(world.ChunkPos{X: -3, Z: 17})
		s.Close()
		if err != nil || got == nil {
			t.Fatalf("load: %v %v", got, err)
		}
		for y := -64; y < 320; y++ {
			for z := range 16 {
				for x := range 16 {
					if a, b := c.Block(x, y, z), got.Block(x, y, z); a != b {
						t.Fatalf("block %d,%d,%d: %s != %s", x, y, z, data.StateString(a), data.StateString(b))
					}
					if a, b := c.Biome(x, y, z), got.Biome(x, y, z); a != b {
						t.Fatalf("biome mismatch at %d,%d,%d", x, y, z)
					}
				}
			}
		}
		for i := range c.SkyLight {
			if !bytes.Equal(lightBytes(c.SkyLight[i]), lightBytes(got.SkyLight[i])) || !bytes.Equal(lightBytes(c.BlockLight[i]), lightBytes(got.BlockLight[i])) {
				t.Fatalf("light section %d differs", i)
			}
		}
		for i := range c.Sections {
			if c.Sections[i].NonAir() != got.Sections[i].NonAir() || c.Sections[i].Fluids() != got.Sections[i].Fluids() {
				t.Fatalf("section %d counts", i)
			}
		}
		for x := range 16 {
			if c.Height(world.MotionBlocking, x, 5) != got.Height(world.MotionBlocking, x, 5) {
				t.Fatal("heightmap")
			}
		}
		if len(got.BlockEntities) != 1 || got.BlockEntities[0].ID != "minecraft:chest" || got.BlockEntities[0].Data["Lock"] != "x" {
			t.Fatalf("block entities %+v", got.BlockEntities)
		}
		if _, ok := got.Extra["structures"]; !ok || !got.LightReady {
			t.Fatal("extra tags or light flag lost")
		}
	}
}

func lightBytes(n world.Nibbles) []byte {
	if n.IsZero() {
		return make([]byte, 2048)
	}
	if n.IsFull() {
		return bytes.Repeat([]byte{0xFF}, 2048)
	}
	return n.Bytes()
}

// Palette entry spellings across data versions.
func TestPaletteFormats(t *testing.T) {
	stairs := "minecraft:oak_stairs"
	want := data.MustState("oak_stairs[facing=east,half=top,shape=straight,waterlogged=false]")
	def := data.MustState("oak_stairs")
	tests := []struct {
		name  string
		entry any
		want  data.StateID
	}{
		{"pre-26.3", nbt.Compound{"Name": stairs, "Properties": nbt.Compound{"facing": "east", "half": "top"}}, want},
		{"26.3", nbt.Compound{"id": stairs, "properties": nbt.Compound{"facing": "east", "half": "top"}}, want},
		{"26.3 default elided", nbt.Compound{"id": stairs}, def},
		{"string", stairs, def},
		{"empty key", nbt.Compound{"": stairs}, def},
	}
	for _, tt := range tests {
		got, ok := paletteState(data.Canonical(), tt.entry)
		if !ok || got != tt.want {
			t.Errorf("%s: %s", tt.name, data.StateString(got))
		}
	}
	if _, ok := paletteState(data.Canonical(), "minecraft:not_a_block"); ok {
		t.Error("unknown block accepted")
	}
}

// A hand-built vanilla-shaped chunk (pre-26.3 keys, omitted sky arrays).
func TestDecodeVanillaShaped(t *testing.T) {
	stone := data.MustState("stone")
	idx := make([]uint32, 4096)
	idx[0] = 1
	sec0 := nbt.Compound{
		"Y": int8(-4),
		"block_states": nbt.Compound{
			"palette": nbt.List{Type: nbt.TagCompound, Items: []any{
				nbt.Compound{"Name": "minecraft:air"}, nbt.Compound{"Name": "minecraft:stone"},
			}},
			"data": packLongs(idx, 4),
		},
		"biomes":   nbt.Compound{"palette": nbt.List{Type: nbt.TagString, Items: []any{"minecraft:desert"}}},
		"SkyLight": bytes.Repeat([]byte{0x77}, 2048),
	}
	root := nbt.Compound{
		"DataVersion": int32(4786), "xPos": int32(1), "zPos": int32(2), "yPos": int32(-4),
		"Status": "minecraft:full", "isLightOn": int8(1),
		"sections":       nbt.List{Type: nbt.TagCompound, Items: []any{sec0}},
		"block_ticks":    nbt.List{Type: nbt.TagEnd},
		"PostProcessing": nbt.List{Type: nbt.TagEnd},
	}
	raw, err := nbt.Marshal(nil, "", root, false)
	if err != nil {
		t.Fatal(err)
	}
	_, back, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := DecodeChunk(back, world.Overworld)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	desert, _ := data.Canonical().BiomeByName("desert")
	if c.Block(0, -64, 0) != stone || c.Block(1, -64, 0) != data.Air || c.Biome(0, -64, 0) != desert {
		t.Fatal("decoded content")
	}
	// omitted sections inherit sky light: top is full, the gap below copies down
	if !c.SkyLight[len(c.SkyLight)-1].IsFull() || c.SkyLight[1].Get(5) != 7 {
		t.Fatal("sky light inheritance")
	}
	if _, ok := c.Extra["block_ticks"]; !ok {
		t.Fatal("unknown tags dropped")
	}
	proto := nbt.Compound{"xPos": int32(0), "zPos": int32(0), "Status": "minecraft:noise"}
	if c, _, err := DecodeChunk(proto, world.Overworld); c != nil || err != nil {
		t.Fatal("proto-chunk should decode as absent")
	}
}

func BenchmarkSaveLoadChunk(b *testing.B) {
	c := world.NewChunk(0, 0, world.Overworld, 0)
	generator.NewTerrain(1).Generate(c)
	world.NewLightEngine(c.Dim, nil).LightChunk(c)
	s := NewStorage(b.TempDir(), world.Overworld)
	defer s.Close()
	for b.Loop() {
		if err := s.SaveChunk(c); err != nil {
			b.Fatal(err)
		}
		if _, err := s.LoadChunk(world.ChunkPos{}); err != nil {
			b.Fatal(err)
		}
	}
}

type countGen struct {
	world.Generator
	n atomic.Int64
}

func (g *countGen) Generate(c *world.Chunk) { g.n.Add(1); g.Generator.Generate(c) }

func TestWorldPersistence(t *testing.T) {
	dir := t.TempDir()
	glass := data.MustState("glass")
	open := func(g world.Generator) (*world.World, *Storage) {
		s := NewStorage(dir, world.Overworld)
		w, err := world.New(world.Config{Generator: g, Storage: s, Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		return w, s
	}
	w, s := open(generator.NewTerrain(3))
	w.AddTicket(world.ChunkPos{}, 1, world.TicketForced)
	for w.Loaded() < 9 {
		w.DoTick()
	}
	w.SetBlock(5, 200, 5, glass)
	w.DoTick()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s.Close()

	g := &countGen{Generator: generator.NewTerrain(3)}
	w, s = open(g)
	defer s.Close()
	defer w.Close()
	w.AddTicket(world.ChunkPos{}, 1, world.TicketForced)
	for w.Loaded() < 9 {
		w.DoTick()
	}
	if w.Block(5, 200, 5) != glass {
		t.Fatal("block change not persisted")
	}
	if n := g.n.Load(); n != 0 {
		t.Fatalf("%d chunks regenerated", n)
	}
}
