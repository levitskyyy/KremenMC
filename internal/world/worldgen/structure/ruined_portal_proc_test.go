package structure_test

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

var procOut = regexp.MustCompile(`(?:^| )(\d+)=(\S+)`)

func readProcProbe(t *testing.T, in, out string) (states []string, params []string, outs []map[int]string) {
	t.Helper()
	fi, err := os.ReadFile(in)
	if err != nil && in != "" {
		t.Skip(err)
	}
	if in != "" {
		for _, l := range strings.Split(strings.TrimSpace(string(fi)), "\n") {
			params = append(params, l[strings.LastIndex(l, " ")+1:])
		}
	}
	f, err := os.Open(out)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		l := sc.Text()
		head, rest, _ := strings.Cut(l, " => ")
		states = append(states, strings.Fields(head)[0])
		rest = regexp.MustCompile(`\[\d+( [^\]]*)?\]`).ReplaceAllString(rest, "")
		m := map[int]string{}
		for _, g := range procOut.FindAllStringSubmatch(rest, -1) {
			x, _ := strconv.Atoi(g[1])
			m[x] = g[2]
		}
		outs = append(outs, m)
	}
	return
}

// TestBlockAgeProbe replays the block_age probe: every ruined portal
// template block, 300 positions, mossiness 0, 0.2, 0.5, 0.8.
func TestBlockAgeProbe(t *testing.T) {
	states, params, outs := readProcProbe(t, "testdata/ruined_portal/place/block_age_all.in", "testdata/ruined_portal/place/block_age_all.txt")
	reg := data.Canonical()
	mossRe := regexp.MustCompile(`"mossiness":([0-9.]+)`)
	n, bad := 0, 0
	for i, st := range states {
		s, ok := reg.ParseState("minecraft:" + st)
		if !ok {
			t.Fatalf("state %s", st)
		}
		m, _ := strconv.ParseFloat(mossRe.FindStringSubmatch(params[i])[1], 32)
		for x := 0; x < 300; x++ {
			want := s
			if o, ok := outs[i][x]; ok {
				want, ok = reg.ParseState("minecraft:" + o)
				if !ok {
					t.Fatalf("state %s", o)
				}
			}
			got := structure.BlockAgeForTest(s, float32(m), int32(x), 0, 0)
			n++
			if got != want {
				bad++
				if bad < 10 {
					t.Errorf("%s m=%v x=%d: got %s want %s", st, m, x, reg.StateString(got), reg.StateString(want))
				}
			}
		}
	}
	t.Logf("block_age %d/%d", n-bad, n)
}

func TestBlackstoneReplaceProbe(t *testing.T) {
	states, _, outs := readProcProbe(t, "", "testdata/ruined_portal/place/blackstone_replace.txt")
	reg := data.Canonical()
	bad := 0
	for i, st := range states {
		s, _ := reg.ParseState("minecraft:" + st)
		want := s
		if o, ok := outs[i][0]; ok {
			want, _ = reg.ParseState("minecraft:" + o)
		}
		if got := structure.BlackstoneForTest(s); got != want {
			bad++
			t.Errorf("%s: got %s want %s", st, reg.StateString(got), reg.StateString(want))
		}
	}
	t.Logf("blackstone_replace %d/%d", len(states)-bad, len(states))
}
