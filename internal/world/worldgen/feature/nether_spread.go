package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// neighborSpread is random_neighbor_spread (glowstone blobs, the nether
// wart patches of weeping vines). Inferred from
// kremenmc.probe.feat_nether.Place draw and read logs: the origin gets the
// block (see TestNetherSpread for the origin rule); then attempts × (x, y,
// z offsets from xz_offset, y_offset, xz_offset in that order); a target
// that can_replace accepts counts accepted neighbours below, above, north,
// south, west, east, stopping at the second, and gets the block when
// exactly one was found.
type neighborSpread struct {
	block    data.StateID
	attempts int
	xz, y    IntProvider
	replace  Predicate
	accepted blockSet
}

var spreadSides = [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (f *neighborSpread) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	lv.SetBlock(p, f.block)
	for range f.attempts {
		dx := int(f.xz.Sample(r))
		dy := int(f.y.Sample(r))
		dz := int(f.xz.Sample(r))
		q := p.Add(Pos{dx, dy, dz})
		if !f.replace.Test(lv, q) {
			continue
		}
		n := 0
		for _, s := range spreadSides {
			if f.accepted.has(lv.Block(q.Add(s))) {
				if n++; n > 1 {
					break
				}
			}
		}
		if n == 1 {
			lv.SetBlock(q, f.block)
		}
	}
	return true
}

func init() {
	featureTypes["random_neighbor_spread"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Block    json.RawMessage `json:"block"`
			Attempts int             `json:"attempts"`
			XZ       json.RawMessage `json:"xz_offset"`
			Y        json.RawMessage `json:"y_offset"`
			Replace  json.RawMessage `json:"can_replace"`
			Accepted json.RawMessage `json:"accepted_neighbors"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		f := &neighborSpread{attempts: raw.Attempts}
		var err error
		if f.block, err = parseBlockState(raw.Block); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		if f.xz, err = parseInt(raw.XZ); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		if f.y, err = parseInt(raw.Y); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		if f.replace, err = rg.parsePredicate(raw.Replace); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		names, err := stringList(raw.Accepted)
		if err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		if f.accepted, err = rg.blocks(names); err != nil {
			return nil, fmt.Errorf("random_neighbor_spread: %w", err)
		}
		return f, nil
	}
}
