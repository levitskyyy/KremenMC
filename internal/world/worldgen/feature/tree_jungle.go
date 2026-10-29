package feature

import (
	"encoding/json"
	"math"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/carver"
)

// megaJungleTrunk is mega_jungle_trunk_placer: the giant 2x2 trunk plus
// branches, fitted to kremenmc.probe.feat_tree.Draws (20 isolated trees). The
// first branch level is h-2-nextInt(4); while it is above h/2 a float gives
// the angle, four logs go out at ((int)(1.5+cos·j), i-3+j/2, (int)(1.5+sin·j))
// for j = 1..4 (table sine/cosine: in one probe tree only the table value
// lands two logs on the same block), the foliage sits on the outermost log, and the level
// drops by 2+nextInt(4).
type megaJungleTrunk struct{ trunkHeight }

func (m megaJungleTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	out := giantTrunk{m.trunkHeight}.place(t, r, h, p)
	for i := h - 2 - int(r.NextIntn(4)); i > h/2; i -= 2 + int(r.NextIntn(4)) {
		ang := r.NextFloat() * (float32(math.Pi) * 2)
		cs, sn := float64(carver.Cos(float64(ang))), float64(carver.Sin(float64(ang)))
		var end Pos
		for j := 1; j <= 4; j++ {
			end = p.Add(Pos{int(1.5 + cs*float64(j)), i - 3 + j/2, int(1.5 + sn*float64(j))})
			t.placeLog(r, end)
		}
		out = append(out, attachment{pos: end.Add(Pos{0, 1, 0})})
	}
	return out
}

// jungleFoliage is jungle_foliage_placer, fitted to the same trees: from the
// attachment down, layers whose radius grows by one per layer, keeping
// offsets with dx²+dx+dz²+dz <= r²+r (dx, dz measured from the trunk centre
// as for every double trunk). The top of the double trunk has height+1
// layers starting at radius+1; a branch has 2 or 3 layers (one nextInt(2) per
// branch) starting at radius-1.
type jungleFoliage struct {
	radius, offset IntProvider
	h              int
}

func (j jungleFoliage) foliageHeight(*Random, int) int     { return j.h }
func (j jungleFoliage) foliageRadius(r *Random, _ int) int { return j.radius.Sample(r) }

func (j jungleFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	n, off := fh, 1
	if !a.double {
		n, off = 1+int(r.NextIntn(2)), -1
	}
	o := j.offset.Sample(r)
	for y := o; y >= o-n; y-- {
		leavesRow(t, r, a.pos, fr+off-y, y, a.double, func(_ *Random, ax, _, az, rng int) bool {
			return ax*ax+ax+az*az+az > rng*rng+rng
		})
	}
}

// bushFoliage is bush_foliage_placer (jungle_bush), fitted to
// kremenmc.probe.feat_tree.Draws: layers from offset down to offset-height with
// radius radius-1-y, every layer corner (also the single block of a radius 0
// layer) drawing nextInt(2) and kept only on 1.
type bushFoliage struct {
	radius, offset IntProvider
	h              int
}

func (b bushFoliage) foliageHeight(*Random, int) int     { return b.h }
func (b bushFoliage) foliageRadius(r *Random, _ int) int { return b.radius.Sample(r) }

func (b bushFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	off := b.offset.Sample(r)
	for y := off; y >= off-fh; y-- {
		leavesRow(t, r, a.pos, max(fr+a.radiusOff-1-y, 0), y, a.double, func(r *Random, ax, _, az, rng int) bool {
			return ax == rng && az == rng && r.NextIntn(2) == 0
		})
	}
}

func init() {
	trunkTypes["mega_jungle_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var m megaJungleTrunk
		return m, json.Unmarshal(b, &m.trunkHeight)
	}
	foliageTypes["jungle_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		var p struct {
			Radius, Offset json.RawMessage
			Height         int
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		var j jungleFoliage
		var err error
		if j.radius, err = parseInt(p.Radius); err != nil {
			return nil, err
		}
		if j.offset, err = parseInt(p.Offset); err != nil {
			return nil, err
		}
		j.h = p.Height
		return j, nil
	}
	foliageTypes["bush_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		j, err := foliageTypes["jungle_foliage_placer"](b)
		if err != nil {
			return nil, err
		}
		jf := j.(jungleFoliage)
		return bushFoliage(jf), nil
	}
}
