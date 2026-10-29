package feature

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// hugeMushroom is huge_red_mushroom / huge_brown_mushroom, fitted to
// kremenmc.probe.feat_tree.Draws (30 isolated mushrooms, draw for draw):
//   - height 4 + nextInt(3), doubled when nextInt(12) == 0;
//   - a stem from the origin up to height-1;
//   - red: rings of radius 2 without corners from height-3 to height-1 and
//     a full 3x3 at height; brown: one layer of radius foliage_radius
//     without corners at height;
//   - a cap face is shown where the neighbour on that side is outside the
//     cap layer's filled outline (the ring counts as filled), up where it
//     is outside the layer above; down keeps the provider's value.
//
// The block below must match can_place_on and the column from the origin to
// the height must be air or #replaceable_by_mushrooms; cap and stem blocks
// are then placed where that holds (a dark oak log in a red cap ring stays:
// kremenmc.probe.feat_region.Region reads). The brown cap's check is inferred
// from the red one only.
type hugeMushroom struct {
	red        bool
	cap, stem  StateProvider
	canPlaceOn Predicate
	radius     int
}

var mushroomReplaceable = sync.OnceValue(func() blockSet {
	rg, err := theRegistry()
	if err != nil {
		return nil
	}
	s, _ := rg.tag("minecraft:replaceable_by_mushrooms")
	return s
})

func mushroomFree(lv *Level, p Pos) bool {
	st := lv.Block(p)
	return data.IsAir(st) || mushroomReplaceable().has(st)
}

// layer is the cap outline at height y: radius and whether corners are cut;
// ok false where the cap has no layer.
func (m *hugeMushroom) layer(h, y int) (rad int, cut, ring, ok bool) {
	if m.red {
		switch {
		case y == h:
			return m.radius - 1, false, false, true
		case y >= h-3 && y < h:
			return m.radius, true, true, true
		}
		return 0, false, false, false
	}
	if y == h {
		return m.radius, true, false, true
	}
	return 0, false, false, false
}

func (m *hugeMushroom) filled(h, y, dx, dz int) bool {
	rad, cut, _, ok := m.layer(h, y)
	if !ok || abs(dx) > rad || abs(dz) > rad {
		return false
	}
	return !cut || abs(dx) != rad || abs(dz) != rad
}

func (m *hugeMushroom) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	h := 4 + int(r.NextIntn(3))
	if r.NextIntn(12) == 0 {
		h *= 2
	}
	if p.Y < lv.MinY()+1 || p.Y+h+1 >= lv.MaxY() {
		return false
	}
	if !m.canPlaceOn.Test(lv, p.Add(Pos{0, -1, 0})) {
		return false
	}
	for i := 0; i <= h; i++ {
		if !mushroomFree(lv, p.Add(Pos{0, i, 0})) {
			return false
		}
	}
	cells := m.cells(h)
	reg := data.Canonical()
	for _, o := range cells {
		q := p.Add(o)
		if !mushroomFree(lv, q) {
			continue
		}
		st := m.cap.State(lv, r, q)
		for _, f := range [5]struct {
			name string
			show bool
		}{
			{"north", !m.filled(h, o.Y, o.X, o.Z-1)},
			{"south", !m.filled(h, o.Y, o.X, o.Z+1)},
			{"west", !m.filled(h, o.Y, o.X-1, o.Z)},
			{"east", !m.filled(h, o.Y, o.X+1, o.Z)},
			{"up", !m.filled(h, o.Y+1, o.X, o.Z)},
		} {
			if s, ok := reg.With(st, f.name, fmt.Sprint(f.show)); ok {
				st = s
			}
		}
		lv.SetBlock(q, st)
	}
	for i := range h {
		q := p.Add(Pos{0, i, 0})
		if mushroomFree(lv, q) {
			lv.SetBlock(q, m.stem.State(lv, r, q))
		}
	}
	return true
}

func (m *hugeMushroom) cells(h int) []Pos {
	var out []Pos
	for y := 0; y <= h; y++ {
		rad, _, ring, ok := m.layer(h, y)
		if !ok {
			continue
		}
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if !m.filled(h, y, dx, dz) || ring && abs(dx) != rad && abs(dz) != rad {
					continue
				}
				out = append(out, Pos{dx, y, dz})
			}
		}
	}
	return out
}

func init() {
	for _, name := range []string{"huge_red_mushroom", "huge_brown_mushroom"} {
		red := strings.Contains(name, "red")
		featureTypes[name] = func(rg *registry, b json.RawMessage) (Feature, error) {
			var p struct {
				Cap    json.RawMessage `json:"cap_provider"`
				Stem   json.RawMessage `json:"stem_provider"`
				On     json.RawMessage `json:"can_place_on"`
				Radius *int            `json:"foliage_radius"`
			}
			if err := json.Unmarshal(b, &p); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			m := &hugeMushroom{red: red, radius: 2}
			if p.Radius != nil {
				m.radius = *p.Radius
			}
			var err error
			if m.cap, err = rg.parseStateProvider(p.Cap); err != nil {
				return nil, err
			}
			if m.stem, err = rg.parseStateProvider(p.Stem); err != nil {
				return nil, err
			}
			if m.canPlaceOn, err = rg.parsePredicate(p.On); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
}
