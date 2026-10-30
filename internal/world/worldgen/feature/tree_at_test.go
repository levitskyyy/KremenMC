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

// shiftedRegion is mapRegion with its ground at oy-1 (grass) and oy-2 (dirt).
type shiftedRegion struct {
	m      map[Pos]data.StateID
	oy     int
	cx, cz int
}

func (m *shiftedRegion) Dim() world.Dimension { return world.Overworld }
func (m *shiftedRegion) Block(x, y, z int) data.StateID {
	if s, ok := m.m[Pos{x, y, z}]; ok {
		return s
	}
	if (abs(x-m.cx) > 12 || abs(z-m.cz) > 12) && y <= 3 {
		// The probe level outside the reset box: solid up to its surface.
		s, _ := data.DefaultState("stone")
		return s
	}
	switch {
	case y == m.oy-1:
		s, _ := data.DefaultState("grass_block")
		return s
	case y < m.oy-1 && y >= m.oy-2:
		s, _ := data.DefaultState("dirt")
		return s
	}
	return data.Air
}
func (m *shiftedRegion) SetBlock(x, y, z int, s data.StateID) bool {
	m.m[Pos{x, y, z}] = s
	return true
}
func (m *shiftedRegion) Height(h world.Heightmap, x, z int) int {
	for y := m.oy + 41; y > m.oy-3; y-- {
		f := data.Flags(m.Block(x, y, z))
		var occ bool
		switch h {
		case world.WorldSurface, world.WorldSurfaceWG:
			occ = f&data.FlagAir == 0
		case world.OceanFloor, world.OceanFloorWG:
			occ = f&data.FlagSolid != 0
		case world.MotionBlocking:
			occ = f&(data.FlagSolid|data.FlagFluid) != 0
		case world.MotionBlockingNoLeaves:
			occ = f&(data.FlagSolid|data.FlagFluid) != 0 && f&data.FlagLeaves == 0
		}
		if occ {
			return y + 1
		}
	}
	return m.oy - 2
}
func (m *shiftedRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}

// TestTreesAt replays raw kremenmc.probe.feat_tree.Draws output (lines with an
// explicit block list; "at=Y" moves the origin): testdata/trees_at*.txt.
func TestTreesAt(t *testing.T) {
	files := []string{"testdata/trees_at.txt", "testdata/trees_jungle.txt", "testdata/trees_cherry.txt", "testdata/trees_poplar.txt"}
	if f := os.Getenv("TREEAT"); f != "" {
		files = strings.Split(f, ",")
	}
	replayTreesAt(t, files, nil, os.Getenv("TREEAT_LENIENT") == "")
}

// TestPaleOakAt replays pale_oak and pale_oak_creaking with the pale_moss
// decorator enabled. The probe's pale_moss_carpet toppers draw from the
// ServerLevel random, which cannot be replayed, so carpets are compared by
// block only and the pass count is a floor: every remaining mismatch is a
// topper, or a hanging-moss column that a different topper cut short.
func TestPaleOakAt(t *testing.T) {
	paleMossWired = true
	defer func() { paleMossWired = false }()
	norm := func(s string) string {
		if strings.HasPrefix(s, "pale_moss_carpet") {
			return "pale_moss_carpet"
		}
		return s
	}
	pass := replayTreesAt(t, []string{"testdata/trees_pale.txt"}, norm, false)
	if pass["minecraft:pale_oak"] < 15 || pass["minecraft:pale_oak_creaking"] < 6 {
		t.Errorf("pale oak regressed: %v", pass)
	}
}

func replayTreesAt(t *testing.T, files []string, norm func(string) string, strict bool) map[string]int {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]int{}
	if norm == nil {
		norm = func(s string) string { return s }
	}
	for _, name := range files {
		f, err := os.Open(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		pass, total := map[string]int{}, map[string]int{}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || line[0] == '#' || strings.HasPrefix(line, "R ") {
				continue
			}
			in, out, ok := strings.Cut(line, " => ")
			if !ok {
				continue
			}
			fs := strings.Fields(in)
			key := fs[0]
			seed, _ := strconv.ParseInt(fs[1], 10, 64)
			x, _ := strconv.Atoi(fs[2])
			z, _ := strconv.Atoi(fs[3])
			total[key]++
			feat, err := rg.parseFeature(json.RawMessage(strconv.Quote(key)))
			if err != nil {
				t.Logf("%s: %v", key, err)
				continue
			}
			reg := &shiftedRegion{m: map[Pos]data.StateID{}, oy: -60, cx: x, cz: z}
			var obs []string
			for _, ob := range fs[4:] {
				if v, ok := strings.CutPrefix(ob, "at="); ok {
					reg.oy, _ = strconv.Atoi(v)
				} else {
					obs = append(obs, ob)
				}
			}
			for _, ob := range obs {
				c, name, ok := strings.Cut(ob, "=")
				if !ok {
					continue
				}
				var o Pos
				fmt.Sscanf(c, "%d,%d,%d", &o.X, &o.Y, &o.Z)
				st, _ := data.DefaultState(strings.TrimPrefix(name, "minecraft:"))
				reg.m[Pos{x + o.X, reg.oy + o.Y, z + o.Z}] = st
			}
			lv := &Level{rg: reg}
			r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
			placed := feat.Place(&placeCtx{lv: lv}, r, Pos{x, reg.oy, z})
			got := []string{strconv.FormatBool(placed)}
			for y := reg.oy - 2; y <= reg.oy+40; y++ {
				for dx := -12; dx <= 12; dx++ {
					for dz := -12; dz <= 12; dz++ {
						s, set := reg.m[Pos{x + dx, y, z + dz}]
						if !set || data.IsAir(s) {
							continue
						}
						n := normState(data.StateString(s))
						if y == reg.oy-2 && n == "dirt" || y == reg.oy-1 && n == "grass_block" {
							continue
						}
						got = append(got, fmt.Sprintf("%d,%d,%d=%s", dx, y-reg.oy-60, dz, norm(n)))
					}
				}
			}
			blocks, next, _ := strings.Cut(out, " | ")
			want := strings.Fields(blocks)
			for i := 1; i < len(want); i++ {
				c, s, _ := strings.Cut(want[i], "=")
				want[i] = c + "=" + norm(normState(s))
			}
			gotS := strings.Join(got, " ") + " | " + strconv.FormatInt(r.NextLong(), 10)
			wantS := strings.Join(want, " ") + " | " + next
			if gotS == wantS {
				pass[key]++
			} else if os.Getenv("TREEKEY") == key || os.Getenv("TREEKEY") == "all" {
				t.Logf("%s mismatch\n got %s\nwant %s", in, gotS, wantS)
			}
		}
		f.Close()
		for k, n := range total {
			t.Logf("%s %s: %d/%d", name, k, pass[k], n)
			all[k] += pass[k]
			if pass[k] != n && strict {
				t.Errorf("%s %s: %d/%d", name, k, pass[k], n)
			}
		}
	}
	return all
}
