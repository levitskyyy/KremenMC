package server

import (
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

// Nether portal shapes, lighting and breaking. The frame rules (interior
// 2..21 wide and 3..21 tall, obsidian sides, bottom and top, corners free,
// interior of air/fire/portal) follow the minecraft.wiki description.

var portalBlocks = sync.OnceValue(func() (b struct {
	obsidian, portal, fire, soulFire, endPortal, endFrame data.BlockID
	portalX, portalZ                                      data.StateID
}) {
	id := func(n string) data.BlockID {
		s, _ := data.DefaultState(n)
		return data.Canonical().BlockID(s)
	}
	b.obsidian, b.portal, b.fire, b.soulFire = id("obsidian"), id("nether_portal"), id("fire"), id("soul_fire")
	b.endPortal, b.endFrame = id("end_portal"), id("end_portal_frame")
	p, _ := data.DefaultState("nether_portal")
	b.portalX, _ = data.With(p, "axis", "x")
	b.portalZ, _ = data.With(p, "axis", "z")
	return b
})

func blockID(s data.StateID) data.BlockID { return data.Canonical().BlockID(s) }

func isObsidian(s data.StateID) bool { return blockID(s) == portalBlocks().obsidian }

func isNetherPortal(s data.StateID) bool { return blockID(s) == portalBlocks().portal }

func portalEmpty(s data.StateID) bool {
	if data.IsAir(s) {
		return true
	}
	b := portalBlocks()
	id := blockID(s)
	return id == b.fire || id == b.soulFire || id == b.portal
}

const (
	portalMaxSize   = 21
	portalMinWidth  = 2
	portalMinHeight = 3
)

type portalAxis uint8

const (
	axisX portalAxis = iota
	axisZ
)

func (a portalAxis) step() (int, int) {
	if a == axisX {
		return 1, 0
	}
	return 0, 1
}

func (a portalAxis) state() data.StateID {
	if a == axisX {
		return portalBlocks().portalX
	}
	return portalBlocks().portalZ
}

func portalAxisOf(s data.StateID) portalAxis {
	if v, _ := data.Canonical().Value(s, "axis"); v == "z" {
		return axisZ
	}
	return axisX
}

// portalShape is the interior of a frame: origin is the bottom cell with
// the lowest coordinate along the axis.
type portalShape struct {
	axis          portalAxis
	origin        api.BlockPos
	width, height int
	portals       int
}

func (sh portalShape) cell(i, h int) api.BlockPos {
	dx, dz := sh.axis.step()
	return api.BlockPos{X: sh.origin.X + i*dx, Y: sh.origin.Y + h, Z: sh.origin.Z + i*dz}
}

// findPortalShape finds the frame around p along axis a.
func findPortalShape(src exBlocks, p api.BlockPos, minY int, a portalAxis) (portalShape, bool) {
	dx, dz := a.step()
	at := func(q api.BlockPos, i, h int) data.StateID {
		return src.Block(q.X+i*dx, q.Y+h, q.Z+i*dz)
	}
	if !portalEmpty(src.Block(p.X, p.Y, p.Z)) {
		return portalShape{}, false
	}
	b := p
	for n := 0; n < portalMaxSize && b.Y > minY && portalEmpty(src.Block(b.X, b.Y-1, b.Z)); n++ {
		b.Y--
	}
	if !isObsidian(src.Block(b.X, b.Y-1, b.Z)) {
		return portalShape{}, false
	}
	left := 0
	for left < portalMaxSize && portalEmpty(at(b, -left-1, 0)) && isObsidian(at(b, -left-1, -1)) {
		left++
	}
	if left >= portalMaxSize || !isObsidian(at(b, -left-1, 0)) {
		return portalShape{}, false
	}
	o := api.BlockPos{X: b.X - left*dx, Y: b.Y, Z: b.Z - left*dz}
	w := 0
	for w <= portalMaxSize && portalEmpty(at(o, w, 0)) && isObsidian(at(o, w, -1)) {
		w++
	}
	if w < portalMinWidth || w > portalMaxSize || !isObsidian(at(o, w, 0)) {
		return portalShape{}, false
	}
	sh := portalShape{axis: a, origin: o, width: w}
	h := 0
rows:
	for ; h <= portalMaxSize; h++ {
		for i := range w {
			s := at(o, i, h)
			if !portalEmpty(s) {
				break rows
			}
			if isNetherPortal(s) {
				sh.portals++
			}
		}
		if !isObsidian(at(o, -1, h)) || !isObsidian(at(o, w, h)) {
			return portalShape{}, false
		}
	}
	if h < portalMinHeight || h > portalMaxSize {
		return portalShape{}, false
	}
	for i := range w {
		if !isObsidian(at(o, i, h)) {
			return portalShape{}, false
		}
	}
	sh.height = h
	return sh, true
}

// findEmptyPortalShape is the frame lighting fire at p would fill: axis X
// first, then Z; the interior must not already hold portal blocks.
func findEmptyPortalShape(src exBlocks, p api.BlockPos, minY int) (portalShape, bool) {
	for _, a := range [...]portalAxis{axisX, axisZ} {
		if sh, ok := findPortalShape(src, p, minY, a); ok && sh.portals == 0 {
			return sh, true
		}
	}
	return portalShape{}, false
}

// portalLevel is implemented by igniteWorld: the dimension decides whether
// fire may open a portal (overworld and nether only).
type portalLevel interface {
	portalDim() (dimKind, int)
}

func (l *igniteWorld) portalDim() (dimKind, int) { return l.w.dim, l.w.MinY() }

// portalCanLight is the isPortal half of BaseFireBlock.canBePlacedAt.
func portalCanLight(lv exBlocks, p api.BlockPos) bool {
	pl, ok := lv.(portalLevel)
	if !ok {
		return false
	}
	d, minY := pl.portalDim()
	if d == dimEnd || !data.IsAir(lv.Block(p.X, p.Y, p.Z)) {
		return false
	}
	_, ok = findEmptyPortalShape(lv, p, minY)
	return ok
}

// lightPortal replaces the fire just placed at p with a portal when it sits
// in a valid frame (BaseFireBlock.onPlace).
func lightPortal(lv igniteLevel, p api.BlockPos) bool {
	pl, ok := lv.(portalLevel)
	if !ok {
		return false
	}
	d, minY := pl.portalDim()
	if d == dimEnd {
		return false
	}
	sh, ok := findEmptyPortalShape(lv, p, minY)
	if !ok {
		return false
	}
	st := sh.axis.state()
	for h := range sh.height {
		for i := range sh.width {
			lv.setBlock(sh.cell(i, h), st)
		}
	}
	return true
}

// portalPOI indexes nether portal blocks of a world (the vanilla
// nether_portal point of interest), persisted in kremenmc-portals.json.
type portalPOI struct {
	mu     sync.Mutex
	loaded bool
	set    map[api.BlockPos]struct{}
	dirty  bool
	queue  []api.BlockPos // obsidian/portal removals to re-check (tick)
}

func (w *World) poiLoad() {
	if w.poi.loaded {
		return
	}
	w.poi.loaded = true
	w.poi.set = map[api.BlockPos]struct{}{}
	var l [][3]int
	if err := readJSON(filepath.Join(w.dir, "kremenmc-portals.json"), &l); err == nil {
		for _, p := range l {
			w.poi.set[api.BlockPos{X: p[0], Y: p[1], Z: p[2]}] = struct{}{}
		}
	}
}

func (w *World) savePortals() error {
	w.poi.mu.Lock()
	defer w.poi.mu.Unlock()
	if !w.poi.dirty {
		return nil
	}
	l := make([][3]int, 0, len(w.poi.set))
	for p := range w.poi.set {
		l = append(l, [3]int{p.X, p.Y, p.Z})
	}
	slices.SortFunc(l, func(a, b [3]int) int { return slices.Compare(a[:], b[:]) })
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	w.poi.dirty = false
	return writeJSON(filepath.Join(w.dir, "kremenmc-portals.json"), l)
}

// portalBlockChanged runs from the BlockChanged hook (any goroutine).
func (w *World) portalBlockChanged(x, y, z int, old, s data.StateID) {
	b := portalBlocks()
	oid, nid := blockID(old), blockID(s)
	if oid != b.portal && nid != b.portal && oid != b.obsidian {
		return
	}
	p := api.BlockPos{X: x, Y: y, Z: z}
	w.poi.mu.Lock()
	w.poiLoad()
	switch {
	case nid == b.portal && oid != b.portal:
		w.poi.set[p] = struct{}{}
		w.poi.dirty = true
	case oid == b.portal && nid != b.portal:
		delete(w.poi.set, p)
		w.poi.dirty = true
	}
	if oid != nid && (oid == b.portal || oid == b.obsidian) {
		w.poi.queue = append(w.poi.queue, p)
	}
	w.poi.mu.Unlock()
}

// portalSupported reports whether the portal block at p still has portal or
// obsidian on both sides along its axis and above and below.
func portalSupported(src exBlocks, p api.BlockPos, s data.StateID) bool {
	dx, dz := portalAxisOf(s).step()
	ok := func(q data.StateID) bool { return isObsidian(q) || isNetherPortal(q) && q == s }
	return ok(src.Block(p.X+dx, p.Y, p.Z+dz)) && ok(src.Block(p.X-dx, p.Y, p.Z-dz)) &&
		ok(src.Block(p.X, p.Y+1, p.Z)) && ok(src.Block(p.X, p.Y-1, p.Z))
}

// tickPortalBreaks removes portals whose frame was broken: an unsupported
// portal block takes every connected portal block of its axis with it.
func (w *World) tickPortalBreaks() {
	w.poi.mu.Lock()
	q := w.poi.queue
	w.poi.queue = nil
	w.poi.mu.Unlock()
	gone := map[api.BlockPos]bool{}
	for _, p := range q {
		for _, d := range portalNeighbours {
			n := p.Add(d[0], d[1], d[2])
			s := w.w.Block(n.X, n.Y, n.Z)
			if gone[n] || !isNetherPortal(s) || portalSupported(w.w, n, s) {
				continue
			}
			stack := []api.BlockPos{n}
			gone[n] = true
			for len(stack) > 0 {
				c := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				w.w.SetBlock(c.X, c.Y, c.Z, data.Air)
				for _, d := range portalNeighbours {
					m := c.Add(d[0], d[1], d[2])
					if !gone[m] && w.w.Block(m.X, m.Y, m.Z) == s {
						gone[m] = true
						stack = append(stack, m)
					}
				}
			}
		}
	}
}

var portalNeighbours = [...][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}}

// portalCandidates returns indexed portal blocks within the square radius r
// of c, nearest first (distance, then lower Y).
func (w *World) portalCandidates(c api.BlockPos, r int) []api.BlockPos {
	w.poi.mu.Lock()
	w.poiLoad()
	var out []api.BlockPos
	for p := range w.poi.set {
		if abs(p.X-c.X) <= r && abs(p.Z-c.Z) <= r {
			out = append(out, p)
		}
	}
	w.poi.mu.Unlock()
	slices.SortFunc(out, func(a, b api.BlockPos) int {
		da, db := distSq(a, c), distSq(b, c)
		if da != db {
			if da < db {
				return -1
			}
			return 1
		}
		return a.Y - b.Y
	})
	return out
}

func distSq(a, b api.BlockPos) int {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
