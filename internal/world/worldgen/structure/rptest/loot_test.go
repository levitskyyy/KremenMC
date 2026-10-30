package rptest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// TestTemplateChestLoot (KREMENMC_LOOTREF=1) compares, in every full
// reference chunk holding a chest whose LootTable matches the list, the
// chest's LootTable and LootTableSeed with kremenmc's full chunk.
func TestTemplateChestLoot(t *testing.T) {
	if os.Getenv("KREMENMC_LOOTREF") == "" {
		t.Skip("KREMENMC_LOOTREF not set")
	}
	tables := []string{"shipwreck", "igloo", "ruined_portal", "buried_treasure"}
	for _, w := range []string{"8266389061079214669-overworld", "0-overworld", "12345-overworld", "7-overworld"} {
		var seed int64
		fmt.Sscan(strings.TrimSuffix(w, "-overworld"), &seed)
		dir := filepath.Join(refRoot, w, "world/dimensions/minecraft/overworld/region")
		files, _ := filepath.Glob(filepath.Join(dir, "r.*.mca"))
		type chest struct {
			cx, cz, x, y, z int32
			lt              string
			seed            int64
		}
		var want []chest
		for _, f := range files {
			var rx, rz int32
			fmt.Sscanf(filepath.Base(f), "r.%d.%d.mca", &rx, &rz)
			r, err := anvil.OpenRegionReadOnly(dir, rx, rz)
			if err != nil {
				continue
			}
			for i := 0; i < 1024; i++ {
				if !r.Has(i&31, i>>5) {
					continue
				}
				b, err := r.Read(i&31, i>>5)
				if err != nil {
					continue
				}
				_, root, _, err := nbt.Unmarshal(b, false)
				if err != nil || root["Status"] != "minecraft:full" {
					continue
				}
				l, _ := root["block_entities"].(nbt.List)
				for _, it := range l.Items {
					be, _ := it.(nbt.Compound)
					lt, _ := be["LootTable"].(string)
					for _, tb := range tables {
						if strings.Contains(lt, tb) {
							s, _ := be["LootTableSeed"].(int64)
							want = append(want, chest{rx<<5 + int32(i&31), rz<<5 + int32(i>>5), be["x"].(int32), be["y"].(int32), be["z"].(int32), lt, s})
						}
					}
				}
			}
			r.Close()
		}
		if len(want) == 0 {
			continue
		}
		tr, _ := vanilla.NewTerrain("overworld", seed)
		st, _ := tr.Stages("overworld", seed)
		p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: st, Logf: func(string, ...any) {}})
		if err != nil {
			t.Fatal(err)
		}
		ok := 0
		for _, c := range want {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: c.cx, Z: c.cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			for _, be := range pc.Chunk.BlockEntities {
				if c.cx<<4+int32(be.X) == c.x && int32(be.Y) == c.y && c.cz<<4+int32(be.Z) == c.z {
					lt, _ := be.Data["LootTable"].(string)
					s, _ := be.Data["LootTableSeed"].(int64)
					got = fmt.Sprintf("%s %d", lt, s)
				}
			}
			if got == fmt.Sprintf("%s %d", c.lt, c.seed) {
				ok++
			} else {
				t.Logf("%s chest %d,%d,%d: want %s %d got %q", w, c.x, c.y, c.z, c.lt, c.seed, got)
			}
		}
		t.Logf("%s: %d/%d chests match", w, ok, len(want))
	}
}
