package ai

import (
	"math"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
)

// PathType classifies a node for ground navigation (names and default
// costs follow the minecraft.wiki description of mob pathfinding).
type PathType uint8

const (
	PathBlocked PathType = iota
	PathOpen
	PathWalkable
	PathTrapdoor
	PathFence
	PathLava
	PathWater
	PathRail
	PathDangerFire
	PathDamageFire
	PathDangerOther
	PathDamageOther
	PathDoorOpen
	PathDoorWoodClosed
	PathDoorIronClosed
	PathLeaves
	PathHoney
	PathPowderSnow
	numPathTypes
)

// Malus is a per-mob cost table; a negative cost makes the type impassable.
type Malus [numPathTypes]float32

// DefaultMalus is the land-mob default.
var DefaultMalus = Malus{
	PathBlocked: -1, PathOpen: 0, PathWalkable: 0, PathTrapdoor: 0, PathFence: -1,
	PathLava: -1, PathWater: 8, PathRail: 0, PathDangerFire: 8, PathDamageFire: 16,
	PathDangerOther: 8, PathDamageOther: -1, PathDoorOpen: 0, PathDoorWoodClosed: -1,
	PathDoorIronClosed: -1, PathLeaves: -1, PathHoney: 8, PathPowderSnow: -1,
}

// block kinds, precomputed per state
const (
	kAir uint8 = iota
	kSolid
	kPartial // collision top ≤ 0.5 (slabs, carpets, snow layers)
	kFence
	kDoorWood
	kDoorWoodOpen
	kDoorIron
	kTrapdoor
	kWater
	kLava
	kFire
	kCactus // damage on contact
	kMagma  // damage when stood on
	kLeaves
	kHoney
	kPowder
	kRail
	kPassable // no collision, not special (grass, flowers)
)

var (
	kindOnce sync.Once
	kinds    []uint8
	tops     []float32
)

func initKinds() {
	r := data.Canonical()
	n := r.NumStates()
	kinds, tops = make([]uint8, n), make([]float32, n)
	for s := 0; s < n; s++ {
		st := data.StateID(s)
		b := r.BlockOf(st)
		name := b.Name
		top := float32(0)
		for _, c := range r.Collision(st) {
			top = max(top, float32(c.MaxY))
		}
		tops[s] = top
		prop := func(p, v string) bool {
			i := b.PropIndex(p)
			return i >= 0 && b.Props[i].Values[b.ValueIndex(st, i)] == v
		}
		k := kPassable
		switch {
		case r.IsAir(st):
			k = kAir
		case name == "lava":
			k = kLava
		case name == "water" || name == "bubble_column" || prop("waterlogged", "true") && top == 0:
			k = kWater
		case name == "fire" || name == "soul_fire" || (strings.HasSuffix(name, "campfire") && prop("lit", "true")):
			k = kFire
		case name == "cactus" || name == "sweet_berry_bush" || name == "wither_rose" || name == "pointed_dripstone":
			k = kCactus
		case name == "magma_block":
			k = kMagma
		case name == "honey_block":
			k = kHoney
		case name == "powder_snow":
			k = kPowder
		case strings.HasSuffix(name, "_door"):
			switch {
			case name == "iron_door" || name == "copper_door" || strings.Contains(name, "copper_door"):
				if prop("open", "true") {
					k = kDoorWoodOpen
				} else {
					k = kDoorIron
				}
			case prop("open", "true"):
				k = kDoorWoodOpen
			default:
				k = kDoorWood
			}
		case strings.HasSuffix(name, "_trapdoor"):
			k = kTrapdoor
		case strings.HasSuffix(name, "_fence") || strings.HasSuffix(name, "_wall") || (strings.HasSuffix(name, "_fence_gate") && !prop("open", "true")):
			k = kFence
		case strings.HasSuffix(name, "leaves"):
			k = kLeaves
		case strings.HasSuffix(name, "rail"):
			k = kRail
		case top == 0:
			k = kPassable
		case top <= 0.5:
			k = kPartial
		default:
			k = kSolid
		}
		kinds[s] = k
	}
}

func kindOf(s data.StateID) (uint8, float32) {
	kindOnce.Do(initKinds)
	if int(s) >= len(kinds) {
		return kSolid, 1
	}
	return kinds[s], tops[s]
}

// IsWoodDoor reports whether s is a closed door a zombie can break.
func IsWoodDoor(s data.StateID) bool { k, _ := kindOf(s); return k == kDoorWood }

// Node is a path waypoint: the block the feet occupy.
type Node struct{ X, Y, Z int32 }

