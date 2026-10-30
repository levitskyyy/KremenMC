package feature

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// mushroomSurvival is red and brown mushroom canSurvive. Measured with
// kremenmc.probe.feat_nether.Dark (canSurvive through a reader forcing every
// brightness query; testdata/mushroom_dark.txt): the
// overrides_mushroom_light_requirement tag always supports them; at
// brightness below 13 so does any full opaque cube, soul sand and mud. In
// generation the light is not computed yet: dimensions without sky light
// (the nether) read 0 and take the dark rule; the overworld keeps the tag
// rule.
func mushroomSurvival(tag survivalRule) survivalRule {
	return func(lv *Level, st data.StateID, p Pos) bool {
		if tag(lv, st, p) {
			return true
		}
		switch lv.rg.Dim() {
		case world.Nether, world.End:
			return darkMushroomFloor(lv.Block(p.Add(Pos{0, -1, 0})))
		}
		return false
	}
}

var soulSandBlock, mudBlock = func() (data.BlockID, data.BlockID) {
	s, _ := data.Canonical().BlockByName("soul_sand")
	m, _ := data.Canonical().BlockByName("mud")
	return s.ID, m.ID
}()

func darkMushroomFloor(s data.StateID) bool {
	f := data.Flags(s)
	if f&data.FlagFullCube != 0 && f&data.FlagTransparent == 0 {
		return true
	}
	b := data.Canonical().BlockID(s)
	return b == soulSandBlock || b == mudBlock
}
