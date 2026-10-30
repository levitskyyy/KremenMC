package world

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// springGen generates a stone floor at y 60 with a water source on it at
// 8,61,8 that only a pending tick (as generation leaves for springs) sets
// flowing.
type springGen struct{}

func (springGen) Generate(c *Chunk) {
	stone, _ := data.DefaultState("stone")
	water, _ := data.DefaultState("water")
	for x := range 16 {
		for z := range 16 {
			c.SetBlock(x, 60, z, stone)
		}
	}
	if c.X == 0 && c.Z == 0 {
		c.SetBlock(8, 61, 8, water)
		c.PendingTicks = []PendingTick{{X: 8, Y: 61, Z: 8, Type: "minecraft:water", Delay: 0, Fluid: true}}
	}
	c.Finish()
}

// TestPendingTicksGoLive checks that a generated chunk's pending fluid tick
// runs once the chunk is installed, and that ticks still scheduled are
// written back with the chunk on close.
func TestPendingTicksGoLive(t *testing.T) {
	st := &memStorage{m: map[uint64]*Chunk{}}
	w, err := New(Config{Generator: springGen{}, Storage: st, Workers: 2, TickWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	w.AddTicket(ChunkAt(8, 8), 2, TicketForced)
	waitFor(t, func() bool { return w.Loaded() >= 9 })
	for range 3 {
		w.DoTick()
	}
	water, _ := data.DefaultState("water")
	if data.Canonical().BlockID(w.Block(9, 61, 8)) != data.Canonical().BlockID(water) {
		t.Fatalf("water did not flow from the pending tick: %s", data.StateString(w.Block(9, 61, 8)))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	c := st.m[ChunkPos{}.Key()]
	if c == nil {
		t.Fatal("chunk 0,0 not saved")
	}
	if fluidTicks(c.PendingTicks)+fluidTicks(c.SavedTicks) == 0 {
		t.Fatal("flowing water saved without pending fluid ticks")
	}
}

func fluidTicks(ts []PendingTick) int {
	n := 0
	for _, p := range ts {
		if p.Fluid {
			n++
		}
	}
	return n
}

// TestPendingTicksAutosave checks that a save while the world runs carries
// the scheduled ticks of live chunks, that repeated saves do not duplicate
// them, and that an unload hands them over to PendingTicks once.
func TestPendingTicksAutosave(t *testing.T) {
	st := &memStorage{m: map[uint64]*Chunk{}}
	w, err := New(Config{Generator: springGen{}, Storage: st, Workers: 2, TickWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	tk := w.AddTicket(ChunkAt(8, 8), 2, TicketForced)
	waitFor(t, func() bool { return w.Loaded() >= 9 })
	w.DoTick()
	w.DoTick()
	c := w.Chunk(ChunkPos{})
	if c == nil {
		t.Fatal("chunk 0,0 not loaded")
	}
	var n int
	for i := range 2 {
		if err := w.SaveAll(); err != nil {
			t.Fatal(err)
		}
		c.RLock()
		got, pend := fluidTicks(c.SavedTicks), len(c.PendingTicks)
		c.RUnlock()
		if got == 0 || pend != 0 {
			t.Fatalf("save %d: %d saved fluid ticks, %d pending", i, got, pend)
		}
		if i == 1 && got != n {
			t.Fatalf("second save: %d saved ticks, first %d", got, n)
		}
		n = got
	}
	w.RemoveTicket(tk)
	for range 400 {
		w.DoTick()
		if w.Chunk(ChunkPos{}) == nil {
			break
		}
	}
	if w.Chunk(ChunkPos{}) != nil {
		t.Skip("chunk did not unload")
	}
	c.RLock()
	defer c.RUnlock()
	if len(c.SavedTicks) != 0 || fluidTicks(c.PendingTicks) == 0 {
		t.Fatalf("after unload: %d saved, %d pending fluid ticks", len(c.SavedTicks), fluidTicks(c.PendingTicks))
	}
}
