package feature

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// envRegion mirrors the kremenmc.probe.feat_env.Place scenes around (cx, y0, cz).
type envRegion struct {
	m          map[Pos]data.StateID
	mode       string
	biome      data.BiomeID
	seed       int64
	cx, y0, cz int
}

const envR = 16

func envHash(seed int64, dx, dy, dz int) uint32 {
	h := int32(dx)*73856093 ^ int32(dy)*19349663 ^ int32(dz)*83492791 ^ int32(seed)
	h ^= int32(uint32(h) >> 13)
	h *= 0x5bd1e995
	h ^= int32(uint32(h) >> 15)
	return uint32(h)
}

func envState(name string) data.StateID { s, _ := data.DefaultState(name); return s }

func envScene(mode string, seed int64, dx, dy, dz int) data.StateID {
	abs := func(v int) int { return max(v, -v) }
	rock := func(h uint32) data.StateID {
		return envState([...]string{"stone", "stone", "stone", "deepslate", "dirt", "granite", "tuff", "andesite"}[h>>8&7])
	}
	switch mode {
	case "cave":
		if abs(dx) == envR || abs(dy) == envR || abs(dz) == envR {
			return envState("stone")
		}
		h := envHash(seed, dx, dy, dz)
		if h>>4&3 == 0 {
			return data.Air
		}
		return envState([...]string{"stone", "stone", "stone", "deepslate", "dirt", "granite", "water", "air"}[h>>8&7])
	case "solid", "solid0", "tunnel":
		h := envHash(seed, dx, dy, dz)
		if mode == "solid" && h>>4&15 == 0 {
			return data.Air
		}
		fm := seed % 9
		if fm < 0 {
			fm += 9
		}
		if mode == "tunnel" && (dy == 0 || dy == 1) && dz == int(fm)-4 {
			return data.Air
		}
		return rock(h)
	case "ocean":
		floor := -1 - int(envHash(seed, dx, 0, dz)>>4&7)
		switch {
		case dy <= floor:
			return envState("stone")
		case dy <= 8:
			return envState("water")
		}
		return data.Air
	}
	h := envHash(seed, dx, 0, dz)
	top := int(h >> 4 & 7)
	if dy > top {
		return data.Air
	}
	kind := h >> 8 & 7
	if mode == "dry" && kind == 2 {
		kind = 0
	}
	if dy == top {
		return envState([...]string{"grass_block", "grass_block", "water", "sand", "oak_leaves", "ice", "podzol", "short_grass"}[kind])
	}
	if kind == 2 && dy == top-1 {
		return envState("water")
	}
	if kind == 7 && dy == top-1 {
		return envState("grass_block")
	}
	return envState("stone")
}

func (e *envRegion) Dim() world.Dimension { return world.Overworld }
func (e *envRegion) Block(x, y, z int) data.StateID {
	if s, ok := e.m[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-e.cx, y-e.y0, z-e.cz
	if max(dx, -dx, dy, -dy, dz, -dz) > envR {
		// The GameTest flat world: bedrock, dirt, dirt, grass.
		switch {
		case y == -64:
			return envState("bedrock")
		case y < -61:
			return envState("dirt")
		case y == -61:
			return envState("grass_block")
		}
		return data.Air
	}
	return envScene(e.mode, e.seed, dx, dy, dz)
}
func (e *envRegion) SetBlock(x, y, z int, s data.StateID) bool { e.m[Pos{x, y, z}] = s; return true }
func (e *envRegion) Height(h world.Heightmap, x, z int) int {
	for y := e.y0 + envR + 1; y >= -64; y-- {
		f := data.Flags(e.Block(x, y, z))
		if f&(data.FlagSolid|data.FlagFluid) != 0 {
			return y + 1
		}
	}
	return -64
}
func (e *envRegion) Biome(int, int, int) (data.BiomeID, bool) { return e.biome, true }

// envSupported lists the feature keys that must match
// testdata/freeze_feat_env.txt draw for draw.
var envSupported = map[string]bool{
	"minecraft:spring_water": true, "minecraft:spring_lava_overworld": true, "minecraft:spring_lava_frozen": true,
	"minecraft:forest_rock": true, "minecraft:lake_lava": true,
	"minecraft:freeze_top_layer": true, "minecraft:monster_room": true, "minecraft:underwater_magma": true,
}

// TestFeatEnvGolden replays tools/refprobe kremenmc.probe.feat_env.Place.
func TestFeatEnvGolden(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/freeze_feat_env.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<22)
	// The probe runs on the GameTest flat world, whose sea level is -63.
	freezeSeaLevel = -63
	defer func() { freezeSeaLevel = 63 }()
	pass, total := map[string]int{}, map[string]int{}
	shown := map[string]int{}
	for sc.Scan() {
		in, want, _ := strings.Cut(sc.Text(), "\t")
		fs := strings.SplitN(in, " ", 7)
		key, _ := strconv.Unquote(fs[6])
		if !envSupported[key] {
			continue
		}
		biome, _ := data.Canonical().BiomeByName(fs[1])
		y0, _ := strconv.Atoi(fs[2])
		seed, _ := strconv.ParseInt(fs[3], 10, 64)
		x, _ := strconv.Atoi(fs[4])
		z, _ := strconv.Atoi(fs[5])
		total[key]++
		feat, err := rg.parseFeature(json.RawMessage(fs[6]))
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		reg := &envRegion{m: map[Pos]data.StateID{}, mode: fs[0], biome: biome, seed: seed, cx: x, y0: y0, cz: z}
		lv := &Level{rg: reg, st: &Stage{}}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		ok := feat.Place(&placeCtx{lv: lv}, r, Pos{x, y0, z})
		var b strings.Builder
		fmt.Fprint(&b, ok)
		for dy := -envR; dy <= envR; dy++ {
			for dx := -envR; dx <= envR; dx++ {
				for dz := -envR; dz <= envR; dz++ {
					s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
					if set && s != envScene(fs[0], seed, dx, dy, dz) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		// The probe runs in a ServerLevel, where neighbour shape updates pop
		// plants that lost their support; a WorldGenRegion does not.
		var kept []string
		for _, tok := range strings.Fields(want) {
			if c, v, ok := strings.Cut(tok, "="); ok && v == "air" {
				var dx, dy, dz int
				if _, err := fmt.Sscanf(c, "%d,%d,%d", &dx, &dy, &dz); err == nil && blockName(envScene(fs[0], seed, dx, dy, dz)) == "short_grass" {
					continue
				}
			}
			kept = append(kept, tok)
		}
		want = strings.Join(kept, " ")
		if norm(b.String()) == norm(want) {
			pass[key]++
		} else if shown[key] < 2 {
			shown[key]++
			t.Logf("%s\n got %s\nwant %s", in, b.String(), want)
		}
	}
	keys := make([]string, 0, len(total))
	for k := range total {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %d/%d", k, pass[k], total[k])
		if pass[k] != total[k] {
			t.Errorf("%s: %d/%d placements identical", k, pass[k], total[k])
		}
	}
}
