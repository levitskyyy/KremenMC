package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// endRing lists the 12 frame offsets around the 3x3 portal centre with the
// facing each frame needs (towards the centre).
var endRing = func() (r [12]struct {
	dx, dz int
	facing string
}) {
	n := 0
	for i := -1; i <= 1; i++ {
		for _, f := range [...]struct {
			dx, dz int
			facing string
		}{{i, -2, "south"}, {i, 2, "north"}, {-2, i, "east"}, {2, i, "west"}} {
			r[n] = f
			n++
		}
	}
	return r
}()

// useEyeOnFrame inserts an eye of ender into an empty end portal frame and
// opens the portal when the ring is complete. Throwing an eye (towards a
// stronghold) is not implemented.
func (p *Player) useEyeOnFrame(w *World, clicked api.BlockPos, hand api.Hand, held item.Stack) bool {
	if item.NameOf(held.Item) != "minecraft:ender_eye" {
		return false
	}
	s := w.w.Block(clicked.X, clicked.Y, clicked.Z)
	if blockID(s) != portalBlocks().endFrame {
		return false
	}
	if v, _ := data.Canonical().Value(s, "eye"); v == "true" {
		return false
	}
	filled, ok := data.With(s, "eye", "true")
	if !ok {
		return false
	}
	w.w.SetBlock(clicked.X, clicked.Y, clicked.Z, filled)
	if p.GameMode() != api.Creative {
		p.inv.mu.Lock()
		if cur := p.inv.hand(hand); sameItem(cur, held) {
			cur.Shrink(1)
			p.inv.setHand(hand, cur)
		}
		p.inv.mu.Unlock()
		p.sendInventory()
	}
	at := api.Vec3{X: float64(clicked.X) + 0.5, Y: float64(clicked.Y) + 0.5, Z: float64(clicked.Z) + 0.5}
	w.playSoundAt(at, "minecraft:block.end_portal_frame.fill", soundBlocks, 1, 1)
	over := map[api.BlockPos]data.StateID{clicked: filled}
	src := overlay{w.w, over}
	if c, ok := findEndRing(src, clicked); ok {
		ep, _ := data.DefaultState("end_portal")
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				w.w.SetBlock(c.X+dx, c.Y, c.Z+dz, ep)
			}
		}
		w.playSoundAt(api.Vec3{X: float64(c.X) + 0.5, Y: float64(c.Y) + 0.5, Z: float64(c.Z) + 0.5}, "minecraft:block.end_portal.spawn", soundBlocks, 1, 1)
	}
	return true
}

type overlay struct {
	base exBlocks
	over map[api.BlockPos]data.StateID
}

func (o overlay) Block(x, y, z int) data.StateID {
	if s, ok := o.over[api.BlockPos{X: x, Y: y, Z: z}]; ok {
		return s
	}
	return o.base.Block(x, y, z)
}

// findEndRing returns the centre of a complete ring of 12 filled, inward
// facing frames that contains the frame at p.
func findEndRing(src exBlocks, p api.BlockPos) (api.BlockPos, bool) {
	frame := portalBlocks().endFrame
	reg := data.Canonical()
next:
	for _, r := range endRing {
		c := api.BlockPos{X: p.X - r.dx, Y: p.Y, Z: p.Z - r.dz}
		for _, f := range endRing {
			s := src.Block(c.X+f.dx, c.Y, c.Z+f.dz)
			if blockID(s) != frame {
				continue next
			}
			if v, _ := reg.Value(s, "eye"); v != "true" {
				continue next
			}
			if v, _ := reg.Value(s, "facing"); v != f.facing {
				continue next
			}
		}
		return c, true
	}
	return api.BlockPos{}, false
}
