package structure_test

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// Scene files for kremenmc.probe.legacy.MineScene: a block scene taken from
// kremenmc's terrain-status chunks (caves, aquifers, lava), a saved start and
// the chunks to place in order. TestMineshaftSceneDump writes one,
// TestMineshaftScene replays it in kremenmc and diffs the probe's output.

func snbt(v any) string {
	var b strings.Builder
	var w func(v any)
	w = func(v any) {
		switch t := v.(type) {
		case nbt.Compound:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strconv.Quote(k))
				b.WriteByte(':')
				w(t[k])
			}
			b.WriteByte('}')
		case nbt.List:
			b.WriteByte('[')
			for i, it := range t.Items {
				if i > 0 {
					b.WriteByte(',')
				}
				w(it)
			}
			b.WriteByte(']')
		case string:
			b.WriteString(strconv.Quote(t))
		case int8:
			fmt.Fprintf(&b, "%db", t)
		case int16:
			fmt.Fprintf(&b, "%ds", t)
		case int32:
			fmt.Fprintf(&b, "%d", t)
		case int64:
			fmt.Fprintf(&b, "%dL", t)
		case float32:
			fmt.Fprintf(&b, "%gf", t)
		case float64:
			fmt.Fprintf(&b, "%gd", t)
		case []int32:
			b.WriteString("[I;")
			for i, x := range t {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "%d", x)
			}
			b.WriteByte(']')
		case []int64:
			b.WriteString("[L;")
			for i, x := range t {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "%dL", x)
			}
			b.WriteByte(']')
		case []byte:
			b.WriteString("[B;")
			for i, x := range t {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "%db", int8(x))
			}
			b.WriteByte(']')
		default:
			panic(fmt.Sprintf("snbt: %T", v))
		}
	}
	w(v)
	return b.String()
}

type msScene struct {
	seed        int64
	index, step int32
	rg          structure.Box
	blocks      []data.StateID
	start       nbt.Compound
	startLine   string
	chunks      [][2]int32
}

func (s *msScene) idx(x, y, z int32) int {
	sx, sz := s.rg.MaxX-s.rg.MinX+1, s.rg.MaxZ-s.rg.MinZ+1
	return int(((y-s.rg.MinY)*sz+(z-s.rg.MinZ))*sx + (x - s.rg.MinX))
}

// TestMineshaftSceneDump (KREMENMC_MSSCENE_DUMP=out, KREMENMC_MSSCENE_SPEC=
// world@startcx,startcz@cx,cz+cx,cz...) writes a MineScene input.
func TestMineshaftSceneDump(t *testing.T) {
	outf := os.Getenv("KREMENMC_MSSCENE_DUMP")
	if outf == "" {
		t.Skip("KREMENMC_MSSCENE_DUMP not set")
	}
	spec := strings.Split(os.Getenv("KREMENMC_MSSCENE_SPEC"), "@")
	w := spec[0]
	seed := msWorldSeeds[w]
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/region")
	var start nbt.Compound
	for k, c := range ref {
		if strings.HasSuffix(k, "@"+spec[1]) && strings.HasPrefix(k, "minecraft:mineshaft") {
			start = c
		}
	}
	if start == nil {
		t.Fatalf("no start at %s", spec[1])
	}
	var chunks [][2]int32
	for _, c := range strings.Split(spec[2], "+") {
		var cx, cz int32
		fmt.Sscanf(c, "%d,%d", &cx, &cz)
		chunks = append(chunks, [2]int32{cx, cz})
	}
	st, err := structure.DecodeStart(start)
	if err != nil {
		t.Fatal(err)
	}
	sb := st.Box()
	rg := structure.Box{MinX: 1 << 30, MinY: -64, MinZ: 1 << 30, MaxX: -1 << 30, MaxY: min(sb.MaxY+3, 319), MaxZ: -1 << 30}
	for _, c := range chunks {
		rg.MinX, rg.MinZ = min(rg.MinX, (c[0]-1)<<4), min(rg.MinZ, (c[1]-1)<<4)
		rg.MaxX, rg.MaxZ = max(rg.MaxX, (c[0]+2)<<4-1), max(rg.MaxZ, (c[1]+2)<<4-1)
	}
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: func(string, ...any) {}})
	reg := data.Canonical()
	pcs := map[[2]int32]*world.Chunk{}
	for cx := rg.MinX >> 4; cx <= rg.MaxX>>4; cx++ {
		for cz := rg.MinZ >> 4; cz <= rg.MaxZ>>4; cz++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusTerrain)
			if err != nil {
				t.Fatal(err)
			}
			pcs[[2]int32{cx, cz}] = pc.Chunk
		}
	}
	pal := map[data.StateID]int{}
	var palList []data.StateID
	var rle strings.Builder
	last, run := -1, 0
	flush := func() {
		if run > 0 {
			fmt.Fprintf(&rle, " %d*%d", last, run)
		}
	}
	for y := rg.MinY; y <= rg.MaxY; y++ {
		for z := rg.MinZ; z <= rg.MaxZ; z++ {
			for x := rg.MinX; x <= rg.MaxX; x++ {
				s := pcs[[2]int32{x >> 4, z >> 4}].Block(int(x&15), int(y), int(z&15))
				i, ok := pal[s]
				if !ok {
					i = len(palList)
					pal[s] = i
					palList = append(palList, s)
				}
				if i == last {
					run++
				} else {
					flush()
					last, run = i, 1
				}
			}
		}
	}
	flush()
	f, err := os.Create(outf)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "SEED %d %d %d\n", seed, 0, 3)
	fmt.Fprintf(f, "REGION %d %d %d %d %d %d\n", rg.MinX, rg.MinY, rg.MinZ, rg.MaxX, rg.MaxY, rg.MaxZ)
	fmt.Fprintf(f, "PAL %d\n", len(palList))
	for _, s := range palList {
		fmt.Fprintln(f, reg.StateString(s))
	}
	fmt.Fprintf(f, "DATA%s\n", rle.String())
	// Placement sets hps on the saved start: reset it.
	for _, it := range start["Children"].(nbt.List).Items {
		if c := it.(nbt.Compound); c["hps"] != nil {
			c["hps"] = int8(0)
		}
	}
	fmt.Fprintf(f, "START %s\n", snbt(start))
	var cs []string
	for _, c := range chunks {
		cs = append(cs, fmt.Sprintf("%d,%d", c[0], c[1]))
	}
	fmt.Fprintf(f, "CHUNKS %s\n", strings.Join(cs, " "))
	t.Logf("scene %v, %d palette states", rg, len(palList))
}

