package biome

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world"
)

func fillHash(src Source, cx, cz int32, wd world.Dimension) uint64 {
	h := fnv.New64a()
	FillChunk(src, cx, cz, int32(wd.MinSection()), int32(wd.Sections()), func(qx, qy, qz int32, b string) {
		fmt.Fprintf(h, "%d %d %d %s;", qx, qy, qz, b)
	})
	return h.Sum64()
}

// TestPinned checks chunk biome fills against hashes recorded from fills
// that matched the vanilla BIOMES stage exactly (KREMENMC_PIN=1 with the
// oracle dumps present regenerates testdata/pinned.txt). The set includes
// chunks where the quart order and lookup memory change the result.
func TestPinned(t *testing.T) {
	f, err := os.Open("testdata/pinned.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dims := map[string]world.Dimension{"overworld": world.Overworld, "the_nether": world.Nether, "the_end": world.End}
	srcs := map[string]Source{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		seed, _ := strconv.ParseInt(p[0], 10, 64)
		cx, _ := strconv.Atoi(p[2])
		cz, _ := strconv.Atoi(p[3])
		k := p[0] + p[1]
		if srcs[k] == nil {
			srcs[k], err = ForDimension(p[1], seed)
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := fmt.Sprintf("%016x", fillHash(srcs[k], int32(cx), int32(cz), dims[p[1]])); got != p[4] {
			t.Errorf("%s chunk %d,%d: hash %s want %s", p[1], cx, cz, got, p[4])
		}
		n++
	}
	if n == 0 {
		t.Fatal("no pinned chunks")
	}
	t.Logf("%d chunks", n)
}