// Center is the waypoint's walk target.
func (n Node) Center() (float64, float64, float64) {
	return float64(n.X) + 0.5, float64(n.Y), float64(n.Z) + 0.5
}

// Finder is a reusable A* pathfinder; get one with GetFinder and return it
// with PutFinder. It is not safe for concurrent use.
type Finder struct {
	src    entity.BlockSource
	malus  *Malus
	w, h   int // footprint (blocks) and height (blocks)
	step   float64
	fall   int
	nodes  []pnode
	index  map[int64]int32
	heap   []int32
	scrach []Node
}

type pnode struct {
	n       Node
	floor   float32
	g, f    float32
	parent  int32
	closed  bool
	heapIdx int32
	typ     PathType
}

var finders = sync.Pool{New: func() any { return &Finder{index: map[int64]int32{}} }}

func GetFinder() *Finder  { return finders.Get().(*Finder) }
func PutFinder(f *Finder) { f.src = nil; finders.Put(f) }

func key(x, y, z int32) int64 {
	return int64(x)&0x3FFFFFF<<38 | int64(z)&0x3FFFFFF<<12 | int64(y)&0xFFF
}

// Request describes one search.
type Request struct {
	Src        entity.BlockSource
	Malus      *Malus
	Width      float64
	Height     float64
	StepHeight float64
	MaxFall    int
	From       Node
	To         Node
	Reach      float64 // stop within this distance of To
	MaxNodes   int     // visited-node budget
	MaxDist    float64 // nodes farther than this from From are skipped
}

// Find runs A* and appends the path (excluding From) to out. When To is not
// reached it returns the path to the node closest to To; ok is false when
// no step at all was possible.
func (f *Finder) Find(r Request, out []Node) ([]Node, bool) {
	f.src, f.malus = r.Src, r.Malus
	if f.malus == nil {
		f.malus = &DefaultMalus
	}
	f.w = max(1, int(math.Floor(r.Width+1)))
	if r.Width <= 1 {
		f.w = 1
	}
	f.h = max(1, int(math.Ceil(r.Height)))
	f.step, f.fall = r.StepHeight, r.MaxFall
	if f.fall <= 0 {
		f.fall = 3
	}
	f.nodes = f.nodes[:0]
	f.heap = f.heap[:0]
	clear(f.index)
	tx, ty, tz := float64(r.To.X), float64(r.To.Y), float64(r.To.Z)
	dist := func(n Node) float32 {
		dx, dy, dz := float64(n.X)-tx, float64(n.Y)-ty, float64(n.Z)-tz
		return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
	}
	start := f.add(r.From, f.floorAt(r.From), PathWalkable)
	f.nodes[start].f = dist(r.From)
	f.push(start)
	best, bestD := start, dist(r.From)
	maxD2 := r.MaxDist * r.MaxDist
	reach := float32(r.Reach)
	visited := 0
	for len(f.heap) > 0 && visited < r.MaxNodes {
		cur := f.pop()
		cn := &f.nodes[cur]
		cn.closed = true
		visited++
		d := dist(cn.n)
		if d < bestD {
			best, bestD = cur, d
		}
		if d <= reach {
			best = cur
			break
		}
		f.neighbours(cur, func(n Node, floor float32, t PathType) {
			dx, dz := float64(n.X-r.From.X), float64(n.Z-r.From.Z)
			if maxD2 > 0 && dx*dx+dz*dz > maxD2 {
				return
			}
			c := &f.nodes[cur]
			ddx, ddy, ddz := float64(n.X-c.n.X), float64(floor-c.floor), float64(n.Z-c.n.Z)
			g := c.g + float32(math.Sqrt(ddx*ddx+ddy*ddy+ddz*ddz)) + f.malus[t]
			k := key(n.X, n.Y, n.Z)
			if i, ok := f.index[k]; ok {
				o := &f.nodes[i]
				if o.closed || g >= o.g {
					return
				}
				o.g, o.f, o.parent = g, g+hw*dist(n), cur
				f.fix(o.heapIdx)
				return
			}
			i := f.add(n, floor, t)
			o := &f.nodes[i]
			o.g, o.f, o.parent = g, g+hw*dist(n), cur
			f.push(i)
		})
	}
	if best == start {
		return out, false
	}
	f.scrach = f.scrach[:0]
	for i := best; i != start; i = f.nodes[i].parent {
		f.scrach = append(f.scrach, f.nodes[i].n)
	}
	for i := len(f.scrach) - 1; i >= 0; i-- {
		out = append(out, f.scrach[i])
	}
	return out, true
}

