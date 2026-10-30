package server

import (
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/entity"
)

// mobGrid buckets a world's mobs into 8-block cells so box queries (item
// merging, explosion targets) touch only nearby entities. It is owned by the
// tick goroutine: rebuilt at the start of each entity tick, fed mobs added
// since then, and kept current as mobs move.
type mobGrid struct {
	mu    sync.Mutex
	cells map[uint64][]*Mob
	at    map[*Mob]uint64
	valid bool
}

// addMobLocked registers m; the caller holds w.mobsMu.
func (w *World) addMobLocked(m *Mob) {
	w.mobs[m.e.ID] = m
	w.added = append(w.added, m)
}

func (g *mobGrid) add(m *Mob) {
	if _, ok := g.at[m]; ok {
		return
	}
	k := m.gridCell()
	g.cells[k] = append(g.cells[k], m)
	g.at[m] = k
}

const gridShift = 3

func gridKey(x, y, z int) uint64 {
	return uint64(uint32(x)&0x1fffff)<<42 | uint64(uint32(y)&0x1fffff)<<21 | uint64(uint32(z)&0x1fffff)
}

func cellOf(v float64) int { return int(math.Floor(v)) >> gridShift }

func (m *Mob) gridCell() uint64 {
	m.mu.Lock()
	p := m.e.Pos
	m.mu.Unlock()
	return gridKey(cellOf(p.X), cellOf(p.Y), cellOf(p.Z))
}

func (g *mobGrid) rebuild(mobs []*Mob) {
	if g.cells == nil {
		g.cells, g.at = map[uint64][]*Mob{}, map[*Mob]uint64{}
	}
	clear(g.cells)
	clear(g.at)
	for _, m := range mobs {
		g.add(m)
	}
	g.valid = true
}

func (g *mobGrid) move(m *Mob) {
	old, ok := g.at[m]
	if !ok {
		return
	}
	k := m.gridCell()
	if k == old {
		return
	}
	cell := g.cells[old]
	for i, o := range cell {
		if o == m {
			cell[i] = cell[len(cell)-1]
			g.cells[old] = cell[:len(cell)-1]
			break
		}
	}
	g.cells[k] = append(g.cells[k], m)
	g.at[m] = k
}

// mobsIn returns the mobs whose cell overlaps box grown by one cell, so
// entities a little outside their recorded cell are still found.
func (w *World) mobsIn(box entity.AABB, out []*Mob) []*Mob {
	g := &w.grid
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.valid {
		g.rebuild(w.mobList())
	}
	w.mobsMu.Lock()
	added := w.added
	w.added = nil
	w.mobsMu.Unlock()
	for _, m := range added {
		g.add(m)
	}
	x0, x1 := cellOf(box.MinX)-1, cellOf(box.MaxX)+1
	y0, y1 := cellOf(box.MinY)-1, cellOf(box.MaxY)+1
	z0, z1 := cellOf(box.MinZ)-1, cellOf(box.MaxZ)+1
	if (x1-x0+1)*(y1-y0+1)*(z1-z0+1) > len(g.cells) {
		for _, cell := range g.cells {
			out = append(out, cell...)
		}
		return out
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				out = append(out, g.cells[gridKey(x, y, z)]...)
			}
		}
	}
	return out
}
