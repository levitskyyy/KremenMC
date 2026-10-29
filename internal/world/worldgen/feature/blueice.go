package feature

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// seaLevel is WorldGenLevel.getSeaLevel: the noise settings' sea level of
// the dimension (overworld 63, nether 32). Test regions may override it.
func (lv *Level) seaLevel() int {
	if s, ok := lv.rg.(interface{ SeaLevel() int }); ok {
		return s.SeaLevel()
	}
	if lv.rg.Dim() == world.Overworld {
		return 63
	}
	return 32
}

// blueIce is the blue_ice feature. Inferred from the feat_disk.Place probe
// (114/114 cases, sea level overridden through the WorldGenLevel):
//   - false without draws when y > sea level - 1, when neither the origin
//     nor the block below is water, or when no packed ice is above or beside
//     the origin (below does not count);
//   - the origin becomes blue ice (whatever it was), then 200 attempts each
//     draw a = nextInt(5), d = nextInt(6), dy = a - d, b = 3 - max(0, -dy)/2,
//     dx = nextInt(b) - nextInt(b), dz = nextInt(b) - nextInt(b); the target
//     becomes blue ice when it is air, water, packed ice or ice and touches
//     blue ice on one of its six faces.
type blueIce struct{ blue, packed data.StateID }

var blueIceSides = [5]Pos{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
var blueIceFaces = [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (f blueIce) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if p.Y > lv.seaLevel()-1 {
		return false
	}
	if blockName(lv.Block(p)) != "water" && blockName(lv.Block(p.Add(Pos{0, -1, 0}))) != "water" {
		return false
	}
	found := false
	for _, d := range blueIceSides {
		if lv.Block(p.Add(d)) == f.packed {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	lv.SetBlock(p, f.blue)
	for range 200 {
		dy := int(r.NextIntn(5)) - int(r.NextIntn(6))
		b := int32(3 - max(0, -dy)/2)
		dx := int(r.NextIntn(b)) - int(r.NextIntn(b))
		dz := int(r.NextIntn(b)) - int(r.NextIntn(b))
		q := p.Add(Pos{dx, dy, dz})
		st := lv.Block(q)
		if !data.IsAir(st) && st != f.packed {
			if n := blockName(st); n != "water" && n != "ice" {
				continue
			}
		}
		for _, d := range blueIceFaces {
			if lv.Block(q.Add(d)) == f.blue {
				lv.SetBlock(q, f.blue)
				break
			}
		}
	}
	return true
}

func init() {
	featureTypes["blue_ice"] = func(_ *registry, _ json.RawMessage) (Feature, error) {
		blue, _ := data.DefaultState("blue_ice")
		packed, _ := data.DefaultState("packed_ice")
		return blueIce{blue, packed}, nil
	}
}
