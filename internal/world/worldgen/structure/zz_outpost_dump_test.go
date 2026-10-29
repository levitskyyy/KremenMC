package structure_test

import (
	"fmt"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

func TestZZTemplateSizes(t *testing.T) {
	l := os.Getenv("KREMENMC_TSIZE")
	if l == "" {
		t.Skip()
	}
	for _, n := range strings.Split(l, ",") {
		t.Log(n, structure.TemplateSize(n))
	}
}

func TestZZListRefStarts(t *testing.T) {
	sub := os.Getenv("KREMENMC_LISTSTARTS")
	if sub == "" {
		t.Skip()
	}
	for _, seed := range []string{"8266389061079214669", "0", "12345", "7"} {
		region := strings.Replace(refWorldRegion, "8266389061079214669", seed, 1)
		for k, c := range refStarts(t, region) {
			if strings.Contains(k, sub) {
				t.Logf("%s %s %v", seed, k, len(c["Children"].(nbt.List).Items))
			}
		}
	}
}

func TestZZStartAt(t *testing.T) {
	v := os.Getenv("KREMENMC_STARTAT")
	if v == "" {
		t.Skip()
	}
	var cx, cz int32
	fmt.Sscanf(v, "%d,%d", &cx, &cz)
	structure.EnableJigsaw("minecraft:pillager_outpost")
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	st, err := ss.StartAt(cx, cz)
	if st != nil {
		t.Logf("%s %d pieces", st.ID, len(st.Pieces))
	}
	pr := ss.TerrainProbe()
	t.Log(err, data.Canonical().Biomes[pr.Biome(cx*16+8, 64, cz*16+8)].Name)
}

func TestZZMultiStarts(t *testing.T) {
	if os.Getenv("KREMENMC_MULTI") == "" {
		t.Skip()
	}
	n := map[string][]string{}
	for _, seed := range []string{"8266389061079214669", "0", "12345", "7"} {
		for k := range refStarts(t, strings.Replace(refWorldRegion, "8266389061079214669", seed, 1)) {
			id, at, _ := strings.Cut(k, "@")
			n[seed+" "+at] = append(n[seed+" "+at], id)
		}
	}
	for at, ids := range n {
		if len(ids) > 1 {
			t.Log(at, ids)
		}
	}
}

func TestZZTrace(t *testing.T) {
	v := os.Getenv("KREMENMC_TRACE")
	if v == "" {
		t.Skip()
	}
	var cx, cz int32
	fmt.Sscanf(v, "%d,%d", &cx, &cz)
	structure.EnableJigsaw("minecraft:pillager_outpost")
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	draws := structure.JigsawDraws()
	ss.StartsAt(cx, cz)
	t.Logf("draws %d", len(draws()))
	for _, l := range structure.JigsawTrace() {
		fmt.Println(l)
	}
}

func TestZZLootSeed(t *testing.T) {
	v := os.Getenv("KREMENMC_LOOT")
	if v == "" {
		t.Skip()
	}
	var cx, cz int32
	var want int64
	var key string
	fmt.Sscanf(v, "%d,%d,%d,%s", &cx, &cz, &want, &key)
	structure.EnableJigsaw("minecraft:pillager_outpost")
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	idx := ss.StructureIndex(key)
	d := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
	ds := d.SetDecorationSeed(refSeed, cx<<4, cz<<4)
	for step := int32(0); step < 11; step++ {
		for i := int32(0); i < 60; i++ {
			r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
			r.SetFeatureSeed(ds, i, step)
			for k := 0; k < 4000; k++ {
				if r.NextLong() == want {
					t.Logf("found step %d index %d (structure index %d) at nextLong #%d", step, i, idx, k)
				}
			}
		}
	}
}

func TestZZKeys(t *testing.T) {
	sub := os.Getenv("KREMENMC_KEYS")
	if sub == "" {
		t.Skip()
	}
	pk, _ := wgdata.Load()
	for _, reg := range []string{"worldgen/placed_feature", "worldgen/configured_feature", "worldgen/template_pool", "worldgen/processor_list", "worldgen/structure"} {
		for _, k := range pk.Keys(reg) {
			if strings.Contains(k, sub) {
				t.Log(reg, k)
			}
		}
	}
}

func TestZZTemplateBlocks(t *testing.T) {
	l := os.Getenv("KREMENMC_TBLOCKS")
	if l == "" {
		t.Skip()
	}
	cnt := map[string]int{}
	for _, n := range strings.Split(l, ",") {
		for _, s := range structure.TemplateStates(n) {
			cnt[s]++
		}
	}
	for k, v := range cnt {
		fmt.Println(v, k)
	}
}
