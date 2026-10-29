package climate

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

const goldenClass = "kremenmc.probe.biome.ClimateGolden"

// rng mirrors ClimateGolden.Rng (SplitMix64).
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

func (r *rng) below(n int64) int64        { return int64(r.next()>>1) % n }
func (r *rng) rangeIn(lo, hi int64) int64 { return lo + r.below(hi-lo+1) }

func fnv(h uint64, v int64) uint64 { return (h ^ uint64(v)) * 0x100000001B3 }

const fnvBasis = 0xCBF29CE484222325

func synthetic(style string, n int, seed int64) []Entry[int] {
	r := &rng{uint64(seed)}
	var out, pool []Entry[int]
	for i := 0; i < n; i++ {
		if style == "dup" && len(pool) > 0 && r.below(3) != 0 {
			e := pool[r.below(int64(len(pool)))]
			out = append(out, Entry[int]{e.Point, len(out)})
			continue
		}
		var ax [6]Param
		for d := range ax {
			var a, w int64
			switch {
			case style == "wide":
				a, w = r.rangeIn(-100, 100), r.rangeIn(0, 50)
			case style == "sparse" && (d == 1 || d == 3 || d == 4):
			case style == "sparse":
				a, w = r.rangeIn(-3, 3), r.rangeIn(0, 2)
			default:
				a, w = r.rangeIn(-3, 3), r.rangeIn(0, 3)
			}
			ax[d] = Param{a, a + w}
		}
		var off int64
		if style != "wide" {
			off = max(0, r.rangeIn(-2, 2))
		}
		e := Entry[int]{ParamPoint{ax[0], ax[1], ax[2], ax[3], ax[4], ax[5], off}, len(out)}
		out = append(out, e)
		pool = append(pool, e)
	}
	return out
}

var reports sync.Map

func reportEntries(t *testing.T, path string) []Entry[int] {
	if v, ok := reports.Load(path); ok {
		return v.([]Entry[int])
	}
	p, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	b, ok := p.Report(strings.TrimPrefix(path, "reports/reports/"))
	if !ok {
		t.Fatalf("report %s not embedded", path)
	}
	es, err := ParseReport(b)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]Entry[int], len(es))
	for i, e := range es {
		out[i] = Entry[int]{e.Point, i}
	}
	reports.Store(path, out)
	return out
}

type run struct {
	spec    string
	entries []Entry[int]
	k       int
	mode    string
	count   int
	seed    int64
	sample  int
	pad     int64
	hash    uint64
	samples []string
	blocks  []string
	total   string
}

func parseRun(t *testing.T, spec string) *run {
	f := strings.Split(spec, "|")
	r := &run{spec: spec, mode: f[2]}
	var err error
	if r.k, err = strconv.Atoi(f[1]); err != nil {
		t.Fatal(err)
	}
	r.count, _ = strconv.Atoi(f[3])
	r.seed, _ = strconv.ParseInt(f[4], 10, 64)
	r.sample, _ = strconv.Atoi(f[5])
	if s, ok := strings.CutPrefix(f[0], "syn:"); ok {
		p := strings.Split(s, ":")
		n, _ := strconv.Atoi(p[1])
		seed, _ := strconv.ParseInt(p[2], 10, 64)
		r.entries, r.pad = synthetic(p[0], n, seed), 2
	} else {
		r.entries, r.pad = reportEntries(t, strings.TrimPrefix(f[0], "report:")), 2000
	}
	r.hash = fnvBasis
	for _, e := range r.entries {
		sp := e.Point.space()
		for _, q := range sp[:6] {
			r.hash = fnv(fnv(r.hash, q.Min), q.Max)
		}
		r.hash = fnv(r.hash, e.Point.Offset)
	}
	return r
}

// targets mirrors ClimateGolden's target generator.
func (r *run) targets(yield func(int, Target)) {
	g := &rng{uint64(r.seed)}
	n := int64(len(r.entries))
	var lo, hi [6]int64
	for d := range 6 {
		lo[d], hi[d] = 1<<62, -1<<62
		for _, e := range r.entries {
			sp := e.Point.space()
			lo[d], hi[d] = min(lo[d], sp[d].Min), max(hi[d], sp[d].Max)
		}
	}
	end := func(e int64, d int) int64 {
		sp := r.entries[e].Point.space()
		if g.below(2) == 0 {
			return sp[d].Min
		}
		return sp[d].Max
	}
	for i := 0; i < r.count; i++ {
		var v [6]int64
		for d := range v {
			switch r.mode {
			case "random":
				v[d] = g.rangeIn(lo[d]-r.pad, hi[d]+r.pad)
			case "boundary":
				v[d] = end(g.below(n), d) + g.rangeIn(-1, 1)
			default:
				e, e2 := g.below(n), g.below(n)
				a := end(e, d)
				b := end(e2, d)
				switch g.below(3) {
				case 0:
					v[d] = a
				case 1:
					v[d] = (a + b) / 2
				default:
					v[d] = a + g.rangeIn(-1, 1)
				}
			}
		}
		yield(i, Target{v[0], v[1], v[2], v[3], v[4], v[5]})
	}
}

