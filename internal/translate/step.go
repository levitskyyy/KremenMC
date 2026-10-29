package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// xform rewrites one packet payload between adjacent versions and emits the
// result (zero or more packets) through x.
type xform func(x *xctx, r *proto.Reader) error

// step holds the transforms between a version and the next newer one.
// Clientbound transforms are keyed by the newer packet name and emit older
// packets; serverbound ones are keyed by the older name and emit newer ones.
// Packets without a transform keep their payload and are renumbered by name,
// provided their layout fingerprints match or they are listed in same.
type step struct {
	cb, sb   [5]map[string]xform
	same     [5][2]map[string]bool
	dropped  [5][2]map[string]bool
	cbRename [5]map[string]string
	sbRename [5]map[string]string
}

func newStep() *step {
	st := &step{}
	for s := range 5 {
		st.cb[s] = map[string]xform{}
		st.sb[s] = map[string]xform{}
		st.cbRename[s] = map[string]string{}
		st.sbRename[s] = map[string]string{}
		for d := range 2 {
			st.same[s][d] = map[string]bool{}
			st.dropped[s][d] = map[string]bool{}
		}
	}
	return st
}

func (st *step) clientbound(s packets.State, name string, f xform) { st.cb[s][name] = f }
func (st *step) serverbound(s packets.State, name string, f xform) { st.sb[s][name] = f }

// compatible acknowledges packets whose fingerprints differ (mapper value
// lists, schema representation) while their wire layouts are interchangeable.
func (st *step) compatible(s packets.State, d packets.Direction, names ...string) {
	for _, n := range names {
		st.same[s][d][n] = true
	}
}

// drop acknowledges packets that intentionally have no counterpart.
func (st *step) drop(s packets.State, d packets.Direction, names ...string) {
	for _, n := range names {
		st.dropped[s][d][n] = true
	}
}

// renameCB maps a newer clientbound packet name onto an older one.
func (st *step) renameCB(s packets.State, newer, older string) { st.cbRename[s][newer] = older }

// renameSB maps an older serverbound packet name onto a newer one.
func (st *step) renameSB(s packets.State, older, newer string) { st.sbRename[s][older] = newer }
