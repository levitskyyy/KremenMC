package carver

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// setOut records carved positions like the probe: y<<16 | z<<4 | x.
type setOut struct {
	minY, maxY int
	keys       []int64
}

func (s *setOut) Carve(x, y, z int) { s.keys = append(s.keys, int64(y)<<16|int64(z)<<4|int64(x)) }
func (s *setOut) MinY() int         { return s.minY }
func (s *setOut) MaxY() int         { return s.maxY }

func (s *setOut) digest() (int, string) {
	slices.Sort(s.keys)
	s.keys = slices.Compact(s.keys)
	h := crc32.NewIEEE()
	for _, k := range s.keys {
		h.Write([]byte(strconv.FormatInt(k, 10) + "\n"))
	}
	return len(s.keys), strconv.FormatUint(uint64(h.Sum32()), 16)
}

func refCases(n int) []string {
	r := rng.NewLegacy(20261028)
	out := make([]string, n)
	for i := range out {
		sx, sz := r.NextIntn(2001)-1000, r.NextIntn(2001)-1000
		dx, dz := r.NextIntn(9)-4, r.NextIntn(9)-4
		if i%3 != 0 {
			dx, dz = r.NextIntn(3)-1, r.NextIntn(3)-1
		}
		out[i] = fmt.Sprintf("%d:%d:%d:%d:%d", r.NextLong(), sx+dx, sz+dz, sx, sz)
	}
	return out
}

var dimCtx = map[string]Context{
	"overworld": {MinY: -64, Height: 384, SeaLevel: 63},
	"nether":    {MinY: 0, Height: 128, SeaLevel: 32},
}

// TestCarveReference compares carved sets with the reference carvers for
// random source/target chunk pairs (every case carves, ignoring IsStart,
// whose draw is still made first).
func TestCarveReference(t *testing.T) {
	p, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	cases := refCases(1000)
	caseFile := filepath.Join(t.TempDir(), "cases.txt")
	if err := os.WriteFile(caseFile, []byte(strings.Join(cases, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs := func(f string) string { a, _ := filepath.Abs(filepath.Join("testdata", f)); return a }
	for _, tc := range []struct{ name, carver, dim string }{
		{"cave", "minecraft:cave", "overworld"},
		{"cave_extra_underground", "minecraft:cave_extra_underground", "overworld"},
		{"nether_cave", "minecraft:nether_cave", "nether"},
		{"canyon", "minecraft:canyon", "overworld"},
		{"cave_custom", "@" + abs("cave_custom.json"), "overworld"},
		{"canyon_custom", "@" + abs("canyon_custom.json"), "nether"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var src []byte
			if strings.HasPrefix(tc.carver, "@") {
				src, err = os.ReadFile(tc.carver[1:])
			} else {
				var ok bool
				src, ok = p.JSON("worldgen/carver", tc.carver)
				if !ok {
					err = fmt.Errorf("no carver %s", tc.carver)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			c, err := Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			ctx := dimCtx[tc.dim]
			golden := reftest.Golden(t, "carve_"+tc.name+".txt", "kremenmc.probe.carver.Trace", "set", tc.carver, tc.dim, "@"+caseFile)
			sc := bufio.NewScanner(bytes.NewReader(golden))
			n, bad := 0, 0
			for sc.Scan() {
				if n >= len(cases) {
					n++
					break
				}
				var v [5]int64
				for i, s := range strings.Split(cases[n], ":") {
					v[i], _ = strconv.ParseInt(s, 10, 64)
				}
				o := &setOut{minY: ctx.MinY, maxY: ctx.MinY + ctx.Height - 1}
				r := rng.NewLegacy(v[0])
				start := c.IsStart(r)
				c.Carve(ctx, r, int32(v[1]), int32(v[2]), int32(v[3]), int32(v[4]), o)
				cnt, crc := o.digest()
				got := fmt.Sprintf("%t true %d %s", start, cnt, crc)
				if got != sc.Text() {
					bad++
					if bad <= 5 {
						t.Errorf("case %s:\n got %s\nwant %s", cases[n], got, sc.Text())
					}
				}
				n++
			}
			if n != len(cases) {
				t.Fatalf("golden has %d cases, want %d", n, len(cases))
			}
			if bad > 0 {
				t.Fatalf("%d/%d cases differ", bad, n)
			}
		})
	}
}
