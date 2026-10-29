package server

import (
	"bytes"
	"slices"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// stackFromAPI converts a plugin stack. Explicit fields override raw
// components of the same type; invalid raw components are dropped.
func stackFromAPI(a api.ItemStack) item.Stack {
	if a.IsEmpty() {
		return item.Stack{}
	}
	id, ok := item.IDOf(string(a.Type.Normalize()))
	if !ok || id == 0 {
		return item.Stack{}
	}
	s := item.Stack{Item: id, Count: a.Count}
	for k, v := range a.Components {
		t, ok := item.TypeByName(string(k))
		if !ok {
			continue
		}
		r := proto.Reader{B: v}
		if item.SkipValue(t, &r) != nil || r.Remaining() != 0 {
			continue
		}
		s = s.Set(t, v)
	}
	for _, k := range a.Removed {
		if t, ok := item.TypeByName(string(k)); ok {
			s = s.Remove(t)
		}
	}
	if a.Name != nil {
		t := toText(*a.Name)
		s = s.Set(item.CustomName, []byte(t.NBT()))
	}
	if len(a.Lore) > 0 {
		b := proto.AppendVarInt(nil, int32(len(a.Lore)))
		for _, l := range a.Lore {
			t := toText(l)
			b = append(b, []byte(t.NBT())...)
		}
		s = s.Set(item.Lore, b)
	}
	if a.Damage > 0 {
		s = s.Set(item.Damage, item.EncodeVarInt(a.Damage))
	}
	if a.Unbreakable {
		s = s.Set(item.Unbreakable, nil)
	}
	if len(a.CustomModelData) > 0 {
		w := proto.Writer{B: proto.AppendVarInt(nil, int32(len(a.CustomModelData)))}
		for _, f := range a.CustomModelData {
			w.F32(f)
		}
		w.B = append(w.B, 0, 0, 0)
		s = s.Set(item.CustomModelData, w.B)
	}
	if len(a.Enchantments) > 0 {
		keys := make([]string, 0, len(a.Enchantments))
		for k := range a.Enchantments {
			keys = append(keys, string(k.Normalize()))
		}
		slices.Sort(keys)
		var es []item.Enchantment
		for _, k := range keys {
			if id, ok := regsync.Index("minecraft:enchantment", k); ok {
				es = append(es, item.Enchantment{ID: id, Level: a.Enchantments[api.Key(k)]})
			}
		}
		s = s.Set(item.Enchantments, item.EncodeEnchantments(es))
	}
	return s
}

// stackToAPI converts for plugins. Components move to explicit fields only
// when the conversion is lossless, so stackFromAPI(stackToAPI(s)) == s.
func stackToAPI(s item.Stack) api.ItemStack {
	if s.IsEmpty() {
		return api.ItemStack{}
	}
	a := api.ItemStack{Type: api.Key(item.NameOf(s.Item)), Count: s.Count}
	s.Patch.Each(func(t item.Type, v []byte, removed bool) {
		key := api.Key(t.String())
		if removed {
			a.Removed = append(a.Removed, key)
			return
		}
		switch t {
		case item.CustomName:
			if c, ok := textLossless(v); ok {
				a.Name = &c
				return
			}
		case item.Damage:
			if n, err := item.DecodeVarInt(v); err == nil && n > 0 {
				a.Damage = n
				return
			}
		case item.Unbreakable:
			a.Unbreakable = true
			return
		case item.Enchantments:
			if es, err := item.DecodeEnchantments(v); err == nil && enchantsLossless(es, v) {
				a.Enchantments = map[api.Key]int32{}
				for _, e := range es {
					a.Enchantments[api.Key(regName("minecraft:enchantment", e.ID))] = e.Level
				}
				return
			}
		}
		if a.Components == nil {
			a.Components = map[api.Key][]byte{}
		}
		a.Components[key] = bytes.Clone(v)
	})
	return a
}

func textLossless(v []byte) (api.Component, bool) {
	var t proto.Text
	if err := t.Decode(&proto.Reader{B: v}); err != nil {
		return api.Component{}, false
	}
	c := fromText(t)
	back := toText(c)
	return c, bytes.Equal([]byte(back.NBT()), v)
}

func enchantsLossless(es []item.Enchantment, v []byte) bool {
	sorted := slices.IsSortedFunc(es, func(a, b item.Enchantment) int {
		return compareStr(regName("minecraft:enchantment", a.ID), regName("minecraft:enchantment", b.ID))
	})
	return sorted && bytes.Equal(item.EncodeEnchantments(es), v)
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func regName(reg string, id int32) string {
	for _, r := range regsync.Registries() {
		if r.Name == reg && id >= 0 && int(id) < len(r.Entries) {
			return r.Entries[id]
		}
	}
	return ""
}

// playerInv is the plugin view of a player inventory (inventory indices:
// 0-8 hotbar, 9-35 main, 36-39 feet..head, 40 off hand).
type playerInv struct{ p *Player }

func (i playerInv) store() *invStore { return i.p.inv }

func (i playerInv) Type() api.InventoryType { return api.InvPlayer }
func (i playerInv) Title() api.Component    { return api.Text("Inventory") }
func (i playerInv) Size() int               { return invSize }
func (i playerInv) Viewers() []api.Player   { return []api.Player{i.p} }
func (i playerInv) Holder() any             { return i.p }

func (i playerInv) Item(slot int) api.ItemStack {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	return stackToAPI(s.get(slot))
}

func (i playerInv) SetItem(slot int, it api.ItemStack) {
	s := i.store()
	s.mu.Lock()
	s.set(slot, stackFromAPI(it))
	s.mu.Unlock()
}

func (i playerInv) Contents() []api.ItemStack {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]api.ItemStack, invSize)
	for k := range out {
		out[k] = stackToAPI(s.get(k))
	}
	return out
}

