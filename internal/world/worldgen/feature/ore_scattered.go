package feature

import (
	"encoding/json"
	"math"
)

// scatteredOre is scattered_ore: nextInt(size+1) attempts; attempt i is
// offset per axis by round((a-b)*min(i,7)) of two floats, so the first
// attempt is always the origin (inferred from feat_ore.OrePlace draw logs).
type scatteredOre struct{ oreConfig }

func (s scatteredOre) Place(c *placeCtx, r *Random, p Pos) bool {
	n := int(r.NextIntn(int32(s.size + 1)))
	for i := range n {
		spread := float32(min(i, 7))
		var off [3]int
		for a := range off {
			d := (r.NextFloat() - r.NextFloat()) * spread
			off[a] = int(math.Floor(float64(d + 0.5)))
		}
		q := Pos{p.X + off[0], p.Y + off[1], p.Z + off[2]}
		if !c.lv.canWrite(q) {
			continue
		}
		s.tryPlace(c.lv, r, q)
	}
	return true
}

func init() {
	featureTypes["scattered_ore"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		c, err := rg.parseOreConfig(b)
		return scatteredOre{c}, err
	}
}
