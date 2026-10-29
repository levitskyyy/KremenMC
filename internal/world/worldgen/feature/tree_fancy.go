package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// fancyTrunk is fancy_trunk_placer (big oak). Everything here is fitted to
// kremenmc.probe.feat_tree.Draws output (testdata/trees_placers.txt, 120 trees,
// h 3..14):
//   - trunk logs from 0 to max(h-3, (h+2)*3/5);
//   - a crown cluster at (0, h-3, 0), and one more attempt per level from
//     y = h-4 down to max(2, h/3), each drawing two floats a, b;
//   - the cluster sits at distance 0.5*sqrt((y+1)(h+1-y))*(a+0.328) in the
//     direction (sin 2πb, cos 2πb), rounded half up;
//   - its branch starts on the trunk at the absolute cluster y minus
//     0.38*horizontal distance, truncated toward zero, and the attempt is
//     dropped when that start is below (h+2)/5;
//   - the attempt is also dropped when a block of the branch line or of
//     the column from the cluster four blocks up is not free (obstacle
//     probe, testdata/trees_obstacles.txt);
//   - branch logs follow the 3D line from the start, stepped along the
//     longest axis and rounded half up, with axis x unless |dz| > |dx|.
type fancyTrunk struct{ trunkHeight }

func (f fancyTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	t.setDirt(r, p.Add(Pos{0, -1, 0}))
	top := max(h-3, (h+2)*3/5)
	for i := 0; i <= top; i++ {
		t.placeLog(r, p.Add(Pos{0, i, 0}))
	}
	out := []attachment{{pos: p.Add(Pos{0, h - 3, 0})}}
	low := max(2, h/3)
	for y := h - 4; y >= low; y-- {
		a := float64(r.NextFloat())
		b := float64(r.NextFloat())
		d := 0.5 * math.Sqrt(float64((y+1)*(h+1-y))) * (a + 0.328)
		ang := 2 * math.Pi * b
		x := int(math.Floor(d*math.Sin(ang) + 0.5))
		z := int(math.Floor(d*math.Cos(ang) + 0.5))
		// Truncated toward zero in absolute coordinates: the start sits
		// one lower above y 0 than below it (probe at y 96 vs -60).
		yb := int(float64(p.Y+y)-0.38*math.Hypot(float64(x), float64(z))) - p.Y
		if 5*yb < h+2 {
			continue
		}
		c := p.Add(Pos{x, y, z})
		base := p.Add(Pos{0, yb, 0})
		if !lineFree(t.lv, base, Pos{x, y - yb, z}) || !lineFree(t.lv, c, Pos{0, 4, 0}) {
			continue
		}
		f.branch(t, r, base, Pos{x, y - yb, z})
		out = append(out, attachment{pos: c})
	}
	return out
}

// lineOffset is step j of n along delta, each coordinate rounded half up.
func lineOffset(delta Pos, j, n int) Pos {
	return Pos{
		roundHalfUp(float64(delta.X*j) / float64(n)),
		roundHalfUp(float64(delta.Y*j) / float64(n)),
		roundHalfUp(float64(delta.Z*j) / float64(n)),
	}
}

// lineFree: every block of the line is free for the tree (air, replaceable
// or log).
func lineFree(lv *Level, start, delta Pos) bool {
	n := max(abs(delta.X), abs(delta.Y), abs(delta.Z))
	for j := 0; j <= n; j++ {
		q := start
		if n > 0 {
			q = start.Add(lineOffset(delta, j, n))
		}
		if !isFree(lv, q) {
			return false
		}
	}
	return true
}

func roundHalfUp(v float64) int { return int(math.Floor(v + 0.5)) }

func (fancyTrunk) branch(t *treeRun, r *Random, start, delta Pos) {
	n := max(abs(delta.X), abs(delta.Y), abs(delta.Z))
	reg := data.Canonical()
	for j := 1; j <= n; j++ {
		o := lineOffset(delta, j, n)
		q := start.Add(o)
		if !validTreePos(t.lv, q) {
			continue
		}
		st := t.cfg.trunk.State(t.lv, r, q)
		switch {
		case o.X == 0 && o.Z == 0:
		case abs(o.Z) > abs(o.X):
			st, _ = reg.With(st, "axis", "z")
		default:
			st, _ = reg.With(st, "axis", "x")
		}
		t.lv.SetBlock(q, st)
		t.logs = append(t.logs, q)
	}
}

// fancyFoliage is fancy_foliage_placer as configured in the data pack
// (radius 2, offset 4, height 4): five layers upwards from the cluster
// position, radius 1, 2, 2, 2, 1, keeping dx²+dz² <= r²+r-1.
type fancyFoliage struct{}

func (fancyFoliage) foliageHeight(*Random, int) int { return 4 }
func (fancyFoliage) foliageRadius(*Random, int) int { return 2 }
func (fancyFoliage) place(t *treeRun, r *Random, _ int, a attachment, _, _ int) {
	for y := 0; y <= 4; y++ {
		rad := 2
		if y == 0 || y == 4 {
			rad = 1
		}
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if dx*dx+dz*dz <= rad*rad+rad-1 {
					t.placeLeaf(r, a.pos.Add(Pos{dx, y, dz}))
				}
			}
		}
	}
}

func init() {
	trunkTypes["fancy_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var f fancyTrunk
		return f, json.Unmarshal(b, &f.trunkHeight)
	}
	foliageTypes["fancy_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		var p struct{ Radius, Offset, Height json.RawMessage }
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		if string(p.Radius) != "2" || string(p.Offset) != "4" || string(p.Height) != "4" {
			return nil, unsupported("foliage_placer", fmt.Sprintf("fancy_foliage_placer %s/%s/%s", p.Radius, p.Offset, p.Height))
		}
		return fancyFoliage{}, nil
	}
}
