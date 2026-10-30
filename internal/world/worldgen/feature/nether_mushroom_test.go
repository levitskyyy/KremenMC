package feature

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// TestDarkMushroomFloor checks darkMushroomFloor against the blocks
// kremenmc.probe.feat_nether.Dark reports at brightness 0 (default states).
func TestDarkMushroomFloor(t *testing.T) {
	f, err := os.Open("testdata/mushroom_dark.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	rows := 0
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 2 || fs[0] != "light=0" {
			continue
		}
		rows++
		want := map[string]bool{}
		for _, n := range fs[2:] {
			want[n] = true
		}
		for _, b := range data.Canonical().Blocks {
			n := strings.TrimPrefix(b.Name, "minecraft:")
			st, _ := data.DefaultState(n)
			if got := darkMushroomFloor(st) || overridesMushroom(n); got != want[n] {
				t.Errorf("%s on %s: got %v, want %v", fs[1], n, got, want[n])
			}
		}
	}
	if rows != 2 {
		t.Fatalf("%d light=0 rows", rows)
	}
}

func overridesMushroom(n string) bool {
	switch n {
	case "mycelium", "podzol", "crimson_nylium", "warped_nylium":
		return true
	}
	return false
}
