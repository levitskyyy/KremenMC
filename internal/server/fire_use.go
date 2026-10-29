package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// igniteBlock handles flint and steel or a fire charge used on a block face
// (TntBlock.useItemOn, then the item's useOn). It reports whether the use
// was handled; refused uses roll the client's prediction back.
func (p *Player) igniteBlock(w *World, clicked api.BlockPos, face api.BlockFace, hand api.Hand, held item.Stack) bool {
	kind, ok := igniterOf(item.NameOf(held.Item))
	if !ok {
		return false
	}
	// Sneaking with anything in hand skips the block's own interaction.
	sneak := p.Sneaking() && (!p.Hand(api.MainHand).IsEmpty() || !p.Hand(api.OffHand).IsEmpty())
	lv := &igniteWorld{w: w, p: p}
	switch igniteOn(lv, kind, clicked, face, sneak, w.levelRandom()) {
	case ignitePass:
		return false
	case igniteFail:
		o := faceOffsets[face]
		p.resync(w, clicked)
		p.resync(w, clicked.Add(o[0], o[1], o[2]))
		p.sendInventory()
		return true
	}
	if p.GameMode() == api.Creative {
		return true
	}
	if kind == igniteFlint {
		p.hurtHeld(hand, 1)
		return true
	}
	p.inv.mu.Lock()
	if cur := p.inv.hand(hand); sameItem(cur, held) {
		cur.Shrink(1)
		p.inv.setHand(hand, cur)
	}
	p.inv.mu.Unlock()
	return true
}

type igniteWorld struct {
	w       *World
	p       *Player
	written map[api.BlockPos]data.StateID // see worldExLevel
}

func (l *igniteWorld) Block(x, y, z int) data.StateID {
	if s, ok := l.written[api.BlockPos{X: x, Y: y, Z: z}]; ok {
		return s
	}
	return l.w.w.Block(x, y, z)
}

func (l *igniteWorld) setBlock(p api.BlockPos, s data.StateID) {
	if l.written == nil {
		l.written = map[api.BlockPos]data.StateID{}
	}
	l.written[p] = s
	l.w.w.SetBlock(p.X, p.Y, p.Z, s)
}
func (l *igniteWorld) primeTNT(p api.BlockPos) bool { return l.w.primeTNT(p, l.p) }

func (l *igniteWorld) playSound(at api.Vec3, name string, pitch float32) {
	l.w.playSoundAt(at, name, soundBlocks, 1, pitch)
}
