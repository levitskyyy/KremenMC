package menu

import "github.com/levitskyyy/KremenMC/internal/item"

// NewChest builds a chest-like menu: rows*9 container slots, then the
// player's main inventory (27) and hotbar (9).
func NewChest(rows int) *Menu {
	n := rows*9 + 36
	m := &Menu{Items: make([]item.Stack, n), Slots: make([]Slot, n), Changed: make([]bool, n), Container: rows * 9}
	for i := range m.Container {
		m.Slots[i] = Slot{Inv: -1}
	}
	for i := range 27 {
		m.Slots[m.Container+i] = Slot{Inv: 9 + i}
	}
	for i := range 9 {
		m.Slots[m.Container+27+i] = Slot{Inv: i}
	}
	return m
}

// quickMoveChest is ChestMenu.quickMoveStack: container slots go to the
// player inventory from the end, inventory slots into the container.
func (m *Menu) quickMoveChest(i int, p Player) item.Stack {
	s := m.get(i)
	if s.IsEmpty() {
		return item.Stack{}
	}
	orig := s
	if i < m.Container {
		if !m.moveTo(&s, m.Container, len(m.Items), true, p) {
			return item.Stack{}
		}
	} else if !m.moveTo(&s, 0, m.Container, false, p) {
		return item.Stack{}
	}
	m.set(i, s)
	return orig
}
