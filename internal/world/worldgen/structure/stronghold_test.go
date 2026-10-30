package structure_test

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

const shSeed = 8266389061079214669

type shRefStart struct {
	seed   int64
	cx, cz int32
	pieces []string
}

var (
	shPieceRe = regexp.MustCompile(`\{BB:\[I;([-\d,]+)\],([^{}]*)\}`)
	shKVRe    = regexp.MustCompile(`(\w+):("[^"]*"|[-\w.]+)`)
)

func readShStarts(t *testing.T) []shRefStart {
	files, _ := filepath.Glob("testdata/stronghold/tstarts*.txt.gz")
	var out []shRefStart
	for _, fn := range files {
		out = append(out, readShStartFile(t, fn)...)
	}
	return out
}

func readShStartFile(t *testing.T, fn string) []shRefStart {
	f, err := os.Open(fn)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(z)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	var out []shRefStart
	for sc.Scan() {
		l := sc.Text()
		s := shRefStart{seed: shSeed}
		if _, sd, ok := strings.Cut(strings.Fields(l)[0], "@"); ok {
			fmt.Sscan(sd, &s.seed)
		}
		fmt.Sscan(strings.Fields(l)[1], &s.cx)
		fmt.Sscan(strings.Fields(l)[2], &s.cz)
		for _, m := range shPieceRe.FindAllStringSubmatch(l, -1) {
			kv := map[string]string{"BB": m[1]}
			for _, f := range shKVRe.FindAllStringSubmatch(m[2], -1) {
				kv[f[1]] = strings.Trim(f[2], `"`)
			}
			s.pieces = append(s.pieces, shKVString(kv))
		}
		out = append(out, s)
	}
	return out
}

func shKVString(kv map[string]string) string {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s ", k, kv[k])
	}
	return b.String()
}

func shPieceString(c nbt.Compound) string {
	kv := map[string]string{}
	for k, v := range c {
		switch v := v.(type) {
		case []int32:
			p := make([]string, len(v))
			for i, x := range v {
				p[i] = fmt.Sprint(x)
			}
			kv[k] = strings.Join(p, ",")
		case int8:
			kv[k] = fmt.Sprintf("%db", v)
		default:
			kv[k] = fmt.Sprint(v)
		}
	}
	return shKVString(kv)
}

// TestStrongholdStarts compares the layout of the reference starts piece
// for piece: all 128 ring chunks of seed 8266389061079214669, plus 3 starts
// of other seeds that reach the depth bound (TestStrongholdBoundSearch).
func TestStrongholdStarts(t *testing.T) {
	refs := readShStarts(t)
	ok, pieces, total := 0, 0, 0
	for _, ref := range refs {
		got := structure.StrongholdStartForTest(ref.seed, ref.cx, ref.cz, nil)
		total += len(ref.pieces)
		same := len(got) == len(ref.pieces)
		first := -1
		for i := range min(len(got), len(ref.pieces)) {
			if shPieceString(got[i]) == ref.pieces[i] {
				pieces++
			} else if first < 0 {
				first = i
				same = false
			}
		}
		if same {
			ok++
			continue
		}
		t.Errorf("start %d,%d: %d pieces, want %d; first difference at %d", ref.cx, ref.cz, len(got), len(ref.pieces), first)
		if first >= 0 {
			t.Logf("  got  %s\n  want %s", shPieceString(got[first]), ref.pieces[first])
		}
	}
	t.Logf("starts %d/%d, pieces in place %d/%d", ok, len(refs), pieces, total)
}

var shLogDraw = regexp.MustCompile(`^R\d \d+ (\S+) = (\S+) @`)

