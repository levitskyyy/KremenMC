package structure_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/server"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

var dimBounds = map[string]world.Dimension{"overworld": world.Overworld, "the_nether": world.Nether, "the_end": world.End}

// TestDimFullBoxes (KREMENMC_DFULL=dim:seed@x0,y0,z0,x1,y1,z1[;box...])
// is TestLegacyFullBoxes for any dimension: full kremenmc chunks (promoted
// with post-processing) against the reference world <seed>-<dim>, every
// block, block entity and generated entity id inside the boxes.
func TestDimFullBoxes(t *testing.T) {
	v := os.Getenv("KREMENMC_DFULL")
	if v == "" {
		t.Skip("KREMENMC_DFULL not set")
	}
	head, boxS, _ := strings.Cut(v, "@")
	dim, seedS, _ := strings.Cut(head, ":")
	var seed int64
	fmt.Sscan(seedS, &seed)
	d := dimBounds[dim]
	tr, err := vanilla.NewTerrain(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: d, Stages: stages, Logf: t.Logf, PostProcess: server.GenPostProcess})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	plains, _ := reg.BiomeByName("plains")
	dir := fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-%s/world/dimensions/minecraft/%s/region", seed, dim, dim)
	for _, bs := range strings.Split(boxS, ";") {
		var b [6]int32
		fmt.Sscanf(bs, "%d,%d,%d,%d,%d,%d", &b[0], &b[1], &b[2], &b[3], &b[4], &b[5])
		in := func(x, y, z int32) bool {
			return x >= b[0] && x <= b[3] && y >= b[1] && y <= b[4] && z >= b[2] && z <= b[5]
		}
		same, total, beSame, beTotal := 0, 0, 0, 0
		diff := map[string]int{}
		var ents, refEnts []string
		for cz := b[2] >> 4; cz <= b[5]>>4; cz++ {
			for cx := b[0] >> 4; cx <= b[3]>>4; cx++ {
				r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
				if err != nil {
					t.Skipf("no reference region: %v", err)
				}
				raw, err := r.Read(int(cx&31), int(cz&31))
				r.Close()
				if err != nil {
					t.Skipf("no reference chunk %d,%d", cx, cz)
				}
				_, root, _, err := nbt.Unmarshal(raw, false)
				if err != nil {
					t.Fatal(err)
				}
				rc, _, _ := anvil.DecodeChunk(root, d)
				if rc == nil {
					t.Fatalf("reference chunk %d,%d not full (%v)", cx, cz, root["Status"])
				}
				c := world.NewChunk(cx, cz, d, plains)
				if err := (gen.WorldGenerator{P: p}).GenerateContext(context.Background(), c); err != nil {
					t.Fatal(err)
				}
				for y := b[1]; y <= b[4]; y++ {
					for z := max(b[2], cz<<4); z <= min(b[5], cz<<4+15); z++ {
						for x := max(b[0], cx<<4); x <= min(b[3], cx<<4+15); x++ {
							ws, gs := rc.Block(int(x&15), int(y), int(z&15)), c.Block(int(x&15), int(y), int(z&15))
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
					if in(x, y, z) {
						want[[3]int32{x, y, z}] = beString(it)
					}
				}
				for _, be := range c.BlockEntities {
					k := [3]int32{cx<<4 + int32(be.X), int32(be.Y), cz<<4 + int32(be.Z)}
					w, ok := want[k]
					if !ok {
						continue
					}
					beTotal++
					g := beString(nbt.Compound{"id": be.ID, "LootTable": be.Data["LootTable"], "LootTableSeed": be.Data["LootTableSeed"], "spawn_data": be.Data["spawn_data"]})
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
				for _, e := range c.GenEntities {
					ents = append(ents, fmt.Sprint(e["id"]))
				}
				refEnts = append(refEnts, dimRefEntities(seed, dim, cx, cz)...)
			}
		}
		var ks []string
		for k := range diff {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return diff[ks[i]] > diff[ks[j]] || diff[ks[i]] == diff[ks[j]] && ks[i] < ks[j] })
		sort.Strings(ents)
		sort.Strings(refEnts)
		t.Logf("box %v: blocks %d/%d (%.3f%%), block entities %d/%d, entities kremenmc %v ref %v", b, same, total, 100*float64(same)/float64(total), beSame, beTotal, ents, refEnts)
		for i, k := range ks {
			if i < 20 || os.Getenv("KREMENMC_DFULL_ALL") != "" {
				t.Logf("  %4d %s", diff[k], k)
			}
		}
	}
}

func beString(c nbt.Compound) string {
	sd := ""
	if s, ok := c["spawn_data"].(nbt.Compound); ok {
		if e, ok := s["entity"].(nbt.Compound); ok {
			sd = fmt.Sprint(e["id"])
		}
	}
	return fmt.Sprintf("%v %v %v %s", c["id"], c["LootTable"], c["LootTableSeed"], sd)
}

// dimRefEntities lists the entity ids the reference stored for chunk cx,cz.
func dimRefEntities(seed int64, dim string, cx, cz int32) []string {
	dir := fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-%s/world/dimensions/minecraft/%s/entities", seed, dim, dim)
	r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
	if err != nil {
		return nil
	}
	defer r.Close()
	raw, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		return nil
	}
	_, root, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range opCompounds(root["Entities"]) {
		out = append(out, fmt.Sprint(e["id"]))
	}
	return out
}
