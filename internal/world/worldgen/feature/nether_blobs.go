package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// replaceBlobs is netherrack_replace_blobs (basalt and blackstone blobs in
// basalt deltas). Inferred from kremenmc.probe.feat_nether.Place read logs:
// the origin moves down (from at most the top of the world) to the first
// target block, else false without draws; three radius samples give the x,
// y and z spans; every position of withinManhattan(origin, spans) up to
// Manhattan distance max(spans) is read and replaced when it is the target.
type replaceBlobs struct {
	target, state data.StateID
	radius        IntProvider
}

func (f *replaceBlobs) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	p.Y = min(p.Y, lv.MaxY()-1)
	for p.Y > lv.MinY() && lv.Block(p) != f.target {
		p.Y--
	}
	if lv.Block(p) != f.target {
		return false
	}
	rx, ry, rz := int(f.radius.Sample(r)), int(f.radius.Sample(r)), int(f.radius.Sample(r))
	limit := max(rx, ry, rz)
	placed := false
	withinManhattan(rx, ry, rz, func(dx, dy, dz int) bool {
		if abs(dx)+abs(dy)+abs(dz) > limit {
			return false
		}
		q := p.Add(Pos{dx, dy, dz})
		if lv.Block(q) == f.target {
			lv.SetBlock(q, f.state)
			placed = true
		}
		return true
	})
	return placed
}

// withinManhattan visits offsets with |x| <= sx, |y| <= sy, |z| <= sz by
// growing Manhattan distance; inside a distance x ascends, then y, then z
// (positive before negative). Order observed in the probe read logs. fn
// returning false stops the walk.
func withinManhattan(sx, sy, sz int, fn func(x, y, z int) bool) {
	for d := 0; d <= sx+sy+sz; d++ {
		for x := -min(d, sx); x <= min(d, sx); x++ {
			ry := d - abs(x)
			for y := -min(ry, sy); y <= min(ry, sy); y++ {
				z := ry - abs(y)
				if z > sz {
					continue
				}
				if !fn(x, y, z) {
					return
				}
				if z != 0 && !fn(x, y, -z) {
					return
				}
			}
		}
	}
}

func init() {
	featureTypes["netherrack_replace_blobs"] = func(_ *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Target json.RawMessage `json:"target"`
			State  json.RawMessage `json:"state"`
			Radius json.RawMessage `json:"radius"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("netherrack_replace_blobs: %w", err)
		}
		f := &replaceBlobs{}
		var err error
		if f.target, err = parseBlockState(raw.Target); err != nil {
			return nil, fmt.Errorf("netherrack_replace_blobs: %w", err)
		}
		if f.state, err = parseBlockState(raw.State); err != nil {
			return nil, fmt.Errorf("netherrack_replace_blobs: %w", err)
		}
		if f.radius, err = parseInt(raw.Radius); err != nil {
			return nil, fmt.Errorf("netherrack_replace_blobs: %w", err)
		}
		return f, nil
	}
}
