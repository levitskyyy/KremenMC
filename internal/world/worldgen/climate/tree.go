package climate

import (
	"math"
	"slices"
)

// DefaultChildrenPerNode is the reference tree fan-out.
const DefaultChildrenPerNode = 19

// node is a flattened tree node with its box split into bounds. Inner nodes
// have children nodes[first:end]; leaves have first == end. rank is the
// leaf's DFS rank, or the lowest leaf rank below an inner node.
type node struct {
	lo, hi     [7]int64
	first, end int32
	rank       int32
}

// tree stores nodes with siblings adjacent; entry maps a leaf rank to its
// entry index and leaf maps an entry index to its node.
type tree struct {
	nodes []node
	entry []int32
	leaf  []int32
}

type bnode struct {
	box      [7]Param
	children []*bnode
	entry    int
}

func center(p Param) int64 { return (p.Min + p.Max) / 2 }

func unionBox[N any](ns []N, box func(N) *[7]Param) [7]Param {
	u := *box(ns[0])
	for _, n := range ns[1:] {
		b := box(n)
		for d := range u {
			u[d] = u[d].union(b[d])
		}
	}
	return u
}

func leafBox(n *bnode) *[7]Param { return &n.box }

// chainCompare orders by the (optionally absolute) interval centers on axis
// d, then d+1, ... wrapping over all seven axes.
func chainCompare(a, b *[7]Param, d int, abs bool) int {
	for i := range 7 {
		k := (d + i) % 7
		x, y := center(a[k]), center(b[k])
		if abs {
			x, y = absInt(x), absInt(y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func absInt(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func absCenterSum(b *[7]Param) int64 {
	var s int64
	for _, p := range b {
		s += absInt(center(p))
	}
	return s
}

func cost(b *[7]Param) int64 {
	var s int64
	for _, p := range b {
		s += p.Max - p.Min
	}
	return s
}

func bucketSize(n, k int) int {
	s := 1
	for s*k < n {
		s *= k
	}
	return s
}

func buildNode(leaves []*bnode, k int) *bnode {
	if len(leaves) == 1 {
		return leaves[0]
	}
	if len(leaves) <= k {
		slices.SortStableFunc(leaves, func(a, b *bnode) int {
			x, y := absCenterSum(&a.box), absCenterSum(&b.box)
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			return 0
		})
		return &bnode{box: unionBox(leaves, leafBox), children: leaves}
	}
	s := bucketSize(len(leaves), k)
	sortAxis := func(d int) {
		slices.SortStableFunc(leaves, func(a, b *bnode) int { return chainCompare(&a.box, &b.box, d, false) })
	}
	bestAxis, bestCost := 0, int64(math.MaxInt64)
	for d := range 7 {
		sortAxis(d)
		var c int64
		for i := 0; i < len(leaves); i += s {
			u := unionBox(leaves[i:min(i+s, len(leaves))], leafBox)
			c += cost(&u)
		}
		if c < bestCost {
			bestAxis, bestCost = d, c
		}
	}
	sortAxis(bestAxis)
	buckets := make([]*bnode, 0, (len(leaves)+s-1)/s)
	for i := 0; i < len(leaves); i += s {
		b := leaves[i:min(i+s, len(leaves)):min(i+s, len(leaves))]
		buckets = append(buckets, &bnode{box: unionBox(b, leafBox), children: b})
	}
	slices.SortStableFunc(buckets, func(a, b *bnode) int { return chainCompare(&a.box, &b.box, bestAxis, true) })
	out := make([]*bnode, len(buckets))
	for i, b := range buckets {
		out[i] = buildNode(slices.Clone(b.children), k)
	}
	return &bnode{box: unionBox(out, leafBox), children: out}
}

func build[T any](entries []Entry[T], k int) *tree {
	if len(entries) == 0 {
		return nil
	}
	leaves := make([]*bnode, len(entries))
	for i := range entries {
		leaves[i] = &bnode{box: entries[i].Point.space(), entry: i}
	}
	root := buildNode(leaves, max(k, 2))
	t := &tree{nodes: []node{newNode(&root.box)}, leaf: make([]int32, len(entries))}
	t.flatten(0, root)
	return t
}

func newNode(b *[7]Param) node {
	var n node
	for d, p := range b {
		n.lo[d], n.hi[d] = p.Min, p.Max
	}
	return n
}

// flatten fills nodes[i] from b, appending b's children contiguously before
// recursing.
func (t *tree) flatten(i int, b *bnode) {
	t.nodes[i].rank = int32(len(t.entry))
	if b.children == nil {
		t.entry = append(t.entry, int32(b.entry))
		t.leaf[b.entry] = int32(i)
		return
	}
	first := len(t.nodes)
	for _, c := range b.children {
		t.nodes = append(t.nodes, newNode(&c.box))
	}
	t.nodes[i].first, t.nodes[i].end = int32(first), int32(len(t.nodes))
	for j, c := range b.children {
		t.flatten(first+j, c)
	}
}

// distance is the squared distance from p to n's box, or some value above
// bound once it is known to exceed it.
func (n *node) distance(p *[7]int64, bound int64) int64 {
	var s int64
	for d := range 7 {
		if v := p[d]; v > n.hi[d] {
			s += (v - n.hi[d]) * (v - n.hi[d])
		} else if v < n.lo[d] {
			s += (n.lo[d] - v) * (n.lo[d] - v)
		}
		if s > bound {
			return s
		}
	}
	return s
}

const maxSorted = 32

// search finds the leaf nearest to p, preferring the lower DFS rank among
// equally near leaves. The reference visits children in order and replaces
// its candidate only on a strictly smaller distance, so its result is the
// first nearest leaf in DFS order; ranking ties lets children be visited
// nearest-first instead. An incumbent with rank -1 wins every tie, which is
// how the reference treats its remembered last result.
func (t *tree) search(i int32, p *[7]int64, best int32, bestD int64, bestR int32) (int32, int64, int32) {
	n := &t.nodes[i]
	if n.end-n.first > maxSorted {
		for c := n.first; c < n.end; c++ {
			best, bestD, bestR = t.visit(c, t.nodes[c].distance(p, bestD), p, best, bestD, bestR)
		}
		return best, bestD, bestR
	}
	var ds [maxSorted]int64
	var cs [maxSorted]int32
	m := 0
	for c := n.first; c < n.end; c++ {
		d := t.nodes[c].distance(p, bestD)
		if d > bestD {
			continue
		}
		j := m
		for ; j > 0 && ds[j-1] > d; j-- {
			ds[j], cs[j] = ds[j-1], cs[j-1]
		}
		ds[j], cs[j] = d, c
		m++
	}
	for j := range m {
		best, bestD, bestR = t.visit(cs[j], ds[j], p, best, bestD, bestR)
	}
	return best, bestD, bestR
}

func (t *tree) visit(c int32, d int64, p *[7]int64, best int32, bestD int64, bestR int32) (int32, int64, int32) {
	ch := &t.nodes[c]
	if d > bestD || d == bestD && ch.rank >= bestR {
		return best, bestD, bestR
	}
	if ch.first == ch.end {
		return ch.rank, d, ch.rank
	}
	return t.search(c, p, best, bestD, bestR)
}

// find returns the entry index of the reference result. last is the entry
// index remembered from the previous query on the same reference thread, or
// -1 if none.
func (t *tree) find(target Target, last int) int {
	if t == nil {
		return -1
	}
	if len(t.nodes) == 1 {
		return int(t.entry[0])
	}
	p := target.axes()
	best, bestD, bestR := int32(-1), int64(math.MaxInt64), int32(math.MaxInt32)
	if last >= 0 {
		n := &t.nodes[t.leaf[last]]
		best, bestD, bestR = n.rank, n.distance(&p, math.MaxInt64), -1
	}
	best, _, _ = t.search(0, &p, best, bestD, bestR)
	return int(t.entry[best])
}
