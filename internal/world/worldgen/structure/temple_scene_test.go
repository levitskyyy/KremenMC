package structure_test

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// Legacy scene replay: TScene (kremenmc.probe.temple) inputs <name>.txt
// and outputs <name>.out, in testdata/legacy (gzipped) or the directory
// KREMENMC_LSCENES. A name containing "_full" was run with FULLCLIP=1 (one
// pass clipped to the scene region).

type lscene struct {
	seed        int64
	index, step int32
	rg          structure.Box
	blocks      []data.StateID
	start       nbt.Compound
	chunks      [][2]int32
}

func (s *lscene) idx(x, y, z int32) int {
	sx, sz := s.rg.MaxX-s.rg.MinX+1, s.rg.MaxZ-s.rg.MinZ+1
	return int(((y-s.rg.MinY)*sz+(z-s.rg.MinZ))*sx + (x - s.rg.MinX))
}

func openLegacy(path string) (io.ReadCloser, error) {
	if f, err := os.Open(path); err == nil {
		return f, nil
	}
	f, err := os.Open(path + ".gz")
	if err != nil {
		return nil, err
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{z, f}, nil
}

var snbtKey = regexp.MustCompile(`([{,])([A-Za-z0-9_]+):`)

func readLScene(t *testing.T, path string) *lscene {
	f, err := openLegacy(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reg := data.Canonical()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	s := &lscene{}
	var pal []data.StateID
	for sc.Scan() {
		l := sc.Text()
		a := strings.Fields(l)
		if len(a) == 0 {
			continue
		}
		switch a[0] {
		case "SEED":
			fmt.Sscan(l[5:], &s.seed, &s.index, &s.step)
		case "REGION":
			fmt.Sscan(l[7:], &s.rg.MinX, &s.rg.MinY, &s.rg.MinZ, &s.rg.MaxX, &s.rg.MaxY, &s.rg.MaxZ)
		case "PAL":
			n, _ := strconv.Atoi(a[1])
			for range n {
				sc.Scan()
				st, ok := reg.ParseState(sc.Text())
				if !ok {
					t.Fatalf("state %q", sc.Text())
				}
				pal = append(pal, st)
			}
		case "DATA":
			for _, e := range a[1:] {
				i := strings.IndexByte(e, '*')
				pi, _ := strconv.Atoi(e[:i])
				c, _ := strconv.Atoi(e[i+1:])
				for range c {
					s.blocks = append(s.blocks, pal[pi])
				}
			}
		case "START":
			src := l[6:]
			if !strings.Contains(src, `"id"`) {
				src = snbtKey.ReplaceAllString(src, `$1"$2":`)
			}
			s.start = parseSNBT(t, src).(nbt.Compound)
		case "CHUNKS":
			for _, c := range a[1:] {
				var cx, cz int32
				fmt.Sscanf(c, "%d,%d", &cx, &cz)
				s.chunks = append(s.chunks, [2]int32{cx, cz})
			}
		}
	}
	return s
}

// llevel is the probe world: the scene in its region, air above, the
// GameTest floor below y 4 outside it. Heightmaps answer from the scene as
// loaded (the frozen pre-placement heights).
type llevel struct {
	s  *lscene
	m  map[[3]int32]data.StateID
	be map[[3]int32]nbt.Compound
}

func (l *llevel) orig(x, y, z int32) data.StateID {
	if l.s.rg.Contains(x, y, z) {
		return l.s.blocks[l.s.idx(x, y, z)]
	}
	return 0
}
func (l *llevel) MinY() int32 { return -64 }
func (l *llevel) MaxY() int32 { return 320 }
func (l *llevel) Block(x, y, z int32) data.StateID {
	if s, ok := l.m[[3]int32{x, y, z}]; ok {
		return s
	}
	return l.orig(x, y, z)
}
func (l *llevel) SetBlock(x, y, z int32, s data.StateID) bool {
	if old := l.Block(x, y, z); old != s {
		delete(l.be, [3]int32{x, y, z})
	}
	l.m[[3]int32{x, y, z}] = s
	return true
}
func (l *llevel) Height(hm string, x, z int32) int32 {
	reg := data.Canonical()
	for y := l.s.rg.MaxY; y >= -64; y-- {
		s := l.orig(x, y, z)
		if (hm == "OCEAN_FLOOR_WG" && reg.IsSolid(s)) || (hm != "OCEAN_FLOOR_WG" && !reg.IsAir(s)) {
			return y + 1
		}
	}
	return -64
}
func (l *llevel) Biome(int32, int32, int32) data.BiomeID { return 0 }
func (l *llevel) SetBlockEntity(x, y, z int32, id string, c nbt.Compound) {
	c2 := nbt.Compound{"id": id}
	for k, v := range c {
		c2[k] = v
	}
	l.be[[3]int32{x, y, z}] = c2
}
func (l *llevel) AddEntity(nbt.Compound)              {}
func (l *llevel) MarkPostProcess(int32, int32, int32) {}
func (l *llevel) Seed() int64                         { return l.s.seed }

func TestLegacyScenes(t *testing.T) {
	structure.EnableLegacy("minecraft:desert_pyramid", "minecraft:jungle_pyramid")
	files, _ := filepath.Glob("testdata/legacy/*.txt.gz")
	for i := range files {
		files[i] = strings.TrimSuffix(files[i], ".gz")
	}
	if dir := os.Getenv("KREMENMC_LSCENES"); dir != "" {
		files, _ = filepath.Glob(filepath.Join(dir, os.Getenv("KREMENMC_LSCENE_GLOB")+"*.txt"))
	}
	if len(files) == 0 {
		t.Skip("no scenes")
	}
	for _, f := range files {
		out := strings.TrimSuffix(f, ".txt") + ".out"
		if _, err := os.Stat(out); err != nil {
			if _, err := os.Stat(out + ".gz"); err != nil {
				continue
			}
		}
		t.Run(filepath.Base(f), func(t *testing.T) { runLScene(t, f, out) })
	}
}

func runLScene(t *testing.T, in, outPath string) {
	s := readLScene(t, in)
	full := strings.Contains(filepath.Base(in), "_full")
	reg := data.Canonical()
	want := map[[3]int32]string{}
	wantBE := map[[3]int32]string{}
	wantNext := map[[2]int32]string{}
	f, err := openLegacy(outPath)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "B "):
			var x, y, z int32
			var st string
			fmt.Sscan(l[2:], &x, &y, &z, &st)
			want[[3]int32{x, y, z}] = "minecraft:" + st
		case strings.HasPrefix(l, "BE "):
			var x, y, z int32
			fmt.Sscan(l[3:], &x, &y, &z)
			if i := strings.Index(l, "LootTableSeed:"); i >= 0 {
				v := l[i+len("LootTableSeed:"):]
				wantBE[[3]int32{x, y, z}] = strings.TrimSuffix(v[:strings.IndexAny(v, ",}")], "L")
			}
		case strings.HasPrefix(l, "NEXT "):
			var cx, cz int32
			var v string
			fmt.Sscan(l[5:], &cx, &cz, &v)
			wantNext[[2]int32{cx, cz}] = v
		}
	}
	f.Close()
	st, err := structure.DecodeStart(s.start)
	if err != nil {
		t.Fatal(err)
	}
	lv := &llevel{s: s, m: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}}
	nextOK := 0
	for _, c := range s.chunks {
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
		deco := r.SetDecorationSeed(s.seed, c[0]<<4, c[1]<<4)
		r.SetFeatureSeed(deco, s.index, s.step)
		clip := structure.ChunkBox(c[0], c[1], -64, 384)
		if full {
			clip = structure.Box{MinX: s.rg.MinX, MinY: -64, MinZ: s.rg.MinZ, MaxX: s.rg.MaxX, MaxY: 319, MaxZ: s.rg.MaxZ}
		}
		for _, p := range st.Pieces {
			if p.Box().Intersects(clip) {
				if err := p.Place(lv, r, clip, c[0], c[1]); err != nil {
					t.Fatal(err)
				}
			}
		}
		if v := strconv.FormatInt(r.NextLong(), 10); v == wantNext[c] {
			nextOK++
		} else {
			t.Logf("chunk %v: random state differs", c)
		}
	}
	got := map[[3]int32]string{}
	for k, v := range lv.m {
		if s.rg.Contains(k[0], k[1], k[2]) && v != s.blocks[s.idx(k[0], k[1], k[2])] {
			got[k] = reg.StateString(v)
		}
	}
	var diffs []string
	byType := map[string]int{}
	for k, w := range want {
		if got[k] != w {
			diffs = append(diffs, fmt.Sprintf("%d,%d,%d want %s got %s (scene %s)", k[0], k[1], k[2], w, got[k], reg.StateString(s.blocks[s.idx(k[0], k[1], k[2])])))
			byType[strings.SplitN(w, "[", 2)[0]]++
		}
	}
	for k, g := range got {
		if _, ok := want[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%d,%d,%d want unchanged %s got %s", k[0], k[1], k[2], reg.StateString(s.blocks[s.idx(k[0], k[1], k[2])]), g))
			byType["extra "+strings.SplitN(g, "[", 2)[0]]++
		}
	}
	sort.Strings(diffs)
	beBad := 0
	for k, w := range wantBE {
		if g, _ := lv.be[k]["LootTableSeed"].(int64); strconv.FormatInt(g, 10) != w {
			beBad++
			t.Logf("loot seed %v: want %s got %v", k, w, lv.be[k])
		}
	}
	for k, v := range lv.be {
		if _, ok := wantBE[k]; !ok && v["LootTable"] != nil {
			beBad++
			t.Logf("extra loot block entity %v %v", k, v)
		}
	}
	t.Logf("%d/%d written blocks match, %d extra; loot seeds %d/%d; random %d/%d chunks", len(want)-countMissingL(want, got), len(want), len(diffs)-countMissingL(want, got), len(wantBE)-beBad, len(wantBE), nextOK, len(s.chunks))
	for i, d := range diffs {
		if i >= 25 {
			break
		}
		t.Log(d)
	}
	if len(diffs) > 0 || beBad > 0 || nextOK != len(s.chunks) {
		t.Errorf("%s differs: %v", in, byType)
	}
}

