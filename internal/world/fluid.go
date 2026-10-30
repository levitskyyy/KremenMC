package world

import (
	"bufio"
	"bytes"
	"container/heap"
	_ "embed"
	"math"
	"strconv"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Water and lava simulation. Rules follow the minecraft.wiki Fluid, Water
// and Lava pages; timings, spread shapes and the set of blocks a fluid
// washes away were checked against the reference by the scenes in
// internal/world/testdata/fluid_scenes.txt (see fluid_ref_test.go).
//
// A fluid tick is scheduled whenever a fluid block is placed or one of
// its six neighbours changes; nothing scans the world.

type FluidKind uint8

const (
	FluidNone FluidKind = iota
	FluidWater
	FluidLava
)

// FluidEvent is a side effect the world asks the server to perform.
type FluidEvent uint8

const (
	// FluidDestroy: a block at the position is being washed away by water
	// (drop its loot). old is the destroyed state.
	FluidDestroy FluidEvent = iota
	// FluidFizz: lava turned a block into stone, cobblestone, obsidian or
	// basalt, or burnt a block away (level event 1501).
	FluidFizz
)

//go:embed fluid_breaks.txt
var fluidBreaksTxt []byte

// fluidInfo packs kind (2 bits), amount 0..8 (4 bits), falling and source.
type fluidInfo uint8

func (f fluidInfo) kind() FluidKind { return FluidKind(f & 3) }
func (f fluidInfo) amount() int     { return int(f >> 2 & 15) }
func (f fluidInfo) falling() bool   { return f&0x40 != 0 }
func (f fluidInfo) source() bool    { return f&0x80 != 0 }

func mkInfo(k FluidKind, amount int, falling, source bool) fluidInfo {
	f := fluidInfo(k) | fluidInfo(amount)<<2
	if falling {
		f |= 0x40
	}
	if source {
		f |= 0x80
	}
	return f
}

type fluidTables struct {
	info   []fluidInfo
	faces  []uint8 // per state: bit d set when face d is a full square
	solid  []bool  // per state: blocks motion
	breaks []bool  // per block ID
	levels [3][16]data.StateID
	blocks [3]data.BlockID

	air, stone, cobble, obsidian, basalt data.StateID
	soulSoil, blueIce, bubble            data.BlockID
}

// Directions: down, up, north, south, west, east.
var fluidDirs = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// updateOrder is the neighbour update order (minecraft.wiki "Block
// update"): west, east, down, up, north, south. It decides which ticks
// run first and was confirmed by the drain scene.
var updateOrder = [6][3]int{{-1, 0, 0}, {1, 0, 0}, {0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}}

const (
	fdDown = iota
	fdUp
)

func oppDir(d int) int { return d ^ 1 }

var fluidData = sync.OnceValue(func() *fluidTables {
	r := data.Canonical()
	n := r.NumStates()
	t := &fluidTables{info: make([]fluidInfo, n), faces: make([]uint8, n), solid: make([]bool, n), breaks: make([]bool, len(r.Blocks))}
	st := func(name string) data.StateID { s, _ := r.DefaultState(name); return s }
	bid := func(name string) data.BlockID { b, _ := r.BlockByName(name); return b.ID }
	t.air, t.stone, t.cobble, t.obsidian, t.basalt = data.Air, st("stone"), st("cobblestone"), st("obsidian"), st("basalt")
	t.soulSoil, t.blueIce, t.bubble = bid("soul_soil"), bid("blue_ice"), bid("bubble_column")
	for _, k := range [...]FluidKind{FluidWater, FluidLava} {
		name := "water"
		if k == FluidLava {
			name = "lava"
		}
		b, _ := r.BlockByName(name)
		t.blocks[k] = b.ID
		for l := range 16 {
			s, _ := r.With(b.DefaultState, "level", strconv.Itoa(l))
			t.levels[k][l] = s
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(fluidBreaksTxt))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		if b, ok := r.BlockByName(line); ok {
			t.breaks[b.ID] = true
		}
	}
	for i := range n {
		s := data.StateID(i)
		t.solid[i] = r.IsSolid(s)
		t.faces[i] = faceMask(r, s)
		if !r.HasFluid(s) {
			continue
		}
		b := r.BlockOf(s)
		switch b.ID {
		case t.blocks[FluidWater], t.blocks[FluidLava]:
			k := FluidWater
			if b.ID == t.blocks[FluidLava] {
				k = FluidLava
			}
			v, _ := r.Value(s, "level")
			l, _ := strconv.Atoi(v)
			switch {
			case l == 0:
				t.info[i] = mkInfo(k, 8, false, true)
			case l >= 8:
				t.info[i] = mkInfo(k, 8, true, false)
			default:
				t.info[i] = mkInfo(k, 8-l, false, false)
			}
		default:
			t.info[i] = mkInfo(FluidWater, 8, false, true)
		}
	}
	return t
})

// faceMask marks the faces of s fully covered by its collision boxes,
// sampled on an 8x8 grid.
func faceMask(r *data.Registry, s data.StateID) uint8 {
	if r.IsFullCube(s) && r.IsSolid(s) {
		return 0x3f
	}
	boxes := r.Collision(s)
	if len(boxes) == 0 {
		return 0
	}
	var m uint8
	const eps = 1e-6
	for d := range 6 {
		full := true
		for i := 0; i < 8 && full; i++ {
			for j := 0; j < 8 && full; j++ {
				u, v := (float64(i)+0.5)/8, (float64(j)+0.5)/8
				hit := false
				for _, b := range boxes {
					var on, in bool
					switch d {
					case 0:
						on, in = b.MinY <= eps, b.MinX <= u && u <= b.MaxX && b.MinZ <= v && v <= b.MaxZ
					case 1:
						on, in = b.MaxY >= 1-eps, b.MinX <= u && u <= b.MaxX && b.MinZ <= v && v <= b.MaxZ
					case 2:
						on, in = b.MinZ <= eps, b.MinX <= u && u <= b.MaxX && b.MinY <= v && v <= b.MaxY
					case 3:
						on, in = b.MaxZ >= 1-eps, b.MinX <= u && u <= b.MaxX && b.MinY <= v && v <= b.MaxY
					case 4:
						on, in = b.MinX <= eps, b.MinZ <= u && u <= b.MaxZ && b.MinY <= v && v <= b.MaxY
					case 5:
						on, in = b.MaxX >= 1-eps, b.MinZ <= u && u <= b.MaxZ && b.MinY <= v && v <= b.MaxY
					}
					if on && in {
						hit = true
						break
					}
				}
				full = hit
			}
		}
		if full {
			m |= 1 << d
		}
	}
	return m
}

// Ultrawarm reports nether fluid rules (water evaporates, lava is faster).
func (w *World) Ultrawarm() bool { return w.cfg.Ultrawarm }

// FluidOf returns the fluid kind held by state s and whether it is a source.
func FluidOf(s data.StateID) (FluidKind, bool) {
	t := fluidData()
	if int(s) >= len(t.info) {
		return FluidNone, false
	}
	f := t.info[s]
	return f.kind(), f.source()
}

// FluidState returns the water or lava block state for a kind and level.
func FluidState(k FluidKind, level int) data.StateID { return fluidData().levels[k][level&15] }

// FluidBreaks reports whether flowing fluid washes away state s.
func FluidBreaks(s data.StateID) bool {
	t := fluidData()
	return t.breaks[data.Canonical().BlockID(s)]
}

func (t *fluidTables) of(s data.StateID) fluidInfo {
	if int(s) >= len(t.info) {
		return 0
	}
	return t.info[s]
}

// pass reports whether fluid can cross from a (state at the origin) to b
// in direction d.
func (t *fluidTables) pass(a, b data.StateID, d int) bool {
	return t.faces[a]&(1<<d) == 0 && t.faces[b]&(1<<oppDir(d)) == 0
}

// holds reports whether fluid k can flow into state s (air, a block it
// washes away, or weaker fluid of the same kind).
func (t *fluidTables) holds(k FluidKind, s data.StateID) bool {
	f := t.of(s)
	if f.kind() == k {
		return !f.source()
	}
	if f.kind() != FluidNone {
		return false
	}
	if data.IsAir(s) {
		return true
	}
	id := data.Canonical().BlockID(s)
	if id == t.bubble {
		return k == FluidWater
	}
	return t.breaks[id]
}

func fluidDelay(k FluidKind, ultrawarm bool) int {
	switch {
	case k == FluidWater:
		return 5
	case ultrawarm:
		return 10
	}
	return 30
}

func (w *World) fluidDrop(k FluidKind) int {
	if k == FluidLava && !w.cfg.Ultrawarm {
		return 2
	}
	return 1
}

func (w *World) fluidSlope(k FluidKind) int {
	if k == FluidLava && !w.cfg.Ultrawarm {
		return 2
	}
	return 4
}

func (w *World) sourceConversion(k FluidKind) bool {
	if f := w.cfg.Hooks.FluidConversion; f != nil {
		return f(k)
	}
	return k == FluidWater
}

// ScheduleFluidTick queues a fluid tick at x,y,z for the fluid kind now
// there, unless one is already pending.
func (t *TickContext) ScheduleFluidTick(x, y, z int, delay int) {
	k := fluidData().of(t.Block(x, y, z)).kind()
	if k == FluidNone {
		return
	}
	if ChunkAt(x, z).Region() != t.r.Pos {
		due := t.schedNow() + int64(max(delay, 1))
		t.w.Submit(x, z, func(o *TickContext) { o.r.scheduleFluid(x, y, z, due, k) })
		return
	}
	t.r.scheduleFluid(x, y, z, t.schedNow()+int64(max(delay, 1)), k)
}

func fluidKey(x, y, z int) uint64 {
	return uint64(x)&0x3ffffff<<38 | uint64(z)&0x3ffffff<<12 | uint64(y)&0xfff
}

func (r *Region) scheduleFluid(x, y, z int, due int64, k FluidKind) {
	key := fluidKey(x, y, z)
	if r.fluidPending == nil {
		r.fluidPending = map[uint64]struct{}{}
	}
	if _, ok := r.fluidPending[key]; ok {
		return
	}
	r.fluidPending[key] = struct{}{}
	r.seq++
	heap.Push(&r.ticks, scheduled{due: due, seq: r.seq, x: int32(x), y: int32(y), z: int32(z), fluid: k})
}

// fluidChanged runs after a block change at x,y,z applied in this region:
// the changed position and its neighbours re-check their fluid.
func (t *TickContext) fluidChanged(x, y, z int, s data.StateID) {
	ft := fluidData()
	if ft.of(s).kind() != FluidNone {
		t.fluidNotify(x, y, z, s)
	}
	for _, d := range updateOrder {
		nx, ny, nz := x+d[0], y+d[1], z+d[2]
		if !t.w.cfg.Dim.Contains(ny) {
			continue
		}
		ns := t.Block(nx, ny, nz)
		if ft.of(ns).kind() == FluidNone {
			continue
		}
		if ChunkAt(nx, nz).Region() != t.r.Pos {
			t.w.Submit(nx, nz, func(o *TickContext) { o.fluidNotify(nx, ny, nz, o.Block(nx, ny, nz)) })
			continue
		}
		t.fluidNotify(nx, ny, nz, ns)
	}
}

// fluidNotify is the reaction of the fluid at x,y,z to a change at or next
// to it: lava may solidify at once, otherwise a tick is scheduled.
func (t *TickContext) fluidNotify(x, y, z int, s data.StateID) {
	ft := fluidData()
	k := ft.of(s).kind()
	if k == FluidNone {
		return
	}
	if k == FluidLava && ft.of(s) != 0 && data.Canonical().BlockID(s) == ft.blocks[FluidLava] && t.lavaMeets(x, y, z, s) {
		return
	}
	t.ScheduleFluidTick(x, y, z, fluidDelay(k, t.w.cfg.Ultrawarm))
}

// lavaMeets turns lava touching water (from any side but below) into
// obsidian (source) or cobblestone, or into basalt over soul soil next to
// blue ice.
func (t *TickContext) lavaMeets(x, y, z int, s data.StateID) bool {
	ft := fluidData()
	soul := data.Canonical().BlockID(t.Block(x, y-1, z)) == ft.soulSoil
	for d := 1; d < 6; d++ {
		n := t.Block(x+fluidDirs[d][0], y+fluidDirs[d][1], z+fluidDirs[d][2])
		if ft.of(n).kind() == FluidWater {
			to := ft.cobble
			if ft.of(s).source() {
				to = ft.obsidian
			}
			t.SetBlock(x, y, z, to)
			t.fluidEvent(FluidFizz, x, y, z, s)
			return true
		}
		if soul && data.Canonical().BlockID(n) == ft.blueIce {
			t.SetBlock(x, y, z, ft.basalt)
			t.fluidEvent(FluidFizz, x, y, z, s)
			return true
		}
	}
	return false
}

func (t *TickContext) fluidEvent(ev FluidEvent, x, y, z int, old data.StateID) {
	if f := t.w.cfg.Hooks.Fluid; f != nil {
		f(t, ev, x, y, z, old)
	}
}

// fluidTick runs a scheduled fluid tick at x,y,z holding state s.
func (t *TickContext) fluidTick(x, y, z int, s data.StateID) {
	ft := fluidData()
	f := ft.of(s)
	if !f.source() {
		ns := t.fluidTarget(x, y, z, f.kind(), s)
		if ns != s {
			t.SetBlock(x, y, z, ns)
			if ft.of(ns).kind() == FluidNone {
				return
			}
			s, f = ns, ft.of(ns)
		}
	}
	t.fluidSpread(x, y, z, s, f)
}

// fluidTarget is the state the flowing fluid k at x,y,z settles to from
// its neighbours: a new source between two sources, falling fluid under
// fluid, else the strongest horizontal neighbour minus the drop-off, or air.
func (t *TickContext) fluidTarget(x, y, z int, k FluidKind, cur data.StateID) data.StateID {
	ft := fluidData()
	best, sources := 0, 0
	for d := 2; d < 6; d++ {
		n := t.Block(x+fluidDirs[d][0], y, z+fluidDirs[d][2])
		nf := ft.of(n)
		if nf.kind() != k || !ft.pass(cur, n, d) {
			continue
		}
		if nf.source() {
			sources++
		}
		best = max(best, nf.amount())
	}
	if sources >= 2 && t.w.sourceConversion(k) {
		below := t.Block(x, y-1, z)
		if bf := ft.of(below); ft.solid[below] || bf.kind() == k && bf.source() {
			return ft.levels[k][0]
		}
	}
	if up := t.Block(x, y+1, z); ft.of(up).kind() == k && ft.pass(cur, up, fdUp) {
		return ft.levels[k][8]
	}
	a := best - t.w.fluidDrop(k)
	if a <= 0 {
		return ft.air
	}
	return ft.levels[k][8-a]
}

// hole reports whether fluid k standing at x,y,z (state s) could fall.
func (t *TickContext) fluidHole(k FluidKind, x, y, z int, s data.StateID) bool {
	ft := fluidData()
	b := t.Block(x, y-1, z)
	if !ft.pass(s, b, fdDown) {
		return false
	}
	return ft.of(b).kind() == k || ft.holds(k, b)
}

func (t *TickContext) fluidSpread(x, y, z int, s data.StateID, f fluidInfo) {
	ft := fluidData()
	k := f.kind()
	if t.w.cfg.Dim.Contains(y - 1) {
		b := t.Block(x, y-1, z)
		if ft.pass(s, b, fdDown) {
			bf := ft.of(b)
			if k == FluidLava && bf.kind() == FluidWater && data.Canonical().BlockID(b) == ft.blocks[FluidWater] {
				t.SetBlock(x, y-1, z, ft.stone)
				t.fluidEvent(FluidFizz, x, y-1, z, b)
				return
			}
			if ft.holds(k, b) && !(bf.kind() == k && bf.falling()) {
				t.fluidInto(k, x, y-1, z, b, ft.levels[k][8])
				return
			}
			if !f.source() && (bf.kind() == k || ft.holds(k, b)) {
				return
			}
		}
	}
	next := f.amount() - t.w.fluidDrop(k)
	if f.falling() {
		next = 8 - t.w.fluidDrop(k)
	}
	if next <= 0 {
		return
	}
	var dist [6]int
	bestD := 1000
	for d := 2; d < 6; d++ {
		dist[d] = -1
		nx, nz := x+fluidDirs[d][0], z+fluidDirs[d][2]
		n := t.Block(nx, y, nz)
		if !ft.pass(s, n, d) || !ft.holds(k, n) {
			continue
		}
		dd := 0
		if !t.fluidHole(k, nx, y, nz, n) {
			dd = t.fluidSlopeSearch(k, nx, y, nz, n, 1, oppDir(d))
		}
		dist[d] = dd
		bestD = min(bestD, dd)
	}
	for d := 2; d < 6; d++ {
		if dist[d] != bestD {
			continue
		}
		nx, nz := x+fluidDirs[d][0], z+fluidDirs[d][2]
		n := t.Block(nx, y, nz)
		if nf := ft.of(n); nf.kind() == k && nf.amount() >= next {
			continue
		}
		// the new state comes from the target's own neighbourhood, so a
		// spot between two sources becomes a source at once
		ns := t.fluidTarget(nx, y, nz, k, n)
		if ft.of(ns).amount() < next {
			ns = ft.levels[k][8-next]
		}
		t.fluidInto(k, nx, y, nz, n, ns)
	}
}

// fluidSlopeSearch is the distance (1..slope) from the start to the
// nearest spot fluid k could fall from, or 1000.
func (t *TickContext) fluidSlopeSearch(k FluidKind, x, y, z int, s data.StateID, depth, from int) int {
	ft := fluidData()
	best := 1000
	for d := 2; d < 6; d++ {
		if d == from {
			continue
		}
		nx, nz := x+fluidDirs[d][0], z+fluidDirs[d][2]
		n := t.Block(nx, y, nz)
		if !ft.pass(s, n, d) || !ft.holds(k, n) {
			continue
		}
		if t.fluidHole(k, nx, y, nz, n) {
			return depth
		}
		if depth < t.w.fluidSlope(k) {
			best = min(best, t.fluidSlopeSearch(k, nx, y, nz, n, depth+1, oppDir(d)))
		}
	}
	return best
}

// fluidInto places fluid state ns at x,y,z (currently old), washing away
// what was there.
func (t *TickContext) fluidInto(k FluidKind, x, y, z int, old, ns data.StateID) {
	ft := fluidData()
	if !data.IsAir(old) && ft.of(old).kind() == FluidNone {
		if k == FluidLava {
			t.fluidEvent(FluidFizz, x, y, z, old)
		} else {
			t.fluidEvent(FluidDestroy, x, y, z, old)
		}
	}
	t.SetBlock(x, y, z, ns)
}

// FluidFlow is the horizontal flow direction (unit length or zero) of the
// fluid at x,y,z: toward lower neighbouring fluid and open sides.
func FluidFlow(src interface {
	Block(x, y, z int) data.StateID
}, x, y, z int) (fx, fz float64) {
	ft := fluidData()
	s := src.Block(x, y, z)
	f := ft.of(s)
	if f.kind() == FluidNone {
		return 0, 0
	}
	for d := 2; d < 6; d++ {
		dx, dz := fluidDirs[d][0], fluidDirs[d][2]
		n := src.Block(x+dx, y, z+dz)
		nf := ft.of(n)
		var diff int
		switch {
		case nf.kind() == f.kind():
			diff = f.amount() - nf.amount()
		case nf.kind() == FluidNone && !ft.solid[n] && ft.pass(s, n, d):
			diff = f.amount()
		default:
			continue
		}
		fx += float64(diff * dx)
		fz += float64(diff * dz)
	}
	if l := fx*fx + fz*fz; l > 0 {
		l = 1 / math.Sqrt(l)
		fx, fz = fx*l, fz*l
	}
	return fx, fz
}
