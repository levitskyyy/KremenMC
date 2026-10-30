package structure

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Template neighbour shape updates (StructureTemplate's
// updateFromNeighbourShapes after placement) for the connecting block
// families that occur in legacy templates: walls, panes and bars, fences
// and stairs. The connection rules follow internal/server/block_shape.go
// (wiki block pages, checked there against the reference); here they were
// checked against the RPScene probe (walls next to obsidian, iron bars).

type shapeFam uint8

const (
	famNone shapeFam = iota
	famWall
	famPane
	famFence
	famStairs
	famGate
)

type famInfo struct {
	fam          shapeFam
	wooden       bool
	exception    bool
	postOverride bool
}

var famOnce = sync.OnceValue(func() []famInfo {
	reg := data.Canonical()
	pk, _ := wgdata.Load()
	tag := func(t string) map[string]bool {
		if pk == nil {
			return nil
		}
		return blockTag(pk, "#minecraft:"+t)
	}
	walls, fences, gates, wooden, leaves := tag("walls"), tag("fences"), tag("fence_gates"), tag("wooden_fences"), tag("leaves")
	shulkers, post := tag("shulker_boxes"), tag("wall_post_override")
	out := make([]famInfo, len(reg.Blocks))
	for i := range reg.Blocks {
		b := &reg.Blocks[i]
		n := b.Name
		fi := &out[b.ID]
		fi.wooden, fi.postOverride = wooden[n], post[n]
		fi.exception = leaves[n] || shulkers[n] || n == "barrier" || n == "carved_pumpkin" || n == "jack_o_lantern" || n == "melon" || n == "pumpkin"
		switch {
		case walls[n]:
			fi.fam = famWall
		case fences[n]:
			fi.fam = famFence
		case (strings.HasSuffix(n, "_pane") || strings.HasSuffix(n, "_bars")) && b.PropIndex("north") >= 0:
			fi.fam = famPane
		case gates[n]:
			fi.fam = famGate
		case strings.HasSuffix(n, "_stairs"):
			fi.fam = famStairs
		}
	}
	return out
})

func famOf(s data.StateID) famInfo {
	id := data.Canonical().BlockID(s)
	f := famOnce()
	if int(id) >= len(f) {
		return famInfo{}
	}
	return f[id]
}

type hdir struct {
	name    string
	dx, dz  int32
	opp     string
	cw, ccw string
}

var hdirs = [4]hdir{
	{"north", 0, -1, "south", "east", "west"},
	{"east", 1, 0, "west", "south", "north"},
	{"south", 0, 1, "north", "west", "east"},
	{"west", -1, 0, "east", "north", "south"},
}

func hdirByName(n string) (hdir, bool) {
	for _, d := range hdirs {
		if d.name == n {
			return d, true
		}
	}
	return hdir{}, false
}

func stateProp(s data.StateID, k string) string {
	v, _ := data.Canonical().Value(s, k)
	return v
}

func withProp(s data.StateID, k, v string) data.StateID {
	if n, ok := data.Canonical().With(s, k, v); ok {
		return n
	}
	return s
}

func connectsTo(self famInfo, n data.StateID, d hdir) bool {
	ni := famOf(n)
	plain := !ni.exception && feature.FaceSturdy(n, d.opp)
	gate := false
	if ni.fam == famGate {
		if f, ok := hdirByName(stateProp(n, "facing")); ok {
			gate = (f.dx != 0) == (d.dz != 0)
		}
	}
	switch self.fam {
	case famFence:
		return plain || ni.fam == famFence && ni.wooden == self.wooden || gate
	case famWall:
		return ni.fam == famWall || plain || ni.fam == famPane || gate
	case famPane:
		return plain || ni.fam == famPane || ni.fam == famWall
	}
	return false
}

// coversDown reports whether the collision of s touching its bottom face
// covers the rectangle [u0,u1]x[v0,v1] (x, z).
func coversDown(s data.StateID, u0, v0, u1, v1 float64) bool {
	return faceCovers(s, "down", u0, v0, u1, v1)
}

// collisionFaceFull is Block.isFaceFull of the collision shape on face d
// (vines accept leaves, which are not sturdy).
func collisionFaceFull(s data.StateID, d string) bool { return faceCovers(s, d, 0, 0, 1, 1) }

// faceCovers reports whether the collision boxes of s touching face d cover
// the rectangle [u0,u1]x[v0,v1] of that face (x,z for down/up, x,y for
// north/south, z,y for west/east).
func faceCovers(s data.StateID, d string, u0, v0, u1, v1 float64) bool {
	const eps = 1e-7
	var rects [][4]float64
	for _, b := range data.Collision(s) {
		var touch bool
		var r [4]float64
		switch d {
		case "down":
			touch, r = b.MinY <= eps, [4]float64{b.MinX, b.MinZ, b.MaxX, b.MaxZ}
		case "up":
			touch, r = b.MaxY >= 1-eps, [4]float64{b.MinX, b.MinZ, b.MaxX, b.MaxZ}
		case "north":
			touch, r = b.MinZ <= eps, [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}
		case "south":
			touch, r = b.MaxZ >= 1-eps, [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}
		case "west":
			touch, r = b.MinX <= eps, [4]float64{b.MinZ, b.MinY, b.MaxZ, b.MaxY}
		case "east":
			touch, r = b.MaxX >= 1-eps, [4]float64{b.MinZ, b.MinY, b.MaxZ, b.MaxY}
		}
		if touch {
			rects = append(rects, r)
		}
	}
	if len(rects) == 0 {
		return false
	}
	us, vs := []float64{u0, u1}, []float64{v0, v1}
	for _, r := range rects {
		for _, u := range [2]float64{r[0], r[2]} {
			if u > u0 && u < u1 {
				us = append(us, u)
			}
		}
		for _, v := range [2]float64{r[1], r[3]} {
			if v > v0 && v < v1 {
				vs = append(vs, v)
			}
		}
	}
	for _, a := range us {
		for _, c := range vs {
			if a >= u1 || c >= v1 {
				continue
			}
			na, nc := u1, v1
			for _, u := range us {
				if u > a && u < na {
					na = u
				}
			}
			for _, v := range vs {
				if v > c && v < nc {
					nc = v
				}
			}
			cu, cv := (a+na)/2, (c+nc)/2
			in := false
			for _, r := range rects {
				if cu > r[0] && cu < r[2] && cv > r[1] && cv < r[3] {
					in = true
					break
				}
			}
			if !in {
				return false
			}
		}
	}
	return true
}

var wallTallTest = map[string][4]float64{
	"north": {7.0 / 16, 0, 9.0 / 16, 9.0 / 16},
	"south": {7.0 / 16, 7.0 / 16, 9.0 / 16, 1},
	"west":  {0, 7.0 / 16, 9.0 / 16, 9.0 / 16},
	"east":  {7.0 / 16, 7.0 / 16, 1, 9.0 / 16},
}

// reshapeAt is the state s at x,y,z should take from its neighbours.
func reshapeAt(lv Level, x, y, z int32, s data.StateID) data.StateID {
	fi := famOf(s)
	switch fi.fam {
	case famFence, famPane:
		for _, d := range hdirs {
			s = withProp(s, d.name, map[bool]string{false: "false", true: "true"}[connectsTo(fi, lv.Block(x+d.dx, y, z+d.dz), d)])
		}
	case famWall:
		above := lv.Block(x, y+1, z)
		side := map[string]string{}
		for _, d := range hdirs {
			v := "none"
			if connectsTo(fi, lv.Block(x+d.dx, y, z+d.dz), d) {
				v = "low"
				if r := wallTallTest[d.name]; coversDown(above, r[0], r[1], r[2], r[3]) {
					v = "tall"
				}
			}
			side[d.name] = v
			s = withProp(s, d.name, v)
		}
		s = withProp(s, "up", map[bool]string{false: "false", true: "true"}[wallPost(above, side)])
	case famStairs:
		s = withProp(s, "shape", stairsShape(lv, x, y, z, s))
	}
	return s
}

func wallPost(above data.StateID, side map[string]string) bool {
	ai := famOf(above)
	if ai.postOverride || ai.fam == famWall && stateProp(above, "up") == "true" {
		return true
	}
	// Opposite tall sides drop the post even at a crossing (RPScene: four
	// tall sides under a full block, 4/4); the remaining order follows the
	// server's placement observations.
	if side["north"] == "tall" && side["south"] == "tall" || side["east"] == "tall" && side["west"] == "tall" {
		return false
	}
	n, s, e, w := side["north"] != "none", side["south"] != "none", side["east"] != "none", side["west"] != "none"
	if !(n && s && !e && !w || e && w && !n && !s) {
		return true
	}
	return coversDown(above, 7.0/16, 7.0/16, 9.0/16, 9.0/16)
}

func stairsShape(lv Level, x, y, z int32, s data.StateID) string {
	f, ok := hdirByName(stateProp(s, "facing"))
	if !ok {
		return "straight"
	}
	half := stateProp(s, "half")
	at := func(d hdir) data.StateID { return lv.Block(x+d.dx, y, z+d.dz) }
	takes := func(d hdir) bool {
		o := at(d)
		return famOf(o).fam != famStairs || stateProp(o, "facing") != stateProp(s, "facing") || stateProp(o, "half") != half
	}
	if b := at(f); famOf(b).fam == famStairs && stateProp(b, "half") == half {
		if d, ok := hdirByName(stateProp(b, "facing")); ok && (d.dx != 0) != (f.dx != 0) {
			if o, _ := hdirByName(d.opp); takes(o) {
				if d.name == f.ccw {
					return "outer_left"
				}
				return "outer_right"
			}
		}
	}
	if o, _ := hdirByName(f.opp); true {
		if b := at(o); famOf(b).fam == famStairs && stateProp(b, "half") == half {
			if d, ok := hdirByName(stateProp(b, "facing")); ok && (d.dx != 0) != (f.dx != 0) && takes(d) {
				if d.name == f.ccw {
					return "inner_left"
				}
				return "inner_right"
			}
		}
	}
	return "straight"
}

// templateShapeUpdates reshapes every written block in write order, then
// the blocks just outside the written set (updateShapeAtEdge).
func templateShapeUpdates(lv Level, out []rpBlock) {
	in := make(map[[3]int32]bool, len(out))
	for _, b := range out {
		in[[3]int32{b.x, b.y, b.z}] = true
	}
	for _, b := range out {
		s := lv.Block(b.x, b.y, b.z)
		if famOf(s).fam == famNone {
			continue
		}
		if n := reshapeAt(lv, b.x, b.y, b.z, s); n != s {
			lv.SetBlock(b.x, b.y, b.z, n)
		}
	}
	edge := [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for _, b := range out {
		for _, d := range edge {
			x, y, z := b.x+d[0], b.y+d[1], b.z+d[2]
			if in[[3]int32{x, y, z}] {
				continue
			}
			s := lv.Block(x, y, z)
			if famOf(s).fam == famNone {
				continue
			}
			if n := reshapeAt(lv, x, y, z, s); n != s {
				lv.SetBlock(x, y, z, n)
			}
		}
	}
}

// waterSource reports a water source fluid: still water or a waterlogged
// block.
func waterSource(reg *data.Registry, s data.StateID) bool {
	b := reg.BlockOf(s)
	if b == nil {
		return false
	}
	if b.Name == "water" {
		return s == b.DefaultState
	}
	return stateProp(s, "waterlogged") == "true"
}

// templateWaterlog spreads water into the written waterloggable blocks
// (liquid settings APPLY_WATERLOGGING): a dry one next to a water source
// above or on a horizontal side becomes waterlogged; passes repeat in write
// order until nothing changes.
func templateWaterlog(reg *data.Registry, lv Level, out []rpBlock) {
	var wl []rpBlock
	for _, b := range out {
		if stateProp(lv.Block(b.x, b.y, b.z), "waterlogged") == "false" {
			wl = append(wl, b)
		}
	}
	nb := [5][3]int32{{0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
	for changed := true; changed; {
		changed = false
		for _, b := range wl {
			s := lv.Block(b.x, b.y, b.z)
			if stateProp(s, "waterlogged") != "false" {
				continue
			}
			for _, d := range nb {
				if waterSource(reg, lv.Block(b.x+d[0], b.y+d[1], b.z+d[2])) {
					lv.SetBlock(b.x, b.y, b.z, withProp(s, "waterlogged", "true"))
					changed = true
					break
				}
			}
		}
	}
}
