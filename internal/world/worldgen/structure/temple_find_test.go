package structure_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestFindSpreadCandidates (KREMENMC_FIND=seed@set@radius) lists a structure
// set's potential chunks within radius cells with kremenmc's biome at the
// chunk centre (surface y), for picking probe chunks.
func TestFindSpreadCandidates(t *testing.T) {
	v := os.Getenv("KREMENMC_FIND")
	if v == "" {
		t.Skip("KREMENMC_FIND not set")
	}
	p := strings.Split(v, "@")
	var seed int64
	var rad int32
	fmt.Sscan(p[0], &seed)
	fmt.Sscan(p[2], &rad)
	sets, err := structure.LoadSets()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	pr := vanilla.StructureProbeForTest(tr)
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	for _, s := range sets {
		if s.Key != "minecraft:"+p[1] {
			continue
		}
		sp := s.Placement.Spacing
		for gz := -rad; gz <= rad; gz++ {
			for gx := -rad; gx <= rad; gx++ {
				cx, cz := s.Placement.PotentialChunk(seed, gx*sp, gz*sp)
				if !s.Placement.IsSpreadChunk(seed, cx, cz) {
					continue
				}
				x, z := cx<<4+8, cz<<4+8
				h := pr.Height(x, z, "WORLD_SURFACE_WG")
				b := pr.Biome(x, h, z)
				fmt.Printf("CAND %d %d %s h=%d\n", cx, cz, reg.Biomes[b].Name, h)
				if os.Getenv("KREMENMC_FIND_STARTS") != "" {
					sts, _ := ss.StartsAt(cx, cz)
					for _, st := range sts {
						fmt.Printf("START %s %d %d %v\n", st.ID, cx, cz, st.Box())
					}
				}
			}
		}
	}
}

// TestStartSceneDump (KREMENMC_SSCENE_DUMP=out, KREMENMC_SSCENE_START=
// seed@file with one start SNBT, KREMENMC_SSCENE_MAXY=y) writes an RPScene
// input for a probe start (kremenmc.probe.temple.TStarts): kremenmc
// terrain-status blocks of every chunk its box touches plus one chunk.
func TestStartSceneDump(t *testing.T) {
	outf := os.Getenv("KREMENMC_SSCENE_DUMP")
	if outf == "" {
		t.Skip("KREMENMC_SSCENE_DUMP not set")
	}
	seedS, file, _ := strings.Cut(os.Getenv("KREMENMC_SSCENE_START"), "@")
	var seed int64
	fmt.Sscan(seedS, &seed)
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	src := regexp.MustCompile(`([{,])([A-Za-z0-9_]+):`).ReplaceAllString(strings.TrimSpace(string(b)), `$1"$2":`)
	start := parseSNBT(t, src).(nbt.Compound)
	id, _ := start["id"].(string)
	var bb structure.Box
	for i, it := range start["Children"].(nbt.List).Items {
		a := it.(nbt.Compound)["BB"].([]int32)
		pb := structure.Box{MinX: a[0], MinY: a[1], MinZ: a[2], MaxX: a[3], MaxY: a[4], MaxZ: a[5]}
		if i == 0 {
			bb = pb
		} else {
			bb = bb.Encapsulate(pb)
		}
	}
	var chunks [][2]int32
	for cz := bb.MinZ >> 4; cz <= bb.MaxZ>>4; cz++ {
		for cx := bb.MinX >> 4; cx <= bb.MaxX>>4; cx++ {
			chunks = append(chunks, [2]int32{cx, cz})
		}
	}
	maxY := bb.MaxY + 12
	if v := os.Getenv("KREMENMC_SSCENE_MAXY"); v != "" {
		fmt.Sscan(v, &maxY)
	}
	step, index := structStepIndex(t, id)
	writeScene(t, outf, seed, step, index, start, chunks, maxY)
}
