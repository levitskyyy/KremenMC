package structure_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

const refWorldRegion = "../../../../.refcache/26.3/worlds/8266389061079214669-overworld/world/dimensions/minecraft/overworld/region"

func pieceLine(c nbt.Compound) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v GD=%v BB=%v pos=%v,%v,%v rot=%v gld=%v", c["id"], c["GD"], c["BB"], c["PosX"], c["PosY"], c["PosZ"], c["rotation"], c["ground_level_delta"])
	if pe, ok := c["pool_element"].(nbt.Compound); ok {
		fmt.Fprintf(&b, " el=%v %v %v", pe["element_type"], pe["location"], pe["projection"])
		if l, ok := pe["elements"].(nbt.List); ok {
			for _, it := range l.Items {
				e := it.(nbt.Compound)
				fmt.Fprintf(&b, " [%v %v]", e["element_type"], e["location"])
			}
		}
	}
	if j, ok := c["junctions"].(nbt.List); ok {
		for _, it := range j.Items {
			e := it.(nbt.Compound)
			fmt.Fprintf(&b, " J(%v,%v,%v d=%v %v)", e["source_x"], e["source_ground_y"], e["source_z"], e["delta_y"], e["dest_proj"])
		}
	}
	return b.String()
}

// TestRefJigsawDump prints the reference jigsaw starts piece by piece.
func TestRefJigsawDump(t *testing.T) {
	if os.Getenv("KREMENMC_JIGDUMP") == "" {
		t.Skip("set KREMENMC_JIGDUMP=1")
	}
	if _, err := os.Stat(refWorldRegion); err != nil {
		t.Skip("no reference world")
	}
	ref := refStarts(t, refWorldRegion)
	keys := make([]string, 0, len(ref))
	for k := range ref {
		if strings.Contains(k, "village") || strings.Contains(k, "pillager") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		ch := ref[k]["Children"].(nbt.List)
		t.Logf("== %s: %d pieces", k, len(ch.Items))
		for i, it := range ch.Items {
			c := it.(nbt.Compound)
			if i == 0 {
				ks := []string{}
				for kk := range c {
					ks = append(ks, kk)
				}
				sort.Strings(ks)
				t.Logf("keys %v", ks)
			}
			t.Logf("%3d %s", i, pieceLine(c))
		}
	}
}

func TestJigsawTemplateList(t *testing.T) {
	loc := os.Getenv("KREMENMC_JIGTMPL")
	if loc == "" {
		t.Skip()
	}
	js, err := structure.TemplateJigsaws(loc)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range js {
		t.Logf("%d %+v", i, j)
	}
}

func TestJigsawJSON(t *testing.T) {
	k := os.Getenv("KREMENMC_JSON")
	if k == "" {
		t.Skip()
	}
	pk, _ := wgdata.Load()
	reg, key, _ := strings.Cut(k, "|")
	b, ok := pk.JSON(reg, key)
	if !ok {
		t.Fatal("missing")
	}
	fmt.Println(string(b))
}

var rotRef = map[string]string{"NONE": "NONE", "CLOCKWISE_90": "CLOCKWISE_90", "CLOCKWISE_180": "CLOCKWISE_180", "COUNTERCLOCKWISE_90": "COUNTERCLOCKWISE_90"}

func refPieceKey(c nbt.Compound) string {
	pe, _ := c["pool_element"].(nbt.Compound)
	loc := pe["location"]
	if l, ok := pe["elements"].(nbt.List); ok && len(l.Items) > 0 {
		loc = l.Items[0].(nbt.Compound)["location"]
	}
	if f, ok := pe["feature"]; ok {
		loc = f
	}
	var js []string
	if l, ok := c["junctions"].(nbt.List); ok {
		for _, it := range l.Items {
			e := it.(nbt.Compound)
			js = append(js, fmt.Sprintf("%v,%v,%v,%v,%v", e["source_x"], e["source_ground_y"], e["source_z"], e["delta_y"], e["dest_proj"]))
		}
	}
	return fmt.Sprintf("%v %v,%v,%v %v %v gld=%v J=%s", loc, c["PosX"], c["PosY"], c["PosZ"], c["rotation"], c["BB"], c["ground_level_delta"], strings.Join(js, " "))
}

