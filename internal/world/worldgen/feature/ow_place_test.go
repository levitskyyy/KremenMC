package feature

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// owHash mirrors kremenmc.probe.feat_ow.Place.hash.
func owHash(seed int64, dx, dy, dz int) int32 {
	h := int32(dx)*73856093 ^ int32(dy)*19349663 ^ int32(dz)*83492791 ^ int32(seed)
	h ^= int32(uint32(h) >> 13)
	h *= 0x5bd1e995
	h ^= int32(uint32(h) >> 15)
	return h
}

// owScene mirrors kremenmc.probe.feat_ow.Place.scene.
func owScene(mode string, seed int64, dx, dy, dz int) data.StateID {
	name := "stone"
	switch {
	case mode == "snow":
		switch {
		case dy < -1:
			name = "stone"
		case dy == -1:
			name = "snow_block"
			if owHash(seed, dx, 3, dz)&31 == 0 {
				name = "stone"
			}
		case dy <= 1 && owHash(seed, dx, dy, dz)&63 == 0:
			name = "snow_block"
		default:
			name = "air"
		}
	case mode == "sand":
		switch {
		case dy < -4:
			name = "sandstone"
		case dy < 0:
			name = "sand"
		default:
			name = "air"
		}
	case mode == "caveair":
		return owScene("cave", seed, dx, dy+3, dz)
	case mode == "cavezoo" || mode == "cavezoo2":
		b := owScene("cave", seed, dx, dy, dz)
		if !data.IsAir(b) || blockName(owScene("cave", seed, dx, dy-1, dz)) != "deepslate" {
			return b
		}
		k := (uint32(owHash(seed, dx, 104, dz)) >> 4) & 63
		if k >= 16 {
			return b
		}
		if mode == "cavezoo2" {
			return owState(owZoo2[k])
		}
		return owState(owZoo[k])
	case mode == "cavemix":
		b := owScene("cave", seed, dx, dy, dz)
		if !data.IsAir(b) || blockName(owScene("cave", seed, dx, dy-1, dz)) != "deepslate" {
			return b
		}
		st := ""
		switch (uint32(owHash(seed, dx, 103, dz)) >> 4) & 7 {
		case 0:
			st = "snow"
		case 1:
			st = "snow[layers=3]"
		case 2:
			st = "deepslate_tile_stairs[waterlogged=true]"
		case 3:
			st = "deepslate_tile_slab"
		default:
			return b
		}
		return owState(st)
	case strings.HasPrefix(mode, "dripcave"):
		fh := uint32(owHash(seed, dx, 100, dz)) >> 8
		floor, ceil := -12+int(fh&3), 8+int((fh>>2)&7)
		if mode == "dripcave2" {
			floor, ceil = -6+int(fh&3), 4+int((fh>>2)&7)
		}
		switch {
		case abs(dx) > 28 || abs(dz) > 28 || dy < floor || dy >= ceil:
			rocks := [9]string{"dirt", "gravel", "granite", "lava", "water", "deepslate", "deepslate", "dripstone_block", "air"}
			name = "stone"
			if k := (uint32(owHash(seed, dx, dy, dz)) >> 4) & 63; k < 9 {
				name = rocks[k]
			}
		case owHash(seed, dx, 101, dz)&127 == 0:
			name = "stone"
		case dy == floor && owHash(seed, dx, 102, dz)&15 < 2:
			name = [2]string{"water", "lava"}[owHash(seed, dx, 102, dz)&15]
		default:
			name = "air"
		}
	case strings.HasPrefix(mode, "dripzoo"):
		fh := uint32(owHash(seed, dx, 100, dz)) >> 8
		tall := mode == "dripzoo2"
		floor := int(fh&3) - 1
		if tall && fh&7 == 0 {
			floor = -15
		}
		ceil := 4 + int((fh>>3)&7)
		if tall {
			ceil = 4 + int((fh>>3)&15)
		}
		switch {
		case abs(dx) > 12 || abs(dz) > 12 || dy < floor || dy >= ceil:
			rocks := [11]string{"dirt", "gravel", "granite", "lava", "water", "tuff", "deepslate", "copper_ore", "dripstone_block", "calcite", "glass"}
			name = "stone"
			if k := (uint32(owHash(seed, dx, dy, dz)) >> 4) & 31; k < 11 {
				name = rocks[k]
			}
		case owHash(seed, dx, 101, dz)&63 == 0:
			name = "stone"
		case dy == floor && owHash(seed, dx, 102, dz)&15 < 3:
			name = [3]string{"water", "lava", "moss_carpet"}[owHash(seed, dx, 102, dz)&15]
		case dy == ceil-1 && owHash(seed, dx, 105, dz)&15 == 0:
			name = "lava"
		default:
			name = "air"
		}
	case mode == "caveceil":
		return owScene("cavestone", seed, dx, dy+6, dz)
	case mode == "cavewetstone":
		s := owScene("cavewet", seed, dx, dy, dz)
		if blockName(s) == "deepslate" {
			s, _ = data.DefaultState("stone")
		}
		return s
	case strings.HasPrefix(mode, "cave"):
		rock := "deepslate"
		if mode == "cavestone" {
			rock = "stone"
		}
		fh := uint32(owHash(seed, dx, 100, dz)) >> 8
		floor := 0
		if fh&3 == 0 {
			floor = 1
		}
		ceil := 7 + int((fh>>2)&1)
		half := 10
		if mode == "cavebig" {
			half = 20
		}
		switch {
		case abs(dx) > half || abs(dz) > half || dy < floor || dy >= ceil:
			name = rock
			if dy < -4 && owHash(seed, dx, dy, dz)&15 == 0 {
				name = "tuff"
			}
		case owHash(seed, dx, 101, dz)&63 == 0:
			name = rock
		case dy == floor && owHash(seed, dx, 102, dz)&31 == 0:
			name = "water"
		case mode == "cavewet" && dy <= 1:
			name = "water"
		default:
			name = "air"
		}
	case strings.HasPrefix(mode, "roots"):
		n, _ := strconv.Atoi(mode[5:])
		switch {
		case dy <= -6:
			name = "stone"
		case dy < 0 || dy >= n:
			name = "air"
		case dy == n-1:
			name = "grass_block"
		case dy >= n-3:
			name = "dirt"
		}
	case mode == "holes":
		return geodeScene(mode, seed, dx, dy, dz)
	}
	s, _ := data.DefaultState(name)
	return s
}

