// Package menu implements vanilla container menu click semantics
// (AbstractContainerMenu) over item stacks, starting with the player
// inventory menu.
package menu

import (
	"github.com/levitskyyy/KremenMC/internal/item"
)

// Click types (ContainerInput ids).
const (
	Pickup int32 = iota
	QuickMove
	Swap
	Clone
	Throw
	QuickCraft
	PickupAll
)

// Outside is the slot index for clicks outside the window.
const Outside = -999

type Kind uint8

const (
	KindNormal Kind = iota
	KindResult
	KindArmor
)

type Slot struct {
	Kind  Kind
	Armor int32 // equipment slot of an armor slot
	Inv   int   // backing player inventory index, -1 if none
}

// Player is what click handling needs from the clicking player.
type Player interface {
	// InfiniteMaterials is true in creative mode.
	InfiniteMaterials() bool
	Creative() bool
	EntityType() int32
	SelectedSlot() int
	// Drop spawns s as an item entity (throw: thrown forward with spread).
	Drop(s item.Stack, throw bool)
}

// Menu holds the slots of an open container menu, the carried stack and
// the quick-craft (drag) state.
type Menu struct {
	Items   []item.Stack
	Slots   []Slot
	Carried item.Stack

	// Craft computes the result for the crafting grid, if any.
	Craft func(m *Menu)

	qcStatus int32
	qcType   int32
	qcSlots  []int

	// Changed records slot indices written during a click.
	Changed []bool

	// Container is the number of leading container slots of a chest-like
	// menu (0 for the player inventory menu).
	Container int
}

const containerMax = 99

// NewPlayer builds the 46-slot player inventory menu: 0 craft result, 1-4
// craft grid, 5-8 armor (head..feet), 9-35 main, 36-44 hotbar, 45 offhand.
func NewPlayer() *Menu {
	m := &Menu{Items: make([]item.Stack, 46), Slots: make([]Slot, 46), Changed: make([]bool, 46)}
	m.Slots[0] = Slot{Kind: KindResult, Inv: -1}
	for i := 1; i < 5; i++ {
		m.Slots[i] = Slot{Inv: -1}
	}
	for i := 5; i < 9; i++ {
		m.Slots[i] = Slot{Kind: KindArmor, Armor: item.SlotHead - int32(i-5), Inv: 36 + 8 - i}
	}
	for i := 9; i < 36; i++ {
		m.Slots[i] = Slot{Inv: i}
	}
	for i := 36; i < 45; i++ {
		m.Slots[i] = Slot{Inv: i - 36}
	}
	m.Slots[45] = Slot{Inv: 40}
	return m
}

// InvSlot maps a player inventory index to its menu slot (-1 if none).
func (m *Menu) InvSlot(inv int) int {
	for i, s := range m.Slots {
		if s.Inv == inv {
			return i
		}
	}
	return -1
}

func (m *Menu) ValidSlot(i int) bool { return i == -1 || i == Outside || i >= 0 && i < len(m.Items) }

func (m *Menu) get(i int) item.Stack { return m.Items[i] }

func (m *Menu) set(i int, s item.Stack) {
	m.Items[i] = s.Norm()
	m.Changed[i] = true
	if m.Craft != nil && m.Slots[i].Kind == KindNormal && m.Slots[i].Inv < 0 {
		m.Craft(m)
	}
}

func (m *Menu) slotMax(i int) int32 {
	if m.Slots[i].Kind == KindArmor {
		return 1
	}
	return containerMax
}

func (m *Menu) slotMaxFor(i int, s item.Stack) int32 { return min(m.slotMax(i), s.MaxStackSize()) }

func (m *Menu) mayPlace(i int, s item.Stack, p Player) bool {
	switch m.Slots[i].Kind {
	case KindResult:
		return false
	case KindArmor:
		return item.EquippableBy(s, m.Slots[i].Armor, p.EntityType())
	}
	return true
}

func (m *Menu) mayPickup(i int, p Player) bool {
	if m.Slots[i].Kind == KindArmor {
		s := m.get(i)
		if !s.IsEmpty() && !p.Creative() && s.EnchantmentLevel("minecraft:binding_curse") > 0 {
			return false
		}
	}
	return true
}

