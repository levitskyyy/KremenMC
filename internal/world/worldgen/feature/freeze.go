package feature

import (
	"encoding/json"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
)

// freezeTopLayer is freeze_top_layer: over the 16x16 columns from the
// origin, ice on still water just below the MOTION_BLOCKING height and a
// snow layer on top, both where the biome at the top position is cold
// enough there. No random draws (feat_env.Place).
type freezeTopLayer struct {
	cannotSupport, override blockSet
}

// freezeSeaLevel is the overworld noise generator sea level; tests set the
// GameTest flat world's.
var freezeSeaLevel int32 = 63

var (
	freezeClimatesOnce sync.Once
	freezeClimates     []surface.BiomeClimate
	freezeClimatesOK   []bool
)

func freezeLoadClimates(rg *registry) {
	freezeClimatesOnce.Do(func() {
		reg := data.Canonical()
		n := numBiomes()
		freezeClimates = make([]surface.BiomeClimate, n)
		freezeClimatesOK = make([]bool, n)
		for b := range n {
			c, err := surface.LoadBiomeClimate(rg.pack, "minecraft:"+reg.BiomeName(data.BiomeID(b)))
			freezeClimates[b], freezeClimatesOK[b] = c, err == nil
		}
	})
}

func freezeColdAt(b data.BiomeID, p Pos) bool {
	if int(b) < 0 || int(b) >= len(freezeClimates) || !freezeClimatesOK[b] {
		return false
	}
	return freezeClimates[b].ColdEnoughToSnow(int32(p.X), int32(p.Y), int32(p.Z), freezeSeaLevel)
}

func (f *freezeTopLayer) Place(c *placeCtx, _ *Random, o Pos) bool {
	lv := c.lv
	water, _ := data.DefaultState("water")
	snow, _ := data.DefaultState("snow")
	ice, _ := data.DefaultState("ice")
	reg := data.Canonical()
	for i := range 16 {
		for j := range 16 {
			x, z := o.X+i, o.Z+j
			y := lv.Height(world.MotionBlocking, x, z)
			top, below := Pos{x, y, z}, Pos{x, y - 1, z}
			b := lv.Biome(top)
			if below.Y >= lv.MinY() && below.Y < lv.MaxY() && lv.Block(below) == water && freezeColdAt(b, below) {
				lv.SetBlock(below, ice)
			}
			if top.Y >= lv.MinY() && top.Y < lv.MaxY() && data.IsAir(lv.Block(top)) && freezeColdAt(b, top) && f.snowSurvives(lv.Block(below)) {
				lv.SetBlock(top, snow)
				st := lv.Block(below)
				if _, ok := reg.Value(st, "snowy"); ok {
					s2, _ := reg.With(st, "snowy", "true")
					lv.SetBlock(below, s2)
				}
			}
		}
	}
	return true
}

// snowSurvives is the snow layer survival test on the block below.
func (f *freezeTopLayer) snowSurvives(below data.StateID) bool {
	if f.cannotSupport.has(below) {
		return false
	}
	if f.override.has(below) {
		return true
	}
	if blockName(below) == "snow" {
		v, _ := data.Canonical().Value(below, "layers")
		return v == "8"
	}
	// Full top face of the collision shape: sturdy faces plus leaves
	// (full collision, not sturdy). Measured on stone, grass, podzol,
	// sand and leaves only.
	return faceSturdy(below, 1) || data.Flags(below)&data.FlagLeaves != 0
}

func init() {
	featureTypes["freeze_top_layer"] = func(rg *registry, _ json.RawMessage) (Feature, error) {
		freezeLoadClimates(rg)
		cs, err := rg.tag("minecraft:cannot_support_snow_layer")
		if err != nil {
			return nil, err
		}
		ov, err := rg.tag("minecraft:support_override_snow_layer")
		if err != nil {
			return nil, err
		}
		return &freezeTopLayer{cs, ov}, nil
	}
}
