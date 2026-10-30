package structure_test

import (
	"bufio"
	"fmt"
	"math/rand"
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
)

// Ocean ruin scenes for kremenmc.probe.oruin.ORScene: one block scene,
// several START/CHUNKS runs placed in order. Inputs testdata/ocean_ruin/
// <name>.txt(.gz), outputs <name>.out(.gz); KREMENMC_ORSCENES=<dir> runs the
// scenes of a directory instead.

type orRun struct {
	start  nbt.Compound
	chunks [][2]int32
}

func readORScene(t *testing.T, path string) (*lscene, []orRun) {
	s := readLScene(t, path)
	f, err := openLegacy(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	var runs []orRun
	var start nbt.Compound
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "START "):
			src := l[6:]
			if !strings.Contains(src, `"id"`) {
				src = snbtKey.ReplaceAllString(src, `$1"$2":`)
			}
			start = parseSNBT(t, src).(nbt.Compound)
		case strings.HasPrefix(l, "CHUNKS "):
			var cs [][2]int32
			for _, c := range strings.Fields(l)[1:] {
				var cx, cz int32
				fmt.Sscanf(c, "%d,%d", &cx, &cz)
				cs = append(cs, [2]int32{cx, cz})
			}
			runs = append(runs, orRun{start, cs})
		}
	}
	return s, runs
}

// orLevel is llevel plus entities, scheduled ticks and the region random.
type orLevel struct {
	*llevel
	ents  []nbt.Compound
	ticks map[string]bool
	rr    rng.Source
	seed    int64
	hasSeed bool
	biome   data.BiomeID
}

func (l *orLevel) AddEntity(e nbt.Compound) { l.ents = append(l.ents, e) }
func (l *orLevel) ScheduleBlockTick(x, y, z int32, typ string, delay int32, fluid bool) {
	k := "block:"
	if fluid {
		k = "fluid:"
	}
	l.ticks[fmt.Sprintf("%d %d %d %s%s %d", x, y, z, k, strings.TrimPrefix(typ, "minecraft:"), delay)] = true
}
func (l *orLevel) RegionRandom() rng.Source { return l.rr }
func (l *orLevel) Seed() int64 {
	if l.hasSeed {
		return l.seed
	}
	return l.s.seed
}
func (l *orLevel) Biome(int32, int32, int32) data.BiomeID { return l.biome }
func (l *orLevel) SeaLevel() int32         { return 63 }

func TestOceanRuinScenes(t *testing.T) {
	files, _ := filepath.Glob("testdata/ocean_ruin/scene_*.txt.gz")
	for i := range files {
		files[i] = strings.TrimSuffix(files[i], ".gz")
	}
	if dir := os.Getenv("KREMENMC_ORSCENES"); dir != "" {
		files, _ = filepath.Glob(filepath.Join(dir, "*.txt"))
	}
	if len(files) == 0 {
		t.Skip("no scenes")
	}
	for _, f := range files {
		out := strings.TrimSuffix(f, ".txt") + ".out"
		t.Run(filepath.Base(f), func(t *testing.T) { runORScene(t, f, out) })
	}
}