func (m *Menu) allowModification(i int, p Player) bool {
	return m.mayPickup(i, p) && m.mayPlace(i, m.get(i), p)
}

func (m *Menu) remove(i int, n int32) item.Stack {
	s := m.get(i)
	if s.IsEmpty() || n <= 0 {
		return item.Stack{}
	}
	out := s.Split(n)
	m.set(i, s)
	return out
}

func (m *Menu) tryRemove(i int, n, limit int32, p Player) (item.Stack, bool) {
	if !m.mayPickup(i, p) {
		return item.Stack{}, false
	}
	if !m.allowModification(i, p) && limit < m.get(i).Count {
		return item.Stack{}, false
	}
	out := m.remove(i, min(n, limit))
	if out.IsEmpty() {
		return item.Stack{}, false
	}
	return out, true
}

func (m *Menu) safeTake(i int, n, limit int32, p Player) item.Stack {
	s, ok := m.tryRemove(i, n, limit, p)
	if !ok {
		return item.Stack{}
	}
	m.onTake(i, s, p)
	return s
}

func (m *Menu) onTake(i int, s item.Stack, p Player) {
	if m.Slots[i].Kind == KindResult {
		m.onCraft(p)
	}
}

// onCraft consumes one of each grid ingredient (shapeless 2x2 stub).
func (m *Menu) onCraft(Player) {
	for i := range m.Slots {
		if m.Slots[i].Kind == KindNormal && m.Slots[i].Inv < 0 {
			s := m.get(i)
			if !s.IsEmpty() {
				s.Shrink(1)
				m.set(i, s)
			}
		}
	}
}

// safeInsert moves up to n items of *s into slot i.
func (m *Menu) safeInsert(i int, s *item.Stack, n int32, p Player) {
	if s.IsEmpty() || !m.mayPlace(i, *s, p) {
		return
	}
	cur := m.get(i)
	k := min(min(n, s.Count), m.slotMaxFor(i, *s)-cur.Count)
	if k <= 0 {
		return
	}
	if cur.IsEmpty() {
		m.set(i, s.Split(k))
	} else if item.SameItemSameComponents(cur, *s) {
		s.Shrink(k)
		cur.Grow(k)
		m.set(i, cur)
	}
}

func canQuickReplace(slot item.Stack, s item.Stack, sizeMatters bool) bool {
	if slot.IsEmpty() {
		return true
	}
	if !item.SameItemSameComponents(s, slot) {
		return false
	}
	extra := s.Count
	if sizeMatters {
		extra = 0
	}
	return slot.Count+extra <= s.MaxStackSize()
}

func quickCraftPlaceCount(n int, kind int32, s item.Stack) int32 {
	switch kind {
	case 0:
		return s.Count / int32(n)
	case 1:
		return 1
	case 2:
		return s.MaxStackSize()
	}
	return s.Count
}

func (m *Menu) resetQuickCraft() {
	m.qcStatus = 0
	m.qcSlots = m.qcSlots[:0]
}

func (m *Menu) canDragTo(i int) bool { return m.Slots[i].Kind != KindResult }

// Click applies one click as AbstractContainerMenu.clicked does.
func (m *Menu) Click(slot int, button int32, kind int32, p Player) {
	if !m.ValidSlot(slot) {
		return
	}
	m.doClick(slot, button, kind, p)
}