func readMsScene(t *testing.T, path string) *msScene {
	f, err := os.Open(path)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	reg := data.Canonical()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	s := &msScene{}
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
			for i := 0; i < n; i++ {
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
				for j := 0; j < c; j++ {
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

type sceneLevel struct {
	s    *msScene
	hm   map[[2]int32][]int32 // OCEAN_FLOOR_WG per chunk, primed on first use
	m    map[[3]int32]data.StateID
	be   map[[3]int32]nbt.Compound
	ents []nbt.Compound
}

func (l *sceneLevel) MinY() int32 { return -64 }
func (l *sceneLevel) MaxY() int32 { return 320 }
func (l *sceneLevel) Block(x, y, z int32) data.StateID {
	if s, ok := l.m[[3]int32{x, y, z}]; ok {
		return s
	}
	if l.s.rg.Contains(x, y, z) {
		return l.s.blocks[l.s.idx(x, y, z)]
	}
	if y <= 3 {
		// The probe's flat world is solid (sandstone layers) up to y 3.
		st, _ := data.Canonical().ParseState("minecraft:sandstone")
		return st
	}
	return 0
}
func (l *sceneLevel) SetBlock(x, y, z int32, s data.StateID) bool {
	l.m[[3]int32{x, y, z}] = s
	return true
}

// Height mimics the probe: a full chunk primes OCEAN_FLOOR_WG from its
// blocks the first time it is asked (top motion-blocking block + 1).
func (l *sceneLevel) Height(_ string, x, z int32) int32 {
	k := [2]int32{x >> 4, z >> 4}
	h, ok := l.hm[k]
	if !ok {
		reg := data.Canonical()
		h = make([]int32, 256)
		for i := range h {
			cx, cz := k[0]<<4+int32(i&15), k[1]<<4+int32(i>>4)
			h[i] = -64
			for y := int32(319); y >= -64; y-- {
				if reg.IsSolid(l.Block(cx, y, cz)) {
					h[i] = y + 1
					break
				}
			}
		}
		if l.hm == nil {
			l.hm = map[[2]int32][]int32{}
		}
		l.hm[k] = h
	}
	return h[(z&15)<<4|(x&15)]
}
func (l *sceneLevel) Biome(int32, int32, int32) data.BiomeID { return 0 }
func (l *sceneLevel) SetBlockEntity(x, y, z int32, id string, c nbt.Compound) {
	l.be[[3]int32{x, y, z}] = c
}
func (l *sceneLevel) AddEntity(e nbt.Compound)            { l.ents = append(l.ents, e) }
func (l *sceneLevel) RemoveBlockEntity(x, y, z int32)     { delete(l.be, [3]int32{x, y, z}) }
func (l *sceneLevel) MarkPostProcess(int32, int32, int32) {}

// TestMineshaftScene (KREMENMC_MSSCENE=in[,out]) replays a MineScene case.
func TestMineshaftScene(t *testing.T) {
	spec := os.Getenv("KREMENMC_MSSCENE")
	if spec == "" {
		t.Skip("KREMENMC_MSSCENE not set")
	}
	for _, in := range strings.Split(spec, "+") {
		runMsScene(t, in, in+".out")
	}
}

func runMsScene(t *testing.T, in, outPath string) {
	s := readMsScene(t, in)
	reg := data.Canonical()
	want := map[[3]int32]string{}
	var wantEnts []string
	wantNext := map[[2]int32]string{}
	wantBE := map[[3]int32]string{}
	f, err := os.Open(outPath)
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
			if _, ok := want[[3]int32{x, y, z}]; ok {
				wantBE[[3]int32{x, y, z}] = l
			}
		case strings.HasPrefix(l, "E "):
			fl := strings.Fields(l)
			wantEnts = append(wantEnts, fl[1]+" "+fl[2]+" "+fl[3]+" "+fl[4]+" "+strings.TrimSuffix(fl[6], "L"))
		case strings.HasPrefix(l, "NEXT "):
			var cx, cz int32
			var v string
			fmt.Sscan(l[5:], &cx, &cz, &v)
			wantNext[[2]int32{cx, cz}] = v
		}
	}
	f.Close()
	var startC nbt.Compound
	{
		// The start is re-read from the reference world by the dump; here
		// parse it back through the kremenmc decoder via the saved SNBT keys.
		startC = parseSNBT(t, s.startLine).(nbt.Compound)
	}
	st, err := structure.DecodeStart(startC)
	if err != nil {
		t.Fatal(err)
	}
	lv := &sceneLevel{s: s, m: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}}
	vlog := readMsLog(t, strings.TrimSuffix(in, ".txt")+".log")
	drawBad := 0
	nextOK := 0
	for _, c := range s.chunks {
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
		deco := r.SetDecorationSeed(s.seed, c[0]<<4, c[1]<<4)
		r.SetFeatureSeed(deco, s.index, s.step)
		clip := structure.ChunkBox(c[0], c[1], -64, 384)
		for pi, p := range st.Pieces {
			if !p.Box().Intersects(clip) {
				continue
			}
			if os.Getenv("KREMENMC_MSLIQ") != "" && p.Type() == "minecraft:mscorridor" && vlog != nil {
				b := p.Box()
				var all, inClip, shellClip, shellAll int
				for y := b.MinY - 1; y <= b.MaxY+1; y++ {
					for z := b.MinZ - 1; z <= b.MaxZ+1; z++ {
						for x := b.MinX - 1; x <= b.MaxX+1; x++ {
							n := reg.StateString(lv.Block(x, y, z))
							if !strings.HasPrefix(n, "minecraft:water") && !strings.HasPrefix(n, "minecraft:lava") {
								continue
							}
							inside := b.Contains(x, y, z)
							shell := inside && (x == b.MinX || x == b.MaxX || y == b.MinY || y == b.MaxY || z == b.MinZ || z == b.MaxZ)
							if inside {
								all++
							} else if clip.Contains(x, y, z) {
								t.Logf("LIQ outside-adjacent %d,%d,%d %s", x, y, z, n)
							}
							if inside && clip.Contains(x, y, z) {
								inClip++
							}
							if shell {
								shellAll++
								if clip.Contains(x, y, z) {
									shellClip++
								}
							}
						}
					}
				}
				t.Logf("LIQ chunk %v piece %d accepted=%v all=%d inClip=%d shell=%d shellClip=%d", c, pi, len(vlog[[3]int32{c[0], c[1], int32(pi)}]) > 0, all, inClip, shellAll, shellClip)
			}
			var gd []msDraw
			structure.MsPlaceTraced(p, lv, r, clip, func(l, v string) { gd = append(gd, msDraw{l, v}) })
			if vlog == nil {
				continue
			}
			vd := vlog[[3]int32{c[0], c[1], int32(pi)}]
			n := 0
			for n < len(gd) && n < len(vd) && msDrawEq(gd[n], vd[n]) {
				n++
			}
			if n < len(gd) || n < len(vd) {
				drawBad++
				ctx := func(d []msDraw) string {
					var b []string
					for i := max(0, n-3); i < min(len(d), n+4); i++ {
						b = append(b, fmt.Sprintf("%d:%s=%s", i, d[i].label, d[i].val))
					}
					return strings.Join(b, " ")
				}
				t.Logf("chunk %v piece %d %s %v: draws differ at %d (kremenmc %d, ref %d)\n   kremenmc %s\n   ref  %s", c, pi, p.Type(), p.Box(), n, len(gd), len(vd), ctx(gd), ctx(vd))
			}
		}
		if n := strconv.FormatInt(r.NextLong(), 10); n == wantNext[c] {
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
	norm := func(x string) string {
		if i := strings.IndexByte(x, '['); i >= 0 && strings.HasSuffix(x[:i], "_fence") {
			return x[:i]
		}
		return x
	}
	var diffs []string
	byKind := map[string]int{}
	for k, w := range want {
		if norm(got[k]) != norm(w) {
			g := got[k]
			if g == "" {
				g = "(scene " + reg.StateString(s.blocks[s.idx(k[0], k[1], k[2])]) + ")"
			}
			diffs = append(diffs, fmt.Sprintf("%v kremenmc %s ref %s", k, g, w))
			byKind[strings.SplitN(g, "[", 2)[0]+" -> "+strings.SplitN(w, "[", 2)[0]]++
		}
	}
	for k, g := range got {
		if _, ok := want[k]; !ok {
			w := "(scene " + reg.StateString(s.blocks[s.idx(k[0], k[1], k[2])]) + ")"
			diffs = append(diffs, fmt.Sprintf("%v kremenmc %s ref %s", k, g, w))
			byKind[strings.SplitN(g, "[", 2)[0]+" -> "+strings.SplitN(w, "[", 2)[0]]++
		}
	}
	sort.Strings(diffs)
	var gotEnts []string
	for _, e := range lv.ents {
		p := e["Pos"].(nbt.List).Items
		gotEnts = append(gotEnts, fmt.Sprint(e["id"], " ", p[0], " ", p[1], " ", p[2], " ", e["LootTableSeed"]))
	}
	sort.Strings(gotEnts)
	sort.Strings(wantEnts)
	var gotBE []string
	for k, c := range lv.be {
		gotBE = append(gotBE, fmt.Sprint(k, c))
	}
	lim := 60
	if os.Getenv("KREMENMC_MSALL") != "" {
		lim = len(diffs)
	}
	t.Logf("%s: pieces with draw diffs %d; blocks ref-changed %d, diffs %d %v; random %d/%d chunks; entities kremenmc %v ref %v; block entities kremenmc %v ref %v\n  %s",
		in, drawBad, len(want), len(diffs), byKind, nextOK, len(s.chunks), gotEnts, wantEnts, gotBE, wantBE, strings.Join(diffs[:min(lim, len(diffs))], "\n  "))
	if len(diffs) > 0 || nextOK != len(s.chunks) || fmt.Sprint(gotEnts) != fmt.Sprint(wantEnts) || len(gotBE) != len(wantBE) {
		t.Errorf("%s differs", in)
	}
}

// parseSNBT reads the subset snbt writes.
func parseSNBT(t *testing.T, s string) any {
	i := 0
	var val func() any
	num := func() string {
		j := i
		for i < len(s) && strings.IndexByte("-+.0123456789eE", s[i]) >= 0 {
			i++
		}
		return s[j:i]
	}
	val = func() any {
		switch s[i] {
		case '{':
			i++
			c := nbt.Compound{}
			for s[i] != '}' {
				j := i + 1
				for s[j] != '"' {
					if s[j] == '\\' {
						j++
					}
					j++
				}
				k, _ := strconv.Unquote(s[i : j+1])
				i = j + 2
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
				kind := s[i]
				i += 2
				var i32 []int32
				var i64 []int64
				var b []byte
				for s[i] != ']' {
					n, _ := strconv.ParseInt(num(), 10, 64)
					if i < len(s) && (s[i] == 'L' || s[i] == 'b') {
						i++
					}
					switch kind {
					case 'I':
						i32 = append(i32, int32(n))
					case 'L':
						i64 = append(i64, n)
					default:
						b = append(b, byte(n))
					}
					if s[i] == ',' {
						i++
					}
				}
				i++
				switch kind {
				case 'I':
					if i32 == nil {
						i32 = []int32{}
					}
					return i32
				case 'L':
					return i64
				}
				return b
			}
			l := nbt.List{Type: nbt.TagEnd}
			for s[i] != ']' {
				v := val()
				switch v.(type) {
				case nbt.Compound:
					l.Type = nbt.TagCompound
				case []int32:
					l.Type = nbt.TagIntArray
				case string:
					l.Type = nbt.TagString
				case int32:
					l.Type = nbt.TagInt
				}
				l.Items = append(l.Items, v)
				if s[i] == ',' {
					i++
				}
			}
			i++
			return l
		case '"':
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

type msDraw struct {
	label, val string
}

// msVanillaLabel maps a MineScene draw path to the label kremenmc traces.
func msVanillaLabel(what, path string) string {
	p := strings.SplitN(path, "<TickTask", 2)[0]
	switch {
	case strings.HasPrefix(p, "StructurePiece.generateMaybeBox"):
		return "maybeBox"
	case strings.HasPrefix(p, "StructurePiece.maybeGenerateBlock<MineShaftCorridor.placeSupport"):
		return "torch"
	case strings.HasPrefix(p, "MineShaftCorridor.placeSupport"):
		return "support"
	case strings.HasPrefix(p, "MineShaftCorridor.maybePlaceCobWeb"):
		return "web"
	case strings.HasPrefix(p, "StructurePiece.maybeGenerateBlock<MineShaftCorridor.postProcess"):
		return "rail"
	case strings.HasPrefix(p, "MineShaftCorridor.createChest") && strings.HasPrefix(what, "nextBoolean"):
		return "chestRail"
	case strings.HasPrefix(p, "MineShaftCorridor.createChest") && strings.HasPrefix(what, "nextLong"):
		return "loot"
	case strings.HasPrefix(p, "MineShaftCorridor.postProcess") && what == "nextInt(100)":
		return "chest"
	case strings.HasPrefix(p, "MineShaftCorridor.postProcess") && what == "nextInt(3)":
		return "spawner"
	}
	return what + "@" + p
}

// readMsLog reads a LOG=1 PERPIECE=1 MineScene log: draws per chunk and piece.
func readMsLog(t *testing.T, path string) map[[3]int32][]msDraw {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[[3]int32][]msDraw{}
	var cx, cz, pi int32
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "CHUNK "):
			fmt.Sscan(l[6:], &cx, &cz)
		case strings.HasPrefix(l, "PIECE "):
			fmt.Sscan(l[6:], &pi)
			out[[3]int32{cx, cz, pi}] = []msDraw{}
		case strings.HasPrefix(l, "R"):
			// R<nest> <n> <what> = <v> @<path>
			a := strings.SplitN(l, " ", 3)
			rest := a[2]
			eq := strings.Index(rest, " = ")
			at := strings.Index(rest, " @")
			out[[3]int32{cx, cz, pi}] = append(out[[3]int32{cx, cz, pi}], msDraw{msVanillaLabel(rest[:eq], rest[at+2:]), rest[eq+3 : at]})
		}
	}
	return out
}

// msSynCase is one synthetic chunk: pieces (relative to the chunk's min
// corner) and scene blocks set over the base.
type msSynCase struct {
	pieces []nbt.Compound
	set    map[[3]int32]string // relative to the chunk min corner
}

// writeMsSyn writes a MineScene input with one case per chunk, chunks laid
// out on a grid of side n starting at chunk 0,0, over a base scene of
// state base for y in [y0, y1].
func writeMsSyn(t *testing.T, path string, cases []msSynCase, base string, y0, y1 int32) {
	writeMsSynStride(t, path, cases, base, y0, y1, 1)
}

// writeMsSynStride places case i at chunk (stride*(i%n), stride*(i/n)).
func writeMsSynStride(t *testing.T, path string, cases []msSynCase, base string, y0, y1, stride int32) {
	n := int32(1)
	for n*n < int32(len(cases)) {
		n++
	}
	k := n
	off := stride - 1
	n = n*stride + off
	reg := data.Canonical()
	rg := structure.Box{MinX: 0, MinY: y0, MinZ: 0, MaxX: n*16 - 1, MaxY: y1, MaxZ: n*16 - 1}
	sx, sz := n*16, n*16
	sy := y1 - y0 + 1
	bs, ok := reg.ParseState(base)
	if !ok {
		t.Fatal(base)
	}
	blocks := make([]data.StateID, sx*sy*sz)
	for i := range blocks {
		blocks[i] = bs
	}
	var children []any
	var chunks []string
	for i, c := range cases {
		cx, cz := int32(i)%k*stride+off, int32(i)/k*stride+off
		ox, oz := cx*16, cz*16
		for k, v := range c.set {
			st, ok := reg.ParseState(v)
			if !ok {
				t.Fatal(v)
			}
			x, y, z := ox+k[0], k[1], oz+k[2]
			if x < 0 || z < 0 || x >= sx || z >= sz || y < y0 || y > y1 {
				continue
			}
			blocks[((y-y0)*sz+z)*sx+x] = st
		}
		for _, p := range c.pieces {
			q := nbt.Compound{}
			for k, v := range p {
				q[k] = v
			}
			bb := append([]int32(nil), p["BB"].([]int32)...)
			bb[0], bb[2], bb[3], bb[5] = bb[0]+ox, bb[2]+oz, bb[3]+ox, bb[5]+oz
			q["BB"] = bb
			if e, ok := p["Entrances"].(nbt.List); ok {
				var items []any
				for _, it := range e.Items {
					b := append([]int32(nil), it.([]int32)...)
					b[0], b[2], b[3], b[5] = b[0]+ox, b[2]+oz, b[3]+ox, b[5]+oz
					items = append(items, b)
				}
				q["Entrances"] = nbt.List{Type: nbt.TagIntArray, Items: items}
			}
			children = append(children, q)
		}
		chunks = append(chunks, fmt.Sprintf("%d,%d", cx, cz))
	}
	start := nbt.Compound{"id": "minecraft:mineshaft", "ChunkX": int32(0), "ChunkZ": int32(0), "references": int32(0),
		"Children": nbt.List{Type: nbt.TagCompound, Items: children}}
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
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "SEED 1 0 3\nREGION %d %d %d %d %d %d\nPAL %d\n", rg.MinX, rg.MinY, rg.MinZ, rg.MaxX, rg.MaxY, rg.MaxZ, len(palList))
	for _, s := range palList {
		fmt.Fprintln(f, reg.StateString(s))
	}
	fmt.Fprintf(f, "DATA%s\nSTART %s\nCHUNKS %s\n", rle.String(), snbt(start), strings.Join(chunks, " "))
}

func msCorr(bb []int32, o int32, num int32, rails, spider bool) nbt.Compound {
	b := func(v bool) int8 {
		if v {
			return 1
		}
		return 0
	}
	return nbt.Compound{"id": "minecraft:mscorridor", "BB": bb, "O": o, "GD": int32(1), "MST": int32(0), "hr": b(rails), "sc": b(spider), "hps": int8(0), "Num": num}
}

// TestMineshaftSynDump (KREMENMC_MSSYN=dir) writes the synthetic scenes.
func TestMineshaftSynDump(t *testing.T) {
	dir := os.Getenv("KREMENMC_MSSYN")
	if dir == "" {
		t.Skip("KREMENMC_MSSYN not set")
	}
	// liq1: an east corridor x 4..8, y -40..-38, z 6..8; one water block at
	// every position of the box grown by 1.
	var cases []msSynCase
	for y := int32(-41); y <= -37; y++ {
		for z := int32(5); z <= 9; z++ {
			for x := int32(3); x <= 9; x++ {
				cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, -40, 6, 8, -38, 8}, 3, 1, false, false)},
					set: map[[3]int32]string{{x, y, z}: "minecraft:water"}})
			}
		}
	}
	writeMsSyn(t, dir+"/liq1.txt", cases, "minecraft:stone", -64, 15)

	// liq2: an east corridor x 10..19 crossing into the next (unplaced)
	// chunk; single liquid blocks around it.
	cases = nil
	for _, w := range []struct {
		x, y, z int32
		s       string
	}{{17, -41, 7, "minecraft:water"}, {20, -39, 7, "minecraft:water"}, {16, -39, 7, "minecraft:water"}, {15, -39, 7, "minecraft:water"},
		{17, -37, 7, "minecraft:water"}, {12, -41, 7, "minecraft:lava"}, {12, -41, 7, "minecraft:water[level=1]"}, {12, -41, 7, "minecraft:water"},
		{12, -41, 7, "minecraft:lava[level=3]"}, {9, -39, 7, "minecraft:water"}, {15, -41, 7, "minecraft:water"}, {16, -41, 7, "minecraft:water"},
		{12, -41, 7, "minecraft:seagrass"}, {12, -41, 7, "minecraft:kelp"}, {12, -41, 7, "minecraft:oak_stairs[waterlogged=true]"}, {12, -41, 7, "minecraft:bubble_column"}} {
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{10, -40, 6, 19, -38, 8}, 3, 2, false, false)},
			set: map[[3]int32]string{{w.x, w.y, w.z}: w.s}})
	}
	writeMsSynStride(t, dir+"/liq2.txt", cases, "minecraft:stone", -64, 15, 2)

	// liq3: crossings, stairs and a room with one water block at face
	// centres, corners, inside and on the inner faces of the box.
	cases = nil
	pieces := []nbt.Compound{
		{"id": "minecraft:mscrossing", "BB": []int32{4, -40, 4, 8, -38, 8}, "O": int32(-1), "GD": int32(1), "MST": int32(0), "D": int32(2), "tf": int8(0)},
		{"id": "minecraft:mscrossing", "BB": []int32{4, -40, 4, 8, -34, 8}, "O": int32(-1), "GD": int32(1), "MST": int32(0), "D": int32(2), "tf": int8(1)},
		{"id": "minecraft:msstairs", "BB": []int32{4, -45, 4, 6, -38, 12}, "O": int32(0), "GD": int32(1), "MST": int32(0)},
		{"id": "minecraft:msroom", "BB": []int32{3, -40, 3, 11, -35, 11}, "O": int32(-1), "GD": int32(0), "MST": int32(0), "Entrances": nbt.List{Type: nbt.TagIntArray}},
	}
	for _, p := range pieces {
		bb := p["BB"].([]int32)
		cx, cy, cz := (bb[0]+bb[3])/2, (bb[1]+bb[4])/2, (bb[2]+bb[5])/2
		for _, w := range [][3]int32{
			{bb[0] - 1, cy, cz}, {bb[3] + 1, cy, cz}, {cx, bb[1] - 1, cz}, {cx, bb[4] + 1, cz}, {cx, cy, bb[2] - 1}, {cx, cy, bb[5] + 1},
			{bb[0] - 1, bb[1] - 1, bb[2] - 1}, {bb[3] + 1, bb[4] + 1, bb[5] + 1}, {cx, cy, cz}, {bb[0], cy, cz}, {cx, bb[1], cz}, {cx, bb[4], cz}, {}} {
			c := msSynCase{pieces: []nbt.Compound{p}, set: map[[3]int32]string{}}
			if w != [3]int32{} {
				c.set[w] = "minecraft:water"
			}
			cases = append(cases, c)
		}
	}
	writeMsSyn(t, dir+"/liq3.txt", cases, "minecraft:stone", -64, 15)

	// air1: where does carving write cave_air over air? Corridor x 4..8,
	// y -40..-38, z 6..8 (east, 1 section).
	corr := []nbt.Compound{msCorr([]int32{4, -40, 6, 8, -38, 8}, 3, 1, false, false)}
	fill := func(m map[[3]int32]string, x0, y0, z0, x1, y1, z1 int32, st string) map[[3]int32]string {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				for x := x0; x <= x1; x++ {
					m[[3]int32{x, y, z}] = st
				}
			}
		}
		return m
	}
	cases = []msSynCase{
		{pieces: corr, set: fill(map[[3]int32]string{}, 0, -64, 0, 15, 15, 15, "minecraft:air")},
		{pieces: corr, set: fill(map[[3]int32]string{}, 4, -40, 6, 8, -38, 8, "minecraft:air")},
		{pieces: corr, set: fill(map[[3]int32]string{}, 0, -40, 0, 15, 15, 15, "minecraft:air")},
		{pieces: corr, set: fill(fill(map[[3]int32]string{}, 4, -40, 6, 8, -38, 8, "minecraft:air"), 6, -37, 7, 6, 15, 7, "minecraft:air")},
		{pieces: corr, set: fill(map[[3]int32]string{}, 4, -40, 6, 8, -38, 8, "minecraft:cave_air")},
		{pieces: corr, set: fill(map[[3]int32]string{}, 4, -40, 6, 8, -38, 8, "minecraft:void_air")},
	}
	writeMsSyn(t, dir+"/air1.txt", cases, "minecraft:stone", -64, 15)

	// fam1: A canBeReplaced (block X inside the carve), B floor under
	// rails (X at h -1), C support ceiling (X over the 3 support tops).
	xs := []string{"oak_planks", "oak_fence", "oak_log", "dark_oak_planks", "dark_oak_fence", "dark_oak_log", "rail", "iron_chain", "cobweb",
		"wall_torch", "torch", "spawner", "chest", "bedrock", "obsidian", "glass", "gravel", "sand", "dirt", "cave_air", "air", "void_air",
		"tuff", "granite", "glow_lichen", "short_grass", "oak_leaves", "ice", "snow_block", "amethyst_block", "budding_amethyst",
		"amethyst_cluster", "calcite", "smooth_basalt", "mossy_cobblestone", "cobblestone", "end_portal_frame", "oak_slab", "snow",
		"pointed_dripstone", "moss_carpet", "magma_block", "raw_iron_block", "deepslate_iron_ore", "sculk_vein", "oak_trapdoor", "iron_bars", "cobblestone_wall"}
	cases = nil
	for _, x := range xs {
		cases = append(cases, msSynCase{pieces: corr, set: map[[3]int32]string{{5, -39, 7}: "minecraft:" + x}})
	}
	rcorr := []nbt.Compound{msCorr([]int32{4, -40, 6, 8, -38, 8}, 3, 1, true, false)}
	for _, x := range xs {
		cases = append(cases, msSynCase{pieces: rcorr, set: map[[3]int32]string{{4, -41, 7}: "minecraft:" + x, {5, -41, 7}: "minecraft:" + x, {6, -41, 7}: "minecraft:" + x, {7, -41, 7}: "minecraft:" + x, {8, -41, 7}: "minecraft:" + x}})
	}
	for _, x := range xs {
		cases = append(cases, msSynCase{pieces: corr, set: map[[3]int32]string{{6, -37, 6}: "minecraft:" + x, {6, -37, 7}: "minecraft:" + x, {6, -37, 8}: "minecraft:" + x}})
	}
	writeMsSyn(t, dir+"/fam1.txt", cases, "minecraft:stone", -64, 15)

	// fam2: end pillars and chains, isInterior, fence states.
	col := func(m map[[3]int32]string, y0, y1 int32, st string) map[[3]int32]string {
		for _, z := range []int32{6, 8} {
			for y := y0; y <= y1; y++ {
				m[[3]int32{6, y, z}] = st
			}
		}
		return m
	}
	noCeil := func(m map[[3]int32]string) map[[3]int32]string {
		for z := int32(6); z <= 8; z++ {
			m[[3]int32{6, -37, z}] = "minecraft:air"
		}
		return m
	}
	mk := func() map[[3]int32]string { return map[[3]int32]string{} }
	cases = []msSynCase{
		{pieces: corr, set: col(mk(), -60, -41, "minecraft:air")},
		{pieces: corr, set: col(noCeil(col(mk(), -64, -41, "minecraft:air")), -37, 15, "minecraft:air")},
		{pieces: corr, set: col(noCeil(col(mk(), -49, -41, "minecraft:air")), -37, -36, "minecraft:air")},
		{pieces: corr, set: col(noCeil(col(mk(), -44, -41, "minecraft:air")), -37, -31, "minecraft:air")},
		{pieces: corr, set: col(noCeil(col(mk(), -43, -41, "minecraft:air")), -37, -37, "minecraft:air")},
	}
	for _, x := range []string{"oak_leaves", "glass", "oak_fence", "oak_slab", "cobweb", "torch", "iron_chain", "oak_log", "rail", "spawner", "chest", "oak_slab[type=top]"} {
		cases = append(cases, msSynCase{pieces: corr, set: col(col(noCeil(col(mk(), -64, -41, "minecraft:air")), -37, -36, "minecraft:air"), -35, -35, "minecraft:"+x)})
		cases = append(cases, msSynCase{pieces: corr, set: col(col(noCeil(col(mk(), -64, -41, "minecraft:air")), -37, 15, "minecraft:air"), -45, -45, "minecraft:"+x)})
	}
	cases = append(cases,
		msSynCase{pieces: corr, set: col(col(noCeil(mk()), -50, -42, "minecraft:air"), -41, -41, "minecraft:oak_planks")},
		msSynCase{pieces: corr, set: col(col(noCeil(mk()), -50, -42, "minecraft:air"), -41, -41, "minecraft:dark_oak_planks")},
	)
	low := []nbt.Compound{msCorr([]int32{4, -55, 6, 8, -53, 8}, 3, 1, false, false)}
	lowCol := func(bottom int32) map[[3]int32]string {
		m := mk()
		for _, z := range []int32{6, 8} {
			for y := bottom + 1; y <= -56; y++ {
				m[[3]int32{6, y, z}] = "minecraft:air"
			}
			for y := int32(-52); y <= 15; y++ {
				m[[3]int32{6, y, z}] = "minecraft:air"
			}
		}
		for z := int32(6); z <= 8; z++ {
			m[[3]int32{6, -52, z}] = "minecraft:air"
		}
		return m
	}
	cases = append(cases, msSynCase{pieces: low, set: lowCol(-64)}, msSynCase{pieces: low, set: lowCol(-63)}, msSynCase{pieces: low, set: lowCol(-62)}, msSynCase{pieces: low, set: lowCol(-61)})
	for _, y := range []int32{0, 1, 2, 3, 4, 5} {
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, true, false)}, set: map[[3]int32]string{{4, y - 1, 7}: "minecraft:air", {5, y - 1, 7}: "minecraft:air"}})
	}
	walls := mk()
	for _, z := range []int32{5, 9} {
		for y := int32(-40); y <= -39; y++ {
			walls[[3]int32{6, y, z}] = "minecraft:air"
		}
	}
	cases = append(cases, msSynCase{pieces: corr, set: walls},
		msSynCase{pieces: []nbt.Compound{msCorr([]int32{2, -40, 6, 11, -38, 8}, 3, 2, true, true)}, set: mk()},
		msSynCase{pieces: []nbt.Compound{msCorr([]int32{2, -40, 1, 4, -38, 15}, 0, 3, true, true)}, set: mk()},
		msSynCase{pieces: []nbt.Compound{msCorr([]int32{2, -40, 1, 4, -38, 15}, 2, 3, true, true)}, set: mk()},
		msSynCase{pieces: []nbt.Compound{msCorr([]int32{1, -40, 2, 15, -38, 4}, 1, 3, true, true)}, set: mk()})
	writeMsSyn(t, dir+"/fam2.txt", cases, "minecraft:stone", -64, 15)

	// fam3: the downward search bound. Torch above the lx0 pillar column
	// (up search dies), air over lx1 (no support).
	cases = nil
	for _, c := range [][2]int32{{-47, -64}, {-47, -63}, {-47, -62}, {-40, -64}, {-40, -63}, {-40, -62}, {-40, -61}, {-35, -64}, {-35, -63}} {
		y := c[0]
		m := mk()
		for yy := c[1] + 1; yy <= y-2; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:air"
		}
		m[[3]int32{6, y + 3, 6}] = "minecraft:torch"
		m[[3]int32{6, y + 3, 7}] = "minecraft:air"
		m[[3]int32{6, y - 1, 6}] = "minecraft:air"
		for yy := int32(-64); yy < c[1]; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:air"
		}
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, false, false)}, set: m})
	}
	// up search bound: torch two below the floor (down search dies), air
	// from h0 up to a stone k above h0.
	for _, k := range []int32{19, 20, 21, 22, 23, 30, 45, 54} {
		y := int32(-40)
		m := mk()
		m[[3]int32{6, y - 1, 6}] = "minecraft:air"
		m[[3]int32{6, y - 2, 6}] = "minecraft:torch"
		m[[3]int32{6, y + 3, 7}] = "minecraft:air"
		for yy := y + 3; yy < y+k; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:air"
		}
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, false, false)}, set: m})
	}
	writeMsSyn(t, dir+"/fam3.txt", cases, "minecraft:stone", -64, 15)

	// fam4: what the downward pillar search passes through: floor air, then
	// X from 2 below the floor down to 8 below, stone under it; up search
	// dies on a torch.
	cases = nil
	for _, x := range []string{"water", "water[level=3]", "lava", "cave_vines", "glow_lichen", "short_grass", "moss_carpet", "cobweb", "oak_leaves", "void_air", "cave_air", "seagrass", "pointed_dripstone", "iron_chain", "oak_fence"} {
		y := int32(-40)
		m := mk()
		m[[3]int32{6, y - 1, 6}] = "minecraft:air"
		for yy := y - 8; yy <= y-2; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:" + x
		}
		m[[3]int32{6, y + 3, 6}] = "minecraft:torch"
		m[[3]int32{6, y + 3, 7}] = "minecraft:air"
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, false, false)}, set: m})
	}
	writeMsSyn(t, dir+"/fam4.txt", cases, "minecraft:stone", -64, 15)

	// fam5: blocks the pillar (down) and chain (up) searches pass through.
	cases = nil
	for _, x := range msPassList {
		y := int32(-40)
		m := mk()
		m[[3]int32{6, y - 1, 6}] = "minecraft:air"
		for yy := y - 8; yy <= y-2; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:" + x
		}
		m[[3]int32{6, y + 3, 6}] = "minecraft:torch"
		m[[3]int32{6, y + 3, 7}] = "minecraft:air"
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, false, false)}, set: m})
		m = mk()
		m[[3]int32{6, y - 1, 6}] = "minecraft:air"
		m[[3]int32{6, y - 2, 6}] = "minecraft:torch"
		m[[3]int32{6, y + 3, 7}] = "minecraft:air"
		for yy := y + 3; yy <= y+9; yy++ {
			m[[3]int32{6, yy, 6}] = "minecraft:" + x
		}
		cases = append(cases, msSynCase{pieces: []nbt.Compound{msCorr([]int32{4, y, 6, 8, y + 2, 8}, 3, 1, false, false)}, set: m})
	}
	writeMsSyn(t, dir+"/fam5.txt", cases, "minecraft:stone", -64, 15)

	for i := int64(1); i <= 4; i++ {
		writeMsSynStride(t, fmt.Sprintf("%s/fz%d.txt", dir, i), msFuzzCases(i, 64), "minecraft:stone", -64, 15, 2)
	}
}

