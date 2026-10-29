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

const sceneR, sceneY = 16, -30

// sceneRegion mirrors kremenmc.probe.feat_cave.Place's scene around (cx, cz).
type sceneRegion struct {
	m      map[Pos]data.StateID
	seed   int64
	cx, cz int
}

func sceneBlock(seed int64, dx, dy, dz int) data.StateID {
	abs := func(v int) int { return max(v, -v) }
	if abs(dx) == sceneR || abs(dy) == sceneR || abs(dz) == sceneR {
		s, _ := data.DefaultState("stone")
		return s
	}
	h := int32(dx)*73856093 ^ int32(dy)*19349663 ^ int32(dz)*83492791 ^ int32(seed)
	h ^= int32(uint32(h) >> 13)
	h *= 0x5bd1e995
	h ^= int32(uint32(h) >> 15)
	if uint32(h)>>4&7 >= 2 {
		return data.Air
	}
	name := [...]string{"stone", "stone", "stone", "deepslate", "dirt", "oak_leaves", "water", "moss_block"}[uint32(h)>>8&7]
	s, _ := data.DefaultState(name)
	return s
}

func (m *sceneRegion) Dim() world.Dimension { return world.Overworld }
func (m *sceneRegion) Block(x, y, z int) data.StateID {
	if s, ok := m.m[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-m.cx, y-sceneY, z-m.cz
	if max(dx, -dx, dy, -dy, dz, -dz) > sceneR {
		return data.Air
	}
	return sceneBlock(m.seed, dx, dy, dz)
}
func (m *sceneRegion) SetBlock(x, y, z int, s data.StateID) bool { m.m[Pos{x, y, z}] = s; return true }
func (m *sceneRegion) Height(world.Heightmap, int, int) int      { return 0 }
func (m *sceneRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("lush_caves")
	return b, true
}

// caveSupported lists the feature keys that must match testdata/cave_place.txt
// draw for draw.
var caveSupported = map[string]bool{
	"minecraft:vines": true, "minecraft:glow_lichen": true, "minecraft:sculk_vein": true,
	"minecraft:moss_patch": true, "minecraft:moss_patch_ceiling": true, "minecraft:clay_pool_with_dripleaves": true,
	"minecraft:clay_with_dripleaves": true,
}

// TestCavePlaceGolden replays tools/refprobe kremenmc.probe.feat_cave.Place:
// every block changed in the scene and the random's next long.
func TestCavePlaceGolden(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, name := range []string{"testdata/cave_place.txt", "testdata/cave_place_multiface.txt", "testdata/cave_place_vegpatch.txt"} {
		f, err := os.Open(name)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<22)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		f.Close()
	}
	pass, total := map[string]int{}, map[string]int{}
	for _, line := range lines {
		in, want, _ := strings.Cut(line, " => ")
		fs := strings.Fields(in)
		if !caveSupported[fs[0]] {
			continue
		}
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		z, _ := strconv.Atoi(fs[3])
		total[fs[0]]++
		feat, err := rg.parseFeature(json.RawMessage(strconv.Quote(fs[0])))
		if err != nil {
			t.Errorf("%s: %v", fs[0], err)
			continue
		}
		reg := &sceneRegion{m: map[Pos]data.StateID{}, seed: seed, cx: x, cz: z}
		lv := &Level{rg: reg}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		ok := feat.Place(&placeCtx{lv: lv}, r, Pos{x, sceneY, z})
		var b strings.Builder
		fmt.Fprint(&b, ok)
		for dy := -sceneR; dy <= sceneR; dy++ {
			for dx := -sceneR; dx <= sceneR; dx++ {
				for dz := -sceneR; dz <= sceneR; dz++ {
					s, set := reg.m[Pos{x + dx, sceneY + dy, z + dz}]
					if set && s != sceneBlock(seed, dx, dy, dz) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		if norm(b.String()) == norm(want) {
			pass[fs[0]]++
		} else {
			t.Logf("%s\n got %s\nwant %s", in, b.String(), want)
		}
	}
	for k, n := range total {
		t.Logf("%s: %d/%d", k, pass[k], n)
		if pass[k] != n {
			t.Errorf("%s: %d/%d placements identical", k, pass[k], n)
		}
	}
}

// norm sorts each state's properties so Java and Go spellings compare.
func norm(s string) string {
	fs := strings.Fields(s)
	for i, f := range fs {
		if k, v, ok := strings.Cut(f, "="); ok && strings.Contains(f, ",") && strings.Count(k, ",") == 2 {
			fs[i] = k + "=" + normState(v)
		}
	}
	return strings.Join(fs, " ")
}

// TestCavePlaceFlat replays tools/refprobe kremenmc.probe.feat_tree.Draws on the
// flat grass world (testdata/cave_place_flat.txt): every non-air block in
// the box except the ground layers, and the random's next long.
func TestCavePlaceFlat(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/cave_place_flat.txt")
	if err != nil {
		t.Fatal(err)
	}
	pass, total := map[string]int{}, map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		in, want, _ := strings.Cut(line, " => ")
		fs := strings.Fields(in)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		z, _ := strconv.Atoi(fs[3])
		total[fs[0]]++
		feat, err := rg.parseFeature(json.RawMessage(strconv.Quote(fs[0])))
		if err != nil {
			t.Errorf("%s: %v", fs[0], err)
			continue
		}
		reg := &mapRegion{m: map[Pos]data.StateID{}}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		ok := feat.Place(&placeCtx{lv: &Level{rg: reg}}, r, Pos{x, -60, z})
		var sb strings.Builder
		fmt.Fprint(&sb, ok)
		for y := -62; y <= -20; y++ {
			for dx := -12; dx <= 12; dx++ {
				for dz := -12; dz <= 12; dz++ {
					s := reg.Block(x+dx, y, z+dz)
					n := normState(data.StateString(s))
					if data.IsAir(s) || y == -62 && blockName(s) == "dirt" || y == -61 && blockName(s) == "grass_block" {
						continue
					}
					fmt.Fprintf(&sb, " %d,%d,%d=%s", dx, y, dz, n)
				}
			}
		}
		fmt.Fprintf(&sb, " | %d", r.NextLong())
		if norm(sb.String()) == norm(want) {
			pass[fs[0]]++
		} else {
			t.Logf("%s\n got %s\nwant %s", in, sb.String(), want)
		}
	}
	for k, n := range total {
		t.Logf("%s: %d/%d", k, pass[k], n)
		if pass[k] != n {
			t.Errorf("%s: %d/%d placements identical", k, pass[k], n)
		}
	}
}