func (m *Menu) doClick(slot int, button int32, kind int32, p Player) {
	if kind == QuickCraft {
		m.quickCraft(slot, button, p)
		return
	}
	if m.qcStatus != 0 {
		m.resetQuickCraft()
		return
	}
	switch {
	case (kind == Pickup || kind == QuickMove) && (button == 0 || button == 1):
		primary := button == 0
		if slot == Outside {
			if !m.Carried.IsEmpty() {
				if primary {
					p.Drop(m.Carried, true)
					m.Carried = item.Stack{}
				} else {
					p.Drop(m.Carried.Split(1), true)
				}
			}
			return
		}
		if slot < 0 {
			return
		}
		if kind == QuickMove {
			if !m.mayPickup(slot, p) {
				return
			}
			s := m.QuickMoveStack(slot, p)
			for !s.IsEmpty() && m.get(slot).Item == s.Item && !m.get(slot).IsEmpty() {
				s = m.QuickMoveStack(slot, p)
			}
			return
		}
		m.pickup(slot, primary, p)
	case kind == Swap && (button >= 0 && button < 9 || button == 40):
		m.swap(slot, int(button), p)
	case kind == Clone && p.InfiniteMaterials() && m.Carried.IsEmpty() && slot >= 0:
		if s := m.get(slot); !s.IsEmpty() {
			m.Carried = s.WithCount(s.MaxStackSize())
		}
	case kind == Throw && m.Carried.IsEmpty() && slot >= 0:
		n := int32(1)
		if button != 0 {
			n = m.get(slot).Count
		}
		s := m.safeTake(slot, n, 1<<31-1, p)
		p.Drop(s, true)
		if button == 1 {
			for !s.IsEmpty() && !m.get(slot).IsEmpty() && m.get(slot).Item == s.Item {
				s = m.safeTake(slot, n, 1<<31-1, p)
				p.Drop(s, true)
			}
		}
	case kind == PickupAll && slot >= 0:
		m.pickupAll(slot, button, p)
	}
}

func (m *Menu) pickup(slot int, primary bool, p Player) {
	cur := m.get(slot)
	carried := m.Carried
	switch {
	case cur.IsEmpty():
		if !carried.IsEmpty() {
			n := carried.Count
			if !primary {
				n = 1
			}
			m.safeInsert(slot, &carried, n, p)
			m.Carried = carried.Norm()
		}
	case m.mayPickup(slot, p):
		switch {
		case carried.IsEmpty():
			n := cur.Count
			if !primary {
				n = (cur.Count + 1) / 2
			}
			if s, ok := m.tryRemove(slot, n, 1<<31-1, p); ok {
				m.Carried = s
				m.onTake(slot, s, p)
			}
		case m.mayPlace(slot, carried, p):
			if item.SameItemSameComponents(cur, carried) {
				n := carried.Count
				if !primary {
					n = 1
				}
				m.safeInsert(slot, &carried, n, p)
				m.Carried = carried.Norm()
			} else if carried.Count <= m.slotMaxFor(slot, carried) {
				m.Carried = cur
				m.set(slot, carried)
			}
		case item.SameItemSameComponents(cur, carried):
			if s, ok := m.tryRemove(slot, cur.Count, carried.MaxStackSize()-carried.Count, p); ok {
				carried.Grow(s.Count)
				m.Carried = carried
				m.onTake(slot, s, p)
			}
		}
	}
}

func (m *Menu) swap(slot int, button int, p Player) {
	if slot < 0 {
		return
	}
	src := m.InvSlot(button)
	if src < 0 {
		return
	}
	source := m.get(src)
	target := m.get(slot)
	if source.IsEmpty() && target.IsEmpty() {
		return
	}
	switch {
	case source.IsEmpty():
		if m.mayPickup(slot, p) {
			m.set(src, target)
			m.set(slot, item.Stack{})
			m.onTake(slot, target, p)
		}
	case target.IsEmpty():
		if m.mayPlace(slot, source, p) {
			mx := m.slotMaxFor(slot, source)
			if source.Count > mx {
				part := source.Split(mx)
				m.set(src, source)
				m.set(slot, part)
			} else {
				m.set(src, item.Stack{})
				m.set(slot, source)
			}
		}
	case m.mayPickup(slot, p) && m.mayPlace(slot, source, p):
		mx := m.slotMaxFor(slot, source)
		if source.Count > mx {
			part := source.Split(mx)
			m.set(src, source)
			m.set(slot, part)
			m.onTake(slot, target, p)
			if rest := m.AddToInventory(target, p); !rest.IsEmpty() {
				p.Drop(rest, true)
			}
		} else {
			m.set(src, target)
			m.set(slot, source)
			m.onTake(slot, target, p)
		}
	}
}