func msDrawEq(a, b msDraw) bool {
	if a.label != b.label {
		return false
	}
	if a.val == b.val {
		return true
	}
	x, e1 := strconv.ParseFloat(a.val, 32)
	y, e2 := strconv.ParseFloat(b.val, 32)
	return e1 == nil && e2 == nil && float32(x) == float32(y)
}

// TestMineshaftSceneAir (KREMENMC_MSAIR=in,x0,y0,z0,x1,y1,z1) prints the scene
// and the reference result of a box, layer by layer.
func TestMineshaftSceneAir(t *testing.T) {
	spec := os.Getenv("KREMENMC_MSAIR")
	if spec == "" {
		t.Skip()
	}
	a := strings.Split(spec, ",")
	s := readMsScene(t, a[0])
	var b [6]int32
	for i := range b {
		v, _ := strconv.Atoi(a[i+1])
		b[i] = int32(v)
	}
	want := map[[3]int32]string{}
	f, _ := os.Open(a[0] + ".out")
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "B ") {
			var x, y, z int32
			var st string
			fmt.Sscan(l[2:], &x, &y, &z, &st)
			want[[3]int32{x, y, z}] = st
		}
	}
	reg := data.Canonical()
	short := func(n string) string {
		n = strings.TrimPrefix(n, "minecraft:")
		if i := strings.IndexByte(n, '['); i >= 0 {
			n = n[:i]
		}
		m := map[string]string{"air": ".", "cave_air": "c", "stone": "#", "deepslate": "#", "oak_planks": "P", "oak_fence": "F", "oak_log": "L", "cobweb": "w", "rail": "r", "water": "~", "lava": "!", "wall_torch": "t", "tuff": "#", "spawner": "S"}
		if v, ok := m[n]; ok {
			return v
		}
		return "?"
	}
	for y := b[4]; y >= b[1]; y-- {
		var lines []string
		for z := b[2]; z <= b[5]; z++ {
			var sb, rb strings.Builder
			for x := b[0]; x <= b[3]; x++ {
				sb.WriteString(short(reg.StateString(s.blocks[s.idx(x, y, z)])))
				if w, ok := want[[3]int32{x, y, z}]; ok {
					rb.WriteString(short(w))
				} else {
					rb.WriteString(short(reg.StateString(s.blocks[s.idx(x, y, z)])))
				}
			}
			lines = append(lines, fmt.Sprintf("z%5d %s   %s", z, sb.String(), rb.String()))
		}
		t.Logf("y %d (scene | ref)\n%s", y, strings.Join(lines, "\n"))
	}
}

