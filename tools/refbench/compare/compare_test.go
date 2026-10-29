package compare

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/generator"
)

func gen(x, z int32) *world.Chunk {
	plains, _ := data.Canonical().BiomeByName("plains")
	c := world.NewChunk(x, z, world.Overworld, plains)
	generator.NewTerrain(5).Generate(c)
	return c
}

// A chunk saved with anvil.EncodeChunk and read back with DecodeRaw must
// compare equal to the in-memory chunk.
func TestRawRoundTripEqual(t *testing.T) {
	c := gen(3, -7)
	c.BlockEntities = append(c.BlockEntities, world.BlockEntity{X: 1, Y: 70, Z: 2, ID: "minecraft:chest", Data: nbt.Compound{"Lock": "x"}})
	b, err := nbt.Marshal(nil, "", anvil.EncodeChunk(c, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := anvil.DecodeRaw(root)
	if err != nil {
		t.Fatal(err)
	}
	ref := Set{{3, -7}: FromRaw(rc)}
	got := Set{{3, -7}: FromWorld(c, "minecraft:full")}
	r := Compare(ref, got, nil, Options{Blocks: true, Biomes: true, BlockEntities: true, Data: true,
		Heightmaps: []string{"WORLD_SURFACE", "MOTION_BLOCKING"}})
	if !r.Equal() || r.Blocks.Total != 16*16*384 || r.Biomes.Total != 4*4*96 || r.BlockEnts.Total != 1 {
		t.Fatalf("not equal: %+v first %v", r, r.First)
	}
}

func TestCompareReportsMismatches(t *testing.T) {
	a, b := gen(0, 0), gen(0, 0)
	stone, dirt := data.MustState("stone"), data.MustState("dirt")
	for x := range 16 {
		b.SetBlock(x, 0, 0, stone)
		a.SetBlock(x, 0, 0, dirt)
	}
	b.SetBlock(0, 300, 0, stone)
	desert, _ := data.Canonical().BiomeByName("desert")
	b.SetBiome(0, 0, 0, desert)
	r := Compare(Set{{0, 0}: FromWorld(a, "")}, Set{{0, 0}: FromWorld(b, ""), {1, 1}: FromWorld(gen(1, 1), "")}, []Pos{{0, 0}, {5, 5}}, FullOptions())
	if r.Equal() {
		t.Fatal("equal")
	}
	if r.Blocks.Total-r.Blocks.Match != 17 || r.Confusion[0] != (Pair{"minecraft:dirt", "minecraft:stone", 16}) {
		t.Fatalf("blocks %+v confusion %+v", r.Blocks, r.Confusion)
	}
	if r.Biomes.Total-r.Biomes.Match != 1 || r.BiomeConfusion[0].Got != "minecraft:desert" {
		t.Fatalf("biomes %+v %+v", r.Biomes, r.BiomeConfusion)
	}
	if h := r.Heightmaps["WORLD_SURFACE"]; h.Match != 255 || h.MaxAbs == 0 {
		t.Fatalf("heightmap %+v", h)
	}
	if len(r.MissingRef) != 1 || r.MissingRef[0] != (Pos{5, 5}) {
		t.Fatalf("missing %v", r.MissingRef)
	}
	if r.First[0].Kind != "block" || r.First[0].Y != 0 {
		t.Fatalf("first %+v", r.First[0])
	}
}

func TestRawStateString(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"minecraft:stone", "minecraft:stone"},
		{"minecraft:oak_log", "minecraft:oak_log[axis=y]"},
		{nbt.Compound{"": "minecraft:water"}, "minecraft:water[level=0]"},
		{nbt.Compound{"id": "minecraft:oak_log", "properties": nbt.Compound{"axis": "x"}}, "minecraft:oak_log[axis=x]"},
		{nbt.Compound{"Name": "minecraft:wheat", "Properties": nbt.Compound{"age": "7"}}, "minecraft:wheat[age=7]"},
		{"minecraft:not_a_block", "minecraft:not_a_block"},
	}
	for _, tt := range tests {
		if got := anvil.RawStateString(tt.in); got != tt.want {
			t.Errorf("%v: %q, want %q", tt.in, got, tt.want)
		}
	}
}
