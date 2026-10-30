package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/item/menu"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Inventories of Java plugins (protocol in internal/bridge/inv.go). Custom
// inventories live here; Java proxies hold their id. Never call into Java
// while holding inv.mu or a customInv lock: Java notifies are applied on the
// IPC reader goroutine and take those locks.

type customInv struct {
	id    int64
	kind  string // Bukkit InventoryType name
	title string // component json
	mu    sync.Mutex
	items []item.Stack
	view  map[*Player]struct{}
}

type bridgeInvs struct {
	mu    sync.Mutex
	next  int64
	byID  map[int64]*customInv
	ender map[api.UUID]*customInv
}

var invState sync.Map // *Server -> *bridgeInvs

func (s *Server) bridgeInvs() *bridgeInvs {
	if v, ok := invState.Load(s); ok {
		return v.(*bridgeInvs)
	}
	v, _ := invState.LoadOrStore(s, &bridgeInvs{byID: map[int64]*customInv{}, ender: map[api.UUID]*customInv{}})
	return v.(*bridgeInvs)
}

var menuIDs = func() map[string]int32 {
	out := map[string]int32{}
	if p, err := wgdata.Load(); err == nil {
		l, _ := p.Builtin("menu")
		for i, id := range l {
			out[id] = int32(i)
		}
	}
	return out
}()

// menuType picks the window type for a Bukkit inventory type and size.
func menuType(kind string, size int) (string, int) {
	switch kind {
	case "HOPPER":
		return "minecraft:hopper", 5
	case "DISPENSER", "DROPPER":
		return "minecraft:generic_3x3", 9
	case "SHULKER_BOX":
		return "minecraft:shulker_box", 27
	}
	rows := min(max((size+8)/9, 1), 6)
	return fmt.Sprintf("minecraft:generic_9x%d", rows), rows * 9
}

func newCustomMenu(n int) *menu.Menu {
	m := &menu.Menu{Items: make([]item.Stack, n+36), Slots: make([]menu.Slot, n+36), Changed: make([]bool, n+36), Container: n}
	for i := range n {
		m.Slots[i] = menu.Slot{Inv: -1}
	}
	for i := range 27 {
		m.Slots[n+i] = menu.Slot{Inv: 9 + i}
	}
	for i := range 9 {
		m.Slots[n+27+i] = menu.Slot{Inv: i}
	}
	return m
}

func (ci *customInv) snapshot() []item.Stack {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return append([]item.Stack(nil), ci.items...)
}

func (ci *customInv) viewers() []*Player {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	out := make([]*Player, 0, len(ci.view))
	for p := range ci.view {
		out = append(out, p)
	}
	return out
}

// push refreshes the open windows showing ci, except skip.
func (ci *customInv) push(skip *Player) {
	items := ci.snapshot()
	for _, p := range ci.viewers() {
		if p == skip {
			continue
		}
		p.inv.mu.Lock()
		if oc := p.inv.chest; oc != nil && oc.custom == ci {
			copy(oc.m.Items[:oc.m.Container], items)
			oc.sync.BroadcastChanges(oc.m, invSender{p})
		}
		p.inv.mu.Unlock()
	}
}

func (h BridgeHost) invRef(v any) (*customInv, *Player) {
	switch r := v.(type) {
	case bridge.UUID:
		p, ok := h.s.Player(api.UUID(r))
		if !ok {
			return nil, nil
		}
		return nil, p.(*Player)
	case int64:
		st := h.s.bridgeInvs()
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.byID[r], nil
	case string:
		id, ok := strings.CutPrefix(r, "ender:")
		if !ok {
			return nil, nil
		}
		u, ok := parseUUID(id)
		if !ok {
			return nil, nil
		}
		st := h.s.bridgeInvs()
		st.mu.Lock()
		defer st.mu.Unlock()
		ci := st.ender[u]
		if ci == nil {
			ci = &customInv{id: -1, kind: "ENDER_CHEST", title: `{"translate":"container.enderchest"}`, items: make([]item.Stack, 27), view: map[*Player]struct{}{}}
			st.ender[u] = ci
		}
		return ci, nil
	}
	return nil, nil
}

