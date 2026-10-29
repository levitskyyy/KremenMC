package carver

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
)

// TestPinnedMasks recomputes the masks of every chunk verified against the
// reference terrain dumps by TestOracleShape (pinned in testdata/masks.txt).
func TestPinnedMasks(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "masks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		seed int64
		dim  string
	}
	sets := map[key]*Set{}
	srcs := map[key]biome.Source{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	n := 0
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		seed, _ := strconv.ParseInt(f[0], 10, 64)
		cx, _ := strconv.Atoi(f[2])
		cz, _ := strconv.Atoi(f[3])
		k := key{seed, f[1]}
		if sets[k] == nil {
			if sets[k], err = NewSet(dimContext(k.dim), seed); err != nil {
				t.Fatal(err)
			}
			if srcs[k], err = biome.ForDimension(k.dim, seed); err != nil {
				t.Fatal(err)
			}
		}
		got := maskDigest(chunkMask(t, sets[k], srcs[k], int32(cx), int32(cz)))
		if want := f[4] + " " + f[5]; got != want {
			t.Errorf("%s: got %s", sc.Text(), got)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no pinned masks")
	}
}

func pipelineDigest(t *testing.T, workers int, order []world.ChunkPos) string {
	set, err := NewSet(dimCtx["overworld"], 12345)
	if err != nil {
		t.Fatal(err)
	}
	src, err := biome.ForDimension("overworld", 12345)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: 12345, Dim: world.Overworld, Workers: workers, Stages: []gen.Stage{&Stage{Set: set, Biomes: src, NewAquifer: func(int32, int32) Aquifer { return stoneAquifer{} }}}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	chunks := make(chan error, len(order))
	res := make([]*gen.ProtoChunk, len(order))
	for i, pos := range order {
		go func() {
			pc, err := p.Get(context.Background(), pos, gen.StatusTerrain)
			res[i] = pc
			chunks <- err
		}()
	}
	for range order {
		if err := <-chunks; err != nil {
			t.Fatal(err)
		}
	}
	h := map[world.ChunkPos]uint32{}
	for i, pos := range order {
		var buf bytes.Buffer
		c := res[i].Chunk
		for y := c.Dim.MinY; y < c.Dim.MinY+c.Dim.Height; y++ {
			for z := range 16 {
				for x := range 16 {
					fmt.Fprint(&buf, c.Block(x, y, z), ",")
				}
			}
		}
		h[pos] = crc32.ChecksumIEEE(buf.Bytes())
	}
	var out []string
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-3); x <= 3; x++ {
			out = append(out, strconv.FormatUint(uint64(h[world.ChunkPos{X: x, Z: z}]), 16))
		}
	}
	return strings.Join(out, " ")
}

// TestPipelineDeterminism: 1 vs many workers and reversed request order
// give identical chunks.
func TestPipelineDeterminism(t *testing.T) {
	var order []world.ChunkPos
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-3); x <= 3; x++ {
			order = append(order, world.ChunkPos{X: x, Z: z})
		}
	}
	rev := make([]world.ChunkPos, len(order))
	for i := range order {
		rev[i] = order[len(order)-1-i]
	}
	a := pipelineDigest(t, 1, order)
	b := pipelineDigest(t, 8, rev)
	if f := strings.Fields(a); strings.Count(a, f[0]) == len(f) {
		t.Fatal("nothing carved")
	}
	if a != b {
		t.Fatalf("pipeline results differ:\n1 worker: %s\n8 workers: %s", a, b)
	}
}

// stoneAquifer marks carved positions visibly in an empty chunk.
type stoneAquifer struct{}

func (stoneAquifer) Substance(int, int, int) (data.StateID, bool) {
	st, _ := data.Canonical().DefaultState("minecraft:stone")
	return st, true
}
