package structure_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/server"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestLegacyFullBoxes (KREMENMC_LFULL=seed@x0,y0,z0,x1,y1,z1[;box...])
// generates full kremenmc chunks and compares every block and block entity
// inside the boxes with the reference world <seed>-overworld.
func TestLegacyFullBoxes(t *testing.T) {
	v := os.Getenv("KREMENMC_LFULL")
	if v == "" {
		t.Skip("KREMENMC_LFULL not set")
	}
	if keys := os.Getenv("KREMENMC_LFULL_ENABLE"); keys != "" {
		structure.EnableLegacy(strings.Split(keys, ",")...)
	}
	seedS, boxS, _ := strings.Cut(v, "@")
	var seed int64
	fmt.Sscan(seedS, &seed)
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf, PostProcess: server.GenPostProcess})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	for _, bs := range strings.Split(boxS, ";") {
		var b [6]int32
		fmt.Sscanf(bs, "%d,%d,%d,%d,%d,%d", &b[0], &b[1], &b[2], &b[3], &b[4], &b[5])
		same, total := 0, 0
		diff := map[string]int{}
		beSame, beTotal := 0, 0
		for cz := b[2] >> 4; cz <= b[5]>>4; cz++ {
			for cx := b[0] >> 4; cx <= b[3]>>4; cx++ {
				plains, _ := reg.BiomeByName("plains")
				pc := struct{ Chunk *world.Chunk }{world.NewChunk(cx, cz, world.Overworld, plains)}
				if err := (gen.WorldGenerator{P: p}).GenerateContext(context.Background(), pc.Chunk); err != nil {
					t.Fatal(err)
				}
				rc, root := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
				for y := b[1]; y <= b[4]; y++ {
					for z := max(b[2], cz<<4); z <= min(b[5], cz<<4+15); z++ {
						for x := max(b[0], cx<<4); x <= min(b[3], cx<<4+15); x++ {
							ws, gs := rc.Block(int(x&15), int(y), int(z&15)), pc.Chunk.Block(int(x&15), int(y), int(z&15))
							total++
							if ws == gs {
								same++
							} else {
								diff[reg.StateString(gs)+" -> ref "+reg.StateString(ws)]++
							}
						}
					}
				}
				want := map[[3]int32]string{}
				for _, it := range opCompounds(root["block_entities"]) {
					x, _ := it["x"].(int32)
					y, _ := it["y"].(int32)
					z, _ := it["z"].(int32)
					if x >= b[0] && x <= b[3] && y >= b[1] && y <= b[4] && z >= b[2] && z <= b[5] {
						want[[3]int32{x, y, z}] = fmt.Sprintf("%v %v %v", it["id"], it["LootTable"], it["LootTableSeed"])
					}
				}
				for _, be := range pc.Chunk.BlockEntities {
					k := [3]int32{cx<<4 + int32(be.X), int32(be.Y), cz<<4 + int32(be.Z)}
					w, ok := want[k]
					if !ok {
						continue
					}
					beTotal++
					g := fmt.Sprintf("%v %v %v", be.ID, be.Data["LootTable"], be.Data["LootTableSeed"])
					if g == w {
						beSame++
					} else {
						t.Logf("BE %v: ref %s kremenmc %s", k, w, g)
					}
					delete(want, k)
				}
				for k, w := range want {
					beTotal++
					t.Logf("BE %v missing: ref %s", k, w)
				}
			}
		}
		var ks []string
		for k := range diff {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return diff[ks[i]] > diff[ks[j]] })
		t.Logf("box %v: blocks %d/%d (%.3f%%), block entities %d/%d", b, same, total, 100*float64(same)/float64(total), beSame, beTotal)
		for i, k := range ks {
			if i < 12 || os.Getenv("KREMENMC_LFULL_ALL") != "" {
				t.Logf("  %4d %s", diff[k], k)
			}
		}
	}
}
