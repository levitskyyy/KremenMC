package feature

import (
	"encoding/json"
	"fmt"
)

// singleBlockPillar is single_block_pillar (basalt pillars in basalt
// deltas). From kremenmc.probe.feat_nether.Place draw and write logs: from
// the origin along direction (up or down), while can_replace holds, it
// draws one nextFloat; at or above chance_to_continue (default 1) it stops,
// otherwise it places the block (it stops after the draw when the position
// is outside the build height). cap_feature then runs at the last placed
// block; nothing placed means no cap and false.
type singleBlockPillar struct {
	block      StateProvider
	canReplace Predicate
	chance     float32
	dir        int
	cap        *Placed
}

func (f *singleBlockPillar) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	var last Pos
	placed := false
	for q := p; f.canReplace.Test(lv, q); q.Y += f.dir {
		if r.NextFloat() >= f.chance || q.Y < lv.MinY() || q.Y >= lv.MaxY() {
			break
		}
		lv.SetBlock(q, f.block.State(lv, r, q))
		last, placed = q, true
	}
	if placed && f.cap != nil {
		f.cap.place(lv, r, last, nil)
	}
	return placed
}

// patchySquare is projected_random_patchy_square. From the probe logs over
// sizes 1-4: cells x outer, z inner over [-size, size]; each draws
// nextInt(size²+1) and is kept when draw + |dx·dz| <= size² (corners are
// rarest). A kept cell starts at origin level and moves down while the
// block below matches project_through, at most max_projection_height times
// (at least once: 0 behaves as 1), and places the block where it stopped;
// a rule_based block without a matching rule places nothing.
type patchySquare struct {
	block   StateProvider
	through Predicate
	size    int
	maxProj int
}

func (f *patchySquare) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	s := f.size
	for dx := -s; dx <= s; dx++ {
		for dz := -s; dz <= s; dz++ {
			if int(r.NextIntn(int32(s*s+1)))+abs(dx*dz) > s*s {
				continue
			}
			q := p.Add(Pos{dx, 0, dz})
			for i := 0; i < max(f.maxProj, 1); i++ {
				below := q.Add(Pos{0, -1, 0})
				if !f.through.Test(lv, below) {
					break
				}
				q = below
			}
			if rs, ok := f.block.(*ruleState); ok {
				if st, ok := rs.pick(lv, r, q); ok {
					lv.SetBlock(q, st)
				}
			} else {
				lv.SetBlock(q, f.block.State(lv, r, q))
			}
		}
	}
	return true
}

func init() {
	featureTypes["single_block_pillar"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Block      json.RawMessage `json:"block"`
			CanReplace json.RawMessage `json:"can_replace"`
			Chance     *float32        `json:"chance_to_continue"`
			Direction  string          `json:"direction"`
			Cap        json.RawMessage `json:"cap_feature"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("single_block_pillar: %w", err)
		}
		f := &singleBlockPillar{chance: 1}
		if raw.Chance != nil {
			f.chance = *raw.Chance
		}
		switch raw.Direction {
		case "up":
			f.dir = 1
		case "down":
			f.dir = -1
		default:
			return nil, fmt.Errorf("single_block_pillar: direction %q", raw.Direction)
		}
		var err error
		if f.block, err = rg.parseStateProvider(raw.Block); err != nil {
			return nil, err
		}
		if f.canReplace, err = rg.parsePredicate(raw.CanReplace); err != nil {
			return nil, err
		}
		if len(raw.Cap) > 0 {
			if f.cap, err = rg.parsePlaced(raw.Cap); err != nil {
				return nil, err
			}
		}
		return f, nil
	}
	featureTypes["projected_random_patchy_square"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Block   json.RawMessage `json:"block"`
			Through json.RawMessage `json:"project_through"`
			Size    int             `json:"size"`
			MaxProj int             `json:"max_projection_height"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("projected_random_patchy_square: %w", err)
		}
		f := &patchySquare{size: raw.Size, maxProj: raw.MaxProj}
		var err error
		if f.block, err = rg.parseStateProvider(raw.Block); err != nil {
			return nil, err
		}
		if f.through, err = rg.parsePredicate(raw.Through); err != nil {
			return nil, err
		}
		return f, nil
	}
}
