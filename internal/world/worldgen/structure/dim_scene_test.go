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
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// writeSceneDim is writeScene for any dimension: the terrain-status blocks
// of the chunks around chunks, from the dimension's bottom to maxY.
func writeSceneDim(t *testing.T, dim string, outf string, seed int64, step, index int, start nbt.Compound, chunks [][2]int32, maxY int32) {
	rg := structure.Box{MinX: 1 << 30, MinY: int32(dimBounds[dim].MinY), MinZ: 1 << 30, MaxX: -1 << 30, MaxY: min(maxY, 319), MaxZ: -1 << 30}
	for _, c := range chunks {
		rg.MinX, rg.MinZ = min(rg.MinX, (c[0]-1)<<4), min(rg.MinZ, (c[1]-1)<<4)
		rg.MaxX, rg.MaxZ = max(rg.MaxX, (c[0]+2)<<4-1), max(rg.MaxZ, (c[1]+2)<<4-1)
	}
	tr, _ := vanilla.NewTerrain(dim, seed)
	stages, _ := tr.Stages(dim, seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: dimBounds[dim], Stages: stages, Logf: func(string, ...any) {}})
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


// TestDimSceneDump (KREMENMC_DSCENE_DUMP=out, KREMENMC_DSCENE_SPEC=
// dim:seed@id@cx,cz[@maxY]) writes a scene input for a start of the
// reference world <seed>-<dim>: every chunk its box touches.
func TestDimSceneDump(t *testing.T) {
	outf := os.Getenv("KREMENMC_DSCENE_DUMP")
	if outf == "" {
		t.Skip("KREMENMC_DSCENE_DUMP not set")
	}
	spec := strings.Split(os.Getenv("KREMENMC_DSCENE_SPEC"), "@")
	dim, seedS, _ := strings.Cut(spec[0], ":")
	var seed int64
	fmt.Sscan(seedS, &seed)
	ref := refStarts(t, fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-%s/world/dimensions/minecraft/%s/region", seed, dim, dim))
	id := spec[1]
	start := ref[id+"@"+spec[2]]
	if start == nil {
		t.Fatalf("no start %s@%s", id, spec[2])
	}
	b := structure.Box{MinX: 1 << 30, MinY: 1 << 30, MinZ: 1 << 30, MaxX: -1 << 30, MaxY: -1 << 30, MaxZ: -1 << 30}
	for _, it := range start["Children"].(nbt.List).Items {
		bb := it.(nbt.Compound)["BB"].([]int32)
		b = b.Encapsulate(structure.Box{MinX: bb[0], MinY: bb[1], MinZ: bb[2], MaxX: bb[3], MaxY: bb[4], MaxZ: bb[5]})
	}
	var chunks [][2]int32
	for cz := b.MinZ >> 4; cz <= b.MaxZ>>4; cz++ {
		for cx := b.MinX >> 4; cx <= b.MaxX>>4; cx++ {
			chunks = append(chunks, [2]int32{cx, cz})
		}
	}
	maxY := b.MaxY + 12
	if len(spec) > 3 {
		fmt.Sscan(spec[3], &maxY)
	}
	step, index := structStepIndex(t, id)
	writeSceneDim(t, dim, outf, seed, step, index, start, chunks, min(maxY, int32(dimBounds[dim].MinY+dimBounds[dim].Height-1)))
}
