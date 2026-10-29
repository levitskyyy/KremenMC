package feature

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestPlaceMoreGolden replays testdata/place_more.txt (tools/refprobe
// kremenmc.probe.feat_ore.Place): the world-independent modifiers cuboid,
// randomly_selected and fixed_placement, emitted positions and next long.
func TestPlaceMoreGolden(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/place_more.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	n := 0
	for sc.Scan() {
		in, want, _ := strings.Cut(sc.Text(), " => ")
		fs := strings.SplitN(in, " ", 6)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		y, _ := strconv.Atoi(fs[3])
		z, _ := strconv.Atoi(fs[4])
		m, err := rg.parseModifier(json.RawMessage(fs[5]))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		var out []string
		m.Apply(&placeCtx{}, r, Pos{x, y, z}, func(p Pos) { out = append(out, fmt.Sprintf("%d,%d,%d", p.X, p.Y, p.Z)) })
		got := strings.Join(out, " ") + " | " + strconv.FormatInt(r.NextLong(), 10)
		if got != want {
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no cases")
	}
}
