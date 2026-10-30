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

// geodeScene mirrors kremenmc.probe.feat_geode.Geode.scene.
func geodeScene(mode string, seed int64, dx, dy, dz int) data.StateID {
	name := "stone"
	if mode == "holes" {
		h := int32(dx)*73856093 ^ int32(dy)*19349663 ^ int32(dz)*83492791 ^ int32(seed)
		h ^= int32(uint32(h) >> 13)
		h *= 0x5bd1e995
		h ^= int32(uint32(h) >> 15)
		switch v := (uint32(h) >> 8) & 63; {
		case v < 4:
			name = "air"
		case v < 8:
			name = "water"
		case v == 8:
			name = "lava"
		case v == 9:
			name = "bedrock"
		case v == 10:
			name = "ice"
		case v == 11:
			name = "deepslate"
		}
	}
	s, _ := data.DefaultState(name)
	return s
}

type geodeRegion struct {
	m          map[Pos]data.StateID
	mode       string
	seed       int64
	ox, oy, oz int
	r          int
}

func (g *geodeRegion) Seed() int64                          { return 0 }
func (g *geodeRegion) Dim() world.Dimension                 { return world.Overworld }
func (g *geodeRegion) Height(world.Heightmap, int, int) int { return 0 }
func (g *geodeRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}
func (g *geodeRegion) SetBlock(x, y, z int, s data.StateID) bool { g.m[Pos{x, y, z}] = s; return true }
func (g *geodeRegion) Block(x, y, z int) data.StateID {
	if s, ok := g.m[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-g.ox, y-g.oy, z-g.oz
	if abs(dx) > g.r || abs(dy) > g.r || abs(dz) > g.r {
		s, _ := data.DefaultState("stone")
		return s
	}
	return geodeScene(g.mode, g.seed, dx, dy, dz)
}

// TestGeodePlace replays kremenmc.probe.feat_geode.Geode output
// (testdata/geode_place.txt.gz, or GEODE=file,...): blocks changed in the
// probe box and the random's next long.
func TestGeodePlace(t *testing.T) {
	files := []string{"testdata/geode_place.txt.gz"}
	if f := os.Getenv("GEODE"); f != "" {
		files = strings.Split(f, ",")
	}
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
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
		box := 24
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
			total++
			reg := &geodeRegion{m: map[Pos]data.StateID{}, mode: mode, seed: seed, ox: x, oy: y0, oz: z, r: box}
			// The probe scene's lava next to water turns into obsidian while
			// the scene is built, before the feature runs.
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
			var b strings.Builder
			fmt.Fprint(&b, placed)
			for dy := -box; dy <= box; dy++ {
				for dx := -box; dx <= box; dx++ {
					for dz := -box; dz <= box; dz++ {
						s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
						if y := y0 + dy; y < lv.MinY() || y >= lv.MaxY() {
							s, set = voidAir(), true
						}
						if set && s != geodeScene(mode, seed, dx, dy, dz) {
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

func head(s []string, n int) []string { return s[:min(len(s), n)] }

func voidAir() data.StateID {
	s, _ := data.DefaultState("void_air")
	return s
}
