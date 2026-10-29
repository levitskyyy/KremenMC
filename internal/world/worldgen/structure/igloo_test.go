package structure_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// refChunk decodes one full chunk of a reference world.
func refChunk(t *testing.T, world_ string, cx, cz int32) (*world.Chunk, nbt.Compound) {
	t.Helper()
	dir := "../../../../.refcache/26.3/worlds/" + world_ + "/world/dimensions/minecraft/overworld/region"
	r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
	if err != nil {
		t.Skipf("no reference region: %v", err)
	}
	defer r.Close()
	b, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		t.Skipf("no reference chunk %d,%d: %v", cx, cz, err)
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status %v", root["Status"])
	c, _, err := anvil.DecodeChunk(root, world.Overworld)
	if c == nil {
		t.Skip("reference chunk not full")
	}
	if err != nil {
		t.Fatal(err)
	}
	return c, root
}

func TestIglooRefDump(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	c, root := refChunk(t, "0-overworld", 62512, 62500)
	reg := data.Canonical()
	for y := 60; y < 100; y++ {
		line := ""
		for z := 0; z < 14; z++ {
			for x := 0; x < 10; x++ {
				b := reg.BlockOf(c.Block(x, y, z))
				ch := "?"
				switch b.Name {
				case "air":
					ch = "."
				case "snow_block":
					ch = "S"
				case "snow":
					ch = "s"
				case "ladder":
					ch = "H"
				case "stone_bricks", "mossy_stone_bricks", "cracked_stone_bricks":
					ch = "B"
				case "chest":
					ch = "C"
				case "stone", "dirt", "grass_block", "deepslate", "gravel", "andesite", "granite", "diorite", "tuff":
					ch = "#"
				case "spruce_planks":
					ch = "P"
				case "ice", "packed_ice", "blue_ice":
					ch = "I"
				case "water":
					ch = "~"
				}
				line += ch
			}
			line += " "
		}
		t.Logf("y=%d %s", y, line)
	}
	be, _ := root["block_entities"].(nbt.List)
	for _, e := range be.Items {
		t.Logf("BE %v", e)
	}
	t.Logf("entities %v", root["entities"])
}

func TestIglooRefEntities(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	dir := "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/entities"
	r, err := anvil.OpenRegionReadOnly(dir, 62512>>5, 62500>>5)
	if err != nil {
		t.Skip(err)
	}
	defer r.Close()
	b, err := r.Read(62512&31, 62500&31)
	if err != nil {
		t.Skip(err)
	}
	_, root, _, _ := nbt.Unmarshal(b, false)
	for _, e := range root["Entities"].(nbt.List).Items {
		c := e.(nbt.Compound)
		t.Logf("%v pos=%v rot=%v persist=%v", c["id"], c["Pos"], c["Rotation"], c["PersistenceRequired"])
		for k, v := range c {
			t.Logf("   %s=%v", k, v)
		}
	}
}

// TestIglooReference compares the igloo start of seed 0 at chunk
// 62512,62500 (with a basement of 5 ladder pieces) with the reference
// world piece for piece, then the placed blocks, chest loot and entities.
func TestIglooReference(t *testing.T) {
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	for _, c := range [][2]int32{{62512, 62500}, {294, -375}} {
		t.Run(fmt.Sprint(c), func(t *testing.T) { checkIgloo(t, ref, c[0], c[1]) })
	}
}

