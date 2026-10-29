package world

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

var (
	stone  = data.MustState("stone")
	water  = data.MustState("water")
	leaves = data.MustState("oak_leaves[distance=1,persistent=true,waterlogged=false]")
	glass  = data.MustState("glass")
	torch  = data.MustState("torch")
	slabWL = data.MustState("oak_slab[type=bottom,waterlogged=true]")
)

func TestCountsAndHeightmaps(t *testing.T) {
	c := NewChunk(0, 0, Overworld, 0)
	type step struct {
		x, y, z int
		s       data.StateID
	}
	tests := []struct {
		name   string
		steps  []step
		col    [2]int
		want   map[Heightmap]int
		nonAir int
		fluids int
	}{
		{"empty", nil, [2]int{0, 0}, map[Heightmap]int{WorldSurface: -64, MotionBlocking: -64}, 0, 0},
		{"stone", []step{{0, 10, 0, stone}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: 11, MotionBlocking: 11, MotionBlockingNoLeaves: 11, OceanFloor: 11}, 1, 0},
		{"leaves above", []step{{0, 20, 0, leaves}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: 21, MotionBlocking: 21, MotionBlockingNoLeaves: 11, OceanFloor: 21}, 2, 0},
		{"water above", []step{{0, 30, 0, water}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: 31, MotionBlocking: 31, MotionBlockingNoLeaves: 31, OceanFloor: 21}, 3, 1},
		{"torch above", []step{{0, 40, 0, torch}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: 41, MotionBlocking: 31}, 4, 1},
		{"remove top", []step{{0, 40, 0, data.Air}, {0, 30, 0, data.Air}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: 21, MotionBlocking: 21, MotionBlockingNoLeaves: 11}, 2, 0},
		{"waterlogged", []step{{0, 25, 0, slabWL}}, [2]int{0, 0},
			map[Heightmap]int{MotionBlocking: 26}, 3, 1},
		{"other column", []step{{15, 319, 15, glass}}, [2]int{15, 15},
			map[Heightmap]int{WorldSurface: 320, MotionBlocking: 320}, 4, 1},
		{"remove all", []step{{0, 25, 0, data.Air}, {0, 20, 0, data.Air}, {0, 10, 0, data.Air}}, [2]int{0, 0},
			map[Heightmap]int{WorldSurface: -64, MotionBlocking: -64, OceanFloor: -64}, 1, 0},
	}
	for _, tt := range tests {
		for _, s := range tt.steps {
			c.SetBlock(s.x, s.y, s.z, s.s)
		}
		for h, want := range tt.want {
			if got := c.Height(h, tt.col[0], tt.col[1]); got != want {
				t.Errorf("%s: %v = %d want %d", tt.name, h, got, want)
			}
		}
		nonAir, fluids := 0, 0
		for i := range c.Sections {
			nonAir += c.Sections[i].NonAir()
			fluids += c.Sections[i].Fluids()
		}
		if nonAir != tt.nonAir || fluids != tt.fluids {
			t.Errorf("%s: nonAir=%d fluids=%d want %d/%d", tt.name, nonAir, fluids, tt.nonAir, tt.fluids)
		}
		// incremental maps must equal a full rebuild
		snap := c.heights
		c.RecalcHeightmaps()
		if snap != c.heights {
			t.Errorf("%s: incremental heightmaps differ from rebuild", tt.name)
		}
	}
}

// parsed form of the chunk data body, decoded independently of the encoder
type parsedChunk struct {
	heightmaps map[int32][]int64
	sections   []Section
	nonAir     []int16
	entities   int
	masks      [4][]int64
	sky, block [][]byte
}

