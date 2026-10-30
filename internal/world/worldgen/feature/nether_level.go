package feature

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
)

// genHeight is the generation depth that height providers resolve
// below_top against: the noise settings' height (nether and end 128), not
// the dimension height (256). Observed: with 256, nether quartz and gold
// ores matched 0.4% of the reference positions; with 128 see
// docs/vanilla-parity.md "Nether and End features".
func (lv *Level) genHeight() int {
	switch lv.rg.Dim() {
	case world.Nether, world.End:
		return 128
	}
	return lv.MaxY() - lv.MinY()
}

// tickSink is implemented by regions that keep scheduled ticks: the
// pipeline's StageContext and the probe mirrors in tests.
type tickSink interface {
	ScheduleTick(x, y, z int, typ string, delay, priority int32, fluid bool) bool
}

// scheduleFluidTick is WorldGenLevel.scheduleTick(pos, fluid, delay): the
// tick is kept in the proto-chunk holding p and runs once the chunk is
// live. typ is the fluid id ("minecraft:water", "minecraft:lava").
func (lv *Level) scheduleFluidTick(p Pos, typ string, delay int) {
	if s, ok := lv.rg.(tickSink); ok {
		s.ScheduleTick(p.X, p.Y, p.Z, typ, int32(delay), 0, true)
	}
}

// scheduleBlockTick is WorldGenLevel.scheduleTick(pos, block, delay).
func (lv *Level) scheduleBlockTick(p Pos, typ string, delay int) {
	if s, ok := lv.rg.(tickSink); ok {
		s.ScheduleTick(p.X, p.Y, p.Z, typ, int32(delay), 0, false)
	}
}

// fluidID is the fluid of a liquid block state as a registry id, "" when
// the state holds no fluid.
func fluidID(s data.StateID) string {
	switch fluidOf(s) {
	case "water":
		return "minecraft:water"
	case "flowing_water":
		return "minecraft:flowing_water"
	case "lava":
		return "minecraft:lava"
	case "flowing_lava":
		return "minecraft:flowing_lava"
	}
	return ""
}

// addEntity stores entity NBT with the proto-chunk holding p when that is
// the decorated chunk (vanilla adds generated entities to the region's
// centre chunk).
func (lv *Level) addEntity(p Pos, e nbt.Compound) {
	if lv.sc == nil {
		return
	}
	pc := lv.sc.Chunk()
	if int32(p.X>>4) != pc.X || int32(p.Z>>4) != pc.Z {
		return
	}
	pc.Entities = append(pc.Entities, e)
}

// genUUID is a deterministic kremenmc entity UUID for generated entities
// (vanilla's are random).
func genUUID(x, y, z int, k uint64) []int32 {
	h := uint64(mth.GetSeed(int32(x), int32(y), int32(z)))*31 + k
	return []int32{int32(h >> 32), int32(h), int32(h>>16)&^0xf000 | 0x4000, int32(h>>40) | -0x80000000}
}