func checkIgloo(t *testing.T, ref map[string]nbt.Compound, cx, cz int32) {
	const seed = 0
	rs, ok := ref[fmt.Sprintf("minecraft:igloo@%d,%d", cx, cz)]
	if !ok {
		t.Skip("no reference igloo")
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ss.StartAt(cx, cz)
	if err != nil || st == nil || st.ID != "minecraft:igloo" {
		t.Fatalf("kremenmc start %v %v", st, err)
	}
	want := rs["Children"].(nbt.List).Items
	if len(want) != len(st.Pieces) {
		t.Fatalf("pieces: kremenmc %d, reference %d", len(st.Pieces), len(want))
	}
	match := 0
	for i, it := range want {
		w := it.(nbt.Compound)
		g := st.Pieces[i].Save()
		same := true
		t.Logf("ref piece %d %v TP %v,%v,%v BB %v", i, w["Template"], w["TPX"], w["TPY"], w["TPZ"], w["BB"])
		for _, k := range []string{"id", "Template", "TPX", "TPY", "TPZ", "Rot", "GD", "O"} {
			if fmt.Sprint(w[k]) != fmt.Sprint(g[k]) {
				same = false
				t.Errorf("piece %d %s: kremenmc %v, reference %v", i, k, g[k], w[k])
			}
		}
		// The reference saves the moved box once the igloo is placed, or
		// the box at y 90 when the start chunk was saved before placement.
		if fmt.Sprint(w["BB"]) != fmt.Sprint(g["BB"]) && fmt.Sprint(w["BB"]) != fmt.Sprint(atY90(g)) {
			same = false
			t.Errorf("piece %d BB: kremenmc %v, reference %v", i, g["BB"], w["BB"])
		}
		if same {
			match++
		}
	}
	t.Logf("pieces matching the reference: %d/%d", match, len(want))

	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	rc, root := refChunk(t, "0-overworld", cx, cz)
	reg := data.Canonical()
	// Igloo blocks: every non-terrain block the reference has inside the
	// igloo's xz footprint, y 50..80 (snow layers come from freeze_top_layer).
	igloo := map[string]bool{"snow_block": true, "ice": true, "white_carpet": true, "red_carpet": true, "redstone_torch": true, "furnace": true,
		"crafting_table": true, "white_bed": true, "oak_trapdoor": true, "ladder": true, "stone_bricks": true, "mossy_stone_bricks": true,
		"cracked_stone_bricks": true, "chiseled_stone_bricks": true, "infested_stone_bricks": true, "infested_chiseled_stone_bricks": true,
		"infested_cracked_stone_bricks": true, "infested_mossy_stone_bricks": true, "polished_andesite": true, "chest": true, "brewing_stand": true,
		"cauldron": true, "oak_wall_sign": true, "cobweb": true, "light_gray_carpet": true, "lantern": true, "spruce_planks": true}
	same, total := 0, 0
	for y := -64; y < 200; y++ {
		for z := 0; z < 16; z++ {
			for x := 0; x < 16; x++ {
				ws, gs := rc.Block(x, y, z), pc.Chunk.Block(x, y, z)
				wb, gb := reg.BlockOf(ws), reg.BlockOf(gs)
				if !igloo[wb.Name] && !igloo[gb.Name] {
					continue
				}
				total++
				if ws == gs {
					same++
				} else if total-same < 20 {
					t.Logf("%d,%d,%d: kremenmc %s, reference %s", x, y, z, reg.StateString(gs), reg.StateString(ws))
				}
			}
		}
	}
	t.Logf("igloo blocks (full state) matching the reference: %d/%d", same, total)
	if same != total || total == 0 {
		t.Errorf("igloo blocks differ: %d/%d", same, total)
	}
	var wantChest nbt.Compound
	for _, e := range root["block_entities"].(nbt.List).Items {
		if c := e.(nbt.Compound); c["id"] == "minecraft:chest" {
			wantChest = c
		}
	}
	found := false
	for _, be := range pc.Chunk.BlockEntities {
		if be.Data["LootTable"] != nil {
			found = true
			t.Logf("kremenmc chest %d,%d,%d %v %v; reference %v,%v,%v %v %v", be.X, be.Y, be.Z, be.Data["LootTable"], be.Data["LootTableSeed"],
				wantChest["x"], wantChest["y"], wantChest["z"], wantChest["LootTable"], wantChest["LootTableSeed"])
			if fmt.Sprint(be.Data["LootTable"], cx<<4+int32(be.X), be.Y, cz<<4+int32(be.Z)) != fmt.Sprint(wantChest["LootTable"], wantChest["x"], wantChest["y"], wantChest["z"]) {
				t.Errorf("igloo chest differs")
			}
			if be.Data["LootTableSeed"] != wantChest["LootTableSeed"] {
				t.Errorf("LootTableSeed kremenmc %v, reference %v", be.Data["LootTableSeed"], wantChest["LootTableSeed"])
			}
		}
	}
	if !found {
		t.Error("no igloo chest with loot")
	}
	for _, e := range pc.Entities {
		t.Logf("kremenmc entity %v %v", e["id"], e["Pos"])
	}
	if len(pc.Entities) != 2 {
		t.Errorf("entities = %d, want villager + zombie_villager", len(pc.Entities))
	}
}

func TestIglooHeightsDump(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	tr, _ := vanilla.NewTerrain("overworld", 0)
	stages, _ := tr.Stages("overworld", 0)
	p, _ := gen.New(gen.Config{Seed: 0, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	pc, err := p.Get(context.Background(), world.ChunkPos{X: 62512, Z: 62500}, gen.StatusTerrain)
	if err != nil {
		t.Fatal(err)
	}
	for z := 0; z < 14; z++ {
		s := ""
		for x := 0; x < 10; x++ {
			y := 200
			for ; y > -64 && data.Canonical().IsAir(pc.Chunk.Block(x, y, z)); y-- {
			}
			s += fmt.Sprintf(" %d", y+1)
		}
		t.Logf("z=%d%s", z, s)
	}
}

func TestIglooFindStarts(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	tr, _ := vanilla.NewTerrain("overworld", 0)
	ss, _ := vanilla.StructureStagesForTest(tr, 0)
	sets, _ := structure.LoadSets()
	stages, _ := tr.Stages("overworld", 0)
	p, _ := gen.New(gen.Config{Seed: 0, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	height := func(x, z int32) int32 {
		pc, err := p.Get(context.Background(), world.ChunkPos{X: x >> 4, Z: z >> 4}, gen.StatusTerrain)
		if err != nil {
			t.Fatal(err)
		}
		y := 300
		for ; y > -64 && data.Canonical().IsAir(pc.Chunk.Block(int(x&15), y, int(z&15))); y-- {
		}
		return int32(y + 1)
	}
	for _, s := range sets {
		if s.Key != "minecraft:igloos" {
			continue
		}
		pl := &s.Placement
		n := 0
		for gz := int32(-12); gz <= 12 && n < 6; gz++ {
			for gx := int32(-12); gx <= 12 && n < 6; gx++ {
				cx, cz := pl.PotentialChunk(0, gx*pl.Spacing, gz*pl.Spacing)
				st, _ := ss.StartAt(cx, cz)
				if st == nil || st.ID != "minecraft:igloo" {
					continue
				}
				top := st.Pieces[len(st.Pieces)-1].Save()
				b := top["BB"].([]int32)
				c := []int32{height(b[0], b[2]), height(b[3], b[2]), height(b[0], b[5]), height(b[3], b[5])}
				mn := min(c[0], c[1], c[2], c[3])
				t.Logf("igloo %d,%d rot=%v pieces=%d BB=%v corners=%v min=%d", cx, cz, top["Rot"], len(st.Pieces), b, c, mn)
				n++
			}
		}
	}
}

// atY90 is a saved piece compound's box before the igloo is moved down.
func atY90(g nbt.Compound) []int32 {
	bb := append([]int32(nil), g["BB"].([]int32)...)
	tmplY := g["TPY"].(int32)
	h := bb[4] - bb[1]
	bb[1] = tmplY
	bb[4] = tmplY + h
	return bb
}

func TestSwampHutFindStarts(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	tr, _ := vanilla.NewTerrain("overworld", 0)
	ss, _ := vanilla.StructureStagesForTest(tr, 0)
	sets, _ := structure.LoadSets()
	for _, s := range sets {
		if s.Key != "minecraft:swamp_huts" {
			continue
		}
		pl := &s.Placement
		n := 0
		for gz := int32(-12); gz <= 12 && n < 8; gz++ {
			for gx := int32(-12); gx <= 12 && n < 8; gx++ {
				cx, cz := pl.PotentialChunk(0, gx*pl.Spacing, gz*pl.Spacing)
				st, _ := ss.StartAt(cx, cz)
				if st == nil || st.ID != "minecraft:swamp_hut" {
					continue
				}
				t.Logf("hut %d,%d %v", cx, cz, st.Pieces[0].Save())
				n++
			}
		}
	}
}

// refChunkMaybe is refChunk without skipping: nil when missing or not full.
func refChunkMaybe(t *testing.T, w string, cx, cz int32) (*world.Chunk, nbt.Compound) {
	dir := "../../../../.refcache/26.3/worlds/" + w + "/world/dimensions/minecraft/overworld/region"
	r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
	if err != nil {
		return nil, nil
	}
	defer r.Close()
	b, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		return nil, nil
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		return nil, nil
	}
	c, _, _ := anvil.DecodeChunk(root, world.Overworld)
	return c, root
}
