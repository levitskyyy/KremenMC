package gen

import (
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// ScheduleTick records a scheduled block or fluid tick at absolute
// coordinates in the proto-chunk holding it (vanilla ProtoChunkTicks), with
// the write-zone rules of SetBlock. A second tick of the same type at the
// same position is dropped, like the reference's per-position set. Delays
// count from the moment the chunk goes live.
func (c *StageContext) ScheduleTick(x, y, z int, typ string, delay, priority int32, fluid bool) bool {
	if !c.InWriteZone(x, z) || !c.p.cfg.Dim.Contains(y) {
		return false
	}
	i, _, _ := c.idx(int32(x>>4), int32(z>>4))
	pc := c.grid[i]
	if pc.tickIdx == nil {
		pc.tickIdx = map[tickKey]struct{}{}
		for _, t := range pc.BlockTicks {
			pc.tickIdx[tickKey{t.X, t.Y, t.Z, t.Type, false}] = struct{}{}
		}
		for _, t := range pc.FluidTicks {
			pc.tickIdx[tickKey{t.X, t.Y, t.Z, t.Type, true}] = struct{}{}
		}
	}
	k := tickKey{int32(x), int32(y), int32(z), typ, fluid}
	if _, dup := pc.tickIdx[k]; dup {
		return false
	}
	pc.tickIdx[k] = struct{}{}
	l := &pc.BlockTicks
	if fluid {
		l = &pc.FluidTicks
	}
	*l = append(*l, Tick{X: int32(x), Y: int32(y), Z: int32(z), Type: typ, Delay: delay, Priority: priority})
	return true
}

type tickKey struct {
	x, y, z int32
	typ     string
	fluid   bool
}

// MarkPostProcess marks a position for post-processing on promotion
// (vanilla ChunkAccess.markPosForPostprocessing), with the write-zone
// rules of SetBlock.
func (c *StageContext) MarkPostProcess(x, y, z int) bool {
	if !c.InWriteZone(x, z) || !c.p.cfg.Dim.Contains(y) {
		return false
	}
	i, _, _ := c.idx(int32(x>>4), int32(z>>4))
	pc := c.grid[i]
	si := (y - pc.Dim.MinY) >> 4
	v := int16(x&15 | (y&15)<<4 | (z&15)<<8)
	for _, o := range pc.PostProcessing[si] {
		if o == v {
			return false
		}
	}
	pc.MarkPostProcess(x, y, z)
	return true
}

// pendingTicks converts the chunk's generation ticks for the live world:
// the scheduled block and fluid ticks, then one fluid tick per
// post-processing position that holds a fluid (vanilla post-processing
// runs the fluid's shape update, which schedules a tick after the fluid's
// delay: water 5, lava 30, lava in the nether 10). Observed on the
// reference worlds: post-processed water carries t=5, generation-scheduled
// springs t=0.
func (c *ProtoChunk) pendingTicks() []world.PendingTick {
	var out []world.PendingTick
	for _, t := range c.BlockTicks {
		out = append(out, world.PendingTick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority})
	}
	for _, t := range c.FluidTicks {
		out = append(out, world.PendingTick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority, Fluid: true})
	}
	reg := data.Canonical()
	for si, l := range c.PostProcessing {
		y0 := c.Dim.MinY + si<<4
		for _, v := range l {
			x, y, z := int(v&15), y0+int(v>>4&15), int(v>>8&15)
			st := c.Chunk.Block(x, y, z)
			if !reg.HasFluid(st) {
				continue
			}
			typ, delay := "minecraft:water", int32(5)
			if b := reg.BlockOf(st); b != nil && strings.TrimPrefix(b.Name, "minecraft:") == "lava" {
				typ, delay = "minecraft:lava", 30
				if c.Dim == world.Nether {
					delay = 10
				}
			}
			if lvl, ok := reg.Value(st, "level"); ok && lvl != "0" {
				typ = strings.Replace(typ, "minecraft:", "minecraft:flowing_", 1)
			}
			out = append(out, world.PendingTick{X: c.X<<4 | int32(x), Y: int32(y), Z: c.Z<<4 | int32(z), Type: typ, Delay: delay, Fluid: true})
		}
	}
	return out
}
