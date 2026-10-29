package climate

import "testing"

func benchList(b *testing.B) (*List[int], []Target) {
	t := &testing.T{}
	es := reportEntries(t, "reports/reports/biome_parameters/minecraft/overworld.json")
	r := &run{entries: es, mode: "random", count: 4096, seed: 9, pad: 2000}
	ts := make([]Target, 0, r.count)
	r.targets(func(_ int, tg Target) { ts = append(ts, tg) })
	return NewList(es), ts
}

func BenchmarkFind(b *testing.B) {
	l, ts := benchList(b)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		l.FindIndex(ts[i%len(ts)])
	}
}

// walk is a smooth path through the climate space, like neighbouring quarts.
func walk(n int) []Target {
	g := &rng{7}
	ts := make([]Target, n)
	var v [6]int64
	for i := range ts {
		for d := range v {
			v[d] = max(-11000, min(11000, v[d]+g.rangeIn(-150, 150)))
		}
		ts[i] = Target{v[0], v[1], v[2], v[3], v[4], v[5]}
	}
	return ts
}

func BenchmarkFindWalk(b *testing.B) {
	l, _ := benchList(b)
	ts := walk(4096)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		l.FindIndex(ts[i%len(ts)])
	}
}

func BenchmarkFindWithWalk(b *testing.B) {
	l, _ := benchList(b)
	ts := walk(4096)
	var c Cursor
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		l.FindIndexWith(ts[i%len(ts)], &c)
	}
}

func BenchmarkBruteForce(b *testing.B) {
	l, ts := benchList(b)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		l.BruteForce(ts[i%len(ts)])
	}
}

func BenchmarkBuild(b *testing.B) {
	l, _ := benchList(b)
	b.ReportAllocs()
	for b.Loop() {
		NewList(l.Entries())
	}
}