func countMissingL(want, got map[[3]int32]string) int {
	n := 0
	for k, w := range want {
		if got[k] != w {
			n++
		}
	}
	return n
}

// TestLegacyStartsProbe compares kremenmc's starts with the reference's
// findGenerationPoint (kremenmc.probe.temple.TStarts, real biome source)
// on every spread chunk of testdata/legacy/starts_<structure>_<seed>.txt:
// presence (stub and biome) and every saved piece field.
func TestLegacyStartsProbe(t *testing.T) {
	structure.EnableLegacy("minecraft:desert_pyramid", "minecraft:jungle_pyramid")
	files, _ := filepath.Glob("testdata/legacy/starts_*.txt.gz")
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "starts_"), ".txt.gz")
		i := strings.LastIndexByte(name, '_')
		id := "minecraft:" + name[:i]
		var seed int64
		fmt.Sscan(name[i+1:], &seed)
		t.Run(name, func(t *testing.T) {
			tr, err := vanilla.NewTerrain("overworld", seed)
			if err != nil {
				t.Fatal(err)
			}
			ss, err := vanilla.StructureStagesForTest(tr, seed)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := openLegacy(strings.TrimSuffix(f, ".gz"))
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			sc := bufio.NewScanner(rc)
			sc.Buffer(make([]byte, 1<<20), 1<<24)
			same, total, starts := 0, 0, 0
			for sc.Scan() {
				l := sc.Text()
				var cx, cz int32
				fmt.Sscanf(l[len(id)+1:], "%d %d", &cx, &cz)
				want := ""
				if strings.Contains(l, " valid ") {
					a := strings.Index(l, " START ")
					b := strings.Index(l, " ENDSTART")
					c := parseSNBT(t, snbtKey.ReplaceAllString(l[a+7:b], `$1"$2":`)).(nbt.Compound)
					want = snbt(c["Children"])
					starts++
				}
				sts, err := ss.StartsAt(cx, cz)
				if err != nil {
					t.Fatal(err)
				}
				got := ""
				for _, st := range sts {
					if st.ID == id {
						got = snbt(st.Encode()["Children"])
					}
				}
				total++
				if got == want {
					same++
				} else {
					t.Logf("%d,%d: want %s\n got %s", cx, cz, want, got)
				}
			}
			t.Logf("%s seed %d: %d/%d spread chunks identical (%d reference starts)", id, seed, same, total, starts)
			if same != total {
				t.Errorf("starts differ")
			}
		})
	}
}
