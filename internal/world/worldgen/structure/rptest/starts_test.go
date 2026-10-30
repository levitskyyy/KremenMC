package rptest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

const refRoot = "../../../../../.refcache/26.3/worlds/"

// refChunks reads every stored chunk of a reference world: the start
// compounds by "id@cx,cz" and the set of stored chunks.
func refChunks(t *testing.T, world string) (map[string]nbt.Compound, [][2]int32) {
	dir := filepath.Join(refRoot, world, "world/dimensions/minecraft/overworld/region")
	files, _ := filepath.Glob(filepath.Join(dir, "r.*.mca"))
	starts := map[string]nbt.Compound{}
	var chunks [][2]int32
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
				chunks = append(chunks, [2]int32{rx<<5 + int32(lx), rz<<5 + int32(lz)})
				ss, _ := root["structures"].(nbt.Compound)
				st, _ := ss["starts"].(nbt.Compound)
				for id, v := range st {
					c, _ := v.(nbt.Compound)
					if c["id"] == "INVALID" {
						continue
					}
					starts[fmt.Sprintf("%s@%d,%d", id, c["ChunkX"], c["ChunkZ"])] = c
				}
			}
		}
		r.Close()
	}
	return starts, chunks
}

// TestRuinedPortalRefStarts compares the ruined portal starts of every chunk
// stored in the reference worlds (refbench gen -structures=true) with
// kremenmc's: presence, structure id and every piece field.
func TestRuinedPortalRefStarts(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	for _, w := range []string{"8266389061079214669-overworld", "0-overworld", "12345-overworld", "7-overworld"} {
		if _, err := os.Stat(filepath.Join(refRoot, w)); err != nil {
			continue
		}
		var seed int64
		fmt.Sscan(strings.TrimSuffix(w, "-overworld"), &seed)
		ref, chunks := refChunks(t, w)
		tr, err := vanilla.NewTerrain("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		ss, err := vanilla.StructureStagesForTest(tr, seed)
		if err != nil {
			t.Fatal(err)
		}
		n, bad := 0, 0
		for _, c := range chunks {
			sts, err := ss.StartsAt(c[0], c[1])
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]nbt.Compound{}
			for _, st := range sts {
				if strings.Contains(st.ID, "ruined_portal") {
					got[st.ID] = st.Encode()
				}
			}
			want := map[string]nbt.Compound{}
			for k, v := range ref {
				if strings.Contains(k, "ruined_portal") && strings.HasSuffix(k, fmt.Sprintf("@%d,%d", c[0], c[1])) {
					want[strings.Split(k, "@")[0]] = v
				}
			}
			for id, wc := range want {
				n++
				gc := got[id]
				if gc == nil || pieceString(gc) != pieceString(wc) {
					bad++
					t.Errorf("%s %s@%v:\n want %s\n got  %s", w, id, c, pieceString(wc), pieceString(gc))
				}
			}
			for id := range got {
				if want[id] == nil {
					bad++
					t.Errorf("%s %s@%v: extra start %s", w, id, c, pieceString(got[id]))
				}
			}
		}
		t.Logf("%s: %d stored chunks, %d reference starts, %d mismatches", w, len(chunks), n, bad)
	}
}

func pieceString(c nbt.Compound) string {
	if c == nil {
		return "<nil>"
	}
	ch, _ := c["Children"].(nbt.List)
	var b []string
	for _, it := range ch.Items {
		b = append(b, quoteSNBT(it))
	}
	return strings.Join(b, " ")
}

var rpProbeLine = regexp.MustCompile(`^(\S+) (-?\d+) (-?\d+) (valid|biome-rejected) pos=`)

// TestRuinedPortalBiomeCheck compares the start's biome test with the
// probe's verdict (testdata/ruined_portal/st_*.txt: findGenerationPoint
// valid or biome-rejected) for every probed chunk.
func TestRuinedPortalBiomeCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	files, _ := filepath.Glob("../testdata/ruined_portal/st_*.txt")
	if len(files) == 0 {
		t.Skip("no probe data")
	}
	const seed = 8266389061079214669
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, f := range files {
		fh, _ := os.Open(f)
		sc := bufio.NewScanner(fh)
		sc.Buffer(nil, 1<<20)
		n, bad := 0, 0
		for sc.Scan() {
			m := rpProbeLine.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			var cx, cz int32
			fmt.Sscan(m[2], &cx)
			fmt.Sscan(m[3], &cz)
			st, err := ss.TryStructure(m[1], cx, cz)
			if err != nil {
				t.Fatal(err)
			}
			n++
			if (st != nil) != (m[4] == "valid") {
				bad++
				if bad <= 3 {
					t.Errorf("%s %d,%d: probe %s, kremenmc start %v", m[1], cx, cz, m[4], st != nil)
				}
			}
		}
		fh.Close()
		t.Logf("%s: %d/%d biome verdicts match", filepath.Base(f), n-bad, n)
	}
}

var _ = structure.ChunkBox
