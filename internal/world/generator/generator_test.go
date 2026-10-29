package generator

import (
	"bytes"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world"
)

func encode(c *world.Chunk) []byte {
	var w proto.Writer
	world.EncodeChunkData(&w, c)
	return w.B
}

func gen(g world.Generator, x, z int32) *world.Chunk {
	c := world.NewChunk(x, z, world.Overworld, 0)
	g.Generate(c)
	world.NewLightEngine(c.Dim, nil).LightChunk(c)
	return c
}

func TestTerrainDeterministic(t *testing.T) {
	positions := [][2]int32{{0, 0}, {-1, 3}, {100, -250}, {7, 7}, {-40, -41}, {1000, 1000}}
	a, b := NewTerrain(42), NewTerrain(42)
	want := make([][]byte, len(positions))
	for i, p := range positions {
		want[i] = encode(gen(a, p[0], p[1]))
	}
	// same seed, reversed order, concurrent, separate instance
	var wg sync.WaitGroup
	got := make([][]byte, len(positions))
	for i := len(positions) - 1; i >= 0; i-- {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = encode(gen(b, positions[i][0], positions[i][1]))
		}()
	}
	wg.Wait()
	for i := range positions {
		if !bytes.Equal(want[i], got[i]) {
			t.Fatalf("chunk %v differs between runs", positions[i])
		}
	}
	other := encode(gen(NewTerrain(43), 0, 0))
	if bytes.Equal(other, want[0]) {
		t.Fatal("different seeds produced identical chunks")
	}
}

func TestTerrainShape(t *testing.T) {
	g := NewTerrain(1)
	reg := data.Canonical()
	bedrock := data.MustState("bedrock")
	counts := map[string]int{}
	var biomes = map[data.BiomeID]bool{}
	for cx := int32(-6); cx < 6; cx++ {
		for cz := int32(-6); cz < 6; cz++ {
			c := gen(g, cx*9, cz*9)
			for x := range 16 {
				for z := range 16 {
					if c.Block(x, -64, z) != bedrock {
						t.Fatalf("no bedrock floor at %d,%d", x, z)
					}
					h := c.Height(world.WorldSurface, x, z)
					if h < 0 || h > 319 {
						t.Fatalf("surface height %d", h)
					}
				}
			}
			for si := range c.Sections {
				c.Sections[si].Blocks.Count(func(v uint32, n int) {
					counts[reg.BlockOf(data.StateID(v)).Name] += n
				})
				c.Sections[si].Biomes.Count(func(v uint32, n int) { biomes[data.BiomeID(v)] = true })
			}
		}
	}
	for _, want := range []string{"stone", "deepslate", "water", "cave_air", "coal_ore", "iron_ore", "diamond_ore", "deepslate_diamond_ore", "grass_block"} {
		if counts[want] == 0 {
			t.Errorf("no %s generated", want)
		}
	}
	logs := 0
	for name, n := range counts {
		if len(name) > 4 && name[len(name)-4:] == "_log" {
			logs += n
		}
	}
	if logs == 0 {
		t.Error("no trees")
	}
	if len(biomes) < 4 {
		t.Errorf("only %d biomes", len(biomes))
	}
}

func TestFlat(t *testing.T) {
	tests := []struct {
		spec string
		ok   bool
		top  int
	}{
		{"minecraft:bedrock,2*minecraft:dirt,minecraft:grass_block", true, -61},
		{"bedrock,60*stone,oak_slab[type=top]", true, -3},
		{"bedrock,0*stone", false, 0},
		{"nope", false, 0},
	}
	for _, tt := range tests {
		f, err := ParseFlat(tt.spec, "plains")
		if (err == nil) != tt.ok {
			t.Fatalf("%s: err %v", tt.spec, err)
		}
		if !tt.ok {
			continue
		}
		c := gen(f, 0, 0)
		if h := c.Height(world.WorldSurface, 3, 3); h != tt.top+1 {
			t.Errorf("%s: surface %d want %d", tt.spec, h, tt.top+1)
		}
		if c.Block(0, -64, 0) != data.MustState("bedrock") {
			t.Errorf("%s: no bedrock", tt.spec)
		}
	}
	c := gen(DefaultFlat(), 2, 2)
	if c.Block(5, -61, 5) != data.MustState("grass_block") || c.Sections[0].NonAir() != 4*256 {
		t.Fatal("default flat layout")
	}
}

func BenchmarkTerrainGenerate(b *testing.B) {
	g := NewTerrain(5)
	i := int32(0)
	for b.Loop() {
		c := world.NewChunk(i, i*3, world.Overworld, 0)
		g.Generate(c)
		i++
	}
}

func BenchmarkTerrainGenerateAndLight(b *testing.B) {
	g := NewTerrain(5)
	e := world.NewLightEngine(world.Overworld, nil)
	i := int32(0)
	for b.Loop() {
		c := world.NewChunk(i, i*3, world.Overworld, 0)
		g.Generate(c)
		e.LightChunk(c)
		i++
	}
}

func BenchmarkEncodeChunk(b *testing.B) {
	c := gen(NewTerrain(5), 3, 4)
	w := &proto.Writer{B: make([]byte, 0, 1<<20)}
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		world.EncodeChunkData(w, c)
	}
	b.SetBytes(int64(w.Len()))
}