func hex(h uint64) string { return strconv.FormatUint(h, 16) }

// checkGolden replays every run of a golden file and compares the fresh-thread
// (FindIndex) and same-thread sequence (FindIndexWith) results.
func checkGolden(t *testing.T, name string, specs ...string) {
	b := reftest.Golden(t, name, goldenClass, specs...)
	var runs []*run
	var cur *run
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "R "):
			spec, rest, _ := strings.Cut(line[2:], " entries=")
			cur = parseRun(t, spec)
			if want := fmt.Sprintf("%d hash=%s", len(cur.entries), hex(cur.hash)); rest != want {
				t.Fatalf("%s: entries %s, golden %s", spec, want, rest)
			}
			runs = append(runs, cur)
		case strings.HasPrefix(line, "S "):
			cur.samples = append(cur.samples, line)
		case strings.HasPrefix(line, "B "):
			cur.blocks = append(cur.blocks, line)
		case strings.HasPrefix(line, "T "):
			cur.total = line
		}
	}
	if len(runs) != len(specs) {
		t.Fatalf("golden %s has %d runs, want %d", name, len(runs), len(specs))
	}
	for _, r := range runs {
		t.Run(r.spec, func(t *testing.T) {
			t.Parallel()
			r.verify(t)
		})
	}
}

// verify replays the run; with -short only the first two blocks are checked.
func (r *run) verify(t *testing.T) {
	if testing.Short() && r.count > 2<<16 {
		r.count = 2 << 16
		r.blocks, r.total = r.blocks[:2], ""
	}
	k := r.k
	if k == 0 {
		k = DefaultChildrenPerNode
	}
	l := NewListChildren(r.entries, k)
	var c Cursor
	bf, bs, tf, ts := uint64(fnvBasis), uint64(fnvBasis), uint64(fnvBasis), uint64(fnvBasis)
	var blocks []string
	diff, bad := 0, 0
	r.targets(func(i int, tg Target) {
		seq := l.FindIndexWith(tg, &c)
		fresh := l.FindIndex(tg)
		if fresh != seq {
			diff++
		}
		if i < len(r.samples) {
			want := r.samples[i]
			got := fmt.Sprintf("S %d %d %d %d %d %d %d %d", tg.Temperature, tg.Humidity, tg.Continentalness,
				tg.Erosion, tg.Depth, tg.Weirdness, fresh, seq)
			if got != want && bad < 10 {
				bad++
				t.Errorf("sample %d: got %q, golden %q", i, got, want)
			}
		}
		bf, bs = fnv(bf, int64(fresh)), fnv(bs, int64(seq))
		tf, ts = fnv(tf, int64(fresh)), fnv(ts, int64(seq))
		if (i+1)%(1<<16) == 0 || i+1 == r.count {
			blocks = append(blocks, fmt.Sprintf("B %d %s %s", i>>16, hex(bf), hex(bs)))
			bf, bs = fnvBasis, fnvBasis
		}
	})
	for i := range max(len(blocks), len(r.blocks)) {
		if i >= len(blocks) || i >= len(r.blocks) || blocks[i] != r.blocks[i] {
			t.Errorf("block %d differs", i)
		}
	}
	if got := fmt.Sprintf("T %s %s seqdiff=%d", hex(tf), hex(ts), diff); r.total != "" && got != r.total {
		t.Errorf("total %q, golden %q", got, r.total)
	}
}

const (
	overworld = "report:reports/reports/biome_parameters/minecraft/overworld.json"
	nether    = "report:reports/reports/biome_parameters/minecraft/nether.json"
)

func TestReferenceOverworld(t *testing.T) {
	checkGolden(t, "overworld.txt",
		overworld+"|0|random|1000000|1|300",
		overworld+"|0|boundary|1000000|2|300",
		overworld+"|0|tie|1000000|3|300",
		overworld+"|6|boundary|100000|4|100",
		overworld+"|3|tie|100000|5|100",
	)
}

func TestReferenceNether(t *testing.T) {
	checkGolden(t, "nether.txt",
		nether+"|0|random|1000000|1|200",
		nether+"|0|boundary|1000000|2|200",
		nether+"|0|tie|1000000|3|200",
		nether+"|2|tie|100000|4|100",
	)
}

func TestReferenceSynthetic(t *testing.T) {
	var specs []string
	modes := []string{"random", "boundary", "tie"}
	seed := 0
	for _, style := range []string{"grid", "sparse", "dup", "wide"} {
		for _, n := range []int{1, 2, 3, 7, 19, 20, 21, 45} {
			for _, k := range []int{0, 2, 3, 6} {
				seed++
				specs = append(specs, fmt.Sprintf("syn:%s:%d:%d|%d|%s|20000|%d|20", style, n, seed, k, modes[seed%3], seed))
			}
		}
	}
	checkGolden(t, "synthetic.txt", specs...)
}
