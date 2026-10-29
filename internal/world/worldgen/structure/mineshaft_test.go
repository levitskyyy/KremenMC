package structure_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestMineshaftStarts compares every normal mineshaft start of the 4
// reference worlds piece for piece (all saved fields).
func TestMineshaftStarts(t *testing.T) {
	seeds := map[string]int64{"8266389061079214669-overworld": 8266389061079214669, "0-overworld": 0, "12345-overworld": 12345, "7-overworld": 7}
	starts, pieces, okStarts, okPieces := 0, 0, 0, 0
	for w, seed := range seeds {
		ref := refStarts(t, "../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/region")
		keys := make([]string, 0)
		for k, c := range ref {
			if c["id"] == "minecraft:mineshaft" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := ref[k]
			cx, cz := c["ChunkX"].(int32), c["ChunkZ"].(int32)
			want := c["Children"].(nbt.List).Items
			got := structure.MineshaftStartForTest(seed, cx, cz)
			starts++
			pieces += len(want)
			bad := len(got) != len(want)
			for i := 0; i < len(want) && i < len(got); i++ {
				// hps (spawner placed) is set by placement, not by the layout.
				gc, wc := got[i].Save(), nbt.Compound{}
				for k, v := range want[i].(nbt.Compound) {
					wc[k] = v
				}
				delete(gc, "hps")
				delete(wc, "hps")
				g, w := fmt.Sprint(gc), fmt.Sprint(wc)
				if g == w {
					okPieces++
				} else if !bad {
					bad = true
					t.Errorf("%s %s piece %d:\n kremenmc %s\n ref  %s", w[:0], k, i, g, w)
				}
			}
			if !bad {
				okStarts++
			} else if len(got) != len(want) {
				t.Errorf("%s: kremenmc %d pieces, reference %d", k, len(got), len(want))
			}
		}
	}
	t.Logf("mineshaft starts identical: %d/%d; pieces identical: %d/%d", okStarts, starts, okPieces, pieces)
}

// TestMineshaftRefBlocks (KREMENMC_MSREF=1)
// compares mineshaft blocks of full reference chunks with kremenmc.
func TestMineshaftRefBlocks(t *testing.T) {
	if os.Getenv("KREMENMC_MSREF") == "" {
		t.Skip("KREMENMC_MSREF not set")
	}
	structure.RegisterMineshaftForTest()
	const seed = 8266389061079214669
	w := "8266389061079214669-overworld"
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/region")
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: func(string, ...any) {}})
	reg := data.Canonical()
	ms := map[string]bool{"oak_planks": true, "oak_fence": true, "rail": true, "cobweb": true, "spawner": true, "oak_log": true, "wall_torch": true, "cave_air": true}
	n := 0
	keys := make([]string, 0)
	for k, c := range ref {
		if c["id"] == "minecraft:mineshaft" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := ref[k]
		cx, cz := c["ChunkX"].(int32), c["ChunkZ"].(int32)
		rc, _ := refChunkMaybe(t, w, cx, cz)
		if rc == nil {
			continue
		}
		n++
		if n > 3 {
			break
		}
		pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		same, total := 0, 0
		byName := map[string][2]int{}
		var ex []string
		for y := -64; y < 100; y++ {
			for z := 0; z < 16; z++ {
				for x := 0; x < 16; x++ {
					ws, gs := rc.Block(x, y, z), pc.Chunk.Block(x, y, z)
					wn, gn := reg.BlockOf(ws).Name, reg.BlockOf(gs).Name
					if !ms[wn] && !ms[gn] {
						continue
					}
					total++
					v := byName[wn]
					v[1]++
					if wn == gn && (wn == "oak_fence" || ws == gs) {
						same++
						v[0]++
					} else if len(ex) < 12 {
						ex = append(ex, fmt.Sprintf("%d,%d,%d kremenmc %s ref %s", x, y, z, reg.StateString(gs), reg.StateString(ws)))
					}
					byName[wn] = v
				}
			}
		}
		t.Logf("%s chunk %d,%d: mineshaft blocks %d/%d %v\n%v", k, cx, cz, same, total, byName, ex)
	}
}
