package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/item/menu"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const maxChangedSlots = 128

// handleContainerClick is ServerGamePacketListenerImpl.handleContainerClick
// for the inventory menu (the only menu a player can have open so far).
func (p *Player) handleContainerClick(m *packets.ServerboundContainerClick) {
	r := proto.Reader{B: m.Raw}
	n, err := r.VarInt()
	if err != nil || n < 0 || n > maxChangedSlots {
		p.s.log.Debug("bad container click", "player", p.prof.Name)
		return
	}
	changed := make(map[int]item.Hashed, n)
	for range n {
		slot, err := r.I16()
		if err != nil {
			return
		}
		h, err := item.ReadHashed(&r)
		if err != nil {
			return
		}
		changed[int(slot)] = h
	}
	carried, err := item.ReadHashed(&r)
	if err != nil || m.Mode < 0 || m.Mode > menu.PickupAll {
		return
	}
	if p.bridgeClick(m, changed, carried) {
		return
	}
	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.WindowID != 0 {
		p.chestClick(m, changed, carried)
		return
	}
	s.sync.Click(s.m, m.StateID, int(m.Slot), int32(m.MouseButton), m.Mode, changed, carried, menuPlayer{p}, p.GameMode() == api.Spectator, invSender{p})
}

// handleContainerClose returns the carried stack and the crafting grid to
// the inventory (dropping what does not fit), as InventoryMenu.removed.
func (p *Player) handleContainerClose(m *packets.ServerboundContainerClose) {
	if m.WindowID != 0 && p.bridgeClose(m.WindowID) {
		return
	}
	if m.WindowID != 0 {
		p.inv.mu.Lock()
		if c := p.inv.chest; c != nil && c.id == m.WindowID {
			p.closeChestLocked(false)
		}
		p.inv.mu.Unlock()
		return
	}
	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	mp := menuPlayer{p}
	if c := s.m.Carried; !c.IsEmpty() {
		s.m.Carried = item.Stack{}
		if rest := p.placeBack(c); !rest.IsEmpty() {
			p.dropItem(rest, false)
		}
	}
	for i := 1; i < 5; i++ {
		if st := s.m.Items[i]; !st.IsEmpty() {
			s.m.Items[i] = item.Stack{}
			if rest := p.placeBack(st); !rest.IsEmpty() {
				mp.Drop(rest, false)
			}
		}
	}
	s.m.Items[0] = item.Stack{}
}

// placeBack is Inventory.placeItemBackInInventory: fill matching stacks,
// then free slots; returns the remainder. Holds inv.mu.
func (p *Player) placeBack(st item.Stack) item.Stack {
	return p.inv.m.AddToInventory(st, menuPlayer{p})
}

func (p *Player) handleCreativeSlot(m *packets.ServerboundSetCreativeModeSlot) {
	if p.GameMode() != api.Creative {
		return
	}
	st, err := item.DecodeStack(m.Raw)
	if err != nil {
		p.s.log.Debug("bad creative slot", "player", p.prof.Name, "err", err)
		return
	}
	slot := int(m.Slot)
	valid := slot >= 1 && slot <= 45
	validData := st.IsEmpty() || st.Count <= st.MaxStackSize()
	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case valid && validData:
		s.m.Items[slot] = st
		s.sync.SetRemote(s.m, slot, st)
		s.sync.BroadcastChanges(s.m, invSender{p})
	case slot < 0 && validData && !st.IsEmpty():
		p.dropItem(st, true)
	}
}

// handleDrop is ServerPlayer.drop(all) for the Q / ctrl-Q keys.
func (p *Player) handleDrop(all bool) {
	if p.GameMode() == api.Spectator {
		return
	}
	s := p.inv
	s.mu.Lock()
	sel := s.selected()
	cur := s.get(sel)
	var out item.Stack
	if !cur.IsEmpty() {
		n := int32(1)
		if all {
			n = cur.Count
		}
		out = cur.Split(n)
		s.set(sel, cur)
	}
	s.sync.SetRemote(s.m, s.slotOf(sel), cur)
	s.mu.Unlock()
	if p.use.active && p.use.hand == api.MainHand && p.Hand(api.MainHand).IsEmpty() {
		p.stopUsing()
	}
	if !out.IsEmpty() {
		p.dropItem(out, false)
	}
}

