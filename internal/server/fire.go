package server

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Fire placement (BaseFireBlock.getState / canBePlacedAt), checked against
// the reference by the firestate and use scenes in
// testdata/explosion_scenes.txt. Fire spread, burn-out and ageing are not
// simulated; placing fire still draws the scheduled-tick delay
// (nextInt(10)) from the level random like the reference.

var fireStates = sync.OnceValue(func() (st struct{ fire, soul data.StateID }) {
	st.fire, _ = data.DefaultState("fire")
	st.soul, _ = data.DefaultState("soul_fire")
	return st
})

func fireCanBurn(s data.StateID) bool { return exFlags(s)&exCanBurn != 0 }

func fireSturdyUp(s data.StateID) bool { return exFlags(s)&exSturdyUp != 0 }

// horizontal and up neighbours with their fire side property.
var fireSides = [...]struct {
	dx, dy, dz int
	prop       string
}{{0, 0, -1, "north"}, {1, 0, 0, "east"}, {0, 0, 1, "south"}, {-1, 0, 0, "west"}, {0, 1, 0, "up"}}

// fireStateAt is BaseFireBlock.getState: soul fire on soul fire base
// blocks, else fire attached to the flammable neighbours when the block
// below neither burns nor has a sturdy top.
func fireStateAt(src exBlocks, p api.BlockPos) data.StateID {
	fs := fireStates()
	below := src.Block(p.X, p.Y-1, p.Z)
	if exBlockOf(below).soulBase {
		return fs.soul
	}
	s := fs.fire
	if fireCanBurn(below) || fireSturdyUp(below) {
		return s
	}
	for _, d := range fireSides {
		if fireCanBurn(src.Block(p.X+d.dx, p.Y+d.dy, p.Z+d.dz)) {
			s, _ = data.With(s, d.prop, "true")
		}
	}
	return s
}

// fireSurvives is canSurvive of the state fireStateAt picks.
func fireSurvives(src exBlocks, p api.BlockPos) bool {
	below := src.Block(p.X, p.Y-1, p.Z)
	if exBlockOf(below).soulBase {
		return true
	}
	if fireSturdyUp(below) {
		return true
	}
	for _, d := range [...][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if fireCanBurn(src.Block(p.X+d[0], p.Y+d[1], p.Z+d[2])) {
			return true
		}
	}
	return false
}

// canPlaceFire is BaseFireBlock.canBePlacedAt without the portal case
// (portalCanLight in portal.go).
func canPlaceFire(src exBlocks, p api.BlockPos) bool {
	if !data.IsAir(src.Block(p.X, p.Y, p.Z)) {
		return false
	}
	return fireSurvives(src, p)
}

// fireOnPlace is the random draw FireBlock.onPlace makes for its first
// scheduled tick (soul fire schedules nothing).
func fireOnPlace(s data.StateID, rnd rng.Source) {
	if data.Canonical().BlockID(s) == data.Canonical().BlockID(fireStates().fire) {
		rnd.NextIntn(10)
	}
}

type igniter uint8

const (
	igniteFlint igniter = iota
	igniteFireCharge
)

func igniterOf(name string) (igniter, bool) {
	switch name {
	case "minecraft:flint_and_steel":
		return igniteFlint, true
	case "minecraft:fire_charge":
		return igniteFireCharge, true
	}
	return 0, false
}

// igniteOutcome is what using an igniter on a block did.
type igniteOutcome uint8

const (
	ignitePass   igniteOutcome = iota // nothing to do; fall through to placement
	igniteFail                        // refused (client prediction rolled back)
	igniteLit                         // lit a block or placed fire: item used
	ignitePrimed                      // primed TNT: item used
)

// igniteLevel is the world access igniting needs.
type igniteLevel interface {
	exBlocks
	setBlock(p api.BlockPos, s data.StateID)
	// primeTNT primes the TNT block at p (leaving the block) and reports
	// success.
	primeTNT(p api.BlockPos) bool
	playSound(p api.Vec3, name string, pitch float32)
}

// igniteOn is using flint and steel or a fire charge on face of clicked:
// TntBlock.useItemOn (skipped while sneaking), then the item's useOn:
// light a campfire, candle or candle cake, else place fire on the face.
func igniteOn(lv igniteLevel, kind igniter, clicked api.BlockPos, face api.BlockFace, sneaking bool, rnd rng.Source) igniteOutcome {
	s := lv.Block(clicked.X, clicked.Y, clicked.Z)
	if !sneaking && data.Canonical().BlockID(s) == tntBlock() {
		if lv.primeTNT(clicked) {
			lv.setBlock(clicked, data.Air)
			return ignitePrimed
		}
		return ignitePass
	}
	at := api.Vec3{X: float64(clicked.X) + 0.5, Y: float64(clicked.Y) + 0.5, Z: float64(clicked.Z) + 0.5}
	if lit, ok := lightable(s); ok {
		igniteSound(lv, kind, at, rnd)
		lv.setBlock(clicked, lit)
		return igniteLit
	}
	o := faceOffsets[face]
	target := clicked.Add(o[0], o[1], o[2])
	if !canPlaceFire(lv, target) && !portalCanLight(lv, target) {
		return igniteFail
	}
	at = api.Vec3{X: float64(target.X) + 0.5, Y: float64(target.Y) + 0.5, Z: float64(target.Z) + 0.5}
	igniteSound(lv, kind, at, rnd)
	f := fireStateAt(lv, target)
	lv.setBlock(target, f)
	if lightPortal(lv, target) {
		return igniteLit
	}
	fireOnPlace(f, rnd)
	return igniteLit
}

func igniteSound(lv igniteLevel, kind igniter, at api.Vec3, rnd rng.Source) {
	if kind == igniteFlint {
		lv.playSound(at, "minecraft:item.flintandsteel.use", rnd.NextFloat()*0.4+0.8)
		return
	}
	a := rnd.NextFloat()
	b := rnd.NextFloat()
	lv.playSound(at, "minecraft:item.firecharge.use", (a-b)*0.2+1)
}

// lightable returns the lit state of an unlit, dry campfire, candle or
// candle cake.
func lightable(s data.StateID) (data.StateID, bool) {
	b := data.BlockOf(s)
	if b == nil {
		return 0, false
	}
	n := b.Name
	if n != "campfire" && n != "soul_campfire" && !strings.HasSuffix(n, "candle") && !strings.HasSuffix(n, "candle_cake") {
		return 0, false
	}
	if v, _ := data.Canonical().Value(s, "lit"); v != "false" {
		return 0, false
	}
	if v, ok := data.Canonical().Value(s, "waterlogged"); ok && v == "true" {
		return 0, false
	}
	lit, ok := data.With(s, "lit", "true")
	return lit, ok
}