func (f *Finder) add(n Node, floor float32, t PathType) int32 {
	i := int32(len(f.nodes))
	f.nodes = append(f.nodes, pnode{n: n, floor: floor, parent: -1, typ: t, heapIdx: -1})
	f.index[key(n.X, n.Y, n.Z)] = i
	return i
}

// hw weights the heuristic: slightly greedy search, far fewer nodes on
// open ground for paths that are rarely more than a few percent longer.
const hw = 1.5

var dirs = [8][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}, {1, -1}, {1, 1}, {-1, 1}, {-1, -1}}

func (f *Finder) neighbours(cur int32, emit func(Node, float32, PathType)) {
	c := f.nodes[cur]
	var okY [4]int32
	var ok [4]bool
	for d := 0; d < 8; d++ {
		dx, dz := dirs[d][0], dirs[d][1]
		if d >= 4 {
			a, b := d-4, (d-3)%4
			if d == 7 {
				a, b = 3, 0
			}
			// diagonal only between two open orthogonal steps at the same level
			if !ok[a] || !ok[b] || okY[a] != c.n.Y || okY[b] != c.n.Y {
				continue
			}
		}
		n, floor, t, good := f.stepTo(c, c.n.X+dx, c.n.Z+dz)
		if d >= 4 && (!good || n.Y != c.n.Y) {
			continue
		}
		if d < 4 {
			ok[d], okY[d] = good, n.Y
		}
		if good {
			emit(n, floor, t)
		}
	}
}

// stepTo finds where the mob ends up stepping from c into column (x, z):
// the same level, one up (step or jump), or a drop of at most fall blocks.
func (f *Finder) stepTo(c pnode, x, z int32) (Node, float32, PathType, bool) {
	y := c.n.Y
	t := f.typeAt(x, y, z)
	if f.malus[t] >= 0 && t != PathOpen {
		fl := f.floorAt(Node{x, y, z})
		if float64(fl-c.floor) <= math.Max(f.step, 1.125) {
			return Node{x, y, z}, fl, t, true
		}
	}
	if t == PathBlocked || t == PathFence || t == PathDoorWoodClosed || t == PathDoorIronClosed || t == PathLeaves || (f.malus[t] >= 0 && t != PathOpen) {
		// try one block up: needs head room above the current node
		if f.typeAt(c.n.X, y+int32(f.h), c.n.Z) == PathBlocked && f.solidAt(c.n.X, y+int32(f.h), c.n.Z) {
			return Node{}, 0, t, false
		}
		up := f.typeAt(x, y+1, z)
		if up != PathOpen && f.malus[up] >= 0 {
			fl := f.floorAt(Node{x, y + 1, z})
			if float64(fl-c.floor) <= 1.125 {
				return Node{x, y + 1, z}, fl, up, true
			}
		}
		return Node{}, 0, t, false
	}
	if t != PathOpen {
		return Node{}, 0, t, false
	}
	for dy := int32(1); dy <= int32(f.fall)+1; dy++ {
		dt := f.typeAt(x, y-dy, z)
		if dt == PathOpen {
			continue
		}
		if f.malus[dt] < 0 || dy > int32(f.fall) && dt != PathWater {
			return Node{}, 0, dt, false
		}
		n := Node{x, y - dy, z}
		return n, f.floorAt(n), dt, true
	}
	return Node{}, 0, PathOpen, false
}

func (f *Finder) solidAt(x, y, z int32) bool {
	k, _ := kindOf(f.src.Block(int(x), int(y), int(z)))
	return k == kSolid || k == kFence || k == kDoorWood || k == kDoorIron || k == kLeaves
}

// floorAt is the height the feet rest at in node n.
func (f *Finder) floorAt(n Node) float32 {
	k, top := kindOf(f.src.Block(int(n.X), int(n.Y), int(n.Z)))
	if k == kPartial || k == kTrapdoor && top <= 0.5 {
		return float32(n.Y) + top
	}
	bk, bt := kindOf(f.src.Block(int(n.X), int(n.Y)-1, int(n.Z)))
	if bk == kFence {
		bt = 1.5
	}
	if bt == 0 {
		return float32(n.Y)
	}
	return float32(n.Y-1) + bt
}