// msFuzzCases builds random single-piece cases over noisy scenes.
func msFuzzCases(seed int64, n int) []msSynCase {
	r := rand.New(rand.NewSource(seed))
	type pw struct {
		s string
		w int
	}
	pal := []pw{{"air", 30}, {"cave_air", 10}, {"oak_planks", 5}, {"oak_fence", 5}, {"oak_log", 3}, {"iron_chain", 3}, {"cobweb", 4},
		{"rail", 3}, {"gravel", 5}, {"glass", 3}, {"oak_leaves", 3}, {"oak_slab", 3}, {"torch", 2}, {"deepslate", 5}, {"glow_lichen", 2},
		{"amethyst_cluster", 1}, {"chest", 1}, {"spawner", 1}, {"wall_torch", 1}, {"dark_oak_planks", 1}, {"tuff", 3}, {"oak_slab[type=top]", 1},
		{"water", 1}, {"lava", 1}, {"dark_oak_fence", 1}, {"dark_oak_log", 1}}
	tot := 0
	for _, p := range pal {
		tot += p.w
	}
	pick := func() string {
		v := r.Intn(tot)
		for _, p := range pal {
			if v < p.w {
				return "minecraft:" + p.s
			}
			v -= p.w
		}
		return "minecraft:air"
	}
	dir2D := [4]int32{2, 3, 0, 1}
	var cases []msSynCase
	for len(cases) < n {
		x, y, z := int32(r.Intn(16)), int32(-48+r.Intn(16)), int32(r.Intn(16))
		d := r.Intn(4)
		var p nbt.Compound
		var bb Box3
		switch k := r.Intn(20); {
		case k < 10:
			num := int32(1 + r.Intn(4))
			l := num * 5
			switch d {
			case 0:
				bb = Box3{x, y, z - l + 1, x + 2, y + 2, z}
			case 2:
				bb = Box3{x, y, z, x + 2, y + 2, z + l - 1}
			case 3:
				bb = Box3{x - l + 1, y, z, x, y + 2, z + 2}
			default:
				bb = Box3{x, y, z, x + l - 1, y + 2, z + 2}
			}
			rails := r.Intn(3) == 0
			spider := !rails && r.Intn(3) == 0
			p = msCorr(bb.a(), dir2D[d], num, rails, spider)
		case k < 14:
			h := int32(2)
			two := r.Intn(2) == 0
			if two {
				h = 6
			}
			switch d {
			case 0:
				bb = Box3{x - 1, y, z - 4, x + 3, y + h, z}
			case 2:
				bb = Box3{x - 1, y, z, x + 3, y + h, z + 4}
			case 3:
				bb = Box3{x - 4, y, z - 1, x, y + h, z + 3}
			default:
				bb = Box3{x, y, z - 1, x + 4, y + h, z + 3}
			}
			b := int8(0)
			if two {
				b = 1
			}
			p = nbt.Compound{"id": "minecraft:mscrossing", "BB": bb.a(), "O": int32(-1), "GD": int32(1), "MST": int32(0), "D": dir2D[d], "tf": b}
		case k < 17:
			switch d {
			case 0:
				bb = Box3{x, y - 5, z - 8, x + 2, y + 2, z}
			case 2:
				bb = Box3{x, y - 5, z, x + 2, y + 2, z + 8}
			case 3:
				bb = Box3{x - 8, y - 5, z, x, y + 2, z + 2}
			default:
				bb = Box3{x, y - 5, z, x + 8, y + 2, z + 2}
			}
			p = nbt.Compound{"id": "minecraft:msstairs", "BB": bb.a(), "O": dir2D[d], "GD": int32(1), "MST": int32(0)}
		default:
			bb = Box3{x, y, z, x + 7 + int32(r.Intn(6)), y + 4 + int32(r.Intn(6)), z + 7 + int32(r.Intn(6))}
			var ents []any
			for i := r.Intn(4); i > 0; i-- {
				ey := bb[1] + 1 + int32(r.Intn(2))
				ex := bb[0] + int32(r.Intn(int(bb[3]-bb[0]-2)))
				ez := bb[2] + int32(r.Intn(int(bb[5]-bb[2]-2)))
				switch r.Intn(4) {
				case 0:
					ents = append(ents, []int32{ex, ey, bb[2], ex + 2, ey + 2, bb[2] + 1})
				case 1:
					ents = append(ents, []int32{ex, ey, bb[5] - 1, ex + 2, ey + 2, bb[5]})
				case 2:
					ents = append(ents, []int32{bb[0], ey, ez, bb[0] + 1, ey + 2, ez + 2})
				default:
					ents = append(ents, []int32{bb[3] - 1, ey, ez, bb[3], ey + 2, ez + 2})
				}
			}
			p = nbt.Compound{"id": "minecraft:msroom", "BB": bb.a(), "O": int32(-1), "GD": int32(0), "MST": int32(r.Intn(2)), "Entrances": nbt.List{Type: nbt.TagIntArray, Items: ents}}
		}
		if r.Intn(3) == 0 {
			p["MST"] = int32(1)
		}
		set := map[[3]int32]string{}
		q := r.Float64() * 0.35
		liquids := r.Intn(4) == 0
		for yy := bb[1] - 3; yy <= bb[4]+3; yy++ {
			for zz := bb[2] - 3; zz <= bb[5]+3; zz++ {
				for xx := bb[0] - 3; xx <= bb[3]+3; xx++ {
					if xx < -16 || xx > 31 || zz < -16 || zz > 31 || r.Float64() >= q {
						continue
					}
					s := pick()
					if !liquids && (s == "minecraft:water" || s == "minecraft:lava") {
						s = "minecraft:air"
					}
					set[[3]int32{xx, yy, zz}] = s
				}
			}
		}
		for i := r.Intn(3); i > 0; i-- {
			cx, cy, cz := bb[0]+int32(r.Intn(int(bb[3]-bb[0]+1))), bb[1]+int32(r.Intn(int(bb[4]-bb[1]+1))), bb[2]+int32(r.Intn(int(bb[5]-bb[2]+1)))
			rad := int32(2 + r.Intn(4))
			for yy := cy - rad; yy <= cy+rad; yy++ {
				for zz := cz - rad; zz <= cz+rad; zz++ {
					for xx := cx - rad; xx <= cx+rad; xx++ {
						if (xx-cx)*(xx-cx)+(yy-cy)*(yy-cy)+(zz-cz)*(zz-cz) <= rad*rad && xx >= -16 && xx <= 31 && zz >= -16 && zz <= 31 {
							set[[3]int32{xx, yy, zz}] = "minecraft:air"
						}
					}
				}
			}
		}
		if r.Intn(3) == 0 {
			// air columns under and over the piece (pillars and chains).
			for _, c := range [][2]int32{{bb[0], bb[2]}, {bb[3], bb[5]}, {(bb[0] + bb[3]) / 2, (bb[2] + bb[5]) / 2}} {
				lo, hi := bb[1]-int32(2+r.Intn(14)), bb[4]+int32(2+r.Intn(14))
				for xx := c[0] - 2; xx <= c[0]+2; xx++ {
					for zz := c[1] - 2; zz <= c[1]+2; zz++ {
						if xx < -16 || xx > 31 || zz < -16 || zz > 31 {
							continue
						}
						for yy := lo; yy < bb[1]; yy++ {
							set[[3]int32{xx, yy, zz}] = "minecraft:air"
						}
						for yy := bb[4] + 1; yy <= hi; yy++ {
							set[[3]int32{xx, yy, zz}] = "minecraft:air"
						}
					}
				}
			}
		}
		cases = append(cases, msSynCase{pieces: []nbt.Compound{p}, set: set})
	}
	return cases
}

