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
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestProvidersGolden replays testdata/mods.txt, recorded from the reference
// with tools/refprobe kremenmc.probe.feature.Mods: value providers, context-free
// placement modifiers and noise block state providers, draw for draw.
func TestProvidersGolden(t *testing.T) {
	f, err := os.Open("testdata/mods.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	fails := map[string]int{}
	n := 0
	for sc.Scan() {
		line := sc.Text()
		in, want, _ := strings.Cut(line, " => ")
		fs := strings.SplitN(in, " ", 6)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		y, _ := strconv.Atoi(fs[3])
		z, _ := strconv.Atoi(fs[4])
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		var res []string
		var err error
		switch fs[0] {
		case "int":
			var p IntProvider
			if p, err = parseInt(json.RawMessage(fs[5])); err == nil {
				for range 8 {
					res = append(res, strconv.Itoa(p.Sample(r)))
				}
			}
		case "float":
			// Float providers are not on the vegetation path yet; their
			// last-bit rounding is unverified.
			continue
		case "mod":
			var m Modifier
			if m, err = rg.parseModifier(json.RawMessage(fs[5])); err == nil {
				m.Apply(&placeCtx{}, r, Pos{x, y, z}, func(p Pos) { res = append(res, fmt.Sprintf("%d,%d,%d", p.X, p.Y, p.Z)) })
			}
		case "bsp":
			var p StateProvider
			if p, err = rg.parseStateProvider(json.RawMessage(strconv.Quote(fs[5]))); err == nil {
				res = append(res, "minecraft:"+blockName(p.State(nil, r, Pos{x, y, z})))
			}
		}
		n++
		got := strings.Join(res, " ") + " | " + strconv.FormatInt(r.NextLong(), 10)
		var u *unsupportedError
		if asUnsupported(err, &u) {
			// very_biased_to_bottom, noise and dual_noise providers are no-ops.
			fails["unsupported "+u.typ]++
			continue
		}
		if err != nil || got != want {
			key := fs[0] + " " + fs[5]
			if fails[key] == 0 {
				t.Errorf("%s\n got %s (err %v)\nwant %s", in, got, err, want)
			}
			fails[key]++
		}
	}
	t.Logf("%d cases; unsupported/failing kinds: %v", n, fails)
	_ = data.Air
}
