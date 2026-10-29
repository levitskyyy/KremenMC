package server

import (
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/item/menu"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const (
	invSize       = 41 // 0-8 hotbar, 9-35 main, 36-39 armor (feet..head), 40 off hand
	invOffhand    = 40
	invArmorFeet  = 36
	windowOffhand = 45
)

// invStore owns a player's inventory menu (window 0), its carried stack
// and the client sync state. All access holds mu.
type invStore struct {
	mu   sync.Mutex
	p    *Player
	m    *menu.Menu
	sync menu.Sync
	// chest is the open container window, if any (container_chest.go).
	chest    *openChest
	chestSeq int32
	// equipment last broadcast to viewers, by SetEquipment slot id
	equip [6]item.Stack
	// stateless remote view of other players' equipment is per viewer
}

func newInvStore(p *Player) *invStore {
	return &invStore{p: p, m: menu.NewPlayer()}
}

func (s *invStore) slotOf(inv int) int {
	switch {
	case inv >= 0 && inv < 9:
		return 36 + inv
	case inv >= 9 && inv < 36:
		return inv
	case inv >= 36 && inv < 40:
		return 8 - (inv - 36)
	case inv == invOffhand:
		return windowOffhand
	}
	return -1
}

func (s *invStore) get(inv int) item.Stack {
	if i := s.slotOf(inv); i >= 0 {
		return s.m.Items[i]
	}
	return item.Stack{}
}

func (s *invStore) set(inv int, st item.Stack) {
	if i := s.slotOf(inv); i >= 0 {
		s.m.Items[i] = st.Norm()
	}
}

func (s *invStore) selected() int { return int(s.p.held.Load()) }

func (s *invStore) hand(h api.Hand) item.Stack {
	if h == api.OffHand {
		return s.get(invOffhand)
	}
	return s.get(s.selected())
}

func (s *invStore) setHand(h api.Hand, st item.Stack) {
	if h == api.OffHand {
		s.set(invOffhand, st)
		return
	}
	s.set(s.selected(), st)
}

// Hand returns the stack in a hand.
func (p *Player) Hand(h api.Hand) item.Stack {
	p.inv.mu.Lock()
	defer p.inv.mu.Unlock()
	return p.inv.hand(h)
}

func (p *Player) setHand(h api.Hand, st item.Stack) {
	p.inv.mu.Lock()
	p.inv.setHand(h, st)
	p.inv.mu.Unlock()
}

// menuPlayer adapts a Player for click handling; called with inv.mu held.
type menuPlayer struct{ p *Player }

func (m menuPlayer) InfiniteMaterials() bool { return m.p.GameMode() == api.Creative }
func (m menuPlayer) Creative() bool          { return m.p.GameMode() == api.Creative }
func (m menuPlayer) SelectedSlot() int       { return int(m.p.held.Load()) }
func (m menuPlayer) EntityType() int32       { return playerEntityType() }
func (m menuPlayer) Drop(st item.Stack, throw bool) {
	m.p.dropItem(st, throw)
}

var playerTypeOnce sync.Once
var playerTypeID int32

func playerEntityType() int32 {
	playerTypeOnce.Do(func() {
		t, _ := data.Canonical().EntityByName("player")
		playerTypeID = int32(t.ID)
	})
	return playerTypeID
}

// invSender turns menu sync output into packets.
type invSender struct{ p *Player }

func (s invSender) SetSlot(c, st int32, slot int, it item.Stack) {
	s.p.send(&packets.ClientboundContainerSetSlot{WindowID: c, StateID: st, Slot: int16(slot), Raw: item.AppendStack(nil, it)})
}

func (s invSender) SetCursor(it item.Stack) {
	s.p.send(&packets.ClientboundSetCursorItem{Raw: item.AppendStack(nil, it)})
}

func (s invSender) SetContent(c, st int32, items []item.Stack, carried item.Stack) {
	b := proto.AppendVarInt(nil, int32(len(items)))
	for _, it := range items {
		b = item.AppendStack(b, it)
	}
	b = item.AppendStack(b, carried)
	s.p.send(&packets.ClientboundContainerSetContent{WindowID: c, StateID: st, Raw: b})
}

// sendInventory sends the full window (join, resync).
func (p *Player) sendInventory() {
	p.inv.mu.Lock()
	p.inv.sync.SendAll(p.inv.m, invSender{p})
	p.inv.mu.Unlock()
}

// syncInventory is the per-tick broadcastChanges of the inventory menu.
func (p *Player) syncInventory() {
	p.inv.mu.Lock()
	p.inv.sync.BroadcastChanges(p.inv.m, invSender{p})
	p.inv.mu.Unlock()
}

type contentCapture struct {
	pk *packets.ClientboundContainerSetContent
}

func (c *contentCapture) SetSlot(int32, int32, int, item.Stack) {}
func (c *contentCapture) SetCursor(item.Stack)                  {}
func (c *contentCapture) SetContent(w, st int32, items []item.Stack, carried item.Stack) {
	b := proto.AppendVarInt(nil, int32(len(items)))
	for _, it := range items {
		b = item.AppendStack(b, it)
	}
	c.pk = &packets.ClientboundContainerSetContent{WindowID: w, StateID: st, Raw: item.AppendStack(b, carried)}
}

// inventoryPacket syncs the whole window and returns the content packet.
func (p *Player) inventoryPacket() *packets.ClientboundContainerSetContent {
	var c contentCapture
	p.inv.mu.Lock()
	p.inv.sync.SendAll(p.inv.m, &c)
	p.inv.mu.Unlock()
	return c.pk
}