// Box3 is minX, minY, minZ, maxX, maxY, maxZ.
type Box3 [6]int32

func (b Box3) a() []int32 { return b[:] }

var msPassList = []string{"water", "lava", "glow_lichen", "seagrass", "tall_seagrass", "kelp", "kelp_plant", "sculk_vein", "vine", "resin_clump",
	"short_grass", "fern", "tall_grass", "dead_bush", "cave_vines", "cave_vines_plant", "moss_carpet", "cobweb", "oak_leaves", "pointed_dripstone",
	"iron_chain", "oak_fence", "rail", "torch", "wall_torch", "small_amethyst_bud", "amethyst_cluster", "bubble_column", "snow", "powder_snow",
	"structure_void", "light", "fire", "redstone_wire", "tripwire", "spore_blossom", "hanging_roots", "glow_lichen[down=true]", "big_dripleaf",
	"small_dripleaf", "azalea", "brown_mushroom", "lily_pad", "sugar_cane", "sweet_berry_bush", "dead_brain_coral_fan", "sea_pickle", "frogspawn",
	"bamboo_sapling", "nether_portal", "end_gateway", "moving_piston", "piston_head", "scaffolding", "ladder", "lever", "stone_button",
	"oak_sign", "oak_pressure_plate", "dripstone_block", "oak_slab", "glass", "barrier", "ice", "cave_air", "void_air", "water[level=8]"}

// TestMineshaftSceneGolden replays the committed MineScene cases
// (testdata/mineshaft_scene: a real terrain scene, random noisy scenes,
// the pillar/chain and pass-through families). Regenerate an output with
// tools/refprobe kremenmc.probe.legacy.MineScene <file> > <file>.out.
func TestMineshaftSceneGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/mineshaft_scene/*.txt")
	if len(files) == 0 {
		t.Skip("no scenes")
	}
	for _, f := range files {
		runMsScene(t, f, f+".out")
	}
}