// TestRefJigsawCompare diffs kremenmc's assembled jigsaw starts with the
// reference's piece for piece (KREMENMC_JIGCMP=id@cx,cz).
func TestRefJigsawCompare(t *testing.T) {
	want := os.Getenv("KREMENMC_JIGCMP")
	if want == "" {
		want = "minecraft:village_plains@-13,-28"
	}
	seed, region := int64(refSeed), refWorldRegion
	if v := os.Getenv("KREMENMC_JIGSEED"); v != "" {
		fmt.Sscan(v, &seed)
		region = strings.Replace(region, "8266389061079214669", v, 1)
	}
	if _, err := os.Stat(region); err != nil {
		t.Skip("no reference world")
	}
	ref := refStarts(t, region)
	c, ok := ref[want]
	if !ok {
		t.Fatalf("no reference start %s", want)
	}
	id, at, _ := strings.Cut(want, "@")
	var cx, cz int32
	fmt.Sscanf(at, "%d,%d", &cx, &cz)
	structure.EnableJigsaw(id, "minecraft:village_plains", "minecraft:village_desert", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga")
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	draws := structure.JigsawDraws()
	st, err := ss.StartAt(cx, cz)
	if err != nil || st == nil {
		t.Fatalf("kremenmc start: %v %v", st, err)
	}
	if f := os.Getenv("KREMENMC_JIGDRAWS"); f != "" {
		os.WriteFile(f, []byte(strings.Join(draws(), "\n")+"\n"), 0o644)
		os.WriteFile(f+".trace", []byte(strings.Join(structure.JigsawTrace(), "\n")+"\n"), 0o644)
	}
	// A fresh reference start keeps raised terrain_matching boxes, a
	// reloaded one does not: accept either form.
	var g, g2 []string
	reloaded, err := structure.DecodeStart(st.Encode())
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range st.Pieces {
		g = append(g, refPieceKey(p.Save()))
		g2 = append(g2, refPieceKey(reloaded.Pieces[i].Save()))
	}
	rl := c["Children"].(nbt.List).Items
	match, prefix := 0, -1
	for i := 0; i < len(rl) || i < len(g); i++ {
		r, gg := "-", "-"
		if i < len(rl) {
			r = refPieceKey(rl[i].(nbt.Compound))
		}
		if i < len(g) {
			gg = g[i]
			if r == g2[i] {
				gg = r
			}
		}
		mark := "!="
		if r == gg {
			mark, match = "==", match+1
		} else if prefix < 0 {
			prefix = i
		}
		t.Logf("%3d %s\n      ref  %s\n      kremenmc %s", i, mark, r, gg)
	}
	t.Logf("pieces: ref %d kremenmc %d, identical %d, prefix %d", len(rl), len(g), match, prefix)
	if match != len(rl) || len(g) != len(rl) {
		t.Errorf("%s: %d/%d pieces identical", want, match, len(rl))
	}
}

func TestJigsawHeights(t *testing.T) {
	r := os.Getenv("KREMENMC_JIGH")
	if r == "" {
		t.Skip()
	}
	var x0, z0, x1, z1 int32
	fmt.Sscanf(r, "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	fmt.Printf("%5s", "")
	for x := x0; x <= x1; x++ {
		fmt.Printf("%4d", x%1000)
	}
	fmt.Println()
	for z := z0; z <= z1; z++ {
		fmt.Printf("%5d", z)
		for x := x0; x <= x1; x++ {
			fmt.Printf("%4d", ss.ProbeHeight(x, z)-1)
		}
		fmt.Println()
	}
}

func TestJigsawSizes(t *testing.T) {
	if os.Getenv("KREMENMC_JIGSIZE") == "" {
		t.Skip()
	}
	for _, n := range []string{"corner_03", "crossroad_02", "straight_03", "crossroad_04"} {
		t.Log(n, structure.TemplateSize("minecraft:village/plains/streets/"+n))
	}
}

func TestJigsawPoolHeights(t *testing.T) {
	if os.Getenv("KREMENMC_JIGSIZE") == "" {
		t.Skip()
	}
	for _, n := range []string{"streets", "decor", "houses", "terminators"} {
		h, err := structure.PoolMaxHeight("minecraft:village/plains/" + n)
		t.Log(n, h, err)
	}
}

func TestJigsawFindStarts(t *testing.T) {
	id := os.Getenv("KREMENMC_JIGFIND")
	if id == "" {
		t.Skip()
	}
	structure.EnableJigsaw(id, "minecraft:village_plains", "minecraft:village_desert", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga")
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	n := 0
	for cz := int32(-64); cz < 64 && n < 3; cz++ {
		for cx := int32(-64); cx < 64 && n < 3; cx++ {
			st, err := ss.StartAt(cx, cz)
			if err == nil && st != nil && strings.Contains(st.ID, "village") && st.ID != "minecraft:village_plains" {
				t.Logf("%s@%d,%d %d pieces", st.ID, cx, cz, len(st.Pieces))
				for i, p := range st.Pieces {
					sv := p.Save()
					if i < 12 {
						t.Logf("G %d %v %v,%v,%v %v %v", i, sv["Template"], sv["TPX"], sv["TPY"], sv["TPZ"], sv["Rot"], sv["BB"])
					}
				}
				n++
			}
		}
	}
}
