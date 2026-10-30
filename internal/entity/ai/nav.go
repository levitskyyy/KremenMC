package ai

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/entity"
)

// Nav follows block paths for a ground mob. Paths are recomputed only when
// asked through MoveTo with a goal that moved by more than a block, when
// the mob gets stuck, or after the path is used up.
type Nav struct {
	Malus    *Malus
	MaxFall  int
	MaxNodes int
	Range    float64 // follow range: the search radius

	path    []Node
	i       int
	goal    Node
	speed   float64
	reach   float64
	has     bool
	stuckAt int
	lastX   float64
	lastZ   float64
	tick    int
	// Searches counts A* runs (for budgets and tests).
	Searches int
}

// Done reports whether there is no path being followed.
func (n *Nav) Done() bool { return !n.has || n.i >= len(n.path) }

// SetSpeed changes the speed modifier of the current path.
func (n *Nav) SetSpeed(s float64) { n.speed = s }

func (n *Nav) Stop() { n.has = false; n.path = n.path[:0]; n.i = 0 }

// Target is the end of the current path.
func (n *Nav) Target() (Node, bool) {
	if !n.has || len(n.path) == 0 {
		return Node{}, false
	}
	return n.path[len(n.path)-1], true
}

func floorNode(x, y, z float64) Node {
	return Node{int32(math.Floor(x)), int32(math.Floor(y + 0.01)), int32(math.Floor(z))}
}

// MoveTo plans a path to (x, y, z) and starts following it. It reports
// false when no step toward the goal exists.
func (n *Nav) MoveTo(src entity.BlockSource, b *Body, x, y, z, speed, reach float64) bool {
	g := floorNode(x, y, z)
	n.speed, n.reach = speed, reach
	if n.has && n.i < len(n.path) && g == n.goal {
		return true
	}
	e := b.E
	from := floorNode(e.Pos.X, e.Pos.Y, e.Pos.Z)
	maxNodes := n.MaxNodes
	if maxNodes == 0 {
		maxNodes = max(64, min(int(n.Range*16), 512))
	}
	f := GetFinder()
	var ok bool
	n.path, ok = f.Find(Request{
		Src: src, Malus: n.Malus, Width: e.Width, Height: e.Height, StepHeight: e.StepHeight,
		MaxFall: n.MaxFall, From: from, To: g, Reach: reach, MaxNodes: maxNodes, MaxDist: n.Range + 8,
	}, n.path[:0])
	PutFinder(f)
	n.Searches++
	n.goal, n.i, n.has = g, 0, ok
	n.stuckAt, n.lastX, n.lastZ = n.tick, e.Pos.X, e.Pos.Z
	return ok
}

// Tick advances along the path and feeds the move control. src is used to
// skip waypoints the mob can walk to directly.
func (n *Nav) Tick(b *Body) {
	n.tick++
	if n.Done() {
		return
	}
	e := b.E
	thr := 0.75 - e.Width/2
	if e.Width > 0.75 {
		thr = e.Width / 2
	}
	for n.i < len(n.path) {
		p := n.path[n.i]
		cx, cy, cz := p.Center()
		dx, dz := cx-e.Pos.X, cz-e.Pos.Z
		if math.Abs(dx) < thr && math.Abs(dz) < thr && math.Abs(cy-e.Pos.Y) < 1 {
			n.i++
			continue
		}
		break
	}
	if n.i >= len(n.path) {
		n.has = false
		b.Stop()
		return
	}
	cx, cy, cz := n.path[n.i].Center()
	b.MoveTo(cx, cy, cz, n.speed)
	if n.tick-n.stuckAt >= 60 {
		dx, dz := e.Pos.X-n.lastX, e.Pos.Z-n.lastZ
		if dx*dx+dz*dz < 0.25*n.speed*b.Speed*60*0.25 {
			n.Stop()
			b.Stop()
			return
		}
		n.stuckAt, n.lastX, n.lastZ = n.tick, e.Pos.X, e.Pos.Z
	}
}

// Next returns the waypoint being walked to.
func (n *Nav) Next() (Node, bool) {
	if n.Done() {
		return Node{}, false
	}
	return n.path[n.i], true
}
