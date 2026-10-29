package feature_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/bench"
)

func TestZZLeafDist(t *testing.T) {
	ref, _ := reftest.Open()
	o := bench.Options{Ref: ref, Seed: 8266389061079214669, Dim: "overworld", Spec: "border", Stage: "full", Gen: "features", Structures: true}
	rs, pos, err := bench.Reference(o)
	if err != nil {
		t.Fatal(err)
	}
	type P struct{ x, y, z int }
	blk := map[P]string{}
	for _, p := range pos {
		c := rs[p]
		if c == nil {
			continue
		}
		for sy := c.MinSection; sy < c.MinSection+len(c.Sections); sy++ {
			s := c.Section(sy)
			if s == nil || sy*16 < 40 {
				continue
			}
			for i := range 4096 {
				x, y, z := i&15, i>>8, (i>>4)&15
				b := s.Block(x, y, z)
				if strings.Contains(b, "_leaves") || strings.Contains(b, "_log") {
					blk[P{int(p.X)*16 + x, sy*16 + y, int(p.Z)*16 + z}] = b
				}
			}
		}
	}
	// true distance BFS
	d := map[P]int{}
	q := []P{}
	for p, b := range blk {
		if strings.Contains(b, "_log") {
			d[p] = 0
			q = append(q, p)
		}
	}
	for len(q) > 0 {
		p := q[0]
		q = q[1:]
		for _, o := range []P{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			n := P{p.x + o.x, p.y + o.y, p.z + o.z}
			if b, ok := blk[n]; ok && strings.Contains(b, "_leaves") {
				if _, seen := d[n]; !seen && d[p] < 7 {
					d[n] = d[p] + 1
					q = append(q, n)
				}
			}
		}
	}
	hist := map[string]int{}
	for p, b := range blk {
		if !strings.Contains(b, "_leaves") || strings.Contains(b, "persistent=true") {
			continue
		}
		i := strings.Index(b, "distance=")
		got := int(b[i+9] - '0')
		want, ok := d[p]
		if !ok || want > 7 {
			want = 7
		}
		hist[fmt.Sprintf("stored-true=%d", got-want)]++
	}
	t.Log(hist)
}
