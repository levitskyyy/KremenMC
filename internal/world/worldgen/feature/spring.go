package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// springFeature is spring_feature: one fluid source in a wall whose
// neighbours are the valid blocks except for the configured number of
// air holes. No random draws (feat_env.Place).
type springFeature struct {
	state        data.StateID
	valid        blockSet
	requireBelow bool
	rocks, holes int
}

func (s *springFeature) Place(c *placeCtx, _ *Random, p Pos) bool {
	lv := c.lv
	if !s.valid.has(lv.Block(p.Add(Pos{0, 1, 0}))) {
		return false
	}
	below := p.Add(Pos{0, -1, 0})
	if s.requireBelow && !s.valid.has(lv.Block(below)) {
		return false
	}
	if at := lv.Block(p); !data.IsAir(at) && !s.valid.has(at) {
		return false
	}
	rocks, holes := 0, 0
	for _, d := range [...]Pos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		q := lv.Block(p.Add(d))
		if s.valid.has(q) {
			rocks++
		}
		if data.IsAir(q) {
			holes++
		}
	}
	if rocks != s.rocks || holes != s.holes {
		return false
	}
	lv.SetBlock(p, s.state)
	return true
}

func init() {
	featureTypes["spring_feature"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			State struct {
				ID string `json:"id"`
			} `json:"state"`
			Valid        json.RawMessage `json:"valid_blocks"`
			RequireBelow *bool           `json:"requires_block_below"`
			Rocks        *int            `json:"rock_count"`
			Holes        *int            `json:"hole_count"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("spring_feature: %w", err)
		}
		// The fluid state's block: a source of the fluid (falling is a
		// fluid-only property).
		s := &springFeature{requireBelow: true, rocks: 4, holes: 1}
		var ok bool
		if s.state, ok = data.DefaultState(strings.TrimPrefix(raw.State.ID, "minecraft:")); !ok {
			return nil, fmt.Errorf("spring_feature: unknown fluid %s", raw.State.ID)
		}
		if raw.RequireBelow != nil {
			s.requireBelow = *raw.RequireBelow
		}
		if raw.Rocks != nil {
			s.rocks = *raw.Rocks
		}
		if raw.Holes != nil {
			s.holes = *raw.Holes
		}
		names, err := stringList(raw.Valid)
		if err != nil {
			return nil, fmt.Errorf("spring_feature: %w", err)
		}
		if s.valid, err = rg.blocks(names); err != nil {
			return nil, fmt.Errorf("spring_feature: %w", err)
		}
		return s, nil
	}
}
