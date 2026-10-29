package climate

import (
	"sync"
	"testing"
)

func pt(t0, t1, h0, h1, off int64) ParamPoint {
	return ParamPoint{Temperature: Param{t0, t1}, Humidity: Param{h0, h1}, Offset: off}
}

// Expectations were observed on the reference with ClimateLab.
func TestFindTies(t *testing.T) {
	tests := []struct {
		name    string
		entries []Entry[string]
		k       int
		queries []Target
		fresh   []string // fresh thread per query
		seq     []string // one thread, in order
	}{
		{
			name:    "tree order beats list order",
			entries: []Entry[string]{{pt(20, 30, 0, 0, 0), "b"}, {pt(0, 10, 0, 0, 0), "a"}},
			queries: []Target{{Temperature: 15}},
			fresh:   []string{"a"},
			seq:     []string{"a"},
		},
		{
			name: "remembered result wins ties",
			entries: []Entry[string]{
				{pt(0, 0, 0, 0, 0), "a"}, {pt(10, 10, 0, 0, 0), "b"}, {pt(0, 10, 50, 50, 0), "far"},
			},
			queries: []Target{
				{Temperature: 10}, {Temperature: 5}, {Temperature: 0}, {Temperature: 5},
				{Humidity: 10000}, {Temperature: 5},
			},
			fresh: []string{"b", "a", "a", "a", "far", "a"},
			seq:   []string{"b", "b", "a", "a", "far", "a"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := tc.k
			if k == 0 {
				k = DefaultChildrenPerNode
			}
			l := NewListChildren(tc.entries, k)
			var c Cursor
			for i, q := range tc.queries {
				if got := l.Find(q); got != tc.fresh[i] {
					t.Errorf("query %d: Find = %s, want %s", i, got, tc.fresh[i])
				}
				if got := l.FindWith(q, &c); got != tc.seq[i] {
					t.Errorf("query %d: FindWith = %s, want %s", i, got, tc.seq[i])
				}
			}
		})
	}
}

func TestCursorResetsOnOtherList(t *testing.T) {
	es := []Entry[string]{{pt(0, 0, 0, 0, 0), "a"}, {pt(10, 10, 0, 0, 0), "b"}}
	l1, l2 := NewList(es), NewList(es)
	var c Cursor
	l1.FindWith(Target{Temperature: 10}, &c)
	if got := l2.FindWith(Target{Temperature: 5}, &c); got != "a" {
		t.Errorf("FindWith on another list = %s, want a", got)
	}
}

func TestEmptyAndSingle(t *testing.T) {
	if got := NewList[string](nil).FindIndex(Target{}); got != -1 {
		t.Errorf("empty FindIndex = %d", got)
	}
	l := NewList([]Entry[string]{{pt(5, 5, 0, 0, 0), "x"}})
	var c Cursor
	if l.Find(Target{Temperature: -9}) != "x" || l.FindWith(Target{}, &c) != "x" {
		t.Error("single entry list")
	}
}

func TestFindConcurrent(t *testing.T) {
	es := reportEntries(t, "reports/reports/biome_parameters/minecraft/overworld.json")
	l := NewList(es)
	r := &run{entries: es, mode: "boundary", count: 2000, seed: 11}
	var ts []Target
	r.targets(func(_ int, tg Target) { ts = append(ts, tg) })
	want := make([]int, len(ts))
	for i, tg := range ts {
		want[i] = l.FindIndex(tg)
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			var c Cursor
			for i := range ts {
				j := (i + g*251) % len(ts)
				if got := l.FindIndex(ts[j]); got != want[j] {
					t.Errorf("goroutine %d: FindIndex(%v) = %d, want %d", g, ts[j], got, want[j])
					return
				}
				l.FindIndexWith(ts[j], &c)
			}
		})
	}
	wg.Wait()
}