func runORScene(t *testing.T, in, outPath string) {
	s, runs := readORScene(t, in)
	reg := data.Canonical()
	want := map[[3]int32]string{}
	wantBE := map[[3]int32]string{}
	var wantNext []string
	wantTicks := map[string]bool{}
	var wantEnts []string
	var wantPY []string
	added := 0
	var levelSeed int64
	hasSeed := false
	biome := ""
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
				lt := ""
				if j := strings.Index(l, `LootTable:"`); j >= 0 {
					lt = l[j+11:]
					lt = lt[:strings.IndexByte(lt, '"')]
				}
				wantBE[[3]int32{x, y, z}] = lt + " " + strings.TrimSuffix(v[:strings.IndexAny(v, ",}")], "L")
			}
		case strings.HasPrefix(l, "NEXT "):
			wantNext = append(wantNext, l)
		case strings.HasPrefix(l, "T "):
			fs := strings.Fields(l)
			wantTicks[strings.Join(fs[1:6], " ")] = true
		case strings.HasPrefix(l, "W addFreshEntityWithPassengers Drowned["):
			added++
		case strings.HasPrefix(l, "E "):
			wantEnts = append(wantEnts, orEntKey(l))
		case strings.HasPrefix(l, "PY "):
			fs := strings.Fields(l)
			wantPY = append(wantPY, fs[2])
		case strings.HasPrefix(l, "LEVELSEED "):
			fs := strings.Fields(l)
			fmt.Sscan(fs[1], &levelSeed)
			hasSeed = true
			biome = fs[3]
		}
	}
	f.Close()
	lv := &orLevel{llevel: &llevel{s: s, m: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}}, ticks: map[string]bool{}, seed: levelSeed, hasSeed: hasSeed}
	if id, ok := reg.BiomeByName(strings.TrimPrefix(biome, "minecraft:")); ok {
		lv.biome = id
	}
	var gotNext, gotPY []string
	structure.SetOceanRuinYHook(func(y int32) { gotPY = append(gotPY, strconv.Itoa(int(y))) })
	defer structure.SetOceanRuinYHook(nil)
	for _, run := range runs {
		st, err := structure.DecodeStart(run.start)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range run.chunks {
			r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
			deco := r.SetDecorationSeed(s.seed, c[0]<<4, c[1]<<4)
			r.SetFeatureSeed(deco, s.index, s.step)
			lv.rr = rng.NewXoroshiro(s.seed).ForkPositional().FromHashOf("minecraft:worldgen_region_random").ForkPositional().At(c[0]<<4, 0, c[1]<<4)
			clip := structure.ChunkBox(c[0], c[1], -64, 384)
			for _, p := range st.Pieces {
				if p.Box().Intersects(clip) {
					if err := p.Place(lv, r, clip, c[0], c[1]); err != nil {
						t.Fatal(err)
					}
				}
			}
			gotNext = append(gotNext, fmt.Sprintf("NEXT %d %d %d %d", c[0], c[1], r.NextLong(), lv.rr.NextLong()))
		}
	}
	afterBad := 0
	if len(wantPY) > 0 && strings.Join(wantPY, " ") != strings.Join(gotPY, " ") {
		afterBad = 1
		t.Logf("piece heights differ:\n want %v\n got  %v", wantPY, gotPY)
	}
	nextBad := 0
	for i := range wantNext {
		if i >= len(gotNext) || gotNext[i] != wantNext[i] {
			nextBad++
			if nextBad <= 5 {
				g := ""
				if i < len(gotNext) {
					g = gotNext[i]
				}
				t.Logf("random: want %s got %s", wantNext[i], g)
			}
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
		lt, _ := lv.be[k]["LootTable"].(string)
		sd, _ := lv.be[k]["LootTableSeed"].(int64)
		if g := lt + " " + strconv.FormatInt(sd, 10); g != w {
			beBad++
			if beBad <= 5 {
				t.Logf("loot %v: want %s got %s", k, w, g)
			}
		}
	}
	for k, v := range lv.be {
		if _, ok := wantBE[k]; !ok && v["LootTable"] != nil {
			beBad++
			t.Logf("extra loot block entity %v %v", k, v)
		}
	}
	tickBad := 0
	for k := range wantTicks {
		if !lv.ticks[k] {
			tickBad++
			if tickBad <= 5 || os.Getenv("KREMENMC_ORTICKS") != "" {
				t.Logf("missing tick %s %s", k, orTickCtx(lv, k))
			}
		}
	}
	for k := range lv.ticks {
		if !wantTicks[k] {
			tickBad++
			if tickBad <= 10 || os.Getenv("KREMENMC_ORTICKS") != "" {
				t.Logf("extra tick %s %s", k, orTickCtx(lv, k))
			}
		}
	}
	var gotEnts []string
	var flat func(e nbt.Compound)
	flat = func(e nbt.Compound) {
		gotEnts = append(gotEnts, orGoEntKey(e))
		if ps, ok := e["Passengers"].(nbt.List); ok {
			for _, p := range ps.Items {
				flat(p.(nbt.Compound))
			}
		}
	}
	for _, e := range lv.ents {
		flat(e)
	}
	sort.Strings(wantEnts)
	sort.Strings(gotEnts)
	entBad := 0
	// The probe world loses an added entity now and then (105 drowned
	// added, 104 listed in syn4): every listed entity must match, and the
	// drowned count must equal the probe's adds (a mounted drowned is also
	// added through its vehicle).
	if len(lv.ents) != added && added > 0 {
		entBad = 1
		t.Logf("%d entities added, probe added %d", len(lv.ents), added)
	}
	if !orSubset(wantEnts, gotEnts) {
		entBad = 1
		cnt := map[string]int{}
		for _, w := range wantEnts {
			cnt[w]++
		}
		for _, g := range gotEnts {
			cnt[g]--
		}
		for k, v := range cnt {
			if v > 0 {
				t.Logf("missing entity %s", k)
			} else if v < 0 {
				t.Logf("extra entity %s", k)
			}
		}
	}
	t.Logf("%d/%d written blocks match, %d extra; loot %d/%d; random %d/%d; ticks %d/%d (%d bad); entities %d/%d; heights ok %v",
		len(want)-countMissingL(want, got), len(want), len(diffs)-countMissingL(want, got), len(wantBE)-beBad, len(wantBE),
		len(wantNext)-nextBad, len(wantNext), len(wantTicks), len(lv.ticks), tickBad, len(gotEnts), len(wantEnts), afterBad == 0)
	for i, d := range diffs {
		if i >= 25 {
			break
		}
		t.Log(d)
	}
	if len(diffs) > 0 || beBad > 0 || nextBad > 0 || tickBad > 0 || entBad > 0 || afterBad > 0 {
		t.Errorf("%s differs: %v", in, byType)
	}
}

