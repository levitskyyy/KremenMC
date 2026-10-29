package server

import (
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// TestSurvivalBreakTwoPlayers breaks a block by hand in survival with real
// timing: the watcher sees crack stages, the break effect and the dropped
// item, the digger picks the item up, and an instant finish is refused.
func TestSurvivalBreakTwoPlayers(t *testing.T) {
	s := startServer(t, func(p *config.ServerProperties) { p.Gamemode = "survival" })
	a := dial(t, s.Addr(), "Digger")
	pos := a.joinPlay(s.Addr())
	b := dial(t, s.Addr(), "Watcher")
	b.joinPlay(s.Addr())
	a.send(&packets.ServerboundMovePlayerPos{X: pos.X, Y: pos.Y, Z: pos.Z, Flags: packets.MovementFlagsOnGround})

	w := s.defaultWorld()
	floor := int32(pos.Y) - 1
	early := proto.BlockPos{X: int32(pos.X) - 1, Y: floor, Z: int32(pos.Z)}
	target := proto.BlockPos{X: int32(pos.X) + 1, Y: floor, Z: int32(pos.Z)}
	before := w.w.Block(int(early.X), int(early.Y), int(early.Z))
	if data.IsAir(before) {
		t.Fatal("no floor block")
	}

	a.send(&packets.ServerboundPlayerAction{Status: digStart, Location: target, Face: 1, Sequence: 4})
	expect[*packets.ClientboundBlockDestruction](b, func(p *packets.ClientboundBlockDestruction) bool {
		return p.EntityID == a.eid && p.Location == target && p.DestroyStage == 0
	})
	expect[*packets.ClientboundBlockDestruction](b, func(p *packets.ClientboundBlockDestruction) bool {
		return p.EntityID == a.eid && p.DestroyStage >= 3
	})
	time.Sleep(1200 * time.Millisecond)
	a.send(&packets.ServerboundPlayerAction{Status: digFinish, Location: target, Face: 1, Sequence: 5})
	expect[*packets.ClientboundLevelEvent](b, func(p *packets.ClientboundLevelEvent) bool { return p.EffectID == levelBreak && p.Location == target })
	expect[*packets.ClientboundBlockUpdate](b, func(p *packets.ClientboundBlockUpdate) bool { return p.Location == target && p.Type == 0 })
	expect[*packets.ClientboundBlockChangedAck](a, func(p *packets.ClientboundBlockChangedAck) bool { return p.SequenceID == 5 })
	it, _ := data.Canonical().EntityByName("item")
	spawn := expect[*packets.ClientboundAddEntity](b, func(p *packets.ClientboundAddEntity) bool { return p.Type == int32(it.ID) })
	expect[*packets.ClientboundSetEntityData](b, func(p *packets.ClientboundSetEntityData) bool { return p.EntityID == spawn.EntityID })
	if spawn.Velocity.Y <= 0 {
		t.Fatalf("item spawned without upward toss: %+v", spawn.Velocity)
	}

	a.send(&packets.ServerboundMovePlayerPos{X: float64(target.X) + 0.5, Y: float64(floor), Z: float64(target.Z) + 0.5, Flags: packets.MovementFlagsOnGround})
	take := expect[*packets.ClientboundTakeItemEntity](b, func(p *packets.ClientboundTakeItemEntity) bool { return p.CollectedEntityID == spawn.EntityID })
	if take.CollectorEntityID != a.eid || take.PickupItemCount != 1 {
		t.Fatalf("take %+v", take)
	}
	expect[*packets.ClientboundRemoveEntities](b, nil)
	a.send(&packets.ServerboundPlayerAction{Status: digStart, Location: early, Face: 1, Sequence: 11})
	a.send(&packets.ServerboundPlayerAction{Status: digFinish, Location: early, Face: 1, Sequence: 12})
	expect[*packets.ClientboundBlockChangedAck](a, func(p *packets.ClientboundBlockChangedAck) bool { return p.SequenceID == 12 })
	if st := w.w.Block(int(early.X), int(early.Y), int(early.Z)); st != before {
		t.Fatalf("instant finish broke %s", data.StateString(before))
	}
	a.send(&packets.ServerboundPlayerAction{Status: digAbort, Location: early, Face: 1, Sequence: 13})

	p := mustPlayer(t, s, "Digger").(*Player)
	found := false
	p.inv.mu.Lock()
	for i := range 36 {
		if st := p.inv.get(i); !st.IsEmpty() {
			found = true
		}
	}
	p.inv.mu.Unlock()
	if !found {
		t.Fatal("picked up item not in inventory")
	}
}

// TestPlayerActionCodes pins ServerboundPlayerActionPacket.Action ordinals
// of 26.3 (javap -p declaration order).
func TestPlayerActionCodes(t *testing.T) {
	for i, c := range []int32{digStart, digChangeDirection, digAbort, digFinish, actionDropAll, actionDrop, actionRelease, actionSwapHands} {
		if c != int32(i) {
			t.Errorf("action %d has code %d", i, c)
		}
	}
}
