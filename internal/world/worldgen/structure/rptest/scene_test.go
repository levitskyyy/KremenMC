// Package rptest replays ruined portal scenes of the black-box probe
// kremenmc.probe.rportal.RPScene against kremenmc's placement.
package rptest

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

type scene struct {
	seed        int64
	index, step int32
	rg          structure.Box
	blocks      []data.StateID
	startLine   string
	chunks      [][2]int32
}

func (s *scene) idx(x, y, z int32) int {
	sx, sz := s.rg.MaxX-s.rg.MinX+1, s.rg.MaxZ-s.rg.MinZ+1
	return int(((y-s.rg.MinY)*sz+(z-s.rg.MinZ))*sx + (x - s.rg.MinX))
}

// openMaybeGz opens path, or path.gz when only the compressed file exists.
func openMaybeGz(path string) (io.ReadCloser, error) {
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

func readScene(t *testing.T, path string) *scene {
	f, err := openMaybeGz(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reg := data.Canonical()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	s := &scene{}
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
			s.startLine = l[6:]
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

// level is the probe's world: the scene inside its region, the GameTest
// flat world (solid up to y 3) outside; heightmaps are computed live.
var dbgPos = os.Getenv("KREMENMC_RPDBG")

type level struct {
	s  *scene
	m  map[[3]int32]data.StateID
	be map[[3]int32]nbt.Compound
}

func (l *level) MinY() int32 { return -64 }
func (l *level) MaxY() int32 { return 320 }
func (l *level) Block(x, y, z int32) data.StateID {
	if s, ok := l.m[[3]int32{x, y, z}]; ok {
		return s
	}
	if l.s.rg.Contains(x, y, z) {
		return l.s.blocks[l.s.idx(x, y, z)]
	}
	if y <= 3 {
		st, _ := data.Canonical().ParseState("minecraft:sandstone")
		return st
	}
	return 0
}
func (l *level) SetBlock(x, y, z int32, s data.StateID) bool {
	if dbgPos != "" && fmt.Sprintf("%d,%d,%d", x, y, z) == dbgPos {
		fmt.Printf("SET %d,%d,%d %s\n%s\n", x, y, z, data.Canonical().StateString(s), debug.Stack())
	}
	l.m[[3]int32{x, y, z}] = s
	return true
}
func (l *level) Height(hm string, x, z int32) int32 {
	reg := data.Canonical()
	for y := int32(319); y >= -64; y-- {
		s := l.Block(x, y, z)
		if (hm == "OCEAN_FLOOR_WG" && reg.IsSolid(s)) || (hm != "OCEAN_FLOOR_WG" && !reg.IsAir(s)) {
			return y + 1
		}
	}
	return -64
}
func (l *level) Biome(int32, int32, int32) data.BiomeID { return 0 }
func (l *level) SetBlockEntity(x, y, z int32, _ string, c nbt.Compound) {
	l.be[[3]int32{x, y, z}] = c
}
func (l *level) AddEntity(nbt.Compound)              {}
func (l *level) MarkPostProcess(int32, int32, int32) {}

// TestRuinedPortalScenes replays testdata/*.txt (RPScene inputs) and
// compares the written blocks, chest loot seeds and the placement random's
// final state with the probe output testdata/*.out.
func TestRuinedPortalScenes(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.txt.gz")
	for i := range files {
		files[i] = strings.TrimSuffix(files[i], ".gz")
	}
	if dir := os.Getenv("KREMENMC_RPSCENES"); dir != "" {
		files, _ = filepath.Glob(filepath.Join(dir, "*.txt"))
	}
	if len(files) == 0 {
		t.Skip("no scenes")
	}
	for _, f := range files {
		out := strings.TrimSuffix(f, ".txt") + ".out"
		t.Run(filepath.Base(f), func(t *testing.T) { runScene(t, f, out) })
	}
}

func runScene(t *testing.T, in, outPath string) {
	s := readScene(t, in)
	reg := data.Canonical()
	want := map[[3]int32]string{}
	wantBE := map[[3]int32]string{}
	wantNext := map[[2]int32]string{}
	f, err := openMaybeGz(outPath)
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
	st, err := structure.DecodeStart(parseSNBT(t, s.startLine).(nbt.Compound))
	if err != nil {
		t.Fatal(err)
	}
	lv := &level{s: s, m: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}}
	nextOK := 0
	var lavaAt [][3]int32
	for _, c := range s.chunks {
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
		deco := r.SetDecorationSeed(s.seed, c[0]<<4, c[1]<<4)
		r.SetFeatureSeed(deco, s.index, s.step)
		clip := structure.ChunkBox(c[0], c[1], -64, 384)
		for _, p := range st.Pieces {
			if os.Getenv("KREMENMC_RPTRACE") != "" {
				structure.RuinedPortalTrace(p, func(l string, v any) {
					if l == "lava" {
						lavaAt = append(lavaAt, v.([3]int32))
					}
					t.Logf("TRACE %s %v", l, v)
				})
			}
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
	for _, k := range lavaAt {
		t.Logf("LAVA %v scene %s want %q", k, reg.StateString(s.blocks[s.idx(k[0], k[1], k[2])]), want[k])
	}
	beBad := 0
	for k, w := range wantBE {
		if g, _ := lv.be[k]["LootTableSeed"].(int64); strconv.FormatInt(g, 10) != w {
			beBad++
			t.Logf("loot seed %v: want %s got %v", k, w, lv.be[k])
		}
	}
	t.Logf("%d/%d written blocks match, %d extra; loot seeds %d/%d; random %d/%d chunks", len(want)-countMissing(want, got), len(want), len(got)-countCommon(want, got), len(wantBE)-beBad, len(wantBE), nextOK, len(s.chunks))
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

func countMissing(want, got map[[3]int32]string) int {
	n := 0
	for k, w := range want {
		if got[k] != w {
			n++
		}
	}
	return n
}

func countCommon(want, got map[[3]int32]string) int {
	n := 0
	for k := range got {
		if _, ok := want[k]; ok {
			n++
		}
	}
	return n
}

// parseSNBT reads the quoted-key SNBT the scene dump writes.
func parseSNBT(t *testing.T, s string) any {
	i := 0
	num := func() string {
		j := i
		for i < len(s) && strings.IndexByte("-+.0123456789eE", s[i]) >= 0 {
			i++
		}
		return s[j:i]
	}
	quoted := func() string {
		j := i + 1
		for s[j] != '"' {
			if s[j] == '\\' {
				j++
			}
			j++
		}
		v, _ := strconv.Unquote(s[i : j+1])
		i = j + 1
		return v
	}
	var val func() any
	val = func() any {
		switch s[i] {
		case '{':
			i++
			c := nbt.Compound{}
			for s[i] != '}' {
				k := quoted()
				i++
				c[k] = val()
				if s[i] == ',' {
					i++
				}
			}
			i++
			return c
		case '[':
			i++
			if i+1 < len(s) && s[i+1] == ';' {
				i += 2
				ints := []int32{}
				for s[i] != ']' {
					n, _ := strconv.ParseInt(num(), 10, 32)
					ints = append(ints, int32(n))
					if s[i] == ',' {
						i++
					}
				}
				i++
				return ints
			}
			l := nbt.List{Type: nbt.TagEnd}
			for s[i] != ']' {
				v := val()
				switch v.(type) {
				case nbt.Compound:
					l.Type = nbt.TagCompound
				case string:
					l.Type = nbt.TagString
				}
				l.Items = append(l.Items, v)
				if s[i] == ',' {
					i++
				}
			}
			i++
			return l
		case '"':
			return quoted()
		}
		n := num()
		suf := byte(0)
		if i < len(s) && strings.IndexByte("bsLfd", s[i]) >= 0 {
			suf = s[i]
			i++
		}
		switch suf {
		case 'b':
			v, _ := strconv.ParseInt(n, 10, 8)
			return int8(v)
		case 's':
			v, _ := strconv.ParseInt(n, 10, 16)
			return int16(v)
		case 'L':
			v, _ := strconv.ParseInt(n, 10, 64)
			return v
		case 'f':
			v, _ := strconv.ParseFloat(n, 32)
			return float32(v)
		case 'd':
			v, _ := strconv.ParseFloat(n, 64)
			return v
		}
		v, err := strconv.ParseInt(n, 10, 32)
		if err != nil {
			t.Fatalf("snbt number %q at %d", n, i)
		}
		return int32(v)
	}
	return val()
}
