package feature

import "encoding/json"

// horizontals is north, east, south, west: the order a random horizontal
// direction is drawn from (nextInt(4)).
var horizontals = [4]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

// forkingTrunk is forking_trunk_placer (acacia): a trunk that leans one to
// three blocks in a random direction near the top, and possibly a diagonal
// branch in a second direction. Inferred from draw logs and block output of
// kremenmc.probe.feat_tree.Draws; verified by acacia in testdata/trees.txt.
type forkingTrunk struct{ trunkHeight }

func (f forkingTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	t.setDirt(r, p.Add(Pos{0, -1, 0}))
	d1 := horizontals[r.NextIntn(4)]
	bend := h - int(r.NextIntn(4)) - 1
	lean := 3 - int(r.NextIntn(3))
	x, z := p.X, p.Z
	var out []attachment
	top := p
	for i := range h {
		if i >= bend && lean > 0 {
			x += d1.X
			z += d1.Z
			lean--
		}
		q := Pos{x, p.Y + i, z}
		if t.placeLog(r, q) {
			top = q
		}
	}
	out = append(out, attachment{pos: top.Add(Pos{0, 1, 0}), radiusOff: 1})
	d2 := horizontals[r.NextIntn(4)]
	if d2 == d1 {
		return out
	}
	start := bend - int(r.NextIntn(2)) - 1
	n := 1 + int(r.NextIntn(3))
	x, z = p.X, p.Z
	var last *Pos
	for i := start; i < h && n > 0; i, n = i+1, n-1 {
		if i < 1 {
			continue
		}
		x += d2.X
		z += d2.Z
		q := Pos{x, p.Y + i, z}
		if t.placeLog(r, q) {
			last = &q
		}
	}
	if last != nil {
		out = append(out, attachment{pos: last.Add(Pos{0, 1, 0})})
	}
	return out
}

// acaciaFoliage: a square of radius+1 (corners cut) at the attachment's top
// log and a rounded one a block smaller above it.
type acaciaFoliage struct{ radius, offset IntProvider }

func (acaciaFoliage) foliageHeight(*Random, int) int       { return 0 }
func (a acaciaFoliage) foliageRadius(r *Random, _ int) int { return a.radius.Sample(r) }

func (a acaciaFoliage) place(t *treeRun, r *Random, _ int, at attachment, _, fr int) {
	c := at.pos.Add(Pos{0, a.offset.Sample(r), 0})
	leavesRow(t, r, c, fr+at.radiusOff, -1, at.double, skipCorners)
	leavesRow(t, r, c, fr+at.radiusOff-1, 0, at.double, func(_ *Random, dx, _, dz, _ int) bool {
		return (dx > 1 || dz > 1) && dx != 0 && dz != 0
	})
}

func init() {
	trunkTypes["forking_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var f forkingTrunk
		return f, json.Unmarshal(b, &f.trunkHeight)
	}
	foliageTypes["acacia_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		var p struct{ Radius, Offset json.RawMessage }
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		rad, err := parseInt(p.Radius)
		if err != nil {
			return nil, err
		}
		off, err := parseInt(p.Offset)
		return acaciaFoliage{rad, off}, err
	}
}