func (i playerInv) SetContents(items []api.ItemStack) {
	s := i.store()
	s.mu.Lock()
	for k := range invSize {
		var it api.ItemStack
		if k < len(items) {
			it = items[k]
		}
		s.set(k, stackFromAPI(it))
	}
	s.mu.Unlock()
}

func (i playerInv) AddItem(items ...api.ItemStack) []api.ItemStack {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	var left []api.ItemStack
	for _, it := range items {
		st := stackFromAPI(it)
		if st.IsEmpty() {
			continue
		}
		if rest := s.m.AddToInventory(st, menuPlayer{i.p}); !rest.IsEmpty() {
			left = append(left, stackToAPI(rest))
		}
	}
	return left
}

func (i playerInv) RemoveItem(items ...api.ItemStack) []api.ItemStack {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	var left []api.ItemStack
	for _, it := range items {
		want := stackFromAPI(it)
		n := want.Count
		for k := 0; k < invSize && n > 0; k++ {
			cur := s.get(k)
			if cur.IsEmpty() || !item.SameItemSameComponents(cur, want) {
				continue
			}
			take := min(cur.Count, n)
			cur.Shrink(take)
			n -= take
			s.set(k, cur)
		}
		if n > 0 {
			left = append(left, stackToAPI(want.WithCount(n)))
		}
	}
	return left
}

func (i playerInv) Contains(k api.Key, n int32) bool {
	id, ok := item.IDOf(string(k.Normalize()))
	if !ok {
		return false
	}
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int32
	for x := range invSize {
		if st := s.get(x); !st.IsEmpty() && st.Item == id {
			total += st.Count
		}
	}
	return total >= n
}

func (i playerInv) FirstEmpty() int {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	for x := range 36 {
		if s.get(x).IsEmpty() {
			return x
		}
	}
	return -1
}

func (i playerInv) Clear() {
	s := i.store()
	s.mu.Lock()
	for x := range invSize {
		s.set(x, item.Stack{})
	}
	s.mu.Unlock()
}

func (i playerInv) HeldSlot() int { return int(i.p.held.Load()) }

func (i playerInv) SetHeldSlot(n int) {
	if n < 0 || n > 8 {
		return
	}
	i.p.held.Store(int32(n))
	i.p.send(&packets.ClientboundSetHeldSlot{Slot: int32(n)})
}

func (i playerInv) ItemInHand(h api.Hand) api.ItemStack { return stackToAPI(i.p.Hand(h)) }

func (i playerInv) SetItemInHand(h api.Hand, it api.ItemStack) { i.p.setHand(h, stackFromAPI(it)) }

func (i playerInv) Armor() [4]api.ItemStack {
	return [4]api.ItemStack{i.Item(36), i.Item(37), i.Item(38), i.Item(39)}
}

func (i playerInv) SetArmor(a [4]api.ItemStack) {
	for k, it := range a {
		i.SetItem(36+k, it)
	}
}

func (i playerInv) Cursor() api.ItemStack {
	s := i.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	return stackToAPI(s.m.Carried)
}

func (i playerInv) SetCursor(it api.ItemStack) {
	s := i.store()
	s.mu.Lock()
	s.m.Carried = stackFromAPI(it)
	s.mu.Unlock()
}

var _ api.PlayerInventory = playerInv{}
