package structure_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

const refSeed = 8266389061079214669

// refStarts reads structures.starts from a reference world (refbench gen
// -structures=true), keyed "id@cx,cz".
func refStarts(t *testing.T, dir string) map[string]nbt.Compound {
	out := map[string]nbt.Compound{}
	files, _ := filepath.Glob(filepath.Join(dir, "r.*.mca"))
	for _, f := range files {
		var rx, rz int32
		fmt.Sscanf(filepath.Base(f), "r.%d.%d.mca", &rx, &rz)
		r, err := anvil.OpenRegionReadOnly(dir, rx, rz)
		if err != nil {
			continue
		}
		for lz := 0; lz < 32; lz++ {
			for lx := 0; lx < 32; lx++ {
				if !r.Has(lx, lz) {
					continue
				}
				b, err := r.Read(lx, lz)
				if err != nil {
					continue
				}
				_, root, _, err := nbt.Unmarshal(b, false)
				if err != nil {
					continue
				}
				ss, _ := root["structures"].(nbt.Compound)
				st, _ := ss["starts"].(nbt.Compound)
				for id, v := range st {
					c, _ := v.(nbt.Compound)
					if c["id"] == "INVALID" {
						continue
					}
					cx, _ := c["ChunkX"].(int32)
					cz, _ := c["ChunkZ"].(int32)
					out[fmt.Sprintf("%s@%d,%d", id, cx, cz)] = c
				}
			}
		}
		r.Close()
	}
	return out
}

// TestRefStructureStarts compares kremenmc's starts with the reference world's
// for every start chunk the reference generated.
func TestRefStructureStarts(t *testing.T) {
	dir := "../../../../.refcache/26.3/worlds/8266389061079214669-overworld/world/dimensions/minecraft/overworld/region"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no reference world")
	}
	ref := refStarts(t, dir)
	keys := make([]string, 0, len(ref))
	for k := range ref {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := ref[k]
		ch := c["Children"].(nbt.List)
		first, _ := ch.Items[0].(nbt.Compound)
		t.Logf("ref %s: %d pieces, first %v tmpl=%v TP=%v,%v,%v rot=%v BB=%v", k, len(ch.Items), first["id"], first["Template"], first["TPX"], first["TPY"], first["TPZ"], first["Rot"], first["BB"])
	}
	tr, err := vanilla.NewTerrain("overworld", refSeed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, refSeed)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("supported: %v", ss.Supported())
	match, total := 0, 0
	for _, k := range keys {
		c := ref[k]
		id, _ := c["id"].(string)
		if id != "minecraft:shipwreck" && id != "minecraft:igloo" {
			continue
		}
		total++
		cx, _ := c["ChunkX"].(int32)
		cz, _ := c["ChunkZ"].(int32)
		first := c["Children"].(nbt.List).Items[0].(nbt.Compound)
		st, err := ss.StartAt(cx, cz)
		if err != nil || st == nil {
			t.Errorf("%s: kremenmc has no start (%v)", k, err)
			continue
		}
		g := st.Pieces[0].Save()
		rb, gb := first["BB"].([]int32), g["BB"].([]int32)
		t.Logf("%s ref %v %v BBxz %v,%v..%v,%v | kremenmc %s %v %v BB %v", k, first["Template"], first["Rot"], rb[0], rb[2], rb[3], rb[5], st.ID, g["Template"], g["Rot"], gb)
		if st.ID == id && g["Template"] == first["Template"] && g["Rot"] == first["Rot"] && rb[0] == gb[0] && rb[2] == gb[2] && rb[3] == gb[3] && rb[5] == gb[5] {
			match++
		}
	}
	t.Logf("start chunk + template + rotation + xz box: %d/%d", match, total)
	if match != total {
		t.Errorf("starts differ from the reference: %d/%d", match, total)
	}
	_ = structure.Box{}
}
