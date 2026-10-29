package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// bamboo is the bamboo feature. Inferred from the feat_disk.Place probe:
//   - the origin must be air (false otherwise, no draws); a block below
//     outside #supports_bamboo ends it with true and no draws;
//   - height = nextInt(12)+5, then nextFloat; below probability a podzol disc
//     of radius nextInt(4)+1 (dx²+dz² <= r²) turns the block under each
//     column's WORLD_SURFACE height into podzol when it is in
//     #beneath_bamboo_podzol_replaceable; this runs before the stalk exists;
//   - the stalk fills air upwards from the origin, at most height blocks,
//     stopping at the first non-air block; if it got at least 3 high the
//     block where it stopped (overwritten even when solid) and the two below
//     become the large (stage 1), large and small leaves.
type bamboo struct {
	prob              float32
	supports, beneath blockSet
	stalk, small, big data.StateID
	top, podzol       data.StateID
}

func (f *bamboo) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if !data.IsAir(lv.Block(p)) {
		return false
	}
	if !f.supports.has(lv.Block(p.Add(Pos{0, -1, 0}))) {
		return true
	}
	h := int(r.NextIntn(12)) + 5
	if r.NextFloat() < f.prob {
		rad := int(r.NextIntn(4)) + 1
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if dx*dx+dz*dz > rad*rad {
					continue
				}
				x, z := p.X+dx, p.Z+dz
				q := Pos{x, lv.Height(world.WorldSurface, x, z) - 1, z}
				if f.beneath.has(lv.Block(q)) {
					lv.SetBlock(q, f.podzol)
				}
			}
		}
	}
	m := p
	for range h {
		if !data.IsAir(lv.Block(m)) {
			break
		}
		lv.SetBlock(m, f.stalk)
		m.Y++
	}
	if m.Y-p.Y >= 3 {
		lv.SetBlock(m, f.top)
		lv.SetBlock(m.Add(Pos{0, -1, 0}), f.big)
		lv.SetBlock(m.Add(Pos{0, -2, 0}), f.small)
	}
	return true
}

func bambooState(leaves, stage string) (data.StateID, error) {
	st, ok := data.DefaultState("bamboo")
	if !ok {
		return 0, fmt.Errorf("bamboo: no block")
	}
	for _, kv := range [][2]string{{"age", "1"}, {"leaves", leaves}, {"stage", stage}} {
		if st, ok = data.With(st, kv[0], kv[1]); !ok {
			return 0, fmt.Errorf("bamboo: no state %s=%s", kv[0], kv[1])
		}
	}
	return st, nil
}

func init() {
	featureTypes["bamboo"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Prob float32 `json:"probability"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("bamboo: %w", err)
		}
		f := &bamboo{prob: raw.Prob}
		var err error
		if f.supports, err = rg.tag("supports_bamboo"); err != nil {
			return nil, err
		}
		if f.beneath, err = rg.tag("beneath_bamboo_podzol_replaceable"); err != nil {
			return nil, err
		}
		for _, s := range []struct {
			dst           *data.StateID
			leaves, stage string
		}{{&f.stalk, "none", "0"}, {&f.small, "small", "0"}, {&f.big, "large", "0"}, {&f.top, "large", "1"}} {
			if *s.dst, err = bambooState(s.leaves, s.stage); err != nil {
				return nil, err
			}
		}
		var ok bool
		if f.podzol, ok = data.DefaultState("podzol"); !ok {
			return nil, fmt.Errorf("bamboo: no podzol")
		}
		return f, nil
	}
}