// typeAt classifies the footprint at (x, y, z): the worst cell type over
// the width×width columns and the mob's height.
func (f *Finder) typeAt(x, y, z int32) PathType {
	worst := PathOpen
	for ox := int32(0); ox < int32(f.w); ox++ {
		for oz := int32(0); oz < int32(f.w); oz++ {
			t := f.cellType(x+ox, y, z+oz)
			if t == PathBlocked {
				return PathBlocked
			}
			if worst == PathOpen || f.malus[t] < 0 || (f.malus[worst] >= 0 && f.malus[t] > f.malus[worst]) || worst == PathWalkable && t != PathOpen {
				if !(t == PathOpen && worst != PathOpen) {
					worst = t
				}
			}
		}
	}
	return worst
}

func (f *Finder) cellType(x, y, z int32) PathType {
	k, _ := kindOf(f.src.Block(int(x), int(y), int(z)))
	t := feetType(k)
	if t == PathBlocked || t == PathFence || t == PathDoorWoodClosed || t == PathDoorIronClosed || t == PathLeaves {
		return t
	}
	for hy := int32(1); hy < int32(f.h); hy++ {
		hk, _ := kindOf(f.src.Block(int(x), int(y+hy), int(z)))
		switch hk {
		case kSolid, kFence, kDoorWood, kDoorIron, kLeaves, kPartial:
			return PathBlocked
		case kLava:
			return PathLava
		}
	}
	if t != PathOpen {
		if t == PathWalkable {
			return f.danger(x, y, z, t)
		}
		return t
	}
	bk, _ := kindOf(f.src.Block(int(x), int(y)-1, int(z)))
	switch bk {
	case kSolid, kPartial, kLeaves, kTrapdoor, kHoney, kDoorWood, kDoorIron:
		if bk == kHoney {
			return PathHoney
		}
		return f.danger(x, y, z, PathWalkable)
	case kMagma, kFire:
		return PathDamageFire
	case kCactus:
		return PathDamageOther
	case kFence:
		return PathBlocked // standing on a fence is never a target
	case kLava:
		return PathOpen
	}
	return PathOpen
}

func feetType(k uint8) PathType {
	switch k {
	case kAir, kPassable:
		return PathOpen
	case kSolid:
		return PathBlocked
	case kPartial:
		return PathWalkable
	case kFence:
		return PathFence
	case kDoorWood:
		return PathDoorWoodClosed
	case kDoorWoodOpen:
		return PathDoorOpen
	case kDoorIron:
		return PathDoorIronClosed
	case kTrapdoor:
		return PathTrapdoor
	case kWater:
		return PathWater
	case kLava:
		return PathLava
	case kFire:
		return PathDamageFire
	case kCactus:
		return PathDamageOther
	case kLeaves:
		return PathLeaves
	case kHoney:
		return PathHoney
	case kPowder:
		return PathPowderSnow
	case kRail:
		return PathRail
	}
	return PathBlocked
}

// danger marks walkable cells next to fire or cactus.
func (f *Finder) danger(x, y, z int32, t PathType) PathType {
	for d := 0; d < 4; d++ {
		k, _ := kindOf(f.src.Block(int(x+dirs[d][0]), int(y), int(z+dirs[d][1])))
		switch k {
		case kFire, kLava:
			return PathDangerFire
		case kCactus:
			return PathDangerOther
		}
	}
	return t
}

// heap on f (min by node f score)

func (f *Finder) less(a, b int32) bool { return f.nodes[f.heap[a]].f < f.nodes[f.heap[b]].f }

func (f *Finder) swap(a, b int32) {
	f.heap[a], f.heap[b] = f.heap[b], f.heap[a]
	f.nodes[f.heap[a]].heapIdx = a
	f.nodes[f.heap[b]].heapIdx = b
}

func (f *Finder) push(i int32) {
	f.heap = append(f.heap, i)
	j := int32(len(f.heap) - 1)
	f.nodes[i].heapIdx = j
	f.up(j)
}

func (f *Finder) pop() int32 {
	top := f.heap[0]
	last := int32(len(f.heap) - 1)
	f.swap(0, last)
	f.heap = f.heap[:last]
	if last > 0 {
		f.down(0)
	}
	f.nodes[top].heapIdx = -1
	return top
}

func (f *Finder) fix(j int32) {
	if j >= 0 {
		f.up(j)
	}
}

func (f *Finder) up(j int32) {
	for j > 0 {
		p := (j - 1) / 2
		if !f.less(j, p) {
			return
		}
		f.swap(j, p)
		j = p
	}
}

func (f *Finder) down(j int32) {
	n := int32(len(f.heap))
	for {
		l := 2*j + 1
		if l >= n {
			return
		}
		m := l
		if r := l + 1; r < n && f.less(r, l) {
			m = r
		}
		if !f.less(m, j) {
			return
		}
		f.swap(j, m)
		j = m
	}
}
