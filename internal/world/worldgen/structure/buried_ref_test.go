package structure_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// refChunkStarts lists every stored reference chunk that has a
// structures.starts compound, with the ids of its starts.
func refChunkStarts(t *testing.T) map[[2]int32]map[string]nbt.Compound {
	out := map[[2]int32]map[string]nbt.Compound{}
	files, _ := filepath.Glob(filepath.Join(refRegionDir, "r.*.mca"))
	for _, f := range files {
		var rx, rz int32
		fmt.Sscanf(filepath.Base(f), "r.%d.%d.mca", &rx, &rz)
		r, err := anvil.OpenRegionReadOnly(refRegionDir, rx, rz)
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
				ss, ok := root["structures"].(nbt.Compound)
				if !ok {
					continue
				}
				st, _ := ss["starts"].(nbt.Compound)
				m := map[string]nbt.Compound{}
				for id, v := range st {
					if c, _ := v.(nbt.Compound); c != nil && c["id"] != "INVALID" {
						m[id] = c
					}
				}
				out[[2]int32{rx*32 + int32(lx), rz*32 + int32(lz)}] = m
			}
		}
		r.Close()
	}
	return out
}

// TestBuriedRefStarts checks, for every reference chunk past
// structure_starts, that kremenmc places a buried treasure start exactly where
// the reference does, with the same piece box.
func TestBuriedRefStarts(t *testing.T) {
	chunks := refChunkStarts(t)
	if len(chunks) == 0 {
		t.Skip("no reference world")
	}
	tr, err := vanilla.NewTerrain("overworld", refSeed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, refSeed)
	if err != nil {
		t.Fatal(err)
	}
	n, bad, pos := 0, 0, 0
	for c, m := range chunks {
		ref := m["minecraft:buried_treasure"]
		st, err := ss.SetStartAt("minecraft:buried_treasures", c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		n++
		if (ref != nil) != (st != nil) {
			bad++
			t.Errorf("chunk %v: ref start %v, kremenmc %v", c, ref != nil, st != nil)
			continue
		}
		if ref == nil {
			continue
		}
		pos++
		rb := ref["Children"].(nbt.List).Items[0].(nbt.Compound)["BB"].([]int32)
		gb := st.Pieces[0].Box()
		if rb[0] != gb.MinX || rb[2] != gb.MinZ {
			t.Errorf("chunk %v: ref BB %v, kremenmc %v", c, rb, gb)
		}
	}
	t.Logf("%d chunks, %d with buried treasure, %d mismatches", n, pos, bad)
}
