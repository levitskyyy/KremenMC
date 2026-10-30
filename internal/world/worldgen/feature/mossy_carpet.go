package feature

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// regionRandom is WorldGenRegion.getRandom: one xoroshiro per decorated
// chunk, positional factory of the level seed, hash
// "minecraft:worldgen_region_random", at (chunk min x, 0, chunk min z)
// (state read by reflection in kremenmc.probe.feat_region.Region, 3/3
// chunks). It is shared by everything that asks for it during the chunk.
func (lv *Level) regionRandom() rng.Source {
	if lv.rr == nil {
		var cx, cz int32
		if lv.sc != nil {
			cx, cz = lv.sc.Pos().X, lv.sc.Pos().Z
		}
		f := rng.NewXoroshiro(lv.worldSeed()).ForkPositional().FromHashOf("minecraft:worldgen_region_random").ForkPositional()
		lv.rr = f.At(cx*16, 0, cz*16)
	}
	return lv.rr
}

var carpetSides = [4]struct {
	name string
	d    Pos
	face int // dirOrder index of the neighbour face toward the carpet
}{{"north", Pos{0, 0, -1}, 3}, {"east", Pos{1, 0, 0}, 4}, {"south", Pos{0, 0, 1}, 2}, {"west", Pos{-1, 0, 0}, 5}}

// carpetWall reports a neighbour a carpet side attaches to: a full cube
// (leaves included) or a sturdy face toward the carpet.
func carpetWall(n data.StateID, face int) bool {
	return data.Flags(n)&data.FlagFullCube != 0 || faceSturdy(n, face)
}

var replaceableBlocks = sync.OnceValue(func() blockSet {
	rg, err := theRegistry()
	if err != nil {
		return nil
	}
	s, _ := rg.tag("replaceable")
	return s
})

func init() {
	survival["pale_moss_carpet"] = func(lv *Level, _ data.StateID, p Pos) bool {
		return !data.IsAir(lv.Block(p.Add(Pos{0, -1, 0})))
	}
}

// placeMossyCarpet is MossyCarpetBlock.placeAt as a simple_block places it,
// fitted to kremenmc.probe.feat_geode.Geode carpet scenes with the level
// random logged: it needs a non-air block below; every side whose
// neighbour is a wall (carpetWall) is low. When the block above is air or #replaceable, each low side whose upper
// neighbour is a wall too draws nextBoolean from the region random; the
// sides drawn true go on a carpet above (bottom=false, low) and become tall
// on the base.
func placeMossyCarpet(lv *Level, st data.StateID, p Pos) {
	reg := data.Canonical()
	base, _ := reg.With(st, "bottom", "true")
	var low [4]bool
	for i, s := range carpetSides {
		v := "none"
		if carpetWall(lv.Block(p.Add(s.d)), s.face) {
			v, low[i] = "low", true
		}
		base, _ = reg.With(base, s.name, v)
	}
	lv.SetBlock(p, base)
	up := p.Add(Pos{0, 1, 0})
	if b := lv.Block(up); !data.IsAir(b) && !replaceableBlocks().has(b) {
		return
	}
	r := lv.regionRandom()
	top, _ := reg.With(base, "bottom", "false")
	any := false
	for i, s := range carpetSides {
		v := "none"
		if low[i] && carpetWall(lv.Block(up.Add(s.d)), s.face) && r.NextBool() {
			v, any = "low", true
			base, _ = reg.With(base, s.name, "tall")
		}
		top, _ = reg.With(top, s.name, v)
	}
	if !any {
		return
	}
	lv.SetBlock(up, top)
	lv.SetBlock(p, base)
}
