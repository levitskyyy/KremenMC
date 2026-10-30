package feature

import (
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// scheduleLeafTicks is the leaf part of a tree's final shape update.
// Observed with kremenmc.probe.feat_nether.Place (oak, birch and fancy oak
// on open grass and among stone obstacles; testdata/tree_leaf_ticks.txt):
// every position of the tree still holding leaves schedules its leaf block
// with delay 1 once per face whose neighbour is not one of the tree's
// positions (soil, logs, leaves, decorations). The reference worlds keep
// them as block_ticks of the generated chunks.
func (t *treeRun) scheduleLeafTicks() {
	set := map[Pos]bool{}
	for _, l := range [][]Pos{t.soil, t.logs, t.leaves, t.deco} {
		for _, p := range l {
			set[p] = true
		}
	}
	reg := data.Canonical()
	seen := map[Pos]bool{}
	nb := [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for _, p := range t.leaves {
		if seen[p] {
			continue
		}
		seen[p] = true
		st := t.lv.Block(p)
		if !reg.IsLeaves(st) {
			continue
		}
		id := reg.BlockOf(st).Name
		if !strings.Contains(id, ":") {
			id = "minecraft:" + id
		}
		for _, o := range nb {
			if !set[p.Add(o)] {
				t.lv.scheduleBlockTick(p, id, 1)
			}
		}
	}
}
