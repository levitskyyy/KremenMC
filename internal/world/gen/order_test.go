package gen

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// markerStages: every chunk's feature step appends a wool block coding its
// own decoration rank on top of a marker column at local (0, z=0) in each
// chunk of its 3x3 neighbourhood, reading the column to find the top.
// The column thus records the order in which a chunk's 3x3 decorated.
func markerStages(farAir *atomic.Int64) []Stage {
	terrain := StageFunc{S: StatusTerrain, F: func(ctx *StageContext) error {
		ctx.Chunk().SetBlock(0, 0, 0, bStone)
		return nil
	}}
	features := StageFunc{S: StatusFeatures, F: func(ctx *StageContext) error {
		rank := DecorationRank(ctx.Pos())
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				x, z := ctx.MinBlockX()+16*dx, ctx.MinBlockZ()+16*dz
				y := 1
				for ctx.Block(x, y, z) != data.Air {
					y++
				}
				if !ctx.SetBlock(x, y, z, wool[rank]) {
					return errFar
				}
			}
		}
		// distance 2 is guaranteed only structure_starts: blocks read as air
		// even when that chunk is further along
		if ctx.Block(ctx.MinBlockX()+32, 0, ctx.MinBlockZ()) == data.Air {
			farAir.Add(1)
		}
		return nil
	}}
	return []Stage{terrain, features}
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errFar = constErr("write refused")

// TestDecorationOrderPinned pins the chosen semantics: within any 3x3
// neighbourhood the nine feature steps run in ascending DecorationRank,
// whatever the request order or worker count.
func TestDecorationOrderPinned(t *testing.T) {
	for _, w := range []int{1, 8} {
		for name, ps := range orders(world.ChunkPos{X: -3, Z: 5}, 3) {
			var farAir atomic.Int64
			p, err := New(Config{Seed: 1, Dim: testDim, Workers: w, Stages: markerStages(&farAir)})
			if err != nil {
				t.Fatal(err)
			}
			// pre-generate a far chunk to full: it must stay invisible to
			// distance-2 reads
			if _, err := p.Get(context.Background(), world.ChunkPos{X: 10, Z: 5}, StatusFull); err != nil {
				t.Fatal(err)
			}
			got := requestAll(t, p, ps, StatusInitializeLight)
			for pos, c := range got {
				for i := range 9 {
					if b := c.Block(0, 1+i, 0); b != wool[i] {
						t.Fatalf("%s w%d: chunk %v marker %d is %s, want rank %d", name, w, pos, i, data.Canonical().StateString(b), i)
					}
				}
			}
			n := p.Stats().Steps[StatusFeatures]
			if farAir.Load() != n {
				t.Errorf("distance-2 block read was not air in %d of %d steps", n-farAir.Load(), n)
			}
			p.Close()
		}
	}
}

// TestWriteZone: block writes outside the step's write radius are dropped
// and counted; reads outside the dependency radius are counted.
func TestWriteZone(t *testing.T) {
	p, err := New(Config{Seed: 1, Dim: testDim, Workers: 2, Stages: []Stage{
		StageFunc{S: StatusTerrain, F: func(ctx *StageContext) error {
			if !ctx.SetBlock(ctx.MinBlockX()+3, 5, ctx.MinBlockZ()+3, bStone) {
				t.Error("centre write refused at terrain")
			}
			if ctx.SetBlock(ctx.MinBlockX()+16, 5, ctx.MinBlockZ(), bStone) {
				t.Error("terrain wrote into a neighbour")
			}
			return nil
		}},
		StageFunc{S: StatusFeatures, F: func(ctx *StageContext) error {
			if !ctx.SetBlock(ctx.MinBlockX()-1, 6, ctx.MinBlockZ()+16, bDirt) {
				t.Error("features refused a radius-1 write")
			}
			if ctx.SetBlock(ctx.MinBlockX()+32, 6, ctx.MinBlockZ(), bDirt) {
				t.Error("features wrote at distance 2")
			}
			if _, _, ok := ctx.Neighbour(ctx.Pos().X+9, ctx.Pos().Z); ok {
				t.Error("neighbour beyond radius 8 visible")
			}
			return nil
		}},
		StageFunc{S: StatusLight, F: func(ctx *StageContext) error {
			if ctx.SetBlock(ctx.MinBlockX(), 7, ctx.MinBlockZ(), bDirt) {
				t.Error("light step wrote a block (write radius -1)")
			}
			return nil
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c, err := p.Get(context.Background(), world.ChunkPos{}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	if c.Block(3, 5, 3) != bStone || c.Block(15, 6, 0) != bDirt {
		t.Error("allowed writes missing")
	}
	st := p.Stats()
	// per centre: terrain 1 far write + features 1 + light 1; features also run in neighbours
	if st.FarWrites == 0 || st.FarReads == 0 {
		t.Errorf("violations not counted: %+v", st)
	}
}
