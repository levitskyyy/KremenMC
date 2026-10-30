package feature

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// delta is delta_feature (basalt delta lava pools). From
// kremenmc.probe.feat_nether.Place draw and read logs (flat, pitted and
// open floors): nextDouble < 0.9 enables a rim, whose x and z offsets are
// then sampled from rim_size; then the x and z sizes. Positions q come from
// withinManhattan(size x, 0, size z) and stop beyond Manhattan distance
// max(size x, size z). For each clear q the rim block goes to q (only when
// both rim offsets are non-zero) and the contents to q + rim offset if that
// is clear too. Clear: the block is neither the contents nor
// features_cannot_replace, the block below is not air, the block above is
// air and none of the four horizontal neighbours is air.
type delta struct {
	contents, rim data.StateID
	rimSize, size IntProvider
}

var cannotReplace = sync.OnceValue(func() blockSet {
	rg, err := theRegistry()
	if err != nil {
		return nil
	}
	s, _ := rg.tag("minecraft:features_cannot_replace")
	return s
})

func (f *delta) clear(lv *Level, q Pos) bool {
	st := lv.Block(q)
	if data.Canonical().BlockID(st) == data.Canonical().BlockID(f.contents) || cannotReplace().has(st) {
		return false
	}
	if data.IsAir(lv.Block(q.Add(Pos{0, -1, 0}))) || !data.IsAir(lv.Block(q.Add(Pos{0, 1, 0}))) {
		return false
	}
	for _, d := range [4]Pos{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if data.IsAir(lv.Block(q.Add(d))) {
			return false
		}
	}
	return true
}

func (f *delta) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	rimOn := r.NextDouble() < 0.9
	rx, rz := 0, 0
	if rimOn {
		rx, rz = f.rimSize.Sample(r), f.rimSize.Sample(r)
	}
	sx, sz := f.size.Sample(r), f.size.Sample(r)
	lim := max(sx, sz)
	placed := false
	withinManhattan(sx, 0, sz, func(x, _, z int) bool {
		if abs(x)+abs(z) > lim {
			return false
		}
		q := p.Add(Pos{x, 0, z})
		if !f.clear(lv, q) {
			return true
		}
		if rimOn && rx != 0 && rz != 0 {
			lv.SetBlock(q, f.rim)
			placed = true
		}
		if o := q.Add(Pos{rx, 0, rz}); f.clear(lv, o) {
			lv.SetBlock(o, f.contents)
			placed = true
		}
		return true
	})
	return placed
}

func init() {
	featureTypes["delta_feature"] = func(_ *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Contents json.RawMessage `json:"contents"`
			Rim      json.RawMessage `json:"rim"`
			RimSize  json.RawMessage `json:"rim_size"`
			Size     json.RawMessage `json:"size"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("delta_feature: %w", err)
		}
		f := &delta{}
		var err error
		if f.contents, err = parseBlockState(raw.Contents); err != nil {
			return nil, err
		}
		if f.rim, err = parseBlockState(raw.Rim); err != nil {
			return nil, err
		}
		if f.rimSize, err = parseInt(raw.RimSize); err != nil {
			return nil, err
		}
		if f.size, err = parseInt(raw.Size); err != nil {
			return nil, err
		}
		return f, nil
	}
}