func (m *Menu) pickupAll(slot int, button int32, p Player) {
	if m.Carried.IsEmpty() || !m.get(slot).IsEmpty() && m.mayPickup(slot, p) {
		return
	}
	start, step := 0, 1
	if button != 0 {
		start, step = len(m.Items)-1, -1
	}
	for pass := 0; pass < 2; pass++ {
		for i := start; i >= 0 && i < len(m.Items) && m.Carried.Count < m.Carried.MaxStackSize(); i += step {
			s := m.get(i)
			if s.IsEmpty() || !canQuickReplace(s, m.Carried, true) || !m.mayPickup(i, p) || m.Slots[i].Kind == KindResult {
				continue
			}
			if pass == 0 && s.Count == s.MaxStackSize() {
				continue
			}
			r := m.safeTake(i, s.Count, m.Carried.MaxStackSize()-m.Carried.Count, p)
			m.Carried.Grow(r.Count)
		}
	}
}

func (m *Menu) quickCraft(slot int, button int32, p Player) {
	prev := m.qcStatus
	m.qcStatus = button & 3
	switch {
	case (prev != 1 || m.qcStatus != 2) && prev != m.qcStatus:
		m.resetQuickCraft()
	case m.Carried.IsEmpty():
		m.resetQuickCraft()
	case m.qcStatus == 0:
		m.qcType = button >> 2 & 3
		if m.qcType == 0 || m.qcType == 1 || m.qcType == 2 && p.InfiniteMaterials() {
			m.qcStatus = 1
			m.qcSlots = m.qcSlots[:0]
		} else {
			m.resetQuickCraft()
		}
	case m.qcStatus == 1:
		if slot < 0 || slot >= len(m.Items) {
			return
		}
		c := m.Carried
		if canQuickReplace(m.get(slot), c, true) && m.mayPlace(slot, c, p) && (m.qcType == 2 || int(c.Count) > len(m.qcSlots)) && m.canDragTo(slot) {
			for _, x := range m.qcSlots {
				if x == slot {
					return
				}
			}
			m.qcSlots = append(m.qcSlots, slot)
		}
	case m.qcStatus == 2:
		if len(m.qcSlots) > 0 {
			if len(m.qcSlots) == 1 {
				s := m.qcSlots[0]
				t := m.qcType
				m.resetQuickCraft()
				m.doClick(s, t, Pickup, p)
				return
			}
			src := m.Carried
			remaining := src.Count
			for _, i := range m.qcSlots {
				c := m.Carried
				if canQuickReplace(m.get(i), c, true) && m.mayPlace(i, c, p) && (m.qcType == 2 || int(c.Count) >= len(m.qcSlots)) && m.canDragTo(i) {
					have := int32(0)
					if cur := m.get(i); !cur.IsEmpty() {
						have = cur.Count
					}
					mx := min(src.MaxStackSize(), m.slotMaxFor(i, src))
					put := min(quickCraftPlaceCount(len(m.qcSlots), m.qcType, src)+have, mx)
					remaining -= put - have
					m.set(i, src.WithCount(put))
				}
			}
			m.Carried = src.WithCount(remaining)
		}
		m.resetQuickCraft()
	default:
		m.resetQuickCraft()
	}
}

// QuickMoveStack is InventoryMenu.quickMoveStack (shift-click).
func (m *Menu) QuickMoveStack(i int, p Player) item.Stack {
	if m.Container > 0 {
		return m.quickMoveChest(i, p)
	}
	s := m.get(i)
	if s.IsEmpty() {
		return item.Stack{}
	}
	orig := s
	eq := item.EquipmentSlotFor(s)
	moved := func(from, to int, rev bool) bool { return m.moveTo(&s, from, to, rev, p) }
	switch {
	case i == 0:
		if !moved(9, 45, true) {
			return item.Stack{}
		}
	case i >= 1 && i < 9:
		if !moved(9, 45, false) {
			return item.Stack{}
		}
	case eq >= item.SlotFeet && eq <= item.SlotHead && m.get(8-int(eq-item.SlotFeet)).IsEmpty():
		t := 8 - int(eq-item.SlotFeet)
		if !moved(t, t+1, false) {
			return item.Stack{}
		}
	case eq == item.SlotOffHand && m.get(45).IsEmpty():
		if !moved(45, 46, false) {
			return item.Stack{}
		}
	case i >= 9 && i < 36:
		if !moved(36, 45, false) {
			return item.Stack{}
		}
	case i >= 36 && i < 45:
		if !moved(9, 36, false) {
			return item.Stack{}
		}
	default:
		if !moved(9, 45, false) {
			return item.Stack{}
		}
	}
	m.set(i, s)
	if s.Count == orig.Count {
		return item.Stack{}
	}
	m.onTake(i, s, p)
	if i == 0 {
		p.Drop(s, false)
	}
	return orig
}

