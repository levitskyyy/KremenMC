package feature

import (
	"encoding/json"
	"fmt"
)

// cherryTrunk is cherry_trunk_placer, fitted to kremenmc.probe.feat_tree.Draws
// and Writes (80 isolated trees). Draws: the first branch start offset
// (uniform in [min, max]), a second one that differs from it, the branch
// count, a horizontal direction; then per branch its end offset, its
// horizontal length and one float per step after the first.
//
// A branch starts at h+offset-1 and ends at h+endOffset-1. The trunk goes up
// to the only branch start (1 branch), the higher of both (2 branches) or
// h-1 with its own crown (3). A branch below the trunk top first steps one
// block out; then one horizontal step, and every further step goes up when
// the float is below up/(up+horizontal) of the steps left, else outwards.
// The second branch takes the opposite direction.
type cherryTrunk struct {
	trunkHeight
	startMin, startMax  int
	count, endOff, hlen IntProvider
}

func (c cherryTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	t.setDirt(r, p.Add(Pos{0, -1, 0}))
	s1 := c.startMin + int(r.NextIntn(int32(c.startMax-c.startMin+1)))
	s2 := c.startMin + int(r.NextIntn(int32(max(c.startMax-c.startMin, 1))))
	if s2 >= s1 {
		s2++
	}
	n := c.count.Sample(r)
	dir := horizontals[r.NextIntn(4)]
	y1, y2 := h+s1-1, h+s2-1
	top := y1
	switch {
	case n >= 3:
		top = h - 1
	case n == 2:
		top = max(y1, y2)
	}
	for i := 0; i <= top; i++ {
		t.placeLog(r, p.Add(Pos{0, i, 0}))
	}
	var out []attachment
	if n >= 3 {
		out = append(out, attachment{pos: p.Add(Pos{0, top, 0})})
	}
	out = append(out, c.branch(t, r, h, p, y1, top, dir))
	if n >= 2 {
		out = append(out, c.branch(t, r, h, p, y2, top, Pos{-dir.X, 0, -dir.Z}))
	}
	return out
}

func (c cherryTrunk) branch(t *treeRun, r *Random, h int, p Pos, ys, top int, dir Pos) attachment {
	endY := p.Y + h + c.endOff.Sample(r) - 1
	hl := c.hlen.Sample(r)
	axis := "x"
	if dir.X == 0 {
		axis = "z"
	}
	q := p.Add(Pos{0, ys, 0})
	out := func() {
		q = q.Add(dir)
		t.placeAxisLog(r, q, axis)
	}
	if ys < top {
		out()
	}
	out()
	hor, up := hl-1, endY-q.Y
	for hor+up > 0 {
		if r.NextFloat() < float32(up)/float32(hor+up) {
			q.Y++
			up--
			t.placeAxisLog(r, q, "y")
		} else {
			hor--
			out()
		}
	}
	return attachment{pos: q}
}

// placeAxisLog is placeLog with the log turned along axis.
func (t *treeRun) placeAxisLog(r *Random, p Pos, axis string) {
	if !validTreePos(t.lv, p) {
		return
	}
	t.lv.SetBlock(p, withAxis(t.cfg.trunk.State(t.lv, r, p), axis))
	t.logs = append(t.logs, p)
}

// cherryFoliage is cherry_foliage_placer for the data pack's only
// configuration (radius 4, height 5, offset 0), fitted to the same probes.
// Around the attachment, top-down: radius 1, 2, 3 at y+3..y+1, a "wide"
// radius 3 layer at y and radius 2 at y-1. Corners of radius 1 and 2 layers
// and the next-to-corner blocks of radius 3 layers draw the corner hole
// chance; radius 3 corners are always cut. In the wide layer every edge block
// first draws the wide hole chance. After each of the two lowest layers a
// walk around its edge (east side going north, south going east, west going
// south, north going west, 2r blocks each) tries hanging leaves below every
// block of the tree's foliage that is within 6 blocks (Manhattan) of the
// attachment, and an extension one further down under the same limit.
type cherryFoliage struct {
	wideHole, cornerHole, hang, hangExt float32
}

func (cherryFoliage) foliageHeight(*Random, int) int { return 5 }
func (cherryFoliage) foliageRadius(*Random, int) int { return 4 }

