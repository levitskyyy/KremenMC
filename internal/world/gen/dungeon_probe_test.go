package gen_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// TestDungeonProbe generates full chunks through the vanilla pipeline and
// prints their spawner/chest block entities and the blocks of a column of
// interest. Opt-in: KREMENMC_DUNGEON="seed x,y,z [x,y,z...]".
func TestDungeonProbe(t *testing.T) {
	spec := strings.Fields(os.Getenv("KREMENMC_DUNGEON"))
	if len(spec) < 2 {
		t.Skip("set KREMENMC_DUNGEON=\"seed x,y,z ...\"")
	}
	seed, _ := strconv.ParseInt(spec[0], 10, 64)
	st := anvil.NewStorage(t.TempDir(), world.Overworld)
	p, err := vanillaPipelineSeed(seed, 0, st)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, s := range spec[1:] {
		var x, y, z int
		fmt.Sscanf(s, "%d,%d,%d", &x, &y, &z)
		pc, err := p.Get(context.Background(), world.ChunkPos{X: int32(x >> 4), Z: int32(z >> 4)}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("chunk %d,%d target %d,%d,%d\n", x>>4, z>>4, x, y, z)
		for _, be := range pc.Chunk.BlockEntities {
			fmt.Printf("  be %s at %d,%d,%d %v\n", be.ID, int(pc.X)*16+int(be.X), be.Y, int(pc.Z)*16+int(be.Z), be.Data)
		}
		for dy := 5; dy >= -2; dy-- {
			fmt.Printf("  y=%d\n", y+dy)
			for dz := -5; dz <= 5; dz++ {
				var row []string
				for dx := -5; dx <= 5; dx++ {
					bx, bz := x+dx, z+dz
					var name string
					if bx>>4 == int(pc.X) && bz>>4 == int(pc.Z) {
						name = data.StateString(pc.Chunk.Block(bx&15, y+dy, bz&15))
					} else {
						name = "?"
					}
					name = strings.TrimPrefix(name, "minecraft:")
					if i := strings.IndexByte(name, '['); i >= 0 {
						name = name[:i]
					}
					row = append(row, fmt.Sprintf("%-6.6s", name))
				}
				fmt.Printf("   %s\n", strings.Join(row, " "))
			}
		}
	}
}

func vanillaPipelineSeed(seed int64, workers int, st *anvil.Storage) (*gen.Pipeline, error) {
	return vanillaPipeline(seed, workers, st, func(string, ...any) {})
}
