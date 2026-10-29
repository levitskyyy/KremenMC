package feature

import (
	"encoding/json"
	"fmt"
)

// disk is the disk feature (sand, gravel, clay, grass/mud discs, ice_patch).
// Inferred from the feat_disk.Place probe: one radius draw, then every block
// with dx²+dz² <= r² and |dy| <= half_height that matches target is set to the
// provider's state. No other draws. The result is independent of iteration
// order for the vanilla configurations: the replaced blocks and the placed
// states are all solid, and only air below or solid/water above are tested.
type disk struct {
	radius IntProvider
	half   int
	target Predicate
	state  StateProvider
}

func (f disk) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	rad := f.radius.Sample(r)
	placed := false
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			if dx*dx+dz*dz > rad*rad {
				continue
			}
			for dy := f.half; dy >= -f.half; dy-- {
				q := Pos{p.X + dx, p.Y + dy, p.Z + dz}
				if !f.target.Test(lv, q) {
					continue
				}
				if lv.SetBlock(q, f.state.State(lv, r, q)) {
					placed = true
				}
			}
		}
	}
	return placed
}

func init() {
	featureTypes["disk"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Radius json.RawMessage `json:"radius"`
			Half   int             `json:"half_height"`
			Target json.RawMessage `json:"target"`
			State  json.RawMessage `json:"state_provider"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("disk: %w", err)
		}
		rad, err := parseInt(raw.Radius)
		if err != nil {
			return nil, fmt.Errorf("disk radius: %w", err)
		}
		tg, err := rg.parsePredicate(raw.Target)
		if err != nil {
			return nil, fmt.Errorf("disk target: %w", err)
		}
		sp, err := rg.parseStateProvider(raw.State)
		if err != nil {
			return nil, fmt.Errorf("disk state_provider: %w", err)
		}
		return disk{rad, raw.Half, tg, sp}, nil
	}
}
