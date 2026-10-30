package feature

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestDualNoise replays kremenmc.probe.feat_drip.Fn "dn" output for the
// minecraft:flower_meadow provider (y 64, every 20th point of a 13-block
// grid over ±1500).
func TestDualNoise(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	sp, err := rg.parseStateProvider(json.RawMessage(`"minecraft:flower_meadow"`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/dual_noise_meadow.txt")
	if err != nil {
		t.Fatal(err)
	}
	n, bad := 0, 0
	for _, line := range strings.Fields(string(b)) {
		pos, name, _ := strings.Cut(line, "=")
		xs, zs, _ := strings.Cut(pos, ",")
		x, _ := strconv.Atoi(xs)
		z, _ := strconv.Atoi(zs)
		n++
		if got := blockName(sp.State(nil, nil, Pos{x, 64, z})); got != name {
			bad++
			if bad < 5 {
				t.Errorf("%d,%d: got %s, want %s", x, z, got, name)
			}
		}
	}
	if bad > 0 || n == 0 {
		t.Errorf("%d/%d differ", bad, n)
	}
}
