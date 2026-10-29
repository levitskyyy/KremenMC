package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// underwaterMagma is underwater_magma: from water at p, find the floor within
// floor_search_range, then draw once per position of the box of
// placement_radius_around_floor around it and turn drawn solid blocks with
// solid blocks below and on all four sides (above may be water) into magma.
// Inferred from the feat_env probe (ocean scenes); requiring water at p is
// an assumption (all probe origins are water).
type underwaterMagma struct {
	search, radius int
	prob           float32
}

func (f underwaterMagma) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if blockName(lv.Block(p)) != "water" {
		return false
	}
	floor, ok := 0, false
	for i := range f.search {
		q := p.Add(Pos{0, -i, 0})
		if isSolid(lv.Block(q)) {
			floor, ok = q.Y, true
			break
		}
	}
	if !ok {
		return false
	}
	magma, _ := data.DefaultState("magma_block")
	placed := false
	try := func(q Pos) {
		if r.NextFloat() >= f.prob || !isSolid(lv.Block(q)) {
			return
		}
		for _, d := range magmaDirs {
			if !isSolid(lv.Block(q.Add(d))) {
				return
			}
		}
		lv.SetBlock(q, magma)
		placed = true
	}
	rr := f.radius
	for a := -rr; a <= rr; a++ {
		for b := -rr; b <= rr; b++ {
			for cc := -rr; cc <= rr; cc++ {
				try(Pos{p.X + cc, floor + b, p.Z + a})
			}
		}
	}
	return placed
}

var magmaDirs = [5]Pos{{0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func init() {
	featureTypes["underwater_magma"] = func(_ *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Search int     `json:"floor_search_range"`
			Radius int     `json:"placement_radius_around_floor"`
			Prob   float32 `json:"placement_probability_per_valid_position"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("underwater_magma: %w", err)
		}
		return underwaterMagma{raw.Search, raw.Radius, raw.Prob}, nil
	}
}
