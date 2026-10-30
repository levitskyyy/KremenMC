package rptest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// portalBlocks are the block types a ruined portal writes.
var portalBlocks = []string{"netherrack", "magma_block", "obsidian", "crying_obsidian", "gold_block", "lava", "chest",
	"vine", "jungle_leaves", "stone_bricks", "mossy_stone_bricks", "cracked_stone_bricks", "chiseled_stone_bricks",
	"stone_brick_stairs", "mossy_stone_brick_stairs", "stone_brick_slab", "mossy_stone_brick_slab", "stone_slab",
	"smooth_stone_slab", "stone_brick_wall", "mossy_stone_brick_wall", "iron_bars"}

// TestPortalFullChunks (KREMENMC_RPFULL="seed:cx,cz,cx,cz;...") compares
// full kremenmc chunks with reference full chunks for the portal block
// types: per type, reference positions holding it that kremenmc matches.
func TestPortalFullChunks(t *testing.T) {
	spec := os.Getenv("KREMENMC_RPFULL")
	if spec == "" {
		t.Skip("KREMENMC_RPFULL not set")
	}
	reg := data.Canonical()
	want := map[string]bool{}
	for _, b := range portalBlocks {
		want[b] = true
	}
	tot, ok := map[string]int{}, map[string]int{}
	extra := map[string]int{}
	all, allOK := 0, 0
	for _, one := range strings.Split(spec, ";") {
		var seed int64
		var x0, z0, x1, z1 int32
		fmt.Sscanf(one, "%d:%d,%d,%d,%d", &seed, &x0, &z0, &x1, &z1)
		dir := filepath.Join(refRoot, fmt.Sprintf("%d-overworld", seed), "world/dimensions/minecraft/overworld/region")
		tr, err := vanilla.NewTerrain("overworld", seed)
		if err != nil {
			t.Fatal(err)
		}
		st, _ := tr.Stages("overworld", seed)
		p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: st, Logf: func(string, ...any) {}})
		if err != nil {
			t.Fatal(err)
		}
		for cz := z0; cz <= z1; cz++ {
			for cx := x0; cx <= x1; cx++ {
				r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
				if err != nil {
					t.Fatal(err)
				}
				b, err := r.Read(int(cx&31), int(cz&31))
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				_, root, _, err := nbt.Unmarshal(b, false)
				if err != nil {
					t.Fatal(err)
				}
				if s, _ := root["Status"].(string); s != "minecraft:full" {
					t.Logf("chunk %d,%d: reference status %s, skipped", cx, cz, s)
					continue
				}
				rc, err := anvil.DecodeRaw(root)
				if err != nil {
					t.Fatal(err)
				}
				pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
				if err != nil {
					t.Fatal(err)
				}
				for y := -64; y < 320; y++ {
					for z := 0; z < 16; z++ {
						for x := 0; x < 16; x++ {
							rs := strings.TrimPrefix(rc.Block(x, y, z), "minecraft:")
							gs := strings.TrimPrefix(reg.StateString(pc.Chunk.Block(x, y, z)), "minecraft:")
							rn, gn := strings.SplitN(rs, "[", 2)[0], strings.SplitN(gs, "[", 2)[0]
							all++
							if rs == gs {
								allOK++
							}
							if want[rn] {
								tot[rn]++
								if rs == gs {
									ok[rn]++
								}
							} else if want[gn] {
								extra[gn]++
							}
						}
					}
				}
			}
		}
	}
	names := make([]string, 0, len(tot))
	for n := range tot {
		names = append(names, n)
	}
	sort.Strings(names)
	sum, sumOK := 0, 0
	for _, n := range names {
		t.Logf("%-26s %5d/%5d  (+%d written where the reference has another block)", n, ok[n], tot[n], extra[n])
		sum += tot[n]
		sumOK += ok[n]
	}
	t.Logf("portal block types: %d/%d; all blocks %d/%d (%.3f%%)", sumOK, sum, allOK, all, 100*float64(allOK)/float64(all))
}
