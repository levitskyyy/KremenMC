package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// mapWorld is an in-memory block store running the same neighbour shape
// updates as the region hook.
type mapWorld struct {
	m     map[api.BlockPos]data.StateID
	depth int
}

func (w *mapWorld) Block(x, y, z int) data.StateID {
	if s, ok := w.m[api.BlockPos{X: x, Y: y, Z: z}]; ok {
		return s
	}
	return data.Air
}

func (w *mapWorld) SetBlock(x, y, z int, s data.StateID) bool {
	old := w.Block(x, y, z)
	if old == s {
		return true
	}
	w.m[api.BlockPos{X: x, Y: y, Z: z}] = s
	if w.depth < world.MaxUpdateDepth {
		w.depth++
		updateNeighbours(w, x, y, z, old, s, nil)
		w.depth--
	}
	return true
}

func (w *mapWorld) reset() {
	w.m = map[api.BlockPos]data.StateID{}
	for x := -3; x <= 12; x++ {
		for z := -3; z <= 12; z++ {
			w.m[api.BlockPos{X: x, Y: 99, Z: z}] = data.MustState("minecraft:stone")
		}
	}
}

func atoiAll(t *testing.T, f []string) []int {
	out := make([]int, len(f))
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("bad int %q", s)
		}
		out[i] = n
	}
	return out
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// TestPlaceReference replays the placement script against the reference
// server's results (kremenmc.probe.place.Place): placement states, double
// slabs, stacking, connections, stair corners, chest pairing and survival
// breaks after neighbour updates.
func TestPlaceReference(t *testing.T) {
	in, _ := filepath.Abs(filepath.Join("testdata", "place_in.txt"))
	out := reftest.Golden(t, "place_out.txt", "kremenmc.probe.place.Place", in)
	var want []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "get ") {
			want = append(want, l)
		}
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	w := &mapWorld{}
	w.reset()
	n := 0
	for _, line := range strings.Split(script, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		switch f[0] {
		case "reset":
			w.reset()
		case "set":
			p := atoiAll(t, f[1:4])
			w.SetBlock(p[0], p[1], p[2], data.MustState(f[4]))
		case "break":
			p := atoiAll(t, f[1:4])
			w.SetBlock(p[0], p[1], p[2], data.Air)
		case "place":
			p := atoiAll(t, f[1:4])
			c := &placeCtx{r: w, item: f[11], clicked: api.BlockPos{X: p[0], Y: p[1], Z: p[2]}, face: dirByName(f[4]),
				cursor: api.Vec3{X: atof(f[5]), Y: atof(f[6]), Z: atof(f[7])}, yaw: float32(atof(f[8])), pitch: float32(atof(f[9])), sneak: f[10] == "1"}
			target, _, placed, up, upper, ok := resolvePlace(c, func(int) bool { return true })
			if !ok {
				t.Logf("%s: not placed", line)
				continue
			}
			w.SetBlock(target.X, target.Y, target.Z, placed)
			if upper != 0 {
				w.SetBlock(up.X, up.Y, up.Z, upper)
			}
		case "get":
			if n >= len(want) {
				t.Fatalf("more gets than golden lines")
			}
			p := atoiAll(t, f[1:4])
			wf := strings.Fields(want[n])
			n++
			ws, ok := data.ParseState(wf[4])
			if !ok {
				t.Errorf("unparsable golden state %s", wf[4])
				continue
			}
			if got := w.Block(p[0], p[1], p[2]); got != ws {
				t.Errorf("%v: got %s want %s", p, data.StateString(got), data.StateString(ws))
			}
		}
	}
	if n != len(want) {
		t.Errorf("compared %d of %d golden states", n, len(want))
	}
}

func TestFaceGeometry(t *testing.T) {
	cases := []struct {
		state  string
		sturdy [6]bool
		center uint8
	}{
		{"minecraft:stone", [6]bool{true, true, true, true, true, true}, geoCenterUp | geoCenterDown},
		{"minecraft:oak_slab[type=bottom,waterlogged=false]", [6]bool{true, false, false, false, false, false}, geoCenterDown},
		{"minecraft:oak_slab[type=top,waterlogged=false]", [6]bool{false, true, false, false, false, false}, geoCenterUp},
		{"minecraft:oak_fence[east=false,north=false,south=false,waterlogged=false,west=false]", [6]bool{}, geoCenterUp | geoCenterDown},
		{"minecraft:soul_sand", [6]bool{true, true, true, true, true, true}, geoCenterUp | geoCenterDown},
		{"minecraft:air", [6]bool{}, 0},
	}
	for _, c := range cases {
		st := data.MustState(c.state)
		for d := range 6 {
			if got := sturdy(st, d); got != c.sturdy[d] {
				t.Errorf("%s face %s sturdy=%v want %v", c.state, faceNames[d], got, c.sturdy[d])
			}
		}
		if got := geometry(st) & (geoCenterUp | geoCenterDown); got != c.center {
			t.Errorf("%s centre support %b want %b", c.state, got, c.center)
		}
	}
}