var orBBRe = regexp.MustCompile(`BB:\[I;(-?\d+,-?\d+,-?\d+,-?\d+,-?\d+,-?\d+)\]`)

// orEntKey reduces a probe entity line to the compared fields.
func orEntKey(l string) string {
	fs := strings.SplitN(l, " ", 6)
	if i := strings.Index(fs[5], "Passengers:["); i >= 0 {
		d, j := 0, i+len("Passengers:")
		for ; j < len(fs[5]); j++ {
			if fs[5][j] == '[' || fs[5][j] == '{' {
				d++
			} else if fs[5][j] == ']' || fs[5][j] == '}' {
				d--
				if d == 0 {
					break
				}
			}
		}
		fs[5] = fs[5][:i] + fs[5][j+1:]
	}
	k := fmt.Sprintf("%s %s %s %s", fs[1], fs[2], fs[3], fs[4])
	for _, re := range orEntRes {
		m := re.FindStringSubmatch(fs[5])
		v := "-"
		if m != nil {
			v = m[1]
		}
		k += " " + v
	}
	return k
}

var orEntRes = []*regexp.Regexp{
	regexp.MustCompile(`IsBaby:(\d)b`), regexp.MustCompile(`LeftHanded:(\d)b`), regexp.MustCompile(`PersistenceRequired:(\d)b`),
	regexp.MustCompile(`mainhand:\{[^}]*id:"minecraft:([^"]+)"`), regexp.MustCompile(`offhand:\{[^}]*id:"minecraft:([^"]+)"`),
}

func orGoEntKey(e nbt.Compound) string {
	id, _ := e["id"].(string)
	pos, _ := e["Pos"].(nbt.List)
	var p [3]float64
	for i := range 3 {
		if i < len(pos.Items) {
			p[i], _ = pos.Items[i].(float64)
		}
	}
	jf := func(v float64) string {
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s
	}
	k := fmt.Sprintf("%s %s %s %s", id, jf(p[0]), jf(p[1]), jf(p[2]))
	for _, f := range []string{"IsBaby", "LeftHanded", "PersistenceRequired"} {
		v := "-"
		if b, ok := e[f].(int8); ok {
			v = strconv.Itoa(int(b))
		}
		k += " " + v
	}
	eq, _ := e["equipment"].(nbt.Compound)
	for _, f := range []string{"mainhand", "offhand"} {
		v := "-"
		if it, ok := eq[f].(nbt.Compound); ok {
			v, _ = it["id"].(string)
			v = strings.TrimPrefix(v, "minecraft:")
		}
		k += " " + v
	}
	return k
}

