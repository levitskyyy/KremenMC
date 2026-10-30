package server

import (
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// Container open/close feedback: chests and ender chests send block event
// 1 with the viewer count (lid animation) and play their open/close sound
// on the first opener and the last closer; barrels flip their open
// property instead.

const blockEventViewers = 1

var (
	viewersMu sync.Mutex
	viewers   = map[quietKey]int{}
)

// containerViewers adds delta viewers to the container made of parts
// (whose block is st) and sends the resulting events.
func (w *World) containerViewers(parts []api.BlockPos, st data.StateID, delta int) {
	b := data.BlockOf(st)
	if b == nil || len(parts) == 0 {
		return
	}
	viewersMu.Lock()
	k := quietKey{w, parts[0]}
	before := viewers[k]
	n := max(before+delta, 0)
	if n == 0 {
		delete(viewers, k)
	} else {
		viewers[k] = n
	}
	viewersMu.Unlock()
	if _, ok := chestBlocks[b.Name]; n == before || !ok && b.Name != "ender_chest" && !strings.HasSuffix(b.Name, "shulker_box") {
		return
	}
	var mid api.Vec3
	for _, q := range parts {
		c := q.Center()
		mid.X, mid.Y, mid.Z = mid.X+c.X/float64(len(parts)), mid.Y+c.Y/float64(len(parts)), mid.Z+c.Z/float64(len(parts))
	}
	first, last := before == 0 && n > 0, before > 0 && n == 0
	if b.Name == "barrel" {
		if first || last {
			pos := parts[0]
			cur := w.w.Block(pos.X, pos.Y, pos.Z)
			w.w.SetBlock(pos.X, pos.Y, pos.Z, with(cur, "open", boolStr(first)))
			if f := dirByName(prop(cur, "facing")); f >= 0 {
				o := faceOffsets[f]
				mid.X, mid.Y, mid.Z = mid.X+float64(o[0])/2, mid.Y+float64(o[1])/2, mid.Z+float64(o[2])/2
			}
			w.playSoundAt(mid, "block.barrel."+pickStr(first, "open", "close"), 4, 0.5, rand.Float32()*0.1+0.9)
		}
		return
	}
	bid := int32(data.Canonical().BlockID(st))
	for _, q := range parts {
		w.broadcastNear(q.Center(), &packets.ClientboundBlockEvent{Location: protoPos(q), Byte1: blockEventViewers, Byte2: uint8(min(n, 255)), BlockID: bid})
	}
	if first || last {
		w.playSoundAt(mid, containerSound(b.Name, first), 4, 0.5, rand.Float32()*0.1+0.9)
	}
}

func containerSound(name string, open bool) string {
	act := pickStr(open, ".open", ".close")
	switch {
	case name == "ender_chest":
		return "block.ender_chest" + act
	case strings.HasSuffix(name, "shulker_box"):
		return "block.shulker_box" + act
	case strings.HasSuffix(name, "copper_chest"):
		if s := "block.copper_chest" + act; soundID(s) >= 0 {
			return s
		}
	}
	return "block.chest" + act
}