func parseUUID(s string) (api.UUID, bool) {
	var u api.UUID
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return u, false
	}
	for i := range 16 {
		var b byte
		for _, c := range s[i*2 : i*2+2] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				b |= byte(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				b |= byte(c - 'A' + 10)
			default:
				return u, false
			}
		}
		u[i] = b
	}
	return u, true
}

// InvCall serves the inventory calls from Java.
func (h BridgeHost) InvCall(op uint16, a []any) ([]any, error) {
	switch op {
	case bridge.OpInvContents:
		ci, p := h.invRef(arg(a, 0))
		switch {
		case p != nil:
			s := p.inv
			s.mu.Lock()
			items := make([]item.Stack, invSize)
			for i := range items {
				items[i] = s.get(i)
			}
			cur := s.m.Carried
			if s.chest != nil {
				cur = s.chest.m.Carried
			}
			s.mu.Unlock()
			return []any{itemsToWire(items), int64(p.held.Load()), itemToWire(cur)}, nil
		case ci != nil:
			return []any{itemsToWire(ci.snapshot()), int64(0), nil}, nil
		}
		return []any{nil}, nil
	case bridge.OpItemInfo:
		id, ok := item.IDOf(bridge.ArgString(a, 0))
		st := item.Stack{Item: id, Count: 1}
		return []any{map[string]any{"exists": ok, "maxStack": int64(st.MaxStackSize()), "maxDamage": int64(st.MaxDamage())}}, nil
	case bridge.OpInvCreate:
		kind := bridge.ArgString(a, 0)
		_, n := menuType(kind, int(bridge.ArgInt(a, 1)))
		st := h.s.bridgeInvs()
		st.mu.Lock()
		st.next++
		ci := &customInv{id: st.next, kind: kind, title: bridge.ArgString(a, 2), items: make([]item.Stack, n), view: map[*Player]struct{}{}}
		st.byID[ci.id] = ci
		st.mu.Unlock()
		return []any{ci.id}, nil
	case bridge.OpInvView:
		p := h.playerArg(a, 0)
		if p == nil {
			return []any{nil}, nil
		}
		p.inv.mu.Lock()
		oc := p.inv.chest
		p.inv.mu.Unlock()
		if oc == nil {
			return []any{nil}, nil
		}
		return []any{viewFields(oc)}, nil
	case bridge.OpInvAdd:
		ci, p := h.invRef(arg(a, 0))
		var in []item.Stack
		for _, x := range bridge.ArgList(a, 1) {
			in = append(in, itemFromWire(x))
		}
		var left []item.Stack
		switch {
		case p != nil:
			s := p.inv
			s.mu.Lock()
			for _, st := range in {
				if st.IsEmpty() {
					continue
				}
				if rest := s.m.AddToInventory(st, menuPlayer{p}); !rest.IsEmpty() {
					left = append(left, rest)
				}
			}
			s.mu.Unlock()
		case ci != nil:
			ci.mu.Lock()
			for _, st := range in {
				if rest := addTo(ci.items, st); !rest.IsEmpty() {
					left = append(left, rest)
				}
			}
			ci.mu.Unlock()
			ci.push(nil)
		}
		return []any{itemsToWire(left)}, nil
	}
	return nil, fmt.Errorf("unknown inventory op %d", op)
}

