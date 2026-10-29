package feature

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// mapRegion is a flat test world: dirt at -62, grass_block at -61. Its
// heightmaps sit at y 4 (the probe level's own surface) unless scan is set,
// as for probe cases with the prime token (box cleared up to y 16).
type mapRegion struct {
	m    map[Pos]data.StateID
	scan bool
}

func (m *mapRegion) Dim() world.Dimension { return world.Overworld }
func (m *mapRegion) Block(x, y, z int) data.StateID {
	if s, ok := m.m[Pos{x, y, z}]; ok {
		return s
	}
	switch {
	case y == -61:
		s, _ := data.DefaultState("grass_block")
		return s
	case y < -61 && y > -64:
		s, _ := data.DefaultState("dirt")
		return s
	case y == -64:
		s, _ := data.DefaultState("bedrock")
		return s
	}
	return data.Air
}
func (m *mapRegion) SetBlock(x, y, z int, s data.StateID) bool { m.m[Pos{x, y, z}] = s; return true }
func (m *mapRegion) Height(h world.Heightmap, x, z int) int {
	if !m.scan {
		return 4
	}
	for y := 16; y > -64; y-- {
		st := m.Block(x, y, z)
		f := data.Flags(st)
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
	return -64
}
func (m *mapRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}

// normState drops the leaves' distance: the probe places into a live level
// whose neighbour updates rewrite it, unlike worldgen.
func normState(s string) string {
	name, props, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(s, "minecraft:"), "]"), "[")
	var ps []string
	for _, kv := range strings.Split(props, ",") {
		if kv != "" && !(strings.HasSuffix(name, "_leaves") && strings.HasPrefix(kv, "distance=")) {
			ps = append(ps, kv)
		}
	}
	slices.Sort(ps)
	if len(ps) == 0 {
		return name
	}
	return name + "[" + strings.Join(ps, ",") + "]"
}

// supported trees must match draw for draw; the others use placers that are
// still no-ops.
var supported = map[string]bool{
	"minecraft:oak": true, "minecraft:birch": true, "minecraft:oak_bees_005": true, "minecraft:spruce": true,
	"minecraft:pine": true, "minecraft:jungle_tree_no_vine": true, "minecraft:super_birch_bees_0002": true,
	"minecraft:oak_leaf_litter": true, "minecraft:swamp_oak": true, "minecraft:jungle_tree": true, "minecraft:acacia": true, "minecraft:birch_bees_0002_leaf_litter": true,
	"minecraft:fancy_oak": true, "minecraft:fancy_oak_bees": true, "minecraft:fancy_oak_leaf_litter": true, "minecraft:dark_oak": true,
}

// TestTreesGolden replays testdata/trees.txt (tools/refprobe
// kremenmc.probe.feature.Tree, block lists stored as a hash of the normalized
// states): every block of the placed tree and the number of random draws.
func TestTreesGolden(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pass, total := map[string]int{}, map[string]int{}
	for _, name := range []string{"testdata/trees.txt", "testdata/trees_more.txt", "testdata/trees_placers.txt", "testdata/trees_obstacles.txt", "testdata/trees_bees.txt", "testdata/trees_ground.txt", "testdata/trees_darkoak_obstacles.txt"} {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		treeGoldenFile(t, rg, f, pass, total)
		f.Close()
	}
	for k, n := range total {
		if !supported[k] {
			continue
		}
		t.Logf("%s: %d/%d", k, pass[k], n)
		if pass[k] != n {
			t.Errorf("%s: %d/%d trees identical", k, pass[k], n)
		}
	}
}

func treeGoldenFile(t *testing.T, rg *registry, f *os.File, pass, total map[string]int) {
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		in, out, _ := strings.Cut(sc.Text(), " => ")
		fs := strings.Fields(in)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		x, _ := strconv.Atoi(fs[2])
		z, _ := strconv.Atoi(fs[3])
		total[fs[0]]++
		feat, err := rg.parseFeature(json.RawMessage(strconv.Quote(fs[0])))
		if err != nil {
			if supported[fs[0]] {
				t.Errorf("%s: %v", fs[0], err)
			}
			continue
		}
		reg := &mapRegion{m: map[Pos]data.StateID{}}
		for _, ob := range fs[4:] {
			c, name, ok := strings.Cut(ob, "=")
			if !ok {
				reg.scan = reg.scan || ob == "prime"
				continue
			}
			var o Pos
			fmt.Sscanf(c, "%d,%d,%d", &o.X, &o.Y, &o.Z)
			st, _ := data.DefaultState(strings.TrimPrefix(name, "minecraft:"))
			reg.m[Pos{x + o.X, -60 + o.Y, z + o.Z}] = st
		}
		lv := &Level{rg: reg}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		ok := feat.Place(&placeCtx{lv: lv}, r, Pos{x, -60, z})
		var got []string
		for y := -62; y <= -20; y++ {
			for dx := -12; dx <= 12; dx++ {
				for dz := -12; dz <= 12; dz++ {
					p := Pos{x + dx, y, z + dz}
					s, set := reg.m[p]
					if !set || data.IsAir(s) {
						continue
					}
					n := normState(data.StateString(s))
					if y == -62 && n == "dirt" || y == -61 && n == "grass_block" {
						continue
					}
					got = append(got, fmt.Sprintf("%d,%d,%d=%s", dx, y, dz, n))
				}
			}
		}
		sum := sha256.Sum256([]byte(strings.Join(got, " ")))
		gotS := fmt.Sprintf("%v %d %x | %d", ok, len(got), sum[:8], r.NextLong())
		if gotS == out {
			pass[fs[0]]++
		} else if os.Getenv("TREEKEY") == fs[0] {
			t.Logf("%s mismatch\n got %s\nwant %s\n%s", in, gotS, out, strings.Join(got, " "))
		}
	}
}
