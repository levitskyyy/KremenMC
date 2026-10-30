package feature

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// shelfMushroom is the shelf_mushroom tree decorator, fitted on fallen logs
// (kremenmc.probe.feat_tree.Draws, fallen_poplar_tree 15/15): nextFloat <
// probability, then per log in trunk-decorator order and per side across
// the log (axis x: north, south; axis z: east, west) a nextFloat < 0.25 puts
// a mushroom facing that side on air, unless a shelf mushroom already
// touches the log or the target; the age is nextInt(2). Standing trunks
// (poplar trees, whose trunk placer is not implemented) are not fitted.
type shelfMushroom struct{ p float32 }

// shelfSideOrder is the per-log side order for a log lying along axis x or z.
var shelfSideOrder = map[string][2]struct {
	name string
	d    Pos
}{
	"x": {{"north", Pos{0, 0, -1}}, {"south", Pos{0, 0, 1}}},
	"z": {{"east", Pos{1, 0, 0}}, {"west", Pos{-1, 0, 0}}},
}

func (d shelfMushroom) decorate(t *treeRun, r *Random) {
	if r.NextFloat() >= d.p {
		return
	}
	logs := javaHashOrder(t.logs)
	if len(logs) == 0 {
		return
	}
	axis, _ := data.Canonical().Value(t.lv.Block(logs[0]), "axis")
	sides, ok := shelfSideOrder[axis]
	if !ok {
		return
	}
	for _, l := range logs {
		for _, s := range sides {
			if r.NextFloat() >= 0.25 {
				continue
			}
			q := l.Add(s.d)
			if !data.IsAir(t.lv.Block(q)) || shelfNear(t.lv, q) || shelfNear(t.lv, l) {
				continue
			}
			st, _ := data.DefaultState("shelf_mushroom")
			st, _ = data.Canonical().With(st, "facing", s.name)
			st, _ = data.Canonical().With(st, "age", []string{"0", "1"}[r.NextIntn(2)])
			t.setDeco(q, st)
		}
	}
}

func shelfNear(lv *Level, q Pos) bool {
	for _, o := range sixDirs {
		if blockName(lv.Block(q.Add(o))) == "shelf_mushroom" {
			return true
		}
	}
	return false
}

func init() {
	decoratorTypes["shelf_mushroom"] = func(_ *registry, b json.RawMessage) (decorator, error) {
		var p struct {
			P float32 `json:"probability"`
		}
		return shelfMushroom{p.P}, json.Unmarshal(b, &p)
	}
}