func addTo(items []item.Stack, st item.Stack) item.Stack {
	for i := range items {
		if st.IsEmpty() {
			return st
		}
		cur := items[i]
		if !cur.IsEmpty() && item.SameItemSameComponents(cur, st) {
			n := min(cur.MaxStackSize()-cur.Count, st.Count)
			if n > 0 {
				items[i].Grow(n)
				st.Shrink(n)
			}
		}
	}
	for i := range items {
		if st.IsEmpty() {
			return st
		}
		if items[i].IsEmpty() {
			n := min(st.Count, st.MaxStackSize())
			items[i] = st.WithCount(n)
			st.Shrink(n)
		}
	}
	return st.Norm()
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

func (h BridgeHost) playerArg(a []any, i int) *Player {
	u, ok := bridge.ArgUUID(a, i)
	if !ok {
		return nil
	}
	p, ok := h.s.Player(api.UUID(u))
	if !ok {
		return nil
	}
	return p.(*Player)
}

// InvNotify applies inventory mutations from Java.
func (h BridgeHost) InvNotify(op uint16, a []any) {
	switch op {
	case bridge.OpInvSet, bridge.OpInvSetAll:
		ci, p := h.invRef(arg(a, 0))
		set := map[int]item.Stack{}
		if op == bridge.OpInvSet {
			set[int(bridge.ArgInt(a, 1))] = itemFromWire(arg(a, 2))
		} else {
			for i, x := range bridge.ArgList(a, 1) {
				set[i] = itemFromWire(x)
			}
		}
		switch {
		case p != nil:
			s := p.inv
			s.mu.Lock()
			for slot, st := range set {
				if slot == -1 {
					if s.chest != nil {
						s.chest.m.Carried = st
					} else {
						s.m.Carried = st
					}
					continue
				}
				s.set(slot, st)
				if oc := s.chest; oc != nil && oc.custom != nil {
					for i := oc.m.Container; i < len(oc.m.Items); i++ {
						if oc.m.Slots[i].Inv == slot {
							oc.m.Items[i] = st.Norm()
						}
					}
				}
			}
			if oc := s.chest; oc != nil && oc.custom != nil {
				oc.sync.BroadcastChanges(oc.m, invSender{p})
			}
			s.sync.BroadcastChanges(s.m, invSender{p})
			s.mu.Unlock()
		case ci != nil:
			ci.mu.Lock()
			for slot, st := range set {
				if slot >= 0 && slot < len(ci.items) {
					ci.items[slot] = st.Norm()
				}
			}
			ci.mu.Unlock()
			ci.push(nil)
		}
	case bridge.OpInvFree:
		st := h.s.bridgeInvs()
		st.mu.Lock()
		delete(st.byID, bridge.ArgInt(a, 0))
		st.mu.Unlock()
	case bridge.OpInvOpen:
		p := h.playerArg(a, 0)
		ci, other := h.invRef(arg(a, 1))
		if p == nil {
			return
		}
		if ci == nil && other == p {
			p.closeWindow()
			return
		}
		if ci != nil {
			p.openCustom(ci)
		}
	case bridge.OpInvClose:
		if p := h.playerArg(a, 0); p != nil {
			p.closeWindow()
		}
	case bridge.OpInvHeld:
		if p := h.playerArg(a, 0); p != nil {
			playerInv{p}.SetHeldSlot(int(bridge.ArgInt(a, 1)))
		}
	}
}

func viewFields(oc *openChest) map[string]any {
	if ci := oc.custom; ci != nil {
		var ref any = ci.id
		if ci.id < 0 {
			ref = nil
		}
		return map[string]any{"ref": ref, "type": ci.kind, "size": int64(oc.m.Container), "title": ci.title}
	}
	return map[string]any{"ref": nil, "type": "CHEST", "size": int64(oc.m.Container), "title": `{"translate":"container.chest"}`}
}

func (p *Player) openCustom(ci *customInv) {
	kind, n := menuType(ci.kind, len(ci.items))
	mid, ok := menuIDs[kind]
	if !ok {
		return
	}
	items := ci.snapshot()
	s := p.inv
	s.mu.Lock()
	if s.chest != nil {
		p.closeChestLocked(false)
	}
	s.chestSeq = s.chestSeq%100 + 1
	oc := &openChest{id: s.chestSeq, m: newCustomMenu(n), custom: ci}
	oc.sync.Container = oc.id
	copy(oc.m.Items[:n], items)
	p.copyInvToCustom(oc)
	s.chest = oc
	var title proto.Text
	if err := title.UnmarshalJSON([]byte(ci.title)); err != nil || ci.title == "" {
		title = proto.Translatable("container.chest")
	}
	p.send(&packets.ClientboundOpenScreen{WindowID: oc.id, InventoryType: mid, WindowTitle: title})
	oc.sync.SendAll(oc.m, invSender{p})
	s.mu.Unlock()
	ci.mu.Lock()
	ci.view[p] = struct{}{}
	ci.mu.Unlock()
}

// closeWindow closes the open container from the server side.
func (p *Player) closeWindow() {
	s := p.inv
	s.mu.Lock()
	if s.chest != nil {
		p.closeChestLocked(true)
		s.sync.SendAll(s.m, invSender{p})
	}
	s.mu.Unlock()
}

func (p *Player) copyInvToCustom(oc *openChest) {
	pm := p.inv.m
	for i := oc.m.Container; i < len(oc.m.Items); i++ {
		oc.m.Items[i] = pm.Items[p.inv.slotOf(oc.m.Slots[i].Inv)]
	}
}

func (p *Player) copyCustomToInv(oc *openChest) {
	pm := p.inv.m
	for i := oc.m.Container; i < len(oc.m.Items); i++ {
		pm.Items[p.inv.slotOf(oc.m.Slots[i].Inv)] = oc.m.Items[i]
	}
}

// clickTypeName is Bukkit's ClickType for a container click.
func clickTypeName(mode int32, button int32, slot int) string {
	switch mode {
	case menu.Pickup:
		if button == 1 {
			return "RIGHT"
		}
		return "LEFT"
	case menu.QuickMove:
		if button == 1 {
			return "SHIFT_RIGHT"
		}
		return "SHIFT_LEFT"
	case menu.Swap:
		if button == 40 {
			return "SWAP_OFFHAND"
		}
		return "NUMBER_KEY"
	case menu.Clone:
		return "MIDDLE"
	case menu.Throw:
		if slot == menu.Outside {
			if button == 1 {
				return "WINDOW_BORDER_RIGHT"
			}
			return "WINDOW_BORDER_LEFT"
		}
		if button == 1 {
			return "CONTROL_DROP"
		}
		return "DROP"
	case menu.PickupAll:
		return "DOUBLE_CLICK"
	}
	return "UNKNOWN"
}

// bridgeClick runs a container click through Java plugins. It returns true
// when the click was fully handled here (a custom window, or a cancelled
// click); false lets the normal handler apply it.
func (p *Player) bridgeClick(m *packets.ServerboundContainerClick, changed map[int]item.Hashed, carried item.Hashed) bool {
	b := p.s.javaBridge()
	s := p.inv
	s.mu.Lock()
	oc := s.chest
	custom := m.WindowID != 0 && oc != nil && oc.id == m.WindowID && oc.custom != nil
	click := b != nil && b.HasListeners("InventoryClick")
	drag := b != nil && m.Mode == menu.QuickCraft && m.MouseButton&3 == 2 && b.HasListeners("InventoryDrag")
	if !custom && !click && !drag {
		s.mu.Unlock()
		return false
	}
	var mm *menu.Menu
	var y *menu.Sync
	f := map[string]any{"player": bridge.UUID(p.prof.UUID), "slot": int64(m.Slot), "button": int64(m.MouseButton), "mode": int64(m.Mode)}
	if m.WindowID == 0 {
		mm, y = s.m, &s.sync
		f["type"], f["size"], f["title"] = "CRAFTING", int64(5), `{"translate":"container.crafting"}`
	} else if oc != nil && oc.id == m.WindowID {
		if custom {
			copy(oc.m.Items[:oc.m.Container], oc.custom.snapshot())
			p.copyInvToCustom(oc)
		}
		mm, y = oc.m, &oc.sync
		for k, v := range viewFields(oc) {
			f[k] = v
		}
	} else {
		s.mu.Unlock()
		return false
	}
	slot := int(m.Slot)
	if drag {
		before := append([]item.Stack(nil), mm.Items...)
		oldCursor := mm.Carried
		y.Click(mm, y.StateID(), slot, int32(m.MouseButton), m.Mode, nil, item.Hashed{}, menuPlayer{p}, false, discardSender{})
		after := append([]item.Stack(nil), mm.Items...)
		newCursor := mm.Carried
		copy(mm.Items, before)
		mm.Carried = oldCursor
		var slots, items []any
		for i := range after {
			if !item.Matches(before[i], after[i]) {
				slots = append(slots, int64(i))
				items = append(items, itemToWire(after[i]))
			}
		}
		f["slots"], f["items"], f["cursor"], f["newCursor"] = orEmpty(slots), orEmpty(items), itemToWire(oldCursor), itemToWire(newCursor)
		f["right"] = m.MouseButton == 6
		s.mu.Unlock()
		cancelled := false
		if len(slots) > 0 && b != nil {
			cancelled, _ = b.InvEvent(context.Background(), "InventoryDrag", f)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.chest != oc && m.WindowID != 0 {
			return true
		}
		if custom {
			copy(mm.Items[:mm.Container], oc.custom.snapshot())
			p.copyInvToCustom(oc)
		}
		if !cancelled {
			for i := range after {
				if !item.Matches(before[i], after[i]) {
					mm.Items[i] = after[i]
				}
			}
			mm.Carried = newCursor
		}
		y.SendAll(mm, invSender{p})
		p.finishClick(oc, mm, custom)
		return true
	}
	if m.Mode == menu.QuickCraft || !click {
		s.mu.Unlock()
		if !custom {
			return false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.chest == oc {
			y.Click(mm, m.StateID, slot, int32(m.MouseButton), m.Mode, changed, carried, menuPlayer{p}, p.GameMode() == api.Spectator, invSender{p})
			p.finishClick(oc, mm, custom)
		}
		return true
	}
	var cur item.Stack
	if slot >= 0 && slot < len(mm.Items) {
		cur = mm.Items[slot]
	}
	f["click"] = clickTypeName(m.Mode, int32(m.MouseButton), slot)
	f["current"], f["cursor"] = itemToWire(cur), itemToWire(mm.Carried)
	if m.Mode == menu.Swap && m.MouseButton >= 0 && m.MouseButton < 9 {
		f["hotbar"] = int64(m.MouseButton)
		f["hotbarItem"] = itemToWire(s.get(int(m.MouseButton)))
	}
	s.mu.Unlock()
	cancelled, _ := b.InvEvent(context.Background(), "InventoryClick", f)
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.WindowID != 0 && s.chest != oc {
		// a plugin closed or replaced the window
		s.sync.SendAll(s.m, invSender{p})
		return true
	}
	if custom {
		copy(mm.Items[:mm.Container], oc.custom.snapshot())
		p.copyInvToCustom(oc)
	}
	if cancelled {
		y.SendAll(mm, invSender{p})
		if m.WindowID != 0 {
			s.sync.SendAll(s.m, invSender{p})
		}
		return true
	}
	if !custom {
		if m.WindowID != 0 {
			p.chestClick(m, changed, carried)
		} else {
			y.Click(mm, m.StateID, slot, int32(m.MouseButton), m.Mode, changed, carried, menuPlayer{p}, p.GameMode() == api.Spectator, invSender{p})
		}
		return true
	}
	y.Click(mm, m.StateID, slot, int32(m.MouseButton), m.Mode, changed, carried, menuPlayer{p}, p.GameMode() == api.Spectator, invSender{p})
	p.finishClick(oc, mm, custom)
	return true
}

// finishClick writes a custom window back to its inventory and the
// player's. Holds inv.mu.
func (p *Player) finishClick(oc *openChest, mm *menu.Menu, custom bool) {
	if !custom {
		return
	}
	ci := oc.custom
	ci.mu.Lock()
	copy(ci.items, mm.Items[:mm.Container])
	ci.mu.Unlock()
	p.copyCustomToInv(oc)
	go ci.push(p)
}

type discardSender struct{}

func (discardSender) SetSlot(int32, int32, int, item.Stack)             {}
func (discardSender) SetCursor(item.Stack)                              {}
func (discardSender) SetContent(int32, int32, []item.Stack, item.Stack) {}

// bridgeClose handles the client closing a custom window and tells Java.
func (p *Player) bridgeClose(window int32) bool {
	s := p.inv
	s.mu.Lock()
	oc := s.chest
	if oc == nil || oc.id != window || oc.custom == nil {
		s.mu.Unlock()
		return false
	}
	f := viewFields(oc)
	p.closeChestLocked(false)
	s.mu.Unlock()
	if b := p.s.javaBridge(); b != nil {
		f["player"], f["reason"] = bridge.UUID(p.prof.UUID), "PLAYER"
		b.InvEvent(context.Background(), "InventoryClose", f)
	}
	return true
}

// leaveCustom drops p from the viewers of a closed custom window.
func (oc *openChest) leaveCustom(p *Player) {
	if ci := oc.custom; ci != nil {
		ci.mu.Lock()
		delete(ci.view, p)
		ci.mu.Unlock()
	}
}