// TestOceanRuinSynthDump (KREMENMC_ORSYNTH=out@seed@n) writes a synthetic
// ORScene input: n ruins of random kind on random sea floors (flat, rough,
// sloped, pitted, with plants, air pockets, dry land above sea level).
func TestOceanRuinSynthDump(t *testing.T) {
	v := os.Getenv("KREMENMC_ORSYNTH")
	if v == "" {
		t.Skip("KREMENMC_ORSYNTH not set")
	}
	p := strings.Split(v, "@")
	var seed int64
	var n int
	fmt.Sscan(p[1], &seed)
	fmt.Sscan(p[2], &n)
	rnd := rand.New(rand.NewSource(seed))
	reg := data.Canonical()
	st := func(s string) data.StateID {
		id, ok := reg.ParseState(s)
		if !ok {
			t.Fatalf("state %s", s)
		}
		return id
	}
	const area = 48
	rg := structure.Box{MinX: 0, MinY: 20, MinZ: 0, MaxX: int32(n*area - 1), MaxY: 90, MaxZ: area - 1}
	sx, sy, sz := rg.MaxX-rg.MinX+1, rg.MaxY-rg.MinY+1, rg.MaxZ-rg.MinZ+1
	blocks := make([]data.StateID, sx*sy*sz)
	at := func(x, y, z int32) *data.StateID { return &blocks[((y-rg.MinY)*sz+z)*sx+x] }
	water, air, stone := st("minecraft:water"), st("minecraft:air"), st("minecraft:stone")
	tops := []string{"minecraft:stone", "minecraft:sand", "minecraft:gravel", "minecraft:dirt", "minecraft:clay", "minecraft:sandstone", "minecraft:ice", "minecraft:packed_ice", "minecraft:magma_block"}
	var runs []string
	for i := range n {
		x0 := int32(i * area)
		warm := rnd.Intn(2) == 0
		big := rnd.Intn(3) == 0
		var num int32
		if big {
			num = int32(rnd.Intn(4))
			if warm {
				num += 4
			} else {
				num = []int32{1, 2, 3, 8}[num]
			}
		} else {
			num = int32(1 + rnd.Intn(8))
		}
		rot := structure.Rotation(rnd.Intn(4))
		ox, oz := x0+16+int32(rnd.Intn(16)), int32(16+rnd.Intn(16))
		ps, err := structure.OceanRuinPiecesForTest(warm, ox, oz, rot, big, num)
		if err != nil {
			t.Fatal(err)
		}
		id := "minecraft:ocean_ruin_cold"
		if warm {
			id = "minecraft:ocean_ruin_warm"
		}
		start := &structure.Start{ID: id, ChunkX: ox >> 4, ChunkZ: oz >> 4, Pieces: ps}
		b := start.Box()
		mode := rnd.Intn(8)
		pits := map[[2]int32]int32{}
		if mode >= 6 {
			// exactly k deep columns (3..6) and some depth-2 ones inside the box
			xw := b.MaxX - b.MinX + 1
			k := xw - 4 + int32(rnd.Intn(5))
			for len(pits) < int(k) {
				c := [2]int32{b.MinX + int32(rnd.Intn(int(xw))) - x0, b.MinZ + int32(rnd.Intn(int(b.MaxZ-b.MinZ+1)))}
				if c[0]+x0 == ox && c[1] == oz {
					continue
				}
				pits[c] = 3 + int32(rnd.Intn(4))
			}
			for range 4 {
				c := [2]int32{b.MinX + int32(rnd.Intn(int(xw))) - x0, b.MinZ + int32(rnd.Intn(int(b.MaxZ-b.MinZ+1)))}
				if _, ok := pits[c]; !ok && !(c[0]+x0 == ox && c[1] == oz) {
					pits[c] = 1 + int32(rnd.Intn(2))
				}
			}
		}
		base := int32(38 + rnd.Intn(14))
		dry := mode == 5
		if dry {
			base = int32(56 + rnd.Intn(11))
		}
		top := st(tops[rnd.Intn(len(tops))])
		gx, gz := rnd.Intn(3)-1, rnd.Intn(3)-1
		for z := int32(0); z < area; z++ {
			for x := int32(0); x < area; x++ {
				h := base
				switch mode {
				case 1:
					h += int32(rnd.Intn(5) - 2)
				case 2:
					h += (x*int32(gx) + z*int32(gz)) / 4
				case 3:
					if rnd.Intn(5) == 0 {
						h -= int32(1 + rnd.Intn(6))
					}
				case 4:
					h += int32(rnd.Intn(3) - 1)
				case 6, 7:
					h -= pits[[2]int32{x, z}]
				}
				h = max(h, rg.MinY+8)
				for y := rg.MinY; y <= rg.MaxY; y++ {
					var s data.StateID
					switch {
					case y < h-1:
						s = stone
					case y == h-1:
						s = top
					case y <= 62 && !dry:
						s = water
					default:
						s = air
					}
					*at(x0+x, y, z) = s
				}
				if !dry && mode == 4 && rnd.Intn(4) == 0 {
					k := int32(1 + rnd.Intn(3))
					for y := h; y < h+k; y++ {
						*at(x0+x, y, z) = air
					}
				}
				if !dry && rnd.Intn(6) == 0 {
					switch rnd.Intn(4) {
					case 0:
						*at(x0+x, h, z) = st("minecraft:seagrass")
					case 1:
						k := int32(1 + rnd.Intn(4))
						for y := h; y < h+k; y++ {
							*at(x0+x, y, z) = st("minecraft:kelp_plant")
						}
						*at(x0+x, h+k, z) = st("minecraft:kelp[age=3]")
					case 2:
						*at(x0+x, h, z) = st("minecraft:sea_pickle[pickles=2,waterlogged=true]")
					case 3:
						*at(x0+x, h, z) = st("minecraft:tall_seagrass[half=lower]")
						*at(x0+x, h+1, z) = st("minecraft:tall_seagrass[half=upper]")
					}
				}
			}
		}
		var cs []string
		for cx := b.MinX >> 4; cx <= b.MaxX>>4; cx++ {
			for cz := b.MinZ >> 4; cz <= b.MaxZ>>4; cz++ {
				cs = append(cs, fmt.Sprintf("%d,%d", cx, cz))
			}
		}
		runs = append(runs, "START "+snbt(start.Encode()), "CHUNKS "+strings.Join(cs, " "))
	}
	step, index := structStepIndex(t, "minecraft:ocean_ruin_cold")
	pal := map[data.StateID]int{}
	var palList []data.StateID
	var rle strings.Builder
	last, run := -1, 0
	for _, s := range blocks {
		i, ok := pal[s]
		if !ok {
			i = len(palList)
			pal[s] = i
			palList = append(palList, s)
		}
		if i == last {
			run++
			continue
		}
		if run > 0 {
			fmt.Fprintf(&rle, " %d*%d", last, run)
		}
		last, run = i, 1
	}
	fmt.Fprintf(&rle, " %d*%d", last, run)
	f, err := os.Create(p[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "SEED %d %d %d\n", seed, index, step)
	fmt.Fprintf(f, "REGION %d %d %d %d %d %d\n", rg.MinX, rg.MinY, rg.MinZ, rg.MaxX, rg.MaxY, rg.MaxZ)
	fmt.Fprintf(f, "PAL %d\n", len(palList))
	for _, s := range palList {
		fmt.Fprintln(f, reg.StateString(s))
	}
	fmt.Fprintf(f, "DATA%s\n", rle.String())
	for _, l := range runs {
		fmt.Fprintln(f, l)
	}
}

func orTickCtx(lv *orLevel, k string) string {
	var x, y, z int32
	fmt.Sscan(k, &x, &y, &z)
	reg := data.Canonical()
	return fmt.Sprintf("at %s below %s above %s (scene %s)", reg.StateString(lv.Block(x, y, z)), reg.StateString(lv.Block(x, y-1, z)), reg.StateString(lv.Block(x, y+1, z)), reg.StateString(lv.orig(x, y, z)))
}

func orSubset(want, got []string) bool {
	cnt := map[string]int{}
	for _, g := range got {
		cnt[g]++
	}
	for _, w := range want {
		if cnt[w] == 0 {
			return false
		}
		cnt[w]--
	}
	return true
}
