package feature

import "github.com/levitskyyy/KremenMC/internal/data"

// Measured with tools/refprobe kremenmc.probe.feat_misc.Place: tall seagrass
// needs water at its lower half (air above is fine), seagrass and sea
// pickles place on a sturdy floor in water or air alike.
func init() {
	survival["tall_seagrass"] = doublePlant(func(lv *Level, _ data.StateID, p Pos) bool {
		return fluidOf(lv.Block(p)) == "water"
	})
}
