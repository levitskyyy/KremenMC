package anvil

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/generator"
)

func testProto(status string) *Proto {
	plains, _ := data.Canonical().BiomeByName("plains")
	c := world.NewChunk(-3, 5, world.Overworld, plains)
	generator.NewTerrain(9).Generate(c)
	c.BlockEntities = []world.BlockEntity{{X: 1, Y: 60, Z: 2, ID: "DUMMY", NetType: -1, Data: nbt.Compound{"keepPacked": int8(1)}}}
	c.Extra = nbt.Compound{"blending_data": nbt.Compound{"min_section": int32(-4)}}
	p := &Proto{
		Chunk:          c,
		Status:         status,
		Starts:         map[string]nbt.Compound{"minecraft:mineshaft": {"id": "minecraft:mineshaft", "ChunkX": int32(-3)}},
		References:     map[string][]int64{"minecraft:mineshaft": {int64(world.ChunkPos{X: -3, Z: 5}.Key())}},
		PostProcessing: make([][]int16, world.Overworld.Sections()),
		BlockTicks:     []Tick{{X: -47, Y: 70, Z: 81, Type: "minecraft:oak_leaves"}},
		FluidTicks:     []Tick{{X: -40, Y: 6, Z: 90, Type: "minecraft:lava", Delay: 2, Priority: -1}},
	}
	p.PostProcessing[7] = []int16{0x123, 0x321}
	if status != "minecraft:full" {
		p.Entities = []nbt.Compound{{"id": "minecraft:sheep", "Pos": nbt.List{Type: nbt.TagDouble, Items: []any{-47.5, 70.0, 81.5}}}}
	}
	return p
}

func TestProtoRoundTrip(t *testing.T) {
	tests := []struct {
		status string
		hms    []string
	}{
		{"minecraft:biomes", nil},
		{"minecraft:terrain", []string{"MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES", "OCEAN_FLOOR", "OCEAN_FLOOR_WG", "WORLD_SURFACE", "WORLD_SURFACE_WG"}},
		{"minecraft:features", []string{"MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES", "OCEAN_FLOOR", "OCEAN_FLOOR_WG", "WORLD_SURFACE", "WORLD_SURFACE_WG"}},
		{"minecraft:full", []string{"MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES", "OCEAN_FLOOR", "WORLD_SURFACE"}},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			p := testProto(tt.status)
			root := EncodeProto(p, 7)
			var hms []string
			for k := range root["Heightmaps"].(nbt.Compound) {
				hms = append(hms, k)
			}
			slices.Sort(hms)
			if !slices.Equal(hms, tt.hms) {
				t.Fatalf("heightmaps %v, want %v", hms, tt.hms)
			}
			if _, lit := root["isLightOn"]; lit != (tt.status == "minecraft:full") {
				t.Fatalf("isLightOn present=%v", lit)
			}
			b, err := nbt.Marshal(nil, "", root, false)
			if err != nil {
				t.Fatal(err)
			}
			_, back, _, err := nbt.Unmarshal(b, false)
			if err != nil {
				t.Fatal(err)
			}
			q, st, err := DecodeProto(back, world.Overworld)
			if err != nil || st.UnknownBlocks+st.UnknownBiomes > 0 {
				t.Fatal(err, st)
			}
			if q.Status != tt.status || !reflect.DeepEqual(q.Starts, p.Starts) || !reflect.DeepEqual(q.References, p.References) ||
				!reflect.DeepEqual(q.BlockTicks, p.BlockTicks) || !reflect.DeepEqual(q.FluidTicks, p.FluidTicks) ||
				!reflect.DeepEqual(q.PostProcessing[7], p.PostProcessing[7]) || len(q.Entities) != len(p.Entities) {
				t.Fatalf("generation state lost:\n%+v\n%+v", q, p)
			}
			if fmt.Sprint(q.Chunk.Extra) != fmt.Sprint(p.Chunk.Extra) || q.Chunk.BlockEntities[0].Data["keepPacked"] != int8(1) {
				t.Fatalf("extra %v block entity %+v", q.Chunk.Extra, q.Chunk.BlockEntities)
			}
			for y := -64; y < 320; y += 7 {
				for i := range 256 {
					if p.Chunk.Block(i&15, y, i>>4) != q.Chunk.Block(i&15, y, i>>4) {
						t.Fatalf("block %d %d %d", i&15, y, i>>4)
					}
				}
			}
			if full, _, _ := DecodeChunk(back, world.Overworld); (full != nil) != (tt.status == "minecraft:full") {
				t.Fatal("DecodeChunk must only return full chunks")
			}
		})
	}
}

func TestStorageProtoEntities(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir+"/region", world.Overworld)
	p := testProto("minecraft:full")
	p.Entities = []nbt.Compound{{"id": "minecraft:cow"}}
	if err := s.SaveProto(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = NewStorage(dir+"/region", world.Overworld)
	defer s.Close()
	q, err := s.LoadProto(p.Chunk.Pos())
	if err != nil || q == nil || len(q.Entities) != 1 || q.Entities[0]["id"] != "minecraft:cow" {
		t.Fatalf("%+v %v", q, err)
	}
	if _, ok := q.Chunk.Extra["entities"]; ok {
		t.Fatal("full chunk carries inline entities")
	}
	p.Entities = nil
	if err := s.SaveProto(p); err != nil {
		t.Fatal(err)
	}
	if es, err := s.LoadEntities(p.Chunk.Pos()); err != nil || len(es) != 0 {
		t.Fatalf("entities not removed: %v %v", es, err)
	}
}

func TestPaletteVanillaForm(t *testing.T) {
	p := testProto("minecraft:full")
	root := EncodeProto(p, 0)
	var sawString, sawMixed bool
	for _, it := range root["sections"].([]nbt.Compound) {
		bs, ok := it["block_states"].(nbt.Compound)
		if !ok {
			continue
		}
		l := bs["palette"].(nbt.List)
		switch l.Type {
		case nbt.TagString:
			sawString = true
		case nbt.TagCompound:
			sawMixed = true
			for _, e := range l.Items {
				c := e.(nbt.Compound)
				if _, bare := c[""]; !bare && c["properties"] == nil {
					t.Fatalf("non-default entry without properties: %v", c)
				}
			}
		}
	}
	if !sawString || !sawMixed {
		t.Fatalf("string palettes %v, mixed %v", sawString, sawMixed)
	}
}
