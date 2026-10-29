package feature

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// javaHashOrder is the order the reference's decorators visit a tree's
// positions in: a hash set keyed by (y + 31z)*31 + x, buckets ascending,
// insertion order within a bucket, the table doubling from 16 at 3/4 load,
// then stably sorted by Y.
// Inferred from draw logs (kremenmc.probe.feat_tree.Draws), verified by
// swamp_oak's leave_vine in testdata/trees.txt.
func javaHashOrder(ps []Pos) []Pos {
	seen := make(map[Pos]bool, len(ps))
	uniq := make([]Pos, 0, len(ps))
	for _, p := range ps {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	cp := 16
	for len(uniq) > cp*3/4 {
		cp *= 2
	}
	bucket := func(p Pos) int {
		h := (int32(p.Y)+int32(p.Z)*31)*31 + int32(p.X)
		h ^= int32(uint32(h) >> 16)
		return int(h) & (cp - 1)
	}
	slices.SortStableFunc(uniq, func(a, b Pos) int { return cmp.Compare(bucket(a), bucket(b)) })
	slices.SortStableFunc(uniq, func(a, b Pos) int { return cmp.Compare(a.Y, b.Y) })
	return uniq
}

// leaveVineSides is west, east, north, south: the order of the per-side draws.
var leaveVineSides = [4]struct {
	off  Pos
	face string // the vine face touching the leaf
}{
	{Pos{-1, 0, 0}, "east"},
	{Pos{1, 0, 0}, "west"},
	{Pos{0, 0, -1}, "south"},
	{Pos{0, 0, 1}, "north"},
}

// leaveVine: per leaf and side, a vine on free air hanging up to four more
// blocks down.
type leaveVine struct{ p float32 }

func (d leaveVine) decorate(t *treeRun, r *Random) {
	for _, l := range javaHashOrder(t.leaves) {
		for _, s := range leaveVineSides {
			if r.NextFloat() < d.p {
				p := l.Add(s.off)
				if data.IsAir(t.lv.Block(p)) {
					hangVine(t, p, s.face)
				}
			}
		}
	}
}

func hangVine(t *treeRun, p Pos, face string) {
	st, _ := data.DefaultState("vine")
	st, _ = data.Canonical().With(st, face, "true")
	t.setDeco(p, st)
	for range 4 {
		p.Y--
		if !data.IsAir(t.lv.Block(p)) {
			return
		}
		t.setDeco(p, st)
	}
}

// trunkPositions is what the trunk decorators walk: the block below the trunk
// (where the soil was set) and the logs. Inferred from the reference's draw
// counts, four per log plus four, and verified by jungle_tree's vines.
func (t *treeRun) trunkPositions() []Pos {
	return javaHashOrder(append(append([]Pos(nil), t.soil...), t.logs...))
}

// trunkVine: per trunk position and side, a 2 in 3 chance of a vine on air.
type trunkVine struct{}

func (trunkVine) decorate(t *treeRun, r *Random) {
	for _, l := range t.trunkPositions() {
		for _, s := range leaveVineSides {
			if r.NextIntn(3) > 0 {
				p := l.Add(s.off)
				if data.IsAir(t.lv.Block(p)) {
					setVine(t, p, s.face)
				}
			}
		}
	}
}

func setVine(t *treeRun, p Pos, face string) {
	st, _ := data.DefaultState("vine")
	st, _ = data.Canonical().With(st, face, "true")
	t.setDeco(p, st)
}

// cocoaFacings: north, east, south, west; the pod sits on the side opposite
// its facing, against the log.
var cocoaFacings = [4]struct {
	off    Pos
	facing string
}{
	{Pos{0, 0, 1}, "north"},
	{Pos{-1, 0, 0}, "east"},
	{Pos{0, 0, -1}, "south"},
	{Pos{1, 0, 0}, "west"},
}

// cocoa: with probability p, the lowest three levels of trunk positions each
// get a 1 in 4 chance per side of a pod of random age.
type cocoa struct{ p float32 }

func (c cocoa) decorate(t *treeRun, r *Random) {
	if r.NextFloat() >= c.p {
		return
	}
	ps := t.trunkPositions()
	if len(ps) == 0 {
		return
	}
	minY := ps[0].Y
	for _, l := range ps {
		if l.Y-minY > 2 {
			continue
		}
		for _, f := range cocoaFacings {
			if r.NextFloat() < 0.25 {
				p := l.Add(f.off)
				if data.IsAir(t.lv.Block(p)) {
					age := r.NextIntn(3)
					st, _ := data.DefaultState("cocoa")
					st, _ = data.Canonical().With(st, "facing", f.facing)
					st, _ = data.Canonical().With(st, "age", strconv.Itoa(int(age)))
					t.setDeco(p, st)
				}
			}
		}
	}
}
