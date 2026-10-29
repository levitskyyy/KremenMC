package structure_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// structStepIndex is a structure's decoration step and its index among all
// data pack structures of that step in key order.
func structStepIndex(t *testing.T, key string) (int, int) {
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{"raw_generation", "lakes", "local_modifications", "underground_structures",
		"surface_structures", "strongholds", "underground_ores", "underground_decoration", "fluid_springs",
		"vegetal_decoration", "top_layer_modification"}
	keys := append([]string(nil), pk.Keys("worldgen/structure")...)
	sort.Strings(keys)
	pos := map[string]int{}
	for _, k := range keys {
		b, _ := pk.JSON("worldgen/structure", k)
		var h struct {
			Step string `json:"step"`
		}
		json.Unmarshal(b, &h)
		s := strings.TrimPrefix(h.Step, "minecraft:")
		if "minecraft:"+strings.TrimPrefix(k, "minecraft:") == key {
			for i, n := range steps {
				if n == s {
					return i, pos[s]
				}
			}
		}
		pos[s]++
	}
	t.Fatalf("no structure %s", key)
	return 0, 0
}

// writeScene writes a MineScene input: the kremenmc terrain-status blocks of
// the chunks around chunks (one chunk margin), y -64..maxY, and the start.
func writeScene(t *testing.T, outf string, seed int64, step, index int, start nbt.Compound, chunks [][2]int32, maxY int32) {
	rg := structure.Box{MinX: 1 << 30, MinY: -64, MinZ: 1 << 30, MaxX: -1 << 30, MaxY: min(maxY, 319), MaxZ: -1 << 30}
	for _, c := range chunks {
		rg.MinX, rg.MinZ = min(rg.MinX, (c[0]-1)<<4), min(rg.MinZ, (c[1]-1)<<4)
		rg.MaxX, rg.MaxZ = max(rg.MaxX, (c[0]+2)<<4-1), max(rg.MaxZ, (c[1]+2)<<4-1)
	}
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: func(string, ...any) {}})
	reg := data.Canonical()
	pcs := map[[2]int32]*world.Chunk{}
	for cx := rg.MinX >> 4; cx <= rg.MaxX>>4; cx++ {
		for cz := rg.MinZ >> 4; cz <= rg.MaxZ>>4; cz++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusTerrain)
			if err != nil {
				t.Fatal(err)
			}
			pcs[[2]int32{cx, cz}] = pc.Chunk
		}
	}
	pal := map[data.StateID]int{}
	var palList []data.StateID
	var rle strings.Builder
	last, run := -1, 0
	flush := func() {
		if run > 0 {
			fmt.Fprintf(&rle, " %d*%d", last, run)
		}
	}
	for y := rg.MinY; y <= rg.MaxY; y++ {
		for z := rg.MinZ; z <= rg.MaxZ; z++ {
			for x := rg.MinX; x <= rg.MaxX; x++ {
				s := pcs[[2]int32{x >> 4, z >> 4}].Block(int(x&15), int(y), int(z&15))
				i, ok := pal[s]
				if !ok {
					i = len(palList)
					pal[s] = i
					palList = append(palList, s)
				}
				if i == last {
					run++
				} else {
					flush()
					last, run = i, 1
				}
			}
		}
	}
	flush()
	f, err := os.Create(outf)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "SEED %d %d %d\n", seed, index, step)
	fmt.Fprintf(f, "REGION %d %d %d %d %d %d\n", rg.MinX, rg.MinY, rg.MinZ, rg.MaxX, rg.MaxY, rg.MaxZ)
	fmt.Fprintf(f, "PAL %d\n", len(palList))
	for _, s := range palList {
		fmt.Fprintln(f, reg.StateString(s))
	}
	fmt.Fprintf(f, "DATA%s\n", rle.String())
	fmt.Fprintf(f, "START %s\n", snbt(start))
	var cs []string
	for _, c := range chunks {
		cs = append(cs, fmt.Sprintf("%d,%d", c[0], c[1]))
	}
	fmt.Fprintf(f, "CHUNKS %s\n", strings.Join(cs, " "))
	t.Logf("scene %v, %d palette states, step %d index %d", rg, len(palList), step, index)
}

// TestRuinedPortalSceneDump (KREMENMC_RPSCENE_DUMP=out, KREMENMC_RPSCENE_SPEC=
// world@id@cx,cz) writes a MineScene input for a reference ruined portal
// start: every chunk its box touches.
func TestRuinedPortalSceneDump(t *testing.T) {
	outf := os.Getenv("KREMENMC_RPSCENE_DUMP")
	if outf == "" {
		t.Skip("KREMENMC_RPSCENE_DUMP not set")
	}
	spec := strings.Split(os.Getenv("KREMENMC_RPSCENE_SPEC"), "@")
	w, id := spec[0], spec[1]
	var seed int64
	fmt.Sscan(strings.TrimSuffix(w, "-overworld"), &seed)
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/region")
	start := ref[id+"@"+spec[2]]
	if start == nil {
		t.Fatalf("no start %s@%s", id, spec[2])
	}
	ch := start["Children"].(nbt.List).Items[0].(nbt.Compound)
	bb := ch["BB"].([]int32)
	var chunks [][2]int32
	for cz := bb[2] >> 4; cz <= bb[5]>>4; cz++ {
		for cx := bb[0] >> 4; cx <= bb[3]>>4; cx++ {
			chunks = append(chunks, [2]int32{cx, cz})
		}
	}
	step, index := structStepIndex(t, id)
	writeScene(t, outf, seed, step, index, start, chunks, bb[4]+12)
	t.Logf("piece %v", ch)
}
