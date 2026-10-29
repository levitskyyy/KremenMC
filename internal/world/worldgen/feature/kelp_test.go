package feature

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// waterRegion is the feat_misc.Place setup: stone at -62, sand at -61,
// water from -60 to top (none for "air"), air above.
type waterRegion struct {
	m   map[Pos]data.StateID
	top int
}

func (w *waterRegion) setup(y int) data.StateID {
	switch {
	case y == -62:
		s, _ := data.DefaultState("stone")
		return s
	case y == -61:
		s, _ := data.DefaultState("sand")
		return s
	case y >= -60 && y <= w.top:
		s, _ := data.DefaultState("water")
		return s
	}
	return data.Air
}
func (w *waterRegion) Dim() world.Dimension { return world.Overworld }
func (w *waterRegion) Block(x, y, z int) data.StateID {
	if s, ok := w.m[Pos{x, y, z}]; ok {
		return s
	}
	return w.setup(y)
}
func (w *waterRegion) SetBlock(x, y, z int, s data.StateID) bool { w.m[Pos{x, y, z}] = s; return true }
func (w *waterRegion) Height(world.Heightmap, int, int) int      { return -60 }
func (w *waterRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("warm_ocean")
	return b, true
}

// TestFeatMiscPlace replays testdata/feat_misc_place.txt (tools/refprobe
// kremenmc.probe.feat_misc.Place): blocks written and the random's next long.
func TestFeatMiscPlace(t *testing.T) { replayPlace(t, "testdata/feat_misc_place.txt") }

func replayPlace(t *testing.T, file string) {
	t.Helper()
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<22)
	bad := 0
	for sc.Scan() {
		in, want, _ := strings.Cut(sc.Text(), "\t")
		fs := strings.SplitN(in, " ", 5)
		top := -100
		if strings.HasPrefix(fs[0], "water") {
			top, _ = strconv.Atoi(fs[0][5:])
		}
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		z, _ := strconv.Atoi(fs[3])
		feat, err := rg.parseFeature(json.RawMessage(fs[4]))
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		reg := &waterRegion{m: map[Pos]data.StateID{}, top: top}
		lv := &Level{rg: reg}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		ok := feat.Place(&placeCtx{lv: lv}, r, Pos{x, -60, z})
		var b strings.Builder
		b.WriteString(strconv.FormatBool(ok))
		for y := -62; y <= -20; y++ {
			for dx := -12; dx <= 12; dx++ {
				for dz := -12; dz <= 12; dz++ {
					s, set := reg.m[Pos{x + dx, y, z + dz}]
					if set && s != reg.setup(y) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, y, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		got := b.String()
		if got != want {
			bad++
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
	}
	if bad > 0 {
		t.Logf("%s: %d mismatches", file, bad)
	}
}