type owRegion struct {
	m          map[Pos]data.StateID
	mode       string
	seed       int64
	ox, oy, oz int
	r          int
}

func (g *owRegion) Seed() int64          { return 0 }
func (g *owRegion) Dim() world.Dimension { return world.Overworld }
func (g *owRegion) Height(h world.Heightmap, _, _ int) int {
	if h == world.WorldSurfaceWG {
		return 4 // the probe's GameTest level reports its flat surface there
	}
	return 0
}
func (g *owRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}
func (g *owRegion) SetBlock(x, y, z int, s data.StateID) bool { g.m[Pos{x, y, z}] = s; return true }
func (g *owRegion) Block(x, y, z int) data.StateID {
	if s, ok := g.m[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-g.ox, y-g.oy, z-g.oz
	if abs(dx) > g.r || abs(dy) > g.r || abs(dz) > g.r {
		s, _ := data.DefaultState("stone")
		return s
	}
	return owScene(g.mode, g.seed, dx, dy, dz)
}

// TestOwPlace replays kremenmc.probe.feat_ow.Place output (the goldens
// testdata/ow_*.txt.gz, or OWPLACE=file,...): blocks changed in the probe box
// and the random's next long. OWTRACE=file writes the sculk trace of the
// first case (OWCASE=n selects another).
func TestOwPlace(t *testing.T) {
	files, _ := globTestdata("testdata/ow_*.txt.gz")
	if f := os.Getenv("OWPLACE"); f != "" {
		files = strings.Split(f, ",")
	}
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pick, _ := strconv.Atoi(os.Getenv("OWCASE"))
	if os.Getenv("OWSPREAD") != "" {
		sculkNoCheck = true
		defer func() { sculkNoCheck = false }()
	}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		var in io.Reader = f
		if strings.HasSuffix(file, ".gz") {
			if in, err = gzip.NewReader(f); err != nil {
				t.Fatal(err)
			}
		}
		sc := bufio.NewScanner(in)
		sc.Buffer(nil, 1<<26)
		box := 16
		pass, total, shown := 0, 0, 0
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "# R="); ok {
				box, _ = strconv.Atoi(v)
				continue
			}
			in, want, ok := strings.Cut(line, "\t")
			if !ok || strings.HasPrefix(line, "#") {
				continue
			}
			fs := strings.SplitN(in, " ", 6)
			mode := fs[0]
			y0, _ := strconv.Atoi(fs[1])
			seed, _ := strconv.ParseInt(fs[2], 10, 64)
			x, _ := strconv.Atoi(fs[3])
			z, _ := strconv.Atoi(fs[4])
			feat, err := rg.parseFeature(json.RawMessage(fs[5]))
			if err != nil {
				t.Errorf("%s: %v", in[:min(len(in), 60)], err)
				continue
			}
			if tf := os.Getenv("OWTRACE"); tf != "" && total == pick {
				w, _ := os.Create(tf)
				sculkTrace = w
				defer func() { sculkTrace = nil; w.Close() }()
			}
			total++
			reg := &owRegion{m: map[Pos]data.StateID{}, mode: mode, seed: seed, ox: x, oy: y0, oz: z, r: box}
			// The holes scene's lava next to water turns into obsidian
			// while the probe builds it (see TestGeodePlace).
			obs, _ := data.DefaultState("obsidian")
			for _, tok := range strings.Fields(want) {
				if c, v, ok := strings.Cut(tok, "="); ok && v == "obsidian" {
					var a, b, d int
					fmt.Sscanf(c, "%d,%d,%d", &a, &b, &d)
					reg.m[Pos{x + a, y0 + b, z + d}] = obs
				}
			}
			lv := &Level{rg: reg, st: &Stage{}}
			r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
			placed := feat.Place(&placeCtx{lv: lv}, r, Pos{x, y0, z})
			sculkTrace = nil
			var b strings.Builder
			fmt.Fprint(&b, placed)
			for dy := -box; dy <= box; dy++ {
				for dx := -box; dx <= box; dx++ {
					for dz := -box; dz <= box; dz++ {
						s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
						if y := y0 + dy; y < lv.MinY() || y >= lv.MaxY() {
							s, set = voidAir(), true
						}
						if set && s != owScene(mode, seed, dx, dy, dz) {
							fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
						}
					}
				}
			}
			fmt.Fprintf(&b, " | %d", r.NextLong())
			got := b.String()
			wnt := norm(want)
			if got == wnt {
				pass++
				continue
			}
			t.Logf("FAIL %s", in[:min(len(in), 40)])
			if shown < 3 {
				shown++
				gs, ws := map[string]bool{}, map[string]bool{}
				for _, tok := range strings.Fields(got) {
					gs[tok] = true
				}
				for _, tok := range strings.Fields(wnt) {
					ws[tok] = true
				}
				var extra, miss []string
				for k := range gs {
					if !ws[k] {
						extra = append(extra, k)
					}
				}
				for k := range ws {
					if !gs[k] {
						miss = append(miss, k)
					}
				}
				t.Errorf("%s %s %s %s %s: %d extra %v\n  %d missing %v", fs[0], fs[1], fs[2], fs[3], fs[4], len(extra), head(extra, 8), len(miss), head(miss, 8))
			}
		}
		f.Close()
		t.Logf("%s: %d/%d", file, pass, total)
		if pass != total {
			t.Errorf("%s: %d/%d match", file, pass, total)
		}
	}
}

