package structure_test

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var pieceRe = regexp.MustCompile(`PIECE (\{.*?id:"minecraft:orp"\})`)

// oceanRuinCtx is the start context of chunk cx,cz for an ocean ruin.
func oceanRuinCtx(t *testing.T, seed int64, key string, pr structure.Probe, cx, cz int32) (*structure.GenContext, structure.Type) {
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pk.JSON("worldgen/structure", key)
	typ, err := structure.OceanRuinTypeForTest(b)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := pk.Tag("worldgen/biome", "has_structure/"+key)
	reg := data.Canonical()
	ok := map[data.BiomeID]bool{}
	for _, n := range names {
		if id, found := reg.BiomeByName(strings.TrimPrefix(n, "minecraft:")); found {
			ok[id] = true
		}
	}
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	return &structure.GenContext{Seed: seed, ChunkX: cx, ChunkZ: cz, Random: r,
		Height:  func(x, z int32, hm string) int32 { return pr.Height(x, z, hm) },
		BiomeOK: func(x, y, z int32) bool { return ok[pr.Biome(x, y, z)] },
	}, typ
}

// TestOceanRuinStartsProbe compares kremenmc's ocean ruin starts with the
// probe's findGenerationPoint (testdata/ocean_ruin, seed
// 8266389061079214669, chunks -10..9).
func TestOceanRuinStartsProbe(t *testing.T) {
	seed := int64(8266389061079214669)
	if v := os.Getenv("KREMENMC_ORSEED"); v != "" {
		fmt.Sscan(v, &seed)
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	pr := vanilla.StructureProbeForTest(tr)
	for _, key := range []string{"ocean_ruin_cold", "ocean_ruin_warm"} {
		f, err := os.Open("testdata/ocean_ruin/st_" + key + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		same, total, starts := 0, 0, 0
		for sc.Scan() {
			l := sc.Text()
			fs := strings.Fields(l)
			var cx, cz int32
			fmt.Sscan(fs[1], &cx)
			fmt.Sscan(fs[2], &cz)
			var want []string
			if fs[3] == "valid" {
				starts++
				for _, m := range pieceRe.FindAllStringSubmatch(l, -1) {
					want = append(want, snbt(parseSNBT(t, snbtKey.ReplaceAllString(m[1], `$1"$2":`))))
				}
			}
			ctx, typ := oceanRuinCtx(t, seed, key, pr, cx, cz)
			var dbg []string
			structure.SetOceanRuinDebug(func(s string) { dbg = append(dbg, s) })
			ps, err := typ.Generate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range ps {
				got = append(got, snbt(p.Save()))
			}
			total++
			if strings.Join(got, "\n") == strings.Join(want, "\n") {
				same++
			} else if total-same < 6 {
				t.Logf("%s %d,%d:\n want %v\n got  %v\n%s", key, cx, cz, want, got, strings.Join(dbg, "\n"))
			}
		}
		f.Close()
		t.Logf("%s: %d/%d chunks identical (%d starts)", key, same, total, starts)
		if same != total {
			t.Errorf("%s starts differ", key)
		}
	}
	_ = nbt.Compound{}
}

// TestOceanRuinRefStarts compares kremenmc's ocean ruin starts with every
// ocean ruin start stored in the reference worlds.
func TestOceanRuinRefStarts(t *testing.T) {
	same, total := 0, 0
	for _, w := range []string{"0", "12345", "7", "8266389061079214669"} {
		dir := "../../../../.refcache/26.3/worlds/" + w + "-overworld/world/dimensions/minecraft/overworld/region"
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		var seed int64
		fmt.Sscan(w, &seed)
		tr, err := vanilla.NewTerrain("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		pr := vanilla.StructureProbeForTest(tr)
		for k, c := range refStarts(t, dir) {
			id, _ := c["id"].(string)
			if !strings.HasPrefix(id, "minecraft:ocean_ruin") {
				continue
			}
			cx, _ := c["ChunkX"].(int32)
			cz, _ := c["ChunkZ"].(int32)
			ctx, typ := oceanRuinCtx(t, seed, strings.TrimPrefix(id, "minecraft:"), pr, cx, cz)
			ps, err := typ.Generate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range ps {
				got = append(got, snbt(p.Save()))
			}
			var want []string
			moved := 0
			for i, it := range c["Children"].(nbt.List).Items {
				pc := nbt.Compound{}
				for k, v := range it.(nbt.Compound) {
					pc[k] = v
				}
				// placement moves the piece to the floor; compare at y 90
				if y, _ := pc["TPY"].(int32); y != 90 {
					bb := append([]int32(nil), pc["BB"].([]int32)...)
					bb[1], bb[4] = bb[1]+90-y, bb[4]+90-y
					pc["BB"], pc["TPY"] = bb, int32(90)
					moved++
					if i < len(ps) {
						t.Logf("%s %s piece %d placed at y %d BB %v", w, k, i, y, it.(nbt.Compound)["BB"])
					}
				}
				want = append(want, snbt(pc))
			}
			total++
			if strings.Join(got, "\n") == strings.Join(want, "\n") {
				same++
			} else {
				t.Logf("%s %s:\n want %v\n got  %v", w, k, want, got)
			}
		}
	}
	t.Logf("ocean ruin reference starts: %d/%d identical", same, total)
	if same != total {
		t.Errorf("starts differ")
	}
}

// TestOceanRuinStartDump (KREMENMC_ORDUMP=seed@key@cx,cz@out) writes
// kremenmc's (reference-identical) start of a chunk as SNBT for
// TestStartSceneDump.
func TestOceanRuinStartDump(t *testing.T) {
	v := os.Getenv("KREMENMC_ORDUMP")
	if v == "" {
		t.Skip("KREMENMC_ORDUMP not set")
	}
	p := strings.Split(v, "@")
	var seed int64
	var cx, cz int32
	fmt.Sscan(p[0], &seed)
	fmt.Sscanf(p[2], "%d,%d", &cx, &cz)
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ctx, typ := oceanRuinCtx(t, seed, p[1], vanilla.StructureProbeForTest(tr), cx, cz)
	ps, err := typ.Generate(ctx)
	if err != nil || len(ps) == 0 {
		t.Fatalf("no start: %v", err)
	}
	st := &structure.Start{ID: "minecraft:" + p[1], ChunkX: cx, ChunkZ: cz, Pieces: ps}
	if err := os.WriteFile(p[3], []byte(snbt(st.Encode())), 0o644); err != nil {
		t.Fatal(err)
	}
}
