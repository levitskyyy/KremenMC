package feature

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// vines places one vine in an empty block, attached to the first full face
// among up, north, south, west, east (measured with
// kremenmc.probe.feat_cave.Place; no random draws).
type vines struct{}

var vineFaces = [...]struct {
	prop string
	d    Pos
}{{"up", Pos{0, 1, 0}}, {"north", Pos{0, 0, -1}}, {"south", Pos{0, 0, 1}}, {"west", Pos{-1, 0, 0}}, {"east", Pos{1, 0, 0}}}

func (vines) Place(c *placeCtx, _ *Random, p Pos) bool {
	if !data.IsAir(c.lv.Block(p)) {
		return false
	}
	vine, _ := data.DefaultState("vine")
	for _, f := range vineFaces {
		if data.Flags(c.lv.Block(p.Add(f.d)))&data.FlagFullCube != 0 {
			st, _ := data.Canonical().With(vine, f.prop, "true")
			c.lv.SetBlock(p, st)
			return true
		}
	}
	return false
}

func init() {
	featureTypes["vines"] = func(*registry, json.RawMessage) (Feature, error) { return vines{}, nil }
}
