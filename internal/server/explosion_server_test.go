package server

import (
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// TestFlintPrimesTNT lights a TNT block with flint and steel through the
// use-item-on packet and waits for the primed entity and the explosion.
func TestFlintPrimesTNT(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Bomber")
	pos := c.joinPlay(s.Addr())
	w := s.defaultWorld()
	p := mustPlayer(t, s, "Bomber").(*Player)
	id, _ := item.IDOf("minecraft:flint_and_steel")
	p.setHand(api.MainHand, item.Stack{Item: id, Count: 1})

	at := api.BlockPos{X: int(pos.X) + 3, Y: int(pos.Y), Z: int(pos.Z)}
	tnt := data.MustState("minecraft:tnt")
	w.w.SetBlock(at.X, at.Y, at.Z, tnt)
	waitFor(t, func() bool { return w.w.Block(at.X, at.Y, at.Z) == tnt })
	c.send(&packets.ServerboundUseItemOn{
		Location: proto.BlockPos{X: int32(at.X), Y: int32(at.Y), Z: int32(at.Z)}, Direction: 1,
		CursorX: 0.5, CursorY: 1, CursorZ: 0.5, Sequence: 1,
	})
	tt, _ := data.Canonical().EntityByName("tnt")
	add := expect[*packets.ClientboundAddEntity](c, func(a *packets.ClientboundAddEntity) bool { return a.Type == int32(tt.ID) })
	if math.Abs(add.X-float64(at.X)-0.5) > 0.05 || math.Abs(add.Z-float64(at.Z)-0.5) > 0.05 {
		t.Errorf("primed TNT at %v %v %v", add.X, add.Y, add.Z)
	}
	waitFor(t, func() bool { return w.w.Block(at.X, at.Y, at.Z) == data.Air })
	if d := p.Hand(api.MainHand).Damage(); d != 1 && p.GameMode() == api.Survival {
		t.Errorf("flint damage %d, want 1", d)
	}
	ex := expect[*packets.ClientboundExplode](c, nil)
	if ex.Radius != 4 || ex.BlockCount == 0 {
		t.Errorf("explode packet radius %v blocks %d", ex.Radius, ex.BlockCount)
	}
	waitFor(t, func() bool { return data.IsAir(w.w.Block(at.X, at.Y-1, at.Z)) })
}
