package feature

import (
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// The leaf distance update a tree runs after its decorators, fitted to the
// block writes of kremenmc.probe.feat_region.Region (trees in real
// WorldGenRegions, other trees' leaves and logs around them):
//   - positions wait in buckets by distance; bucket 0 starts with the
//     tree's logs. The lowest non-empty bucket gives up its first element in
//     Java HashSet iteration order;
//   - a leaf taken from bucket k > 0 is written with distance k (even when
//     unchanged) and becomes done; a log is only done;
//   - below k = 6, every neighbour (down, up, north, south, west, east)
//     that is not done and lies in the box of all positions the tree wrote
//     goes into a bucket: a log into 0, a leaf into min(k+1, its current
//     distance), where k drops to the smallest level seen so far among the
//     neighbours already checked in that order (0 for a log, the distance
//     for a leaf). A leaf can wait in two
//     buckets at once and is then written twice, the last time with the
//     larger distance: vanilla leaves often end one above their true
//     distance.

// javaSet is the iteration order of a java.util.HashSet<BlockPos>: buckets
// by spread hash, insertion order within a bucket, table doubling from 16
// above 3/4 load; it never shrinks.
type javaSet struct {
	cap, seq int
	ents     []javaEnt
	has      map[Pos]bool
}

type javaEnt struct {
	p   Pos
	seq int
}

func (s *javaSet) add(p Pos) {
	if s.has == nil {
		s.has = map[Pos]bool{}
		s.cap = 16
	}
	if s.has[p] {
		return
	}
	s.has[p] = true
	s.seq++
	s.ents = append(s.ents, javaEnt{p, s.seq})
	for len(s.ents) > s.cap*3/4 {
		s.cap *= 2
	}
}

func javaBucket(p Pos, cap int) int {
	h := (int32(p.Y)+int32(p.Z)*31)*31 + int32(p.X)
	h ^= int32(uint32(h) >> 16)
	return int(h) & (cap - 1)
}

// pop removes and returns the first element in iteration order.
func (s *javaSet) pop() Pos {
	best, bb := 0, javaBucket(s.ents[0].p, s.cap)
	for i := 1; i < len(s.ents); i++ {
		b := javaBucket(s.ents[i].p, s.cap)
		if b < bb || b == bb && s.ents[i].seq < s.ents[best].seq {
			best, bb = i, b
		}
	}
	p := s.ents[best].p
	s.ents[best] = s.ents[len(s.ents)-1]
	s.ents = s.ents[:len(s.ents)-1]
	delete(s.has, p)
	return p
}

// box is the bounding box of every position the tree wrote.
type box struct {
	min, max Pos
	set      bool
}

func (b *box) add(p Pos) {
	if !b.set {
		b.min, b.max, b.set = p, p, true
		return
	}
	b.min = Pos{min(b.min.X, p.X), min(b.min.Y, p.Y), min(b.min.Z, p.Z)}
	b.max = Pos{max(b.max.X, p.X), max(b.max.Y, p.Y), max(b.max.Z, p.Z)}
}

func (b *box) has(p Pos) bool {
	return b.set && p.X >= b.min.X && p.X <= b.max.X && p.Y >= b.min.Y && p.Y <= b.max.Y && p.Z >= b.min.Z && p.Z <= b.max.Z
}

// leafDistance is the distance property of a leaves state.
func leafDistance(st data.StateID) (int, bool) {
	reg := data.Canonical()
	if !reg.IsLeaves(st) {
		return 0, false
	}
	v, ok := reg.Value(st, "distance")
	if !ok {
		return 0, false
	}
	d, err := strconv.Atoi(v)
	return d, err == nil
}

func (t *treeRun) updateLeaves() {
	var buckets [7]javaSet
	for _, l := range t.logs {
		buckets[0].add(l)
	}
	_, logs := treeTags()
	reg := data.Canonical()
	done := map[Pos]bool{}
	nb := [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for {
		k := 0
		for k < len(buckets) && len(buckets[k].ents) == 0 {
			k++
		}
		if k == len(buckets) {
			return
		}
		p := buckets[k].pop()
		if k > 0 {
			st := t.lv.Block(p)
			if s, ok := reg.With(st, "distance", strconv.Itoa(k)); ok {
				t.lv.SetBlock(p, s)
			}
		}
		done[p] = true
		if k >= 6 {
			continue
		}
		lvl := k
		for _, o := range nb {
			q := p.Add(o)
			if done[q] || !t.box.has(q) {
				continue
			}
			st := t.lv.Block(q)
			if logs.has(st) {
				buckets[0].add(q)
				lvl = 0
			} else if d, ok := leafDistance(st); ok {
				buckets[min(lvl+1, d)].add(q)
				lvl = min(lvl, d)
			}
		}
	}
}

// updateNeighbours is the shape update of the blocks next to the tree:
// a double plant half whose other half the tree replaced is removed (seen in
// the reference: a large_fern whose top became spruce leaves).
func (t *treeRun) updateNeighbours() {
	set := map[Pos]bool{}
	for _, l := range [][]Pos{t.soil, t.logs, t.leaves, t.deco} {
		for _, p := range l {
			set[p] = true
		}
	}
	reg := data.Canonical()
	for _, l := range [][]Pos{t.soil, t.logs, t.leaves, t.deco} {
		for _, p := range l {
			for _, o := range [2]Pos{{0, -1, 0}, {0, 1, 0}} {
				q := p.Add(o)
				if set[q] {
					continue
				}
				st := t.lv.Block(q)
				half, ok := reg.Value(st, "half")
				if !ok || half != "lower" && half != "upper" {
					continue
				}
				if half == "lower" && o.Y != -1 || half == "upper" && o.Y != 1 {
					continue
				}
				other := t.lv.Block(p)
				if b := reg.BlockOf(other); b != nil && b == reg.BlockOf(st) {
					continue
				}
				air := data.Air
				if blockName(st) == "tall_seagrass" {
					air, _ = data.DefaultState("water")
				}
				t.lv.SetBlock(q, air)
			}
		}
	}
}
