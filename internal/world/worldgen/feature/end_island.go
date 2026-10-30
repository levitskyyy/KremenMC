package feature

import (
	"encoding/json"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// endIsland is end_island (small end islands). From
// kremenmc.probe.feat_nether.Place draw and write logs: radius f = 4 +
// nextInt(3); layers from the origin downwards while f > 0.5, each writing
// end stone without reads at x, z in [floor(-f), ceil(f)] with x²+z² <=
// (f+1)², then f -= nextInt(2) + 0.5.
type endIsland struct{}

var endStone, _ = data.DefaultState("end_stone")

func (endIsland) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	f := float32(r.NextIntn(3)) + 4
	for y := 0; f > 0.5; y-- {
		lo, hi := int(math.Floor(float64(-f))), int(math.Ceil(float64(f)))
		for x := lo; x <= hi; x++ {
			for z := lo; z <= hi; z++ {
				if float32(x*x+z*z) <= (f+1)*(f+1) {
					lv.SetBlock(p.Add(Pos{x, y, z}), endStone)
				}
			}
		}
		f -= float32(r.NextIntn(2)) + 0.5
	}
	return true
}

// endPlatform is end_platform, the arrival platform: over x, z within 2 of
// the origin (z outer), obsidian one below the origin and air in the three
// blocks from the origin up, each written only when the block differs (the
// probe reads every cell first).
type endPlatform struct{}

func (endPlatform) Place(c *placeCtx, _ *Random, p Pos) bool {
	lv := c.lv
	for dz := -2; dz <= 2; dz++ {
		for dx := -2; dx <= 2; dx++ {
			for dy := -1; dy < 3; dy++ {
				q := p.Add(Pos{dx, dy, dz})
				want := airState
				if dy == -1 {
					want = obsidianState
				}
				if lv.Block(q) != want {
					lv.SetBlock(q, want)
				}
			}
		}
	}
	return true
}

func init() {
	featureTypes["end_island"] = func(*registry, json.RawMessage) (Feature, error) { return endIsland{}, nil }
	featureTypes["end_platform"] = func(*registry, json.RawMessage) (Feature, error) { return endPlatform{}, nil }
}