// handleSwapOffhand is the F key.
func (p *Player) handleSwapOffhand() {
	if p.GameMode() == api.Spectator {
		return
	}
	s := p.inv
	s.mu.Lock()
	sel := s.selected()
	main, off := s.get(sel), s.get(invOffhand)
	s.set(sel, off)
	s.set(invOffhand, main)
	s.mu.Unlock()
	p.stopUsing()
}

func (p *Player) setSelected(slot int32) {
	if slot < 0 || slot > 8 {
		return
	}
	if p.held.Load() != slot && p.use.active && p.use.hand == api.MainHand {
		p.stopUsing()
	}
	p.held.Store(slot)
}

// tryPickItem is ServerGamePacketListenerImpl.tryPickItem.
func (p *Player) tryPickItem(st item.Stack) {
	if st.IsEmpty() {
		return
	}
	s := p.inv
	s.mu.Lock()
	found := -1
	for i := range 36 {
		if c := s.get(i); !c.IsEmpty() && item.SameItemSameComponents(st, c) {
			found = i
			break
		}
	}
	sel := s.selected()
	switch {
	case found >= 0 && found < 9:
		sel = found
	case found >= 0:
		sel = s.suitableHotbar(sel)
		a, b := s.get(sel), s.get(found)
		s.set(sel, b)
		s.set(found, a)
	case p.GameMode() == api.Creative:
		sel = s.suitableHotbar(sel)
		if cur := s.get(sel); !cur.IsEmpty() {
			for i := range 36 {
				if s.get(i).IsEmpty() {
					s.set(i, cur)
					break
				}
			}
		}
		s.set(sel, st)
	default:
		s.mu.Unlock()
		return
	}
	p.held.Store(int32(sel))
	s.mu.Unlock()
	p.send(&packets.ClientboundSetHeldSlot{Slot: int32(sel)})
	p.syncInventory()
}

func (s *invStore) suitableHotbar(sel int) int {
	for i := range 9 {
		if j := (sel + i) % 9; s.get(j).IsEmpty() {
			return j
		}
	}
	for i := range 9 {
		if j := (sel + i) % 9; !s.get(j).Has(item.Enchantments) || len(s.get(j).Enchantments()) == 0 {
			return j
		}
	}
	return sel
}

func (p *Player) handlePickBlock(m *packets.ServerboundPickItemFromBlock) {
	pos := toAPIPos(m.Position)
	eye := p.EyeLocation().Pos
	if eye.DistanceSq(pos.Center()) > (4.5+1)*(4.5+1)+3 {
		return
	}
	w := p.world.Load()
	b := data.BlockOf(w.w.Block(pos.X, pos.Y, pos.Z))
	if b == nil {
		return
	}
	id, ok := item.IDOf(cloneItemName(b.Name))
	if !ok || id == 0 {
		return
	}
	p.tryPickItem(item.Stack{Item: id, Count: 1})
}

// cloneItemName maps blocks whose item differs from the block name.
func cloneItemName(block string) string {
	switch block {
	case "wall_torch":
		return "torch"
	case "soul_wall_torch":
		return "soul_torch"
	case "redstone_wall_torch":
		return "redstone_torch"
	case "redstone_wire":
		return "redstone"
	case "tripwire":
		return "string"
	case "wheat":
		return "wheat_seeds"
	case "carrots":
		return "carrot"
	case "potatoes":
		return "potato"
	case "beetroots":
		return "beetroot_seeds"
	case "water", "lava", "fire", "soul_fire", "air", "cave_air", "void_air":
		return ""
	}
	return block
}

func (p *Player) handlePickEntity(m *packets.ServerboundPickItemFromEntity) {
	w := p.world.Load()
	for _, mob := range w.mobList() {
		if mob.e.ID != m.EntityID {
			continue
		}
		if id, ok := item.IDOf(mob.typ.Name + "_spawn_egg"); ok {
			p.tryPickItem(item.Stack{Item: id, Count: 1})
		}
		return
	}
}

// Player action statuses handled by the inventory code.
const (
	actionDropAll   = 4
	actionDrop      = 5
	actionRelease   = 6
	actionSwapHands = 7
)

func sameItem(a, b item.Stack) bool { return !a.IsEmpty() && a.Item == b.Item }
