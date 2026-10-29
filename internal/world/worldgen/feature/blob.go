package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// blockBlob is block_blob (forest rocks): sinks to the first non-air block
// it can sit on, then stamps three random balls drifting down and
// sideways.
type blockBlob struct {
	state data.StateID
	on    Predicate
}

func (f *blockBlob) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	for p.Y > lv.MinY()+3 {
		below := p.Add(Pos{0, -1, 0})
		if !data.IsAir(lv.Block(below)) && f.on.Test(lv, below) {
			break
		}
		p = below
	}
	if p.Y <= lv.MinY()+3 {
		return false
	}
	for range 3 {
		i, j, k := int(r.NextIntn(2)), int(r.NextIntn(2)), int(r.NextIntn(2))
		d := float32(i+j+k)*0.333 + 0.5
		for z := p.Z - k; z <= p.Z+k; z++ {
			for y := p.Y - j; y <= p.Y+j; y++ {
				for x := p.X - i; x <= p.X+i; x++ {
					dx, dy, dz := float64(x-p.X), float64(y-p.Y), float64(z-p.Z)
					if dx*dx+dy*dy+dz*dz <= float64(d*d) {
						lv.SetBlock(Pos{x, y, z}, f.state)
					}
				}
			}
		}
		p = p.Add(Pos{-1 + int(r.NextIntn(2)), -int(r.NextIntn(2)), -1 + int(r.NextIntn(2))})
	}
	return true
}

func init() {
	featureTypes["block_blob"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			State json.RawMessage `json:"state"`
			On    json.RawMessage `json:"can_place_on"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("block_blob: %w", err)
		}
		st, err := parseBlockState(raw.State)
		if err != nil {
			return nil, fmt.Errorf("block_blob: %w", err)
		}
		on, err := rg.parsePredicate(raw.On)
		if err != nil {
			return nil, fmt.Errorf("block_blob: %w", err)
		}
		return &blockBlob{st, on}, nil
	}
}
