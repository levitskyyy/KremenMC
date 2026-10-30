package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/item/menu"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Chest-like containers (chest, trapped_chest, barrel) with 27 slots kept
// in the block entity's Items. A LootTable is rolled into the container
// the first time it is opened or broken. Every viewer reads and writes the
// block entity on each click; double chests open as their own half.

const chestSlots = 27

var chestBlocks = map[string]string{
	"chest": "minecraft:chest", "trapped_chest": "minecraft:trapped_chest", "barrel": "minecraft:barrel",
}

var generic9x3, generic9x6 = menuID("minecraft:generic_9x3"), menuID("minecraft:generic_9x6")

func menuID(name string) int32 {
	if p, err := wgdata.Load(); err == nil {
		l, _ := p.Builtin("menu")
		for i, id := range l {
			if id == name {
				return int32(i)
			}
		}
	}
	return -1
}

type openChest struct {
	id  int32
	w   *World
	pos api.BlockPos
	// parts are the block entities behind the window, 27 slots each (two
	// for a double chest, the first holding the top half).
	parts []api.BlockPos
	m     *menu.Menu
	sync  menu.Sync
	// custom is set for a Java plugin inventory (bridgeinv.go); w is nil then.
	custom *customInv
}

// blockEntityAt returns the chunk holding pos and the index of its block
// entity there (-1 if none). The caller locks the chunk.
func blockEntityAt(c *world.Chunk, pos api.BlockPos) int {
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		if int(be.X) == pos.X&15 && int(be.Z) == pos.Z&15 && int(be.Y) == pos.Y {
			return i
		}
	}
	return -1
}

func (w *World) chunkAt(pos api.BlockPos) *world.Chunk {
	return w.w.Chunk(world.ChunkAt(pos.X, pos.Z))
}

// unpackLoot rolls a pending LootTable of the container at pos into its
// Items (RandomizableContainer.unpackLootTable).
func (w *World) unpackLoot(pos api.BlockPos, opener *Player) {
	c := w.chunkAt(pos)
	if c == nil {
		return
	}
	c.Lock()
	i := blockEntityAt(c, pos)
	if i < 0 {
		c.Unlock()
		return
	}
	be := &c.BlockEntities[i]
	table, _ := be.Data["LootTable"].(string)
	seed, _ := be.Data["LootTableSeed"].(int64)
	if table == "" {
		c.Unlock()
		return
	}
	d := cloneCompound(be.Data)
	delete(d, "LootTable")
	delete(d, "LootTableSeed")
	be.Data = d
	c.Unlock()

	t := loot.Get(table)
	if t == nil {
		return
	}
	var src rng.Source
	if seed != 0 {
		src = rng.NewLegacy(seed)
	} else {
		r := w.lootState()
		r.mu.Lock()
		src = rng.NewLegacy(r.rand.NextLong())
		r.mu.Unlock()
	}
	ctx := &loot.Context{Rand: src, Origin: api.Vec3{X: float64(pos.X) + .5, Y: float64(pos.Y) + .5, Z: float64(pos.Z) + .5}, HasOrigin: true}
	if opener != nil {
		ctx.This = lootPlayer{opener}
		if v, ok := opener.AttributeValue("minecraft:luck"); ok {
			ctx.Luck = float32(v)
		}
	}
	drops := w.RollLoot(t, ctx)
	c.Lock()
	defer c.Unlock()
	if i = blockEntityAt(c, pos); i < 0 {
		return
	}
	be = &c.BlockEntities[i]
	items := readContainer(be.Data)
	fillContainer(&items, drops, src)
	d = cloneCompound(be.Data)
	writeContainer(d, &items)
	be.Data = d
	c.MarkDirty()
}

// fillContainer is the container fill after the loot roll, inferred from
// the chest probe (kremenmc.probe.chest.Fill, TestChestFillReference), all on
// the roll's random: the empty slots are shuffled; stacks of more than one
// item are split (a random one, at a random size in 1..count/2) while the
// stacks would not fill the free slots, each half going back to be split
// again only when larger than one and on a coin flip (the remainder is
// decided before the split-off part); the stacks are
// shuffled and each takes the last remaining shuffled slot.
func fillContainer(items *[chestSlots]item.Stack, drops []item.Stack, r rng.Source) {
	var empty []int
	for i, s := range items {
		if s.IsEmpty() {
			empty = append(empty, i)
		}
	}
	shuffleSlice(empty, r)
	var done, split []item.Stack
	for _, s := range drops {
		switch {
		case s.IsEmpty():
		case s.Count > 1:
			split = append(split, s)
		default:
			done = append(done, s)
		}
	}
	for len(empty)-len(done)-len(split) > 0 && len(split) > 0 {
		i := lootRandBetween(r, 0, int32(len(split)-1))
		s := split[i]
		split = append(split[:i], split[i+1:]...)
		k := lootRandBetween(r, 1, s.Count/2)
		a := s
		a.Count, s.Count = k, s.Count-k
		for _, h := range [2]item.Stack{s, a} {
			if h.Count > 1 && r.NextBool() {
				split = append(split, h)
			} else {
				done = append(done, h)
			}
		}
	}
	done = append(done, split...)
	shuffleSlice(done, r)
	for _, s := range done {
		if len(empty) == 0 {
			break
		}
		items[empty[len(empty)-1]] = s
		empty = empty[:len(empty)-1]
	}
}

