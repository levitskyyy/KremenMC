package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// levelEventLavaFizz is LevelEvent 1501: lava turned a block into stone,
// cobblestone, obsidian or basalt, or burnt a block away (hiss + smoke).
const levelEventLavaFizz = 1501

// fluidEffect performs the side effects of the fluid simulation in
// internal/world. Runs on region workers.
func (w *World) fluidEffect(_ *world.TickContext, ev world.FluidEvent, x, y, z int, old data.StateID) {
	pos := api.BlockPos{X: x, Y: y, Z: z}
	switch ev {
	case world.FluidDestroy:
		w.dropBlockNoTool(pos, old)
	case world.FluidFizz:
		w.levelEvent(nil, levelEventLavaFizz, pos, 0)
	}
}

func (w *World) fluidConversion(k world.FluidKind) bool {
	if k == world.FluidLava {
		return w.gameRule("lava_source_conversion", false)
	}
	return w.gameRule("water_source_conversion", true)
}

// randomTick is the world's random tick hook; only lava uses it.
func (w *World) randomTick(t *world.TickContext, x, y, z int, s data.StateID) {
	if k, _ := world.FluidOf(s); k == world.FluidLava && isBlock(s, "lava") {
		lavaIgnite(t, x, y, z)
	}
}

// lavaIgnite sets fire near lava (minecraft.wiki "Lava", "Fire"): an air
// block in the 3x1x3 layer above the lava, or in the 5x1x5 layer two
// above when the picked spot of the first layer is open, catches fire when
// next to a flammable block. Which spots are tried and how often is NOT
// verified against the reference.
func lavaIgnite(t *world.TickContext, x, y, z int) {
	r := t.Rand()
	p := api.BlockPos{X: x + r.IntN(3) - 1, Y: y + 1, Z: z + r.IntN(3) - 1}
	if !data.IsAir(t.Block(p.X, p.Y, p.Z)) {
		return
	}
	if !fireNextToFlammable(t, p) {
		p = api.BlockPos{X: x + r.IntN(5) - 2, Y: y + 2, Z: z + r.IntN(5) - 2}
		if !data.IsAir(t.Block(p.X, p.Y, p.Z)) || !fireNextToFlammable(t, p) {
			return
		}
	}
	t.SetBlock(p.X, p.Y, p.Z, fireStateAt(t, p))
}

func fireNextToFlammable(src exBlocks, p api.BlockPos) bool {
	for _, d := range [...][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if fireCanBurn(src.Block(p.X+d[0], p.Y+d[1], p.Z+d[2])) {
			return true
		}
	}
	return false
}