func globTestdata(pat string) ([]string, error) {
	ents, err := os.ReadDir("testdata")
	if err != nil {
		return nil, err
	}
	pre, suf, _ := strings.Cut(strings.TrimPrefix(pat, "testdata/"), "*")
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), pre) && strings.HasSuffix(e.Name(), suf) {
			out = append(out, "testdata/"+e.Name())
		}
	}
	return out, nil
}

var owZoo = [16]string{"snow", "deepslate_tile_stairs", "deepslate_tile_slab[type=top]", "glass", "oak_leaves[persistent=true]", "oak_fence",
	"torch", "poppy", "white_carpet", "cobweb", "water[level=3]", "lava", "short_grass", "oak_trapdoor", "moss_carpet", "glow_lichen[down=true]"}

var owZoo2 = [16]string{"soul_fire", "fire", "fern", "dead_bush", "seagrass", "vine[north=true]", "leaf_litter", "hanging_roots",
	"snow[layers=8]", "light", "short_dry_grass", "bush", "resin_clump[down=true]", "cave_air", "tall_seagrass", "warped_roots"}

// owState parses name[k=v,...] over the block's default state.
func owState(s string) data.StateID {
	name, props, _ := strings.Cut(strings.TrimSuffix(s, "]"), "[")
	st, _ := data.DefaultState(name)
	if props == "" {
		return st
	}
	for _, kv := range strings.Split(props, ",") {
		k, v, _ := strings.Cut(kv, "=")
		st, _ = data.Canonical().With(st, k, v)
	}
	return st
}
