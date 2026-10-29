package gen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
)

func names(ss []Status) string {
	var b []string
	for _, s := range ss {
		b = append(b, s.ID())
	}
	return "[" + strings.Join(b, " ") + "]"
}

// TestPyramidMatchesReference checks the status list, parents, heightmap
// switch, and every generation step's direct and accumulated dependencies
// and write radius against the 26.3 reference (probe gen.Statuses, public
// API of ChunkStatus / ChunkPyramid / ChunkStep / ChunkDependencies).
func TestPyramidMatchesReference(t *testing.T) {
	golden := string(reftest.Golden(t, "statuses.txt", "kremenmc.probe.gen.Statuses"))
	lines := strings.Split(strings.TrimSpace(golden), "\n")
	if lines[0] != "max_structure_distance 8" {
		t.Errorf("got %q", lines[0])
	}
	statusRe := regexp.MustCompile(`^status (\d+) (\S+) parent=(\S+) type=(\S+) heightmaps=\[.*\] final=(\S+)$`)
	stepRe := regexp.MustCompile(`^generation (\S+) write=(-?\d+) direct=(\[.*?\]) r=(\d+) accumulated=(\[.*?\]) r=(\d+) `)
	nStatus, nStep := 0, 0
	for _, l := range lines[1:] {
		if m := statusRe.FindStringSubmatch(l); m != nil {
			var idx int
			fmt.Sscan(m[1], &idx)
			s := Status(idx)
			want := fmt.Sprintf("%d %s %s %s %v", idx, s.ID(), s.Parent().ID(), map[bool]string{false: "PROTOCHUNK", true: "LEVELCHUNK"}[s == StatusFull], s.FinalHeightmaps())
			got := strings.Join([]string{m[1], m[2], m[3], m[4], m[5]}, " ")
			if got != want {
				t.Errorf("status: got %q want %q", got, want)
			}
			nStatus++
			continue
		}
		if m := stepRe.FindStringSubmatch(l); m != nil {
			s, ok := ParseStatus(m[1])
			if !ok {
				t.Errorf("unknown status %s", m[1])
				continue
			}
			st := &Pyramid[s]
			if got := fmt.Sprint(st.WriteRadius); got != m[2] {
				t.Errorf("%s write radius %s, reference %s", s, got, m[2])
			}
			if got := names(st.Direct); got != m[3] {
				t.Errorf("%s direct %s, reference %s", s, got, m[3])
			}
			if got := fmt.Sprint(max(st.Radius(), 0)); got != m[4] {
				t.Errorf("%s radius %s, reference %s", s, got, m[4])
			}
			acc := Accumulated(s)
			if got := names(acc); got != m[5] {
				t.Errorf("%s accumulated %s, reference %s", s, got, m[5])
			}
			if got := fmt.Sprint(max(len(acc)-1, 0)); got != m[6] {
				t.Errorf("%s accumulated radius %s, reference %s", s, got, m[6])
			}
			nStep++
		}
	}
	if nStatus != NumStatuses || nStep != NumStatuses {
		t.Fatalf("parsed %d statuses, %d steps", nStatus, nStep)
	}
}

func TestParseStatus(t *testing.T) {
	for i := range NumStatuses {
		s := Status(i)
		for _, n := range []string{s.String(), s.ID()} {
			if got, ok := ParseStatus(n); !ok || got != s {
				t.Errorf("ParseStatus(%q) = %v %v", n, got, ok)
			}
		}
	}
	if _, ok := ParseStatus("noise"); ok {
		t.Error("noise is not a 26.3 status")
	}
}

// closure returns the chunks that must finish features before c starts its
// own, and the extents of that set (-x, +x, -z, +z).
func closure(c world.ChunkPos) (map[world.ChunkPos]bool, [4]int32) {
	seen := map[world.ChunkPos]bool{c: true}
	q := []world.ChunkPos{c}
	var ext [4]int32
	for len(q) > 0 {
		a := q[0]
		q = q[1:]
		forSquare(a, conflictRadius, func(b world.ChunkPos) {
			if seen[b] || !decoratesBefore(b, a) {
				return
			}
			seen[b] = true
			q = append(q, b)
			ext[0], ext[1] = max(ext[0], c.X-b.X), max(ext[1], b.X-c.X)
			ext[2], ext[3] = max(ext[2], c.Z-b.Z), max(ext[3], b.Z-c.Z)
		})
	}
	delete(seen, c)
	return seen, ext
}

// TestDecorationClosure pins the decoration order's properties: conflicting
// chunks never share a rank and the transitive predecessor set is bounded.
func TestDecorationClosure(t *testing.T) {
	ranks := map[int]bool{}
	for x := range int32(3) {
		for z := range int32(3) {
			ranks[DecorationRank(world.ChunkPos{X: x - 7, Z: z + 11})] = true
		}
	}
	if len(ranks) != 9 {
		t.Fatalf("3x3 block has %d distinct ranks", len(ranks))
	}
	var all [4]int32
	maxSet := 0
	for x := range int32(3) {
		for z := range int32(3) {
			set, ext := closure(world.ChunkPos{X: x, Z: z})
			for i := range ext {
				all[i] = max(all[i], ext[i])
			}
			maxSet = max(maxSet, len(set))
		}
	}
	if all != [4]int32{6, 6, 7, 5} {
		t.Errorf("closure extents %v, want [6 6 7 5]", all)
	}
	t.Logf("decoration closure extents -x+x-z+z %v, largest predecessor set %d", all, maxSet)
	// rank 0 chunks have no predecessors; rank 8 the most
	for x := range int32(3) {
		for z := range int32(3) {
			p := world.ChunkPos{X: x, Z: z}
			set, _ := closure(p)
			if DecorationRank(p) == 0 && len(set) != 0 {
				t.Errorf("rank 0 chunk %v has predecessors", p)
			}
		}
	}
	if !slices.Equal(decorationRank[:], []int8{3, 5, 4, 2, 6, 1, 8, 7, 0}) {
		t.Error("decoration rank table changed; this changes generated worlds")
	}
}
