package feature

import (
	"encoding/json"
	"fmt"
)

// modCuboid emits the box from p to p+size (inclusive) in x, y, z order,
// sizes drawn y, x, z (xz_size twice):
// face blocks always, edge/corner blocks (on two or more boundaries) with
// include_edges, interior blocks with include_interior.
type modCuboid struct {
	xz, y           IntProvider
	edges, interior bool
}

func (m modCuboid) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	sy := m.y.Sample(r)
	sx := m.xz.Sample(r)
	sz := m.xz.Sample(r)
	on := func(v, n int) int {
		if v == 0 || v == n {
			return 1
		}
		return 0
	}
	for dx := 0; dx <= sx; dx++ {
		for dy := 0; dy <= sy; dy++ {
			for dz := 0; dz <= sz; dz++ {
				k := on(dx, sx) + on(dy, sy) + on(dz, sz)
				if k == 0 && !m.interior || k >= 2 && !m.edges {
					continue
				}
				emit(Pos{p.X + dx, p.Y + dy, p.Z + dz})
			}
		}
	}
}

type modRandomlySelected []Modifier

func (m modRandomlySelected) Apply(c *placeCtx, r *Random, p Pos, emit func(Pos)) {
	m[r.NextIntn(int32(len(m)))].Apply(c, r, p, emit)
}

// modFixed emits the listed positions that lie in the chunk of p.
type modFixed []Pos

func (m modFixed) Apply(_ *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	for _, q := range m {
		if q.X>>4 == p.X>>4 && q.Z>>4 == p.Z>>4 {
			emit(q)
		}
	}
}

// modEnvScan is environment_scan (from the wiki; not yet probed in-world):
// walk from p while allowed holds, emitting the first position matching
// target within max_steps steps.
type modEnvScan struct {
	dy              int
	steps           int
	target, allowed Predicate
}

func (m modEnvScan) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	if !m.allowed.Test(c.lv, p) {
		return
	}
	for range m.steps {
		if m.target.Test(c.lv, p) {
			emit(p)
			return
		}
		p.Y += m.dy
		if p.Y < c.lv.MinY() || p.Y >= c.lv.MaxY() {
			return
		}
		if !m.allowed.Test(c.lv, p) {
			break
		}
	}
	if m.target.Test(c.lv, p) {
		emit(p)
	}
}

func init() {
	modifierTypes["cuboid"] = func(_ *registry, b json.RawMessage) (Modifier, error) {
		var p struct {
			XZ       json.RawMessage `json:"xz_size"`
			Y        json.RawMessage `json:"y_size"`
			Edges    bool            `json:"include_edges"`
			Interior bool            `json:"include_interior"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("cuboid: %w", err)
		}
		m := modCuboid{edges: p.Edges, interior: p.Interior}
		var err error
		if m.xz, err = parseInt(p.XZ); err != nil {
			return nil, err
		}
		m.y, err = parseInt(p.Y)
		return m, err
	}
	modifierTypes["randomly_selected"] = func(rg *registry, b json.RawMessage) (Modifier, error) {
		var p struct {
			Placements []json.RawMessage `json:"placements"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("randomly_selected: %w", err)
		}
		var m modRandomlySelected
		for _, s := range p.Placements {
			mm, err := rg.parseModifier(s)
			if err != nil {
				return nil, err
			}
			m = append(m, mm)
		}
		if len(m) == 0 {
			return nil, fmt.Errorf("randomly_selected: no placements")
		}
		return m, nil
	}
	modifierTypes["fixed_placement"] = func(_ *registry, b json.RawMessage) (Modifier, error) {
		var p struct {
			Positions [][3]int `json:"positions"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("fixed_placement: %w", err)
		}
		var m modFixed
		for _, q := range p.Positions {
			m = append(m, Pos{q[0], q[1], q[2]})
		}
		return m, nil
	}
	modifierTypes["environment_scan"] = func(rg *registry, b json.RawMessage) (Modifier, error) {
		var p struct {
			Dir     string          `json:"direction_of_search"`
			Steps   int             `json:"max_steps"`
			Target  json.RawMessage `json:"target_condition"`
			Allowed json.RawMessage `json:"allowed_search_condition"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("environment_scan: %w", err)
		}
		m := modEnvScan{steps: p.Steps, allowed: predTrue{}}
		switch p.Dir {
		case "up":
			m.dy = 1
		case "down":
			m.dy = -1
		default:
			return nil, fmt.Errorf("environment_scan: direction %q", p.Dir)
		}
		var err error
		if m.target, err = rg.parsePredicate(p.Target); err != nil {
			return nil, err
		}
		if len(p.Allowed) > 0 {
			m.allowed, err = rg.parsePredicate(p.Allowed)
		}
		return m, err
	}
}
