package structure_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

const refRegionDir = "../../../../.refcache/26.3/worlds/8266389061079214669-overworld/world/dimensions/minecraft/overworld/region"

func refChunkRoot(t *testing.T, cx, cz int32) nbt.Compound {
	r, err := anvil.OpenRegionReadOnly(refRegionDir, cx>>5, cz>>5)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		t.Fatal(err)
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestDumpBox prints, for DUMP_BOX="x0,y0,z0,x1,y1,z1", every block that
// differs between the reference world and kremenmc at full status, and the
// reference block entities in the box.
func TestDumpBox(t *testing.T) {
	spec := os.Getenv("DUMP_BOX")
	if spec == "" {
		t.Skip()
	}
	var x0, y0, z0, x1, y1, z1 int32
	fmt.Sscanf(spec, "%d,%d,%d,%d,%d,%d", &x0, &y0, &z0, &x1, &y1, &z1)
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	st, _ := tr.Stages("overworld", refSeed)
	p, err := gen.New(gen.Config{Seed: refSeed, Dim: world.Overworld, Stages: st, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	for cz := z0 >> 4; cz <= z1>>4; cz++ {
		for cx := x0 >> 4; cx <= x1>>4; cx++ {
			root := refChunkRoot(t, cx, cz)
			rc, err := anvil.DecodeRaw(root)
			if err != nil {
				t.Fatal(err)
			}
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			diff := 0
			for y := y0; y <= y1; y++ {
				for z := max(z0, cz<<4); z <= min(z1, cz<<4+15); z++ {
					for x := max(x0, cx<<4); x <= min(x1, cx<<4+15); x++ {
						rb := strings.TrimPrefix(rc.Block(int(x&15), int(y), int(z&15)), "minecraft:")
						gb := strings.TrimPrefix(reg.StateString(pc.Chunk.Block(int(x&15), int(y), int(z&15))), "minecraft:")
						if os.Getenv("DUMP_ALL") != "" || rb != gb {
							t.Logf("%d,%d,%d ref=%s kremenmc=%s", x, y, z, rb, gb)
							diff++
						}
					}
				}
			}
			t.Logf("chunk %d,%d: %d diffs", cx, cz, diff)
			for _, be := range compoundsOf(root["block_entities"]) {
				x, _ := be["x"].(int32)
				y, _ := be["y"].(int32)
				z, _ := be["z"].(int32)
				if x >= x0 && x <= x1 && y >= y0 && y <= y1 && z >= z0 && z <= z1 {
					t.Logf("ref BE %v", be)
				}
			}
			for _, be := range pc.Chunk.BlockEntities {
				bx, bz := cx<<4+int32(be.X), cz<<4+int32(be.Z)
				if bx >= x0 && bx <= x1 && int32(be.Y) >= y0 && int32(be.Y) <= y1 && bz >= z0 && bz <= z1 {
					t.Logf("kremenmc BE %d,%d,%d %v", bx, be.Y, bz, be.Data)
				}
			}
		}
	}
}

func compoundsOf(v any) []nbt.Compound {
	l, _ := v.(nbt.List)
	var out []nbt.Compound
	for _, e := range l.Items {
		if c, ok := e.(nbt.Compound); ok {
			out = append(out, c)
		}
	}
	return out
}
