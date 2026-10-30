package structure_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestOutpostReference generates the 3x3 chunks of the reference outpost
// (seed 8266389061079214669, start -87,-9) and compares, inside the
// outpost's piece boxes, every block (full state), the block entities and
// the entities (id, block position) with the reference world (refbench gen
// -chunks rect:-88,-10,-86,-8 -structures=true).
func TestOutpostReference(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	const seed = int64(refSeed)
	structure.EnableJigsaw("minecraft:pillager_outpost")
	x0, z0, x1, z1 := int32(-88), int32(-10), int32(-86), int32(-8)
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	sts, err := ss.StartsAt(-87, -9)
	if err != nil {
		t.Fatal(err)
	}
	var boxes []structure.Box
	for _, st := range sts {
		if st.ID == "minecraft:pillager_outpost" {
			for _, p := range st.Pieces {
				boxes = append(boxes, p.Box())
			}
		}
	}
	if len(boxes) == 0 {
		t.Fatal("no outpost start")
	}
	in := func(x, y, z int32) bool {
		for _, b := range boxes {
			if b.Contains(x, y, z) {
				return true
			}
		}
		return false
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	diff := map[string]int{}
	same, total, show := 0, 0, 0
	var wantBE, gotBE, wantE, gotE []string
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			rc, root := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
			for y := -64; y < 320; y++ {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						if !in(cx<<4+int32(x), int32(y), cz<<4+int32(z)) {
							continue
						}
						ws, gs := rc.Block(x, y, z), pc.Chunk.Block(x, y, z)
						total++
						if ws == gs {
							same++
							continue
						}
						diff[reg.StateString(gs)+" -> "+reg.StateString(ws)]++
						if show < 60 && os.Getenv("KREMENMC_V") != "" {
							show++
							t.Logf("%d,%d,%d: kremenmc %s, ref %s", cx<<4+int32(x), y, cz<<4+int32(z), reg.StateString(gs), reg.StateString(ws))
						}
					}
				}
			}
			for _, it := range opCompounds(root["block_entities"]) {
				wantBE = append(wantBE, fmt.Sprintf("%v %v,%v,%v %v %v", it["id"], it["x"], it["y"], it["z"], it["LootTable"], it["LootTableSeed"]))
			}
			for _, be := range pc.Chunk.BlockEntities {
				gotBE = append(gotBE, fmt.Sprintf("%v %v,%v,%v %v %v", "minecraft:"+strings.TrimPrefix(be.ID, "minecraft:"), cx<<4+int32(be.X), be.Y, cz<<4+int32(be.Z), be.Data["LootTable"], be.Data["LootTableSeed"]))
			}
			for _, e := range refEntities(t, seed, cx, cz) {
				wantE = append(wantE, entityLine(e))
			}
			for _, e := range pc.Entities {
				gotE = append(gotE, entityLine(e))
			}
		}
	}
	type kv struct {
		k string
		n int
	}
	var l []kv
	for k, n := range diff {
		l = append(l, kv{k, n})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n || l[i].n == l[j].n && l[i].k < l[j].k })
	for i, e := range l {
		if i < 30 {
			t.Logf("%6d kremenmc %s", e.n, e.k)
		}
	}
	t.Logf("outpost box blocks %d/%d (%.4f%%)", same, total, 100*float64(same)/float64(total))
	for _, c := range []struct {
		name      string
		want, got []string
	}{{"block entities", wantBE, gotBE}, {"entities", wantE, gotE}} {
		sort.Strings(c.want)
		sort.Strings(c.got)
		t.Logf("%s: ref %d kremenmc %d\n ref  %s\n kremenmc %s", c.name, len(c.want), len(c.got), strings.Join(c.want, "\n      "), strings.Join(c.got, "\n      "))
	}
}

func refEntities(t *testing.T, seed int64, cx, cz int32) []nbt.Compound {
	dir := fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-overworld/world/dimensions/minecraft/overworld/entities", seed)
	r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
	if err != nil {
		return nil
	}
	defer r.Close()
	b, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		return nil
	}
	_, root, _, _ := nbt.Unmarshal(b, false)
	return opCompounds(root["Entities"])
}

func entityLine(e nbt.Compound) string {
	pos, _ := e["Pos"].(nbt.List)
	var p []string
	for _, v := range pos.Items {
		f, _ := v.(float64)
		p = append(p, fmt.Sprintf("%.1f", f))
	}
	return fmt.Sprintf("%v %s", e["id"], strings.Join(p, ","))
}

func opCompounds(v any) []nbt.Compound {
	l, _ := v.(nbt.List)
	var out []nbt.Compound
	for _, it := range l.Items {
		if c, ok := it.(nbt.Compound); ok {
			out = append(out, c)
		}
	}
	return out
}

// TestJigsawLootSeeds compares LootTable and LootTableSeed of every
// container in reference chunks with kremenmc (KREMENMC_LOOTCMP=seed@x0,z0,x1,z1).
func TestJigsawLootSeeds(t *testing.T) {
	v := os.Getenv("KREMENMC_LOOTCMP")
	if v == "" {
		t.Skip("set KREMENMC_LOOTCMP")
	}
	parts := strings.Split(v, "@")
	var seed int64
	var x0, z0, x1, z1 int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	if v := os.Getenv("KREMENMC_VEXTRA"); v != "" {
		structure.EnableJigsaw(strings.Split(v, ",")...)
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("KREMENMC_LOOTORDER") != "" {
		structure.SetLootHook(func(tmpl string, x, y, z int32, seed int64) {
			if x>>4 >= x0 && x>>4 <= x1 && z>>4 >= z0 && z>>4 <= z1 {
				t.Logf("draw %s %d,%d,%d %d", tmpl, x, y, z, seed)
			}
		})
		defer structure.SetLootHook(nil)
	}
	n, bad := 0, 0
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			_, root := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
			got := map[string]string{}
			for _, be := range pc.Chunk.BlockEntities {
				if be.Data["LootTable"] != nil {
					got[fmt.Sprintf("%d,%d,%d", cx<<4+int32(be.X), be.Y, cz<<4+int32(be.Z))] = fmt.Sprint(be.Data["LootTable"], " ", be.Data["LootTableSeed"])
				}
			}
			for _, it := range opCompounds(root["block_entities"]) {
				if it["LootTable"] == nil {
					continue
				}
				k := fmt.Sprintf("%v,%v,%v", it["x"], it["y"], it["z"])
				w := fmt.Sprint(it["LootTable"], " ", it["LootTableSeed"])
				n++
				if got[k] != w {
					bad++
					t.Errorf("%s: ref %s kremenmc %s", k, w, got[k])
				}
			}
		}
	}
	t.Logf("%d/%d containers identical", n-bad, n)
}
