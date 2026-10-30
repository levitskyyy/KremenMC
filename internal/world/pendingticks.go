package world

import (
	"container/heap"
	"slices"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// PendingTick is a scheduled block or fluid tick kept with a chunk while it
// is not in a region's scheduler: ticks generation scheduled (springs,
// lakes, post-processing of aquifer fluids) and ticks loaded from or saved
// to the Anvil block_ticks / fluid_ticks lists. Delay counts from the moment
// the chunk is installed.
type PendingTick struct {
	X, Y, Z  int32 // absolute
	Type     string
	Delay    int32
	Priority int32
	Fluid    bool
}

func fluidKindOf(id string) FluidKind {
	switch strings.TrimPrefix(id, "minecraft:") {
	case "water", "flowing_water":
		return FluidWater
	case "lava", "flowing_lava":
		return FluidLava
	}
	return FluidNone
}

// adoptPendingTicks moves a freshly installed chunk's pending ticks into
// its region's scheduler. It runs in the region's phase.
func (r *Region) adoptPendingTicks(c *Chunk, now int64) {
	c.mu.Lock()
	ts := c.PendingTicks
	c.PendingTicks = nil
	c.mu.Unlock()
	for _, t := range ts {
		due := now + int64(max(t.Delay, 0))
		if t.Fluid {
			if k := fluidKindOf(t.Type); k != FluidNone {
				r.scheduleFluid(int(t.X), int(t.Y), int(t.Z), due, k)
			}
			continue
		}
		if b, ok := data.Canonical().BlockByName(strings.TrimPrefix(t.Type, "minecraft:")); ok {
			r.schedule(int(t.X), int(t.Y), int(t.Z), due, b.ID)
		}
	}
}

// pendingFor returns the scheduled ticks of chunk c as pending ticks with
// delays relative to now; remove also takes them out of the scheduler.
// It runs on the tick goroutine outside the phases.
func (r *Region) pendingFor(c *Chunk, now int64, remove bool) []PendingTick {
	var out []PendingTick
	keep := r.ticks[:0]
	if !remove {
		keep = nil
	}
	reg := data.Canonical()
	for _, s := range r.ticks {
		if s.x>>4 != c.X || s.z>>4 != c.Z {
			if remove {
				keep = append(keep, s)
			}
			continue
		}
		t := PendingTick{X: s.x, Y: s.y, Z: s.z, Delay: int32(max(s.due-now, 0))}
		if s.fluid != FluidNone {
			t.Fluid = true
			t.Type = "minecraft:water"
			if s.fluid == FluidLava {
				t.Type = "minecraft:lava"
			}
			if remove {
				delete(r.fluidPending, fluidKey(int(s.x), int(s.y), int(s.z)))
			}
		} else if int(s.block) < len(reg.Blocks) {
			t.Type = reg.Blocks[s.block].Name
			if !strings.Contains(t.Type, ":") {
				t.Type = "minecraft:" + t.Type
			}
		} else {
			continue
		}
		out = append(out, t)
	}
	if remove {
		r.ticks = keep
		heap.Init(&r.ticks)
	}
	return out
}

// stashPendingTicks stores the scheduled ticks of chunk c with the chunk
// and drops them from the scheduler, before an unload's save; the last
// save snapshot is superseded.
func (w *World) stashPendingTicks(c *Chunk) {
	r := w.region(c.Pos().Region(), false)
	if r == nil {
		return
	}
	ts := r.pendingFor(c, w.tick.Load(), true)
	c.mu.Lock()
	c.PendingTicks = append(c.PendingTicks, ts...)
	hadSaved := len(c.SavedTicks) > 0
	c.SavedTicks = nil
	c.mu.Unlock()
	if len(ts) > 0 || hadSaved {
		c.MarkDirty()
	}
}

// snapshotPendingTicks copies every region's scheduled ticks into the
// SavedTicks of the live chunks they belong to, for a save (autosave or
// close); the scheduler keeps them. Chunks whose pending ticks are not
// adopted yet keep them in PendingTicks. It must run with tickMu held.
func (w *World) snapshotPendingTicks() {
	now := w.tick.Load()
	w.regMu.RLock()
	rs := make([]*Region, 0, len(w.regions))
	for _, r := range w.regions {
		rs = append(rs, r)
	}
	w.regMu.RUnlock()
	for _, r := range rs {
		for _, c := range r.chunks {
			if slices.Contains(r.stitch, c) {
				continue
			}
			ts := r.pendingFor(c, now, false)
			c.mu.Lock()
			changed := len(ts) > 0 || len(c.SavedTicks) > 0
			c.SavedTicks = ts
			c.mu.Unlock()
			if changed {
				c.MarkDirty()
			}
		}
	}
}