// shuffleSlice swaps each index from the last down to 1 with a random
// index at or below it.
func shuffleSlice[T any](l []T, r rng.Source) {
	for i := len(l) - 1; i > 0; i-- {
		j := r.NextIntn(int32(i + 1))
		l[i], l[j] = l[j], l[i]
	}
}

// lootRandBetween is [lo, hi] with no draw when the range is a single value.
func lootRandBetween(r rng.Source, lo, hi int32) int32 {
	if lo >= hi {
		return lo
	}
	return lo + r.NextIntn(hi-lo+1)
}

func readContainer(d nbt.Compound) (out [chestSlots]item.Stack) {
	for _, v := range nbtList(d["Items"]) {
		e, ok := v.(nbt.Compound)
		if !ok {
			continue
		}
		slot, ok := nbtInt(e["Slot"])
		if !ok || slot < 0 || slot >= chestSlots {
			continue
		}
		if st, ok := stackFromNBT(e); ok {
			out[slot] = st
		}
	}
	return out
}

func writeContainer(d nbt.Compound, items *[chestSlots]item.Stack) {
	var l []nbt.Compound
	for i, s := range items {
		if s.IsEmpty() {
			continue
		}
		e := stackNBT(s)
		e["Slot"] = int8(i)
		l = append(l, e)
	}
	if len(l) == 0 {
		delete(d, "Items")
		return
	}
	d["Items"] = l
}

// containerItems reads the container at pos; ok is false when there is no
// chest-like block entity there.
func (w *World) containerItems(pos api.BlockPos) (items [chestSlots]item.Stack, ok bool) {
	c := w.chunkAt(pos)
	if c == nil {
		return items, false
	}
	c.RLock()
	defer c.RUnlock()
	i := blockEntityAt(c, pos)
	if i < 0 {
		return items, false
	}
	return readContainer(c.BlockEntities[i].Data), true
}

func (w *World) setContainerItems(pos api.BlockPos, items *[chestSlots]item.Stack) {
	c := w.chunkAt(pos)
	if c == nil {
		return
	}
	c.Lock()
	defer c.Unlock()
	i := blockEntityAt(c, pos)
	if i < 0 {
		return
	}
	d := cloneCompound(c.BlockEntities[i].Data)
	writeContainer(d, items)
	c.BlockEntities[i].Data = d
	c.MarkDirty()
}

// openChest opens the chest-like container at pos; false when pos holds
// none. A blocked chest consumes the click without opening.
func (p *Player) openChest(w *World, pos api.BlockPos) bool {
	st := w.w.Block(pos.X, pos.Y, pos.Z)
	b := data.BlockOf(st)
	if b == nil || generic9x3 < 0 {
		return false
	}
	id, ok := chestBlocks[b.Name]
	if !ok {
		return false
	}
	parts := []api.BlockPos{pos}
	if d := chestConnected(st); d >= 0 && infoOf(st).kind == kChest {
		o := faceOffsets[d]
		q := pos.Add(o[0], 0, o[2])
		if n := w.w.Block(q.X, q.Y, q.Z); sameBlock(n, st) && chestConnected(n) == dirOpp[d] {
			// the right half holds the top rows
			if prop(st, "type") == "left" {
				parts = []api.BlockPos{q, pos}
			} else {
				parts = append(parts, q)
			}
		}
	}
	if infoOf(st).kind == kChest {
		for _, q := range parts {
			if conductor(w.w.Block(q.X, q.Y+1, q.Z)) {
				return true
			}
		}
	}
	for _, q := range parts {
		c := w.chunkAt(q)
		if c == nil {
			return false
		}
		c.Lock()
		if blockEntityAt(c, q) < 0 {
			c.BlockEntities = append(c.BlockEntities, world.BlockEntity{
				X: uint8(q.X & 15), Y: int32(q.Y), Z: uint8(q.Z & 15), NetType: world.BlockEntityNetType(id), ID: id, Data: nbt.Compound{},
			})
			c.MarkDirty()
		}
		c.Unlock()
		w.unpackLoot(q, p)
	}

	s := p.inv
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chest != nil {
		p.closeChestLocked(false)
	}
	s.chestSeq = s.chestSeq%100 + 1
	oc := &openChest{id: s.chestSeq, w: w, pos: pos, parts: parts, m: menu.NewChest(3 * len(parts))}
	oc.sync.Container = oc.id
	if !oc.load() {
		return false
	}
	p.copyInvToChest(oc)
	s.chest = oc
	title := proto.Text{Translate: "container.chest"}
	kind := generic9x3
	switch {
	case b.Name == "barrel":
		title.Translate = "container.barrel"
	case len(parts) == 2:
		title.Translate, kind = "container.chestDouble", generic9x6
	}
	p.send(&packets.ClientboundOpenScreen{WindowID: oc.id, InventoryType: kind, WindowTitle: title})
	oc.sync.SendAll(oc.m, invSender{p})
	w.containerViewers(parts, st, 1)
	return true
}

