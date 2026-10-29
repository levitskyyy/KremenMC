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

const diskR = 16

var (
	diskMix = [16]string{"dirt", "grass_block", "clay", "mud", "air", "water", "stone", "sand",
		"snow_block", "ice", "podzol", "coarse_dirt", "mycelium", "gravel", "air", "dirt"}
	diskTop   = [8]string{"grass_block", "grass_block", "dirt", "mud", "clay", "sand", "snow_block", "gravel"}
	diskFlat  = [8]string{"grass_block", "grass_block", "dirt", "podzol", "coarse_dirt", "sand", "mud", "stone"}
	diskFlatX = [8]string{"mycelium", "moss_block", "rooted_dirt", "gravel", "snow_block", "pale_moss_block", "muddy_mangrove_roots", "red_sand"}
	diskOrig  = [4]string{"short_grass", "snow", "dead_bush", "water"}
	diskC0    = [8]string{"air", "water", "short_grass", "grass_block", "stone", "oak_leaves", "vine", "lava"}
	diskC1    = [4]string{"air", "water", "short_grass", "stone"}
	diskIce   = [16]string{"packed_ice", "packed_ice", "packed_ice", "air", "ice", "snow_block", "stone",
		"water", "water", "water", "water", "water", "water", "water", "water", "water"}
)

func diskFlatScene(mode string, seed int64, dx, dy, dz int) data.StateID {
	if mode == "flatlow" {
		dy--
	}
	h := envHash(seed, dx, 0, dz)
	center := dx == 0 && dz == 0
	switch {
	case mode == "flatwater" && dy >= 0 && dy <= 1:
		return envState("water")
	case mode == "flatorigin" && center && dy == 0:
		return envState(diskOrig[seed>>9&3])
	case mode == "flatcombo" && center && dy == 0:
		return envState(diskC0[seed>>9&7])
	case mode == "flatcombo" && center && dy == 1:
		return envState(diskC1[seed>>12&3])
	case dy >= 0:
		if center && seed&3 == 0 && int64(dy) == 2+(seed>>2&15) {
			return envState("stone")
		}
		if !center && h>>16&15 == 0 && dy == int(h>>20&15) {
			return envState("stone")
		}
		return data.Air
	case dy == -1:
		g := diskFlat
		if mode == "flatx" {
			g = diskFlatX
		}
		i := h >> 8 & 7
		if center {
			i = uint32(seed >> 6 & 7)
		}
		return envState(g[i])
	case dy >= -4:
		if envHash(seed, dx, dy, dz)>>4&15 == 0 {
			return data.Air
		}
		return envState("dirt")
	}
	return envState("stone")
}

// diskScene mirrors kremenmc.probe.feat_disk.Place.scene.
func diskScene(mode string, seed int64, dx, dy, dz int) data.StateID {
	if strings.HasPrefix(mode, "flat") {
		return diskFlatScene(mode, seed, dx, dy, dz)
	}
	switch mode {
	case "ice":
		switch {
		case dy > 8:
			return data.Air
		case dy < -8:
			return envState("stone")
		}
		return envState(diskIce[envHash(seed, dx, dy, dz)>>8&15])
	case "mix":
		return envState(diskMix[envHash(seed, dx, dy, dz)>>8&15])
	case "air":
		return data.Air
	}
	h := envHash(seed, dx, 0, dz)
	top := int(h>>4&7) - 4
	wet := h>>12&3 != 0
	switch {
	case dy > top:
		if wet && dy <= 1 {
			return envState("water")
		}
		return data.Air
	case dy == top:
		return envState(diskTop[h>>8&7])
	case envHash(seed, dx, dy, dz)>>4&15 == 0:
		return data.Air
	case dy >= top-3:
		return envState("dirt")
	}
	return envState("stone")
}

type diskRegion struct {
	m          map[Pos]data.StateID
	mode       string
	seed       int64
	cx, y0, cz int
	sea        int
}

// SeaLevel is the probe's: the GameTest flat world's -63 unless the mode
// carries a ":<sea level>" override.
func (e *diskRegion) SeaLevel() int { return e.sea }

func (e *diskRegion) Dim() world.Dimension { return world.Overworld }
func (e *diskRegion) Block(x, y, z int) data.StateID {
	if s, ok := e.m[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-e.cx, y-e.y0, z-e.cz
	if max(dx, -dx, dy, -dy, dz, -dz) > diskR {
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
	return diskScene(e.mode, e.seed, dx, dy, dz)
}
func (e *diskRegion) SetBlock(x, y, z int, s data.StateID) bool { e.m[Pos{x, y, z}] = s; return true }

// Height scans down from the top of the scene box like the probe's Wrap.
func (e *diskRegion) Height(h world.Heightmap, x, z int) int {
	for y := e.y0 + diskR; y >= -64; y-- {
		f := data.Flags(e.Block(x, y, z))
		switch h {
		case world.WorldSurface, world.WorldSurfaceWG:
			if f&data.FlagAir == 0 {
				return y + 1
			}
		default:
			if f&(data.FlagSolid|data.FlagFluid) != 0 {
				return y + 1
			}
		}
	}
	return -64
}
func (e *diskRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}

// TestDiskPlace replays testdata/disk_place.txt (tools/refprobe
// kremenmc.probe.feat_disk.Place): blocks written, result and the random's next
// long. Lines whose feature has a type outside diskTypes are skipped.
func TestDiskPlace(t *testing.T) { replayDisk(t, "testdata/disk_place.txt") }

func replayDisk(t *testing.T, file string) {
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
	pass, total, shown := map[string]int{}, map[string]int{}, 0
	for sc.Scan() {
		in, want, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		fs := strings.SplitN(in, " ", 6)
		y0, _ := strconv.Atoi(fs[1])
		seed, _ := strconv.ParseInt(fs[2], 10, 64)
		x, _ := strconv.Atoi(fs[3])
		z, _ := strconv.Atoi(fs[4])
		key := fs[5]
		mode, sea := fs[0], -63
		if m, v, ok := strings.Cut(mode, ":"); ok {
			mode = m
			sea, _ = strconv.Atoi(v)
		}
		feat, err := rg.parseFeature(json.RawMessage(fs[5]))
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		total[key]++
		reg := &diskRegion{m: map[Pos]data.StateID{}, mode: mode, seed: seed, cx: x, y0: y0, cz: z, sea: sea}
		lv := &Level{rg: reg, st: &Stage{}}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		placed := feat.Place(&placeCtx{lv: lv}, r, Pos{x, y0, z})
		var b strings.Builder
		fmt.Fprint(&b, placed)
		for dy := -diskR; dy <= diskR; dy++ {
			for dx := -diskR; dx <= diskR; dx++ {
				for dz := -diskR; dz <= diskR; dz++ {
					s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
					if set && s != diskScene(mode, seed, dx, dy, dz) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		var wb strings.Builder
		for i, tok := range strings.Fields(want) {
			if i > 0 {
				wb.WriteByte(' ')
			}
			if c, v, ok := strings.Cut(tok, "="); ok {
				tok = c + "=" + normState(v)
			}
			wb.WriteString(tok)
		}
		if b.String() == wb.String() {
			pass[key]++
		} else if shown < 4 {
			shown++
			t.Errorf("%s\n got  %s\n want %s", in, b.String(), wb.String())
		}
	}
	for k, n := range total {
		t.Logf("%s: %d/%d", k, pass[k], n)
		if pass[k] != n {
			t.Errorf("%s: %d/%d match", k, pass[k], n)
		}
	}
}