// TestStrongholdDraws compares the draw sequence of the 3 logged starts
// (testdata/stronghold/starts_log_*) with kremenmc's, up to the first
// difference.
func TestStrongholdDraws(t *testing.T) {
	f, err := os.Open("testdata/stronghold/starts_log_8266389061079214669.txt.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, _ := gzip.NewReader(f)
	sc := bufio.NewScanner(z)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	type logStart struct {
		cx, cz int32
		draws  []string
		paths  []string
	}
	var logs []*logStart
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "START ") {
			s := &logStart{}
			fmt.Sscan(l[6:], &s.cx, &s.cz)
			logs = append(logs, s)
			continue
		}
		m := shLogDraw.FindStringSubmatch(l)
		if m == nil || strings.HasPrefix(m[1], "nextLong") {
			continue
		}
		cur := logs[len(logs)-1]
		cur.draws = append(cur.draws, m[1]+"="+m[2])
		p := l[strings.Index(l, "@")+1:]
		if len(p) > 140 {
			p = p[:140]
		}
		cur.paths = append(cur.paths, p)
	}
	for _, ls := range logs {
		var got, what []string
		structure.StrongholdStartForTest(shSeed, ls.cx, ls.cz, func(s string) {
			if s == "attempt" || strings.HasPrefix(s, "move ") {
				return
			}
			if strings.HasPrefix(s, "~") {
				if len(what) > 0 {
					what[len(what)-1] += " " + s
				}
				return
			}
			d, w, _ := strings.Cut(s, " ")
			got = append(got, d)
			what = append(what, w)
		})
		// the vertical move draw is not traced by name; drop it from the log.
		n := 0
		for i := 0; i < len(ls.draws) && n < len(got); i++ {
			if strings.Contains(ls.paths[i], "moveBelowSeaLevel") {
				continue
			}
			if ls.draws[i] != got[n] {
				lo := max(0, i-6)
				var ctx []string
				for j := lo; j <= i; j++ {
					ctx = append(ctx, ls.draws[j]+" @"+ls.paths[j])
				}
				var gctx []string
				for j := max(0, n-6); j <= n; j++ {
					gctx = append(gctx, got[j]+" "+what[j])
				}
				t.Errorf("start %d,%d: draw %d differs: got %s (%s), want %s\nref:\n  %s\nkremenmc:\n  %s", ls.cx, ls.cz, i, got[n], what[n], ls.draws[i],
					strings.Join(ctx, "\n  "), strings.Join(gctx, "\n  "))
				break
			}
			n++
		}
		t.Logf("start %d,%d: %d/%d draws matched", ls.cx, ls.cz, n, len(got))
	}
}

// TestStrongholdBoundSearch (KREMENMC_SHSEARCH=n) lists seeds 0..n-1 at
// chunk 0,0.. whose start changes when one layout bound is lifted, to pick
// probe starts for the bounds.
func TestStrongholdBoundSearch(t *testing.T) {
	var n int
	if _, err := fmt.Sscan(os.Getenv("KREMENMC_SHSEARCH"), &n); err != nil {
		t.Skip("KREMENMC_SHSEARCH not set")
	}
	key := func(ps []nbt.Compound) string {
		var b strings.Builder
		for _, p := range ps {
			b.WriteString(shPieceString(p))
		}
		return b.String()
	}
	found := map[string]int{}
	for seed := int64(0); seed < int64(n); seed++ {
		cx, cz := int32(seed%7)*13-40, int32(seed%11)*9-45
		base := key(structure.StrongholdStartForTest(seed, cx, cz, nil))
		for _, c := range []struct {
			name    string
			d, m, f int32
		}{{"depth", 1 << 20, 10, 1}, {"minY", 50, -1 << 20, 1}, {"fillerMinY", 50, 10, -1 << 20}} {
			o1, o2, o3 := structure.SetStrongholdBoundsForTest(c.d, c.m, c.f)
			alt := key(structure.StrongholdStartForTest(seed, cx, cz, nil))
			structure.SetStrongholdBoundsForTest(o1, o2, o3)
			if alt != base && found[c.name] < 4 {
				found[c.name]++
				t.Logf("%s: seed %d chunk %d,%d", c.name, seed, cx, cz)
			}
		}
	}
}
