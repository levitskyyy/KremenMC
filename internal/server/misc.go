package server

import (
	"maps"
	"slices"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
)

type dataContainer struct {
	mu sync.RWMutex
	m  map[api.Key][]byte
}

func newDataContainer() *dataContainer { return &dataContainer{m: map[api.Key][]byte{}} }

func (d *dataContainer) Get(k api.Key) ([]byte, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.m[k]
	return slices.Clone(v), ok
}

func (d *dataContainer) Set(k api.Key, v []byte) {
	d.mu.Lock()
	d.m[k] = slices.Clone(v)
	d.mu.Unlock()
}

func (d *dataContainer) Remove(k api.Key) {
	d.mu.Lock()
	delete(d.m, k)
	d.mu.Unlock()
}

func (d *dataContainer) Has(k api.Key) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.m[k]
	return ok
}

func (d *dataContainer) Keys() []api.Key {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return slices.Collect(maps.Keys(d.m))
}

// inventory is a plain item container. Player inventories wrap it and push
// slot changes to the client.
type inventory struct {
	kind   api.InventoryType
	title  api.Component
	holder any
	mu     sync.Mutex
	items  []api.ItemStack
	onSet  func(slot int, item api.ItemStack)
}

func newInventory(kind api.InventoryType, title api.Component, size int, holder any) *inventory {
	return &inventory{kind: kind, title: title, holder: holder, items: make([]api.ItemStack, max(size, 0))}
}

func (i *inventory) Type() api.InventoryType { return i.kind }
func (i *inventory) Title() api.Component    { return i.title }
func (i *inventory) Size() int               { return len(i.items) }
func (i *inventory) Holder() any             { return i.holder }
func (i *inventory) Viewers() []api.Player   { return nil }

func (i *inventory) Item(slot int) api.ItemStack {
	i.mu.Lock()
	defer i.mu.Unlock()
	if slot < 0 || slot >= len(i.items) {
		return api.ItemStack{}
	}
	return i.items[slot]
}

func (i *inventory) SetItem(slot int, it api.ItemStack) {
	i.mu.Lock()
	if slot < 0 || slot >= len(i.items) {
		i.mu.Unlock()
		return
	}
	i.items[slot] = it
	f := i.onSet
	i.mu.Unlock()
	if f != nil {
		f(slot, it)
	}
}

func (i *inventory) Contents() []api.ItemStack {
	i.mu.Lock()
	defer i.mu.Unlock()
	return slices.Clone(i.items)
}

func (i *inventory) SetContents(items []api.ItemStack) {
	for s := range i.Size() {
		var it api.ItemStack
		if s < len(items) {
			it = items[s]
		}
		i.SetItem(s, it)
	}
}

func maxStack(k api.Key) int32 {
	if n := (itemRegistry{}).MaxStackSize(k); n > 0 {
		return n
	}
	return 64
}

func (i *inventory) AddItem(items ...api.ItemStack) []api.ItemStack {
	var left []api.ItemStack
	for _, it := range items {
		for s := 0; s < i.Size() && it.Count > 0; s++ {
			cur := i.Item(s)
			if !cur.IsEmpty() && cur.Similar(it) {
				n := min(maxStack(it.Type)-cur.Count, it.Count)
				if n > 0 {
					cur.Count += n
					it.Count -= n
					i.SetItem(s, cur)
				}
			}
		}
		for s := 0; s < i.Size() && it.Count > 0; s++ {
			if i.Item(s).IsEmpty() {
				put := it
				put.Count = min(it.Count, maxStack(it.Type))
				it.Count -= put.Count
				i.SetItem(s, put)
			}
		}
		if it.Count > 0 {
			left = append(left, it)
		}
	}
	return left
}

func (i *inventory) RemoveItem(items ...api.ItemStack) []api.ItemStack {
	var left []api.ItemStack
	for _, it := range items {
		for s := 0; s < i.Size() && it.Count > 0; s++ {
			cur := i.Item(s)
			if cur.IsEmpty() || !cur.Similar(it) {
				continue
			}
			n := min(cur.Count, it.Count)
			cur.Count -= n
			it.Count -= n
			if cur.Count == 0 {
				cur = api.ItemStack{}
			}
			i.SetItem(s, cur)
		}
		if it.Count > 0 {
			left = append(left, it)
		}
	}
	return left
}

func (i *inventory) Contains(k api.Key, n int32) bool {
	var total int32
	for _, it := range i.Contents() {
		if !it.IsEmpty() && it.Type.Normalize() == k.Normalize() {
			total += it.Count
		}
	}
	return total >= n
}

func (i *inventory) FirstEmpty() int {
	for s, it := range i.Contents() {
		if it.IsEmpty() {
			return s
		}
	}
	return -1
}

func (i *inventory) Clear() { i.SetContents(nil) }

// console is the server console command sender.
type console struct {
	s *Server
	permissible
}

func newConsole(s *Server) *console {
	c := &console{s: s}
	c.permissible = permissible{perms: s.perms, op: func() bool { return true }, setOp: func(bool) {}}
	return c
}

func (c *console) Name() string                { return "Server" }
func (c *console) SendMessage(m api.Component) { c.s.log.Info(m.Plain(), "source", "command") }
func (c *console) HasPermission(string) bool   { return true }