func parseChunkBody(t *testing.T, b []byte, nSections int) parsedChunk {
	t.Helper()
	r := proto.NewReader(b)
	var p parsedChunk
	p.heightmaps = map[int32][]int64{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.VarInt()
	must(err)
	for range n {
		typ, err := r.VarInt()
		must(err)
		l, err := r.VarInt()
		must(err)
		longs := make([]int64, l)
		for i := range longs {
			longs[i], err = r.I64()
			must(err)
		}
		p.heightmaps[typ] = longs
	}
	size, err := r.VarInt()
	must(err)
	sr := proto.NewReader(b[r.Off : r.Off+int(size)])
	r.Off += int(size)
	for range nSections {
		s := newSection(0)
		na, err := sr.I16()
		must(err)
		_, err = sr.I16()
		must(err)
		must(s.Blocks.Decode(sr))
		must(s.Biomes.Decode(sr))
		p.sections = append(p.sections, s)
		p.nonAir = append(p.nonAir, na)
	}
	if sr.Remaining() != 0 {
		t.Fatalf("section data has %d trailing bytes", sr.Remaining())
	}
	ne, err := r.VarInt()
	must(err)
	p.entities = int(ne)
	for range ne {
		_, err = r.U8()
		must(err)
		_, err = r.I16()
		must(err)
		_, err = r.VarInt()
		must(err)
		_, _, used, err := nbt.Unmarshal(b[r.Off:], true)
		must(err)
		r.Off += used
	}
	for i := range p.masks {
		bs, err := r.ByteArray(1 << 12)
		must(err)
		for j, x := range bs {
			if j%8 == 0 {
				p.masks[i] = append(p.masks[i], 0)
			}
			p.masks[i][j/8] |= int64(x) << (uint(j%8) * 8)
		}
	}
	for _, dst := range []*[][]byte{&p.sky, &p.block} {
		l, err := r.VarInt()
		must(err)
		for range l {
			a, err := r.ByteArray(2048)
			must(err)
			if len(a) != 2048 {
				t.Fatalf("light array len %d", len(a))
			}
			*dst = append(*dst, a)
		}
	}
	if r.Remaining() != 0 {
		t.Fatalf("%d trailing bytes", r.Remaining())
	}
	return p
}

func TestEncodeChunkData(t *testing.T) {
	c := NewChunk(3, -2, Overworld, 1)
	for x := range 16 {
		for z := range 16 {
			for y := -64; y < 0; y++ {
				c.SetBlock(x, y, z, stone)
			}
		}
	}
	c.SetBlock(4, 0, 4, torch)
	c.BlockEntities = append(c.BlockEntities,
		BlockEntity{X: 1, Z: 2, Y: 5, NetType: 7, Data: nbt.Compound{"a": int32(1)}},
		BlockEntity{X: 1, Z: 3, Y: 5, NetType: -1})
	NewLightEngine(c.Dim, nil).LightChunk(c)

	var w proto.Writer
	EncodeChunkData(&w, c)
	p := parseChunkBody(t, w.Bytes(), 24)

	if len(p.heightmaps) != 3 {
		t.Fatalf("heightmaps %v", p.heightmaps)
	}
	for _, typ := range []int32{1, 4, 5} {
		hm := p.heightmaps[typ]
		if len(hm) != 37 {
			t.Fatalf("heightmap %d has %d longs", typ, len(hm))
		}
		// column 0: highest block y=-1 -> 64 above MinY; 7 entries of 9 bits per long
		if v := hm[0] & 511; v != 64 {
			t.Errorf("heightmap %d col0 = %d", typ, v)
		}
	}
	if v := (p.heightmaps[1][68/7] >> (uint(68%7) * 9)) & 511; v != 65 {
		t.Errorf("WORLD_SURFACE at torch column = %d", v)
	}
	for i := range 24 {
		want := int16(0)
		if i < 4 {
			want = 4096
		}
		if i == 4 {
			want = 1
		}
		if p.nonAir[i] != want {
			t.Errorf("section %d nonAir %d want %d", i, p.nonAir[i], want)
		}
	}
	if got := data.StateID(p.sections[4].Blocks.Get(sectionIndex(4, 0, 4))); got != torch {
		t.Errorf("torch decoded as %d", got)
	}
	if p.entities != 1 {
		t.Errorf("block entities %d", p.entities)
	}
	if len(p.sky) == 0 || len(p.block) == 0 {
		t.Errorf("light arrays sky=%d block=%d", len(p.sky), len(p.block))
	}
	if len(p.masks[0]) != 1 || len(p.masks[1]) != 1 {
		t.Errorf("masks %v", p.masks)
	}
	// sky mask covers 26 bits; sections fully under stone are empty
	if p.masks[0][0]|p.masks[2][0] != 1<<26-1 {
		t.Errorf("sky + empty sky masks = %b", p.masks[0][0]|p.masks[2][0])
	}
}

func TestEncodeChunkDataAllocs(t *testing.T) {
	c := NewChunk(0, 0, Overworld, 0)
	for i := range 4096 {
		c.Sections[3].Blocks.Set(i, uint32(i%30))
	}
	c.Finish()
	NewLightEngine(c.Dim, nil).LightChunk(c)
	w := &proto.Writer{B: make([]byte, 0, 1<<20)}
	allocs := testing.AllocsPerRun(50, func() {
		w.Reset()
		EncodeChunkData(w, c)
	})
	if allocs != 0 {
		t.Fatalf("EncodeChunkData allocates %.1f times", allocs)
	}
}
