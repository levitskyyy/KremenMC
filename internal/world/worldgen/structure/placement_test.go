package structure

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// TestRandomSpreadReference compares potential chunks, frequency reducers
// and locate positions of every random_spread set with the reference
// (probe kremenmc.probe.structure.Spread): 33x33 cells per set, and 129x129
// chunks for the spacing-1 sets (buried treasure, mineshafts).
func TestRandomSpreadReference(t *testing.T) {
	sets, err := LoadSets()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*Set{}
	for _, s := range sets {
		byKey[s.Key] = s
	}
	const cells, chunks = 16, 64
	for _, seed := range []int64{0, 12345, 8266389061079214669} {
		ref := reftest.Golden(t, fmt.Sprintf("spread_%d.txt", seed), "kremenmc.probe.structure.Spread",
			strconv.FormatInt(seed, 10), strconv.Itoa(cells), strconv.Itoa(chunks))
		want := map[string]map[[2]int32]bool{}
		var b bytes.Buffer
		lines, ok := 0, 0
		sc := bufio.NewScanner(bytes.NewReader(ref))
		for sc.Scan() {
			var key string
			var v [7]int32
			n, _ := fmt.Sscan(sc.Text(), &key, &v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6])
			s := byKey[key]
			if s == nil {
				t.Fatalf("seed %d: unknown set %s", seed, key)
			}
			p := &s.Placement
			if n == 3 {
				if want[key] == nil {
					want[key] = map[[2]int32]bool{}
				}
				want[key][[2]int32{v[0], v[1]}] = true
				continue
			}
			lines++
			px, pz := p.PotentialChunk(seed, v[0]*p.Spacing, v[1]*p.Spacing)
			fr := int32(0)
			if p.FrequencyOK(seed, px, pz) {
				fr = 1
			}
			lx, _, lz := p.LocatePos(px, pz)
			got := [5]int32{px, pz, fr, lx, lz}
			if got != [5]int32{v[2], v[3], v[4], v[5], v[6]} {
				fmt.Fprintf(&b, "%s cell %d,%d: got %v want %v\n", key, v[0], v[1], got, v[2:])
				continue
			}
			ok++
		}
		for _, s := range sets {
			p := &s.Placement
			if p.Type != "random_spread" || p.Spacing != 1 {
				continue
			}
			for cz := int32(-chunks); cz <= chunks; cz++ {
				for cx := int32(-chunks); cx <= chunks; cx++ {
					lines++
					if p.IsSpreadChunk(seed, cx, cz) != want[s.Key][[2]int32{cx, cz}] {
						fmt.Fprintf(&b, "%s chunk %d,%d: got %v\n", s.Key, cx, cz, !want[s.Key][[2]int32{cx, cz}])
						continue
					}
					ok++
				}
			}
		}
		if b.Len() > 0 {
			t.Errorf("seed %d: %d/%d match\n%s", seed, ok, lines, b.String())
		} else {
			t.Logf("seed %d: %d/%d match", seed, ok, lines)
		}
	}
}
