package structure_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

var rpLine = regexp.MustCompile(`^(\S+) (-?\d+) (-?\d+) (?:valid|biome-rejected) pos=\S+ PIECE \{(.*),id:"minecraft:rupo"\}`)

// TestRuinedPortalStartsProbe compares kremenmc's ruined portal starts with the
// black-box probe's (testdata/ruined_portal, probe structlegacy.Starts,
// seed 8266389061079214669, chunks -10..9): template, rotation, mirror,
// origin, box, vertical placement and properties, field for field.
func TestRuinedPortalStartsProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: scans noise columns")
	}
	files, _ := filepath.Glob("testdata/ruined_portal/st_*.txt")
	if len(files) == 0 {
		t.Skip("no probe data")
	}
	tr, err := vanilla.NewTerrain("overworld", refSeed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, refSeed)
	if err != nil {
		t.Fatal(err)
	}
	pr := ss.TerrainProbe()
	col, ok := pr.(interface {
		Block(x, y, z int32) data.StateID
	})
	if !ok {
		t.Skip("probe has no column access")
	}
	for _, f := range files {
		fh, _ := os.Open(f)
		sc := bufio.NewScanner(fh)
		sc.Buffer(nil, 1<<20)
		n, bad := 0, 0
		for sc.Scan() {
			m := rpLine.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			var cx, cz int32
			fmt.Sscan(m[2], &cx)
			fmt.Sscan(m[3], &cz)
			got, err := structure.RuinedPortalStartForTest(m[1], refSeed, cx, cz, pr.Height, col.Block, pr.Biome)
			n++
			if err != nil || got != m[4] {
				bad++
				if bad <= 3 {
					t.Errorf("%s %d,%d:\n want %s\n got  %s (%v)", m[1], cx, cz, m[4], got, err)
				}
			}
		}
		fh.Close()
		t.Logf("%s: %d/%d starts match", strings.TrimPrefix(filepath.Base(f), "st_"), n-bad, n)
	}
}