// load reads the parts' items into the menu; false when one is gone.
func (oc *openChest) load() bool {
	for i, q := range oc.parts {
		items, ok := oc.w.containerItems(q)
		if !ok {
			return false
		}
		copy(oc.m.Items[i*chestSlots:], items[:])
	}
	return true
}

func (oc *openChest) store() {
	for i, q := range oc.parts {
		var items [chestSlots]item.Stack
		copy(items[:], oc.m.Items[i*chestSlots:(i+1)*chestSlots])
		oc.w.setContainerItems(q, &items)
	}
}

func (p *Player) copyInvToChest(oc *openChest) {
	pm := p.inv.m
	for i := oc.m.Container; i < len(oc.m.Items); i++ {
		oc.m.Items[i] = pm.Items[p.inv.slotOf(oc.m.Slots[i].Inv)]
	}
}

func (p *Player) copyChestToInv(oc *openChest) {
	pm := p.inv.m
	for i := oc.m.Container; i < len(oc.m.Items); i++ {
		pm.Items[p.inv.slotOf(oc.m.Slots[i].Inv)] = oc.m.Items[i]
	}
}

// chestClick handles a click in the open container window. Holds inv.mu.
func (p *Player) chestClick(m *packets.ServerboundContainerClick, changed map[int]item.Hashed, carried item.Hashed) {
	oc := p.inv.chest
	if oc == nil || oc.id != m.WindowID || oc.custom != nil {
		return
	}
	if !oc.load() {
		p.closeChestLocked(true)
		return
	}
	p.copyInvToChest(oc)
	oc.sync.Click(oc.m, m.StateID, int(m.Slot), int32(m.MouseButton), m.Mode, changed, carried, menuPlayer{p}, p.GameMode() == api.Spectator, invSender{p})
	oc.store()
	p.copyChestToInv(oc)
}

// closeChestLocked closes the container window, returning the carried
// stack to the inventory. Holds inv.mu.
func (p *Player) closeChestLocked(notify bool) {
	oc := p.inv.chest
	if oc == nil {
		return
	}
	p.inv.chest = nil
	oc.leaveCustom(p)
	if oc.w != nil && len(oc.parts) > 0 {
		oc.w.containerViewers(oc.parts, oc.w.w.Block(oc.pos.X, oc.pos.Y, oc.pos.Z), -1)
	}
	if c := oc.m.Carried; !c.IsEmpty() {
		if rest := p.placeBack(c); !rest.IsEmpty() {
			p.dropItem(rest, false)
		}
	}
	if notify {
		p.send(&packets.ClientboundContainerClose{WindowID: oc.id})
	}
}

// takeContainer unpacks loot and removes the block entity at pos,
// returning a chest-like container's contents (dropped when broken).
func (w *World) takeContainer(pos api.BlockPos, breaker *Player) []item.Stack {
	c := w.chunkAt(pos)
	if c == nil {
		return nil
	}
	c.RLock()
	has := blockEntityAt(c, pos) >= 0
	c.RUnlock()
	if !has {
		return nil
	}
	w.unpackLoot(pos, breaker)
	items, _ := w.containerItems(pos)
	c.RemoveBlockEntity(pos.X&15, pos.Y, pos.Z&15)
	var out []item.Stack
	for _, s := range items {
		if !s.IsEmpty() {
			out = append(out, s)
		}
	}
	return out
}
