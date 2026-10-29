package server

import (
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func survival(p *config.ServerProperties) { p.Gamemode = "survival" }

func stackOf(t *testing.T, name string, n int32) item.Stack {
	t.Helper()
	id, ok := item.IDOf(name)
	if !ok {
		t.Fatalf("unknown item %s", name)
	}
	return item.Stack{Item: id, Count: n}
}

func clickRaw(changed map[int16]item.Stack, carried item.Stack) []byte {
	b := proto.AppendVarInt(nil, int32(len(changed)))
	for slot, s := range changed {
		b = append(b, byte(uint16(slot)>>8), byte(slot))
		b = item.AppendHashed(b, s)
	}
	return item.AppendHashed(b, carried)
}

func slotStack(t *testing.T, raw []byte) item.Stack {
	t.Helper()
	s, err := item.DecodeStack(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInventoryClickRoundTrip(t *testing.T) {
	s := startServer(t, survival)
	c := dial(t, s.Addr(), "Clicker")
	c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Clicker")
	p.Inventory().SetItem(0, api.Item("minecraft:stone", 10))
	set := expect(c, func(p *packets.ClientboundContainerSetSlot) bool { return p.Slot == 36 })
	if got := slotStack(t, set.Raw); got.Count != 10 || set.StateID != 2 {
		t.Fatalf("set slot %+v state %d", got, set.StateID)
	}
	stone := stackOf(t, "stone", 10)
	c.send(&packets.ServerboundContainerClick{WindowID: 0, StateID: 2, Slot: 36, MouseButton: 0, Mode: 0,
		Raw: clickRaw(map[int16]item.Stack{36: {}}, stone)})
	c.send(&packets.ServerboundContainerClick{WindowID: 0, StateID: 2, Slot: 9, MouseButton: 1, Mode: 0,
		Raw: clickRaw(map[int16]item.Stack{9: stone.WithCount(1)}, stone.WithCount(9))})
	waitFor(t, func() bool { return p.Inventory().Item(9).Count == 1 })
	if cur := p.Inventory().Cursor(); cur.Count != 9 || p.Inventory().Item(0).Count != 0 {
		t.Fatalf("cursor %+v slot0 %+v", cur, p.Inventory().Item(0))
	}
	// A stale state id forces a full resync.
	c.send(&packets.ServerboundContainerClick{WindowID: 0, StateID: 99, Slot: 10, MouseButton: 0, Mode: 0,
		Raw: clickRaw(map[int16]item.Stack{10: stone.WithCount(9)}, item.Stack{})})
	full := expect(c, func(p *packets.ClientboundContainerSetContent) bool { return p.StateID == 3 })
	r := proto.Reader{B: full.Raw}
	n, _ := r.VarInt()
	items := make([]item.Stack, n)
	for i := range items {
		items[i], _ = item.ReadStack(&r)
	}
	if items[10].Count != 9 || items[9].Count != 1 {
		t.Fatalf("resync slots 9=%v 10=%v", items[9], items[10])
	}
}

func TestCreativeSlotKeepsComponents(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Creator")
	c.joinPlay(s.Addr())
	sword := stackOf(t, "diamond_sword", 1).WithDamage(5)
	name := proto.Plain("Blade")
	sword = sword.Set(item.CustomName, []byte(name.NBT()))
	c.send(&packets.ServerboundSetCreativeModeSlot{Slot: 36, Raw: item.AppendStack(nil, sword)})
	p := mustPlayer(t, s, "Creator")
	waitFor(t, func() bool { return !p.Inventory().Item(0).IsEmpty() })
	got := p.Inventory().Item(0)
	if got.Damage != 5 || got.Name == nil || got.Name.Plain() != "Blade" {
		t.Fatalf("got %+v", got)
	}
	if back := stackFromAPI(got); !item.Matches(back, sword) {
		t.Fatalf("api round trip changed the stack: %x vs %x", item.AppendStack(nil, back), item.AppendStack(nil, sword))
	}
}

func TestEatAppleRestoresFood(t *testing.T) {
	s := startServer(t, survival)
	c := dial(t, s.Addr(), "Eater")
	c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Eater")
	p.SetFood(10)
	p.SetSaturation(0)
	p.Inventory().SetItem(0, api.Item("minecraft:apple", 3))
	c.send(&packets.ServerboundSetCarriedItem{SlotID: 0})
	c.send(&packets.ServerboundUseItem{Hand: 0, Sequence: 1})
	expect(c, func(h *packets.ClientboundSetHealth) bool { return h.Food == 14 })
	if f, sat := p.Food(), p.Saturation(); f != 14 || sat != 2.4000001 && sat != 2.4 {
		t.Fatalf("food %d saturation %v", f, sat)
	}
	waitFor(t, func() bool { return p.Inventory().Item(0).Count == 2 })
}

func TestDropAndEquipmentBroadcast(t *testing.T) {
	s := startServer(t, survival)
	a := dial(t, s.Addr(), "Dropper")
	a.joinPlay(s.Addr())
	b := dial(t, s.Addr(), "Viewer")
	b.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Dropper")
	p.Inventory().SetItem(0, api.Item("minecraft:stone", 5))
	a.send(&packets.ServerboundSetCarriedItem{SlotID: 0})
	stoneID, _ := data.Canonical().ItemByName("stone")
	expect(b, func(e *packets.ClientboundSetEquipment) bool {
		s, err := item.DecodeStack(e.Raw[1:])
		return err == nil && e.Raw[0]&0x7f == 0 && s.Item == int32(stoneID)
	})
	a.send(&packets.ServerboundPlayerAction{Status: actionDrop})
	itemType, _ := data.Canonical().EntityByName("item")
	expect(b, func(e *packets.ClientboundAddEntity) bool { return e.Type == int32(itemType.ID) })
	waitFor(t, func() bool { return p.Inventory().Item(0).Count == 4 })
}

func TestToolLosesDurability(t *testing.T) {
	s := startServer(t, survival)
	c := dial(t, s.Addr(), "Miner")
	pos := c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Miner")
	p.Inventory().SetItem(0, api.Item("minecraft:diamond_pickaxe", 1))
	below := proto.BlockPos{X: int32(pos.X) + 1, Y: int32(pos.Y) - 1, Z: int32(pos.Z)}
	c.send(&packets.ServerboundMovePlayerPos{X: pos.X, Y: pos.Y, Z: pos.Z, Flags: packets.MovementFlagsOnGround})
	c.send(&packets.ServerboundPlayerAction{Status: digStart, Location: below, Face: 1, Sequence: 1})
	time.Sleep(time.Second) // grass with a pickaxe: 18 ticks, finish accepted from 13
	c.send(&packets.ServerboundPlayerAction{Status: digFinish, Location: below, Face: 1, Sequence: 2})
	waitFor(t, func() bool { return p.Inventory().Item(0).Damage == 1 })
}
