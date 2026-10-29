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

// TestHeightsGolden replays testdata/feat_heights.txt (tools/refprobe
// kremenmc.probe.feat_ore.Place, height_range with a real PlacementContext on
// the overworld, minY -64, height 384).
func TestHeightsGolden(t *testing.T) {
	f, err := os.Open("testdata/feat_heights.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		in, want, _ := strings.Cut(sc.Text(), " => ")
		fs := strings.SplitN(in, " ", 6)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		z, _ := strconv.Atoi(fs[4])
		var m struct {
			Height json.RawMessage `json:"height"`
		}
		if err := json.Unmarshal([]byte(fs[5]), &m); err != nil {
			t.Fatal(err)
		}
		h, err := parseHeight(m.Height)
		if err != nil {
			t.Fatal(err)
		}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		y := h.Sample(r, 0, 384)
		got := fmt.Sprintf("%d,%d,%d | %d", x, y, z, r.NextLong())
		if got != want {
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no cases")
	}
}