func (c cherryFoliage) skip(r *Random, a, b, rad int, wide bool) bool {
	if wide && (a == rad || b == rad) && r.NextFloat() < c.wideHole {
		return true
	}
	if a == rad && b == rad {
		if rad > 2 {
			return true
		}
		return r.NextFloat() < c.cornerHole
	}
	if rad > 2 && (a == rad && b == rad-1 || a == rad-1 && b == rad) {
		return r.NextFloat() < c.cornerHole
	}
	return false
}

func (c cherryFoliage) row(t *treeRun, r *Random, ctr Pos, y, rad int, wide bool) {
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			if !c.skip(r, abs(dx), abs(dz), rad, wide) {
				t.placeLeaf(r, ctr.Add(Pos{dx, y, dz}))
			}
		}
	}
}

func (c cherryFoliage) hanging(t *treeRun, r *Random, ctr Pos, y, rad int) {
	set := make(map[Pos]bool, len(t.leaves))
	for _, l := range t.leaves {
		set[l] = true
	}
	near := func(q Pos) bool { return abs(q.X-ctr.X)+abs(q.Y-ctr.Y)+abs(q.Z-ctr.Z) <= 6 }
	try := func(q Pos) {
		if !set[q] {
			return
		}
		b := q.Add(Pos{0, -1, 0})
		if !near(b) || r.NextFloat() >= c.hang || !t.placeLeaf(r, b) {
			return
		}
		set[b] = true
		b2 := b.Add(Pos{0, -1, 0})
		if near(b2) && r.NextFloat() < c.hangExt && t.placeLeaf(r, b2) {
			set[b2] = true
		}
	}
	for i := rad; i > -rad; i-- {
		try(ctr.Add(Pos{rad, y, i}))
	}
	for i := -rad; i < rad; i++ {
		try(ctr.Add(Pos{i, y, rad}))
	}
	for i := -rad; i < rad; i++ {
		try(ctr.Add(Pos{-rad, y, i}))
	}
	for i := rad; i > -rad; i-- {
		try(ctr.Add(Pos{i, y, -rad}))
	}
}

func (c cherryFoliage) place(t *treeRun, r *Random, _ int, a attachment, _, _ int) {
	ctr := a.pos
	c.row(t, r, ctr, 3, 1, false)
	c.row(t, r, ctr, 2, 2, false)
	c.row(t, r, ctr, 1, 3, false)
	c.row(t, r, ctr, 0, 3, true)
	c.hanging(t, r, ctr, 0, 3)
	c.row(t, r, ctr, -1, 2, false)
	c.hanging(t, r, ctr, -1, 2)
}

func init() {
	trunkTypes["cherry_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var p struct {
			trunkHeight
			Count  json.RawMessage `json:"branch_count"`
			End    json.RawMessage `json:"branch_end_offset_from_top"`
			Len    json.RawMessage `json:"branch_horizontal_length"`
			Starts struct {
				Min int `json:"min_inclusive"`
				Max int `json:"max_inclusive"`
			} `json:"branch_start_offset_from_top"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		c := cherryTrunk{trunkHeight: p.trunkHeight, startMin: p.Starts.Min, startMax: p.Starts.Max}
		var err error
		if c.count, err = parseInt(p.Count); err != nil {
			return nil, err
		}
		if c.endOff, err = parseInt(p.End); err != nil {
			return nil, err
		}
		if c.hlen, err = parseInt(p.Len); err != nil {
			return nil, err
		}
		return c, nil
	}
	foliageTypes["cherry_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		var p struct {
			Radius, Offset, Height json.RawMessage
			Wide                   float32 `json:"wide_bottom_layer_hole_chance"`
			Corner                 float32 `json:"corner_hole_chance"`
			Hang                   float32 `json:"hanging_leaves_chance"`
			Ext                    float32 `json:"hanging_leaves_extension_chance"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		// Only the data pack's configuration was fitted.
		if string(p.Radius) != "4" || string(p.Offset) != "0" || string(p.Height) != "5" {
			return nil, unsupported("cherry_foliage_placer", fmt.Sprintf("radius %s offset %s height %s", p.Radius, p.Offset, p.Height))
		}
		return cherryFoliage{p.Wide, p.Corner, p.Hang, p.Ext}, nil
	}
}
