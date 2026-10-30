package feature

import (
	"encoding/json"
	"fmt"
)

// steppedColumns is stepped_column_cluster (basalt columns). Fitted from
// kremenmc.probe.feat_nether.Place draw and read logs on flat and pitted
// floors (small and large basalt columns):
//   - The origin must be can_replace with a block below that is neither
//     can_replace nor cannot_place_on.
//   - H = height sample; then column_count positions from
//     randomBetweenClosed over ±min(H, cluster_reach) in x and z (draws x,
//     y (bound 1), z). A position at Manhattan distance d0 > H from the
//     origin is skipped; otherwise a column_reach sample follows and it
//     raises a column of that reach and size k = H - d0.
//   - Column cells: z outer, x inner over ±reach; a cell at Manhattan
//     distance d from the column centre gets k + 1 - d/2 blocks. A
//     can_replace cell walks down at most d blocks to the first block below
//     that is not can_replace, which must not be cannot_place_on; any other
//     cell (not cannot_place_on) walks up at most d-1 blocks to the first
//     can_replace block, failing on cannot_place_on. The blocks then go up
//     from there, over can_replace blocks, passing continue_through ones.
type steppedColumns struct {
	block        StateProvider
	canReplace   Predicate
	cannotOn     blockSet
	through      Predicate
	height       IntProvider
	columnReach  IntProvider
	clusterReach int
	count        int
}

func colHeight(k, d int) int { return k + 1 - d/2 }

func (f *steppedColumns) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	below := p.Add(Pos{0, -1, 0})
	if !f.canReplace.Test(lv, p) || f.canReplace.Test(lv, below) || f.cannotOn.has(lv.Block(below)) {
		return false
	}
	h := f.height.Sample(r)
	b := min(h, f.clusterReach)
	placed := false
	for range f.count {
		dx := int(r.NextIntn(int32(2*b+1))) - b
		r.NextIntn(1)
		dz := int(r.NextIntn(int32(2*b+1))) - b
		d0 := abs(dx) + abs(dz)
		if d0 > h {
			continue
		}
		if f.column(lv, r, p.Add(Pos{dx, 0, dz}), h-d0, f.columnReach.Sample(r)) {
			placed = true
		}
	}
	return placed
}

func (f *steppedColumns) column(lv *Level, r *Random, c Pos, k, reach int) bool {
	placed := false
	for dz := -reach; dz <= reach; dz++ {
		for dx := -reach; dx <= reach; dx++ {
			q := c.Add(Pos{dx, 0, dz})
			d := abs(dx) + abs(dz)
			start, ok := f.base(lv, q, d)
			if !ok {
				continue
			}
			for j := range colHeight(k, d) {
				t := start.Add(Pos{0, j, 0})
				if f.canReplace.Test(lv, t) {
					lv.SetBlock(t, f.block.State(lv, r, t))
					placed = true
				} else if !f.through.Test(lv, t) {
					break
				}
			}
		}
	}
	return placed
}

func (f *steppedColumns) base(lv *Level, q Pos, d int) (Pos, bool) {
	if f.canReplace.Test(lv, q) {
		cur := q
		for range d {
			b := cur.Add(Pos{0, -1, 0})
			if f.canReplace.Test(lv, b) {
				cur = b
				continue
			}
			return cur, !f.cannotOn.has(lv.Block(b))
		}
		return q, false
	}
	if f.cannotOn.has(lv.Block(q)) {
		return q, false
	}
	for i := 1; i < d; i++ {
		t := q.Add(Pos{0, i, 0})
		if f.canReplace.Test(lv, t) {
			return t, true
		}
		if f.cannotOn.has(lv.Block(t)) {
			return q, false
		}
	}
	return q, false
}

func init() {
	featureTypes["stepped_column_cluster"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Block        json.RawMessage `json:"block"`
			CanReplace   json.RawMessage `json:"can_replace"`
			CannotOn     string          `json:"cannot_place_on"`
			Through      json.RawMessage `json:"continue_through"`
			Height       json.RawMessage `json:"height"`
			ColumnReach  json.RawMessage `json:"column_reach"`
			ClusterReach int             `json:"cluster_reach"`
			Count        int             `json:"column_count"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("stepped_column_cluster: %w", err)
		}
		f := &steppedColumns{clusterReach: raw.ClusterReach, count: raw.Count}
		var err error
		if f.block, err = rg.parseStateProvider(raw.Block); err != nil {
			return nil, err
		}
		if f.canReplace, err = rg.parsePredicate(raw.CanReplace); err != nil {
			return nil, err
		}
		if f.cannotOn, err = rg.tag(raw.CannotOn); err != nil {
			return nil, err
		}
		if f.through, err = rg.parsePredicate(raw.Through); err != nil {
			return nil, err
		}
		if f.height, err = parseInt(raw.Height); err != nil {
			return nil, err
		}
		if f.columnReach, err = parseInt(raw.ColumnReach); err != nil {
			return nil, err
		}
		return f, nil
	}
}

// levelAnchor is a vertical anchor resolved against the level a block
// predicate tests: absolute, above_bottom, below_top or
// relative_to_sea_level (26.3).
type levelAnchor struct {
	kind byte // a absolute, b above_bottom, t below_top, s relative_to_sea_level
	v    int
}

func (a levelAnchor) resolve(lv *Level) int {
	switch a.kind {
	case 'b':
		return lv.MinY() + a.v
	case 't':
		return lv.MaxY() - 1 - a.v
	case 's':
		return lv.seaLevel() + a.v
	}
	return a.v
}

func parseLevelAnchor(b json.RawMessage) (levelAnchor, error) {
	var m map[string]int
	if err := json.Unmarshal(b, &m); err != nil || len(m) != 1 {
		return levelAnchor{}, fmt.Errorf("vertical anchor %s", b)
	}
	for k, v := range m {
		switch k {
		case "absolute":
			return levelAnchor{'a', v}, nil
		case "above_bottom":
			return levelAnchor{'b', v}, nil
		case "below_top":
			return levelAnchor{'t', v}, nil
		case "relative_to_sea_level":
			return levelAnchor{'s', v}, nil
		}
	}
	return levelAnchor{}, fmt.Errorf("vertical anchor %s", b)
}

type predAnchorRange struct {
	off    Pos
	lo, hi levelAnchor
}

func (m predAnchorRange) Test(lv *Level, p Pos) bool {
	y := p.Y + m.off.Y
	return y >= m.lo.resolve(lv) && y <= m.hi.resolve(lv)
}

// parseHeightRangePred reads the height_range block predicate: integer
// bounds or vertical anchors.
func parseHeightRangePred(off Pos, lo, hi json.RawMessage) (Predicate, error) {
	if isNumber(lo) && isNumber(hi) {
		var a, b int
		if err := json.Unmarshal(lo, &a); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(hi, &b); err != nil {
			return nil, err
		}
		return predHeightRange{off, a, b}, nil
	}
	a, err := parseLevelAnchor(lo)
	if err != nil {
		return nil, err
	}
	b, err := parseLevelAnchor(hi)
	if err != nil {
		return nil, err
	}
	return predAnchorRange{off, a, b}, nil
}
