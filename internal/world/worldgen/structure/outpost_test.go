package structure_test

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestOutpostExclusionProbe compares the pillager outpost exclusion zone
// with the black-box probe kremenmc.probe.outpost.Excl (seed
// 8266389061079214669, 81x81 grid cells): frequency test, isStructureChunk
// and hasStructureChunkInRange(villages, 10) per potential chunk.
func TestOutpostExclusionProbe(t *testing.T) {
	f, err := os.Open("testdata/outpost/excl_8266389061079214669.txt")
	if err != nil {
		t.Skip("no probe data")
	}
	defer f.Close()
	sets, err := structure.LoadSets()
	if err != nil {
		t.Fatal(err)
	}
	var op, vil *structure.Set
	for _, s := range sets {
		switch s.Key {
		case "minecraft:pillager_outposts":
			op = s
		case "minecraft:villages":
			vil = s
		}
	}
	sc := bufio.NewScanner(f)
	n, bad := 0, 0
	for sc.Scan() {
		var cx, cz int32
		var freq, ok, near int
		fmt.Sscan(sc.Text(), &cx, &cz, &freq, &ok, &near)
		gNear := structure.InExclusionRange(&vil.Placement, refSeed, cx, cz, 10)
		gOK := op.Placement.IsSpreadChunk(refSeed, cx, cz) && !gNear
		n++
		if gNear != (near == 1) || gOK != (ok == 1) {
			bad++
			if bad < 5 {
				t.Errorf("%d,%d: near %v want %d, ok %v want %d", cx, cz, gNear, near, gOK, ok)
			}
		}
	}
	t.Logf("%d/%d potential chunks match", n-bad, n)
}

// TestOutpostStartsProbe compares kremenmc's pillager outpost starts with the
// black-box probe (kremenmc.probe.outpost.Starts with -Dkremenmc.op.valid=true:
// findValidGenerationPoint, fresh pieces) over the 150 accepted potential
// chunks nearest the origin, seed 8266389061079214669: presence, and per
// piece the box, position, rotation and element.
func TestOutpostStartsProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	b, err := os.ReadFile("testdata/outpost/starts_8266389061079214669.txt")
	if err != nil {
		t.Skip("no probe data")
	}
	structure.EnableJigsaw("minecraft:pillager_outpost")
	tr, err := vanilla.NewTerrain("overworld", refSeed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, refSeed)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int32][]string{}
	var order [][2]int32
	var cur [2]int32
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) >= 3 && (f[0] == "START" || f[0] == "NONE"):
			fmt.Sscan(f[1], &cur[0])
			fmt.Sscan(f[2], &cur[1])
			order = append(order, cur)
			want[cur] = []string{}
		case strings.HasPrefix(l, "  BB"):
			want[cur] = append(want[cur], strings.TrimSpace(l))
		}
	}
	starts, bad := 0, 0
	for _, c := range order {
		sts, err := ss.StartsAt(c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, st := range sts {
			if st.ID != "minecraft:pillager_outpost" {
				continue
			}
			for _, p := range st.Pieces {
				got = append(got, outpostPieceLine(p.(*structure.JigsawPiece)))
			}
		}
		if len(want[c]) > 0 {
			starts++
		}
		if strings.Join(got, "\n") != strings.Join(want[c], "\n") {
			bad++
			if bad <= 3 || os.Getenv("KREMENMC_V") != "" {
				t.Errorf("%d,%d:\n want %s\n got  %s", c[0], c[1], strings.Join(want[c], "\n      "), strings.Join(got, "\n      "))
			}
		}
	}
	t.Logf("%d/%d chunks identical (%d starts)", len(order)-bad, len(order), starts)
}

var rotProbeNames = []string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}

func outpostPieceLine(p *structure.JigsawPiece) string {
	b := p.Box()
	el := "LegacySingle[Left[" + p.Location() + "]]"
	if p.Location() == "minecraft:pillager_outpost/watchtower" {
		el = "List[LegacySingle[Left[minecraft:pillager_outpost/watchtower]], LegacySingle[Left[minecraft:pillager_outpost/watchtower_overgrown]]]"
	}
	return fmt.Sprintf("BB %d %d %d %d %d %d pos %d, %d, %d rot %s el %s", b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ, p.X, p.Y, p.Z, rotProbeNames[p.Rot], el)
}
