package data

import (
	"bufio"
	"compress/gzip"
	"os"
	"sort"
	"strings"
	"testing"
)

// loadPredicates reads testdata/heightmap_predicates_26.3.tsv.gz, written by
// `go run ./tools/refbench predicates` from the reference server: per block
// (or per state) the mask WS OF MB MBNL air fluid.
func loadPredicates(t testing.TB) map[string]string {
	f, err := os.Open("testdata/heightmap_predicates_26.3.tsv.gz")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "\t")
		if ok {
			m[k] = v
		}
	}
	return m
}

func sortedState(r *Registry, s StateID) (name, full string) {
	str := r.StateString(s)
	name, props, ok := strings.Cut(strings.TrimSuffix(str, "]"), "[")
	if !ok {
		return name, name
	}
	kv := strings.Split(props, ",")
	sort.Strings(kv)
	return name, name + "[" + strings.Join(kv, ",") + "]"
}

// TestHeightmapFlagsMatchReference checks that the flags heightmaps use
// (air, solid = blocksMotion, fluid, leaves) reproduce vanilla's
// Heightmap.Types predicates for every 26.3 block state.
func TestHeightmapFlagsMatchReference(t *testing.T) {
	want := loadPredicates(t)
	r := Canonical()
	bad := map[string]int{}
	var first []string
	for s := StateID(0); int(s) < r.NumStates(); s++ {
		name, full := sortedState(r, s)
		w, ok := want[full]
		if !ok {
			w, ok = want[name]
		}
		if !ok {
			t.Errorf("%s: not in reference", full)
			continue
		}
		f := r.Flags(s)
		air, solid, fluid, leaves := f&FlagAir != 0, f&FlagSolid != 0, f&FlagFluid != 0, f&FlagLeaves != 0
		mb := solid || fluid
		b := func(v bool) byte {
			if v {
				return '1'
			}
			return '0'
		}
		got := string([]byte{b(!air), b(solid), b(mb), b(mb && !leaves), b(air), b(fluid)})
		if got != w {
			bad[name]++
			if len(first) < 30 {
				first = append(first, full+" got "+got+" want "+w)
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d blocks with wrong heightmap flags: %v\n%s", len(bad), bad, strings.Join(first, "\n"))
	}
}