// moveTo is AbstractContainerMenu.moveItemStackTo.
func (m *Menu) moveTo(s *item.Stack, from, to int, rev bool, p Player) bool {
	changed := false
	next := func(i int) int {
		if rev {
			return i - 1
		}
		return i + 1
	}
	in := func(i int) bool { return i >= from && i < to }
	first := from
	if rev {
		first = to - 1
	}
	if s.IsStackable() {
		for i := first; !s.IsEmpty() && in(i); i = next(i) {
			t := m.get(i)
			if t.IsEmpty() || !item.SameItemSameComponents(*s, t) {
				continue
			}
			total := t.Count + s.Count
			mx := m.slotMaxFor(i, t)
			if total <= mx {
				*s = item.Stack{}
				m.set(i, t.WithCount(total))
				changed = true
			} else if t.Count < mx {
				s.Shrink(mx - t.Count)
				m.set(i, t.WithCount(mx))
				changed = true
			}
		}
	}
	if !s.IsEmpty() {
		for i := first; in(i); i = next(i) {
			if m.get(i).IsEmpty() && m.mayPlace(i, *s, p) {
				mx := m.slotMaxFor(i, *s)
				m.set(i, s.Split(min(s.Count, mx)))
				changed = true
				break
			}
		}
	}
	return changed
}

// AddToInventory is Inventory.add over the menu's inventory slots and
// returns what did not fit.
func (m *Menu) AddToInventory(s item.Stack, p Player) item.Stack {
	if s.IsEmpty() {
		return s
	}
	if s.IsDamaged() {
		if i := m.freeInv(); i >= 0 {
			m.set(i, s)
			return item.Stack{}
		}
		if p.InfiniteMaterials() {
			return item.Stack{}
		}
		return s
	}
	for {
		before := s.Count
		i := m.spaceFor(s, p)
		if i < 0 {
			i = m.freeInv()
		}
		if i >= 0 {
			cur := m.get(i)
			k := min(s.Count, m.slotMaxFor(i, s)-cur.Count)
			if k > 0 {
				if cur.IsEmpty() {
					cur = s.WithCount(k)
				} else {
					cur.Grow(k)
				}
				s.Shrink(k)
				m.set(i, cur)
			}
		}
		if s.IsEmpty() || s.Count >= before {
			if s.Count == before && p.InfiniteMaterials() {
				return item.Stack{}
			}
			return s
		}
	}
}

func (m *Menu) hasSpace(i int, s item.Stack) bool {
	if i < 0 {
		return false
	}
	d := m.get(i)
	return !d.IsEmpty() && item.SameItemSameComponents(d, s) && d.IsStackable() && d.Count < m.slotMaxFor(i, d)
}

func (m *Menu) spaceFor(s item.Stack, p Player) int {
	if i := m.InvSlot(p.SelectedSlot()); m.hasSpace(i, s) {
		return i
	}
	if i := m.InvSlot(40); m.hasSpace(i, s) {
		return i
	}
	for inv := 0; inv < 36; inv++ {
		if i := m.InvSlot(inv); m.hasSpace(i, s) {
			return i
		}
	}
	return -1
}

func (m *Menu) freeInv() int {
	for inv := 0; inv < 36; inv++ {
		if i := m.InvSlot(inv); i >= 0 && m.get(i).IsEmpty() {
			return i
		}
	}
	return -1
}
