package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// giantTrunk is giant_trunk_placer (mega spruce/pine), fitted to
// kremenmc.probe.feat_tree.Draws (40 isolated trees): a 2x2 trunk from the
// origin up to h-2, only its (0,0) column at h-1, soil under all four,
// foliage attached at (0,h,0) on a double trunk.
type giantTrunk struct{ trunkHeight }

var giantFoot = [4]Pos{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}, {1, 0, 1}}

func (g giantTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	for _, o := range giantFoot {
		t.setDirt(r, p.Add(o).Add(Pos{0, -1, 0}))
	}
	for i := range h {
		for j, o := range giantFoot {
			if i == h-1 && j > 0 {
				continue
			}
			t.placeLog(r, p.Add(Pos{o.X, i, o.Z}))
		}
	}
	return []attachment{{pos: p.Add(Pos{0, h, 0}), double: true}}
}

// megaPineFoliage is mega_pine_foliage_placer, fitted to the same 40 trees:
// layers from the top (d = 0) down to the crown height; the radius is
// floor(3.5*d/crown) plus one on even y when the layer below has the same
// base radius (never on the top or bottom layer); a double-trunk disc
// keeping offsets with dx²+dz² <= r².
type megaPineFoliage struct{ radius, offset, crown IntProvider }

func (m megaPineFoliage) foliageHeight(r *Random, _ int) int { return m.crown.Sample(r) }
func (m megaPineFoliage) foliageRadius(r *Random, _ int) int { return m.radius.Sample(r) }

func (m megaPineFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	off := m.offset.Sample(r)
	base := func(d int) int {
		if fh <= 0 {
			return 0
		}
		return 7 * d / (2 * fh)
	}
	for d := 0; d <= fh; d++ {
		y := a.pos.Y + off - d
		rad := base(d)
		if d > 0 && d < fh && y&1 == 0 && base(d+1) == rad {
			rad++
		}
		rad += fr + a.radiusOff
		leavesRow(t, r, a.pos, rad, off-d, a.double, func(_ *Random, ax, _, az, rng int) bool {
			return ax*ax+az*az > rng*rng
		})
	}
}

// alterGround is the alter_ground decorator, fitted to
// kremenmc.probe.feat_tree.Deco (decorator alone on one log or a 2x2): for each
// lowest trunk position (the trunk decorators' order, so the soil under a
// tree), discs of radius 2 without
// corners at (-1,-1), (2,-1), (-1,2), (2,2), then five nextInt(64) draws
// v giving a disc at (v%8-3, v/8-3) when v%8 or v/8 is 0 or 7. Each disc
// column is searched from y+2 down to y-3: the first block the provider
// accepts is replaced; a non-air block it rejects below y ends the search.
type alterGround struct{ prov *ruleState }

func (a alterGround) decorate(t *treeRun, r *Random) {
	all := t.trunkPositions()
	if len(all) == 0 {
		return
	}
	var ps []Pos
	for _, l := range all {
		if l.Y == all[0].Y {
			ps = append(ps, l)
		}
	}
	for _, p := range ps {
		for _, c := range [4]Pos{{-1, 0, -1}, {2, 0, -1}, {-1, 0, 2}, {2, 0, 2}} {
			a.disc(t, r, p.Add(c))
		}
		for range 5 {
			v := int(r.NextIntn(64))
			k, l := v%8, v/8
			if k == 0 || k == 7 || l == 0 || l == 7 {
				a.disc(t, r, p.Add(Pos{k - 3, 0, l - 3}))
			}
		}
	}
}

func (a alterGround) disc(t *treeRun, r *Random, c Pos) {
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if abs(dx) == 2 && abs(dz) == 2 {
				continue
			}
			a.column(t, r, c.Add(Pos{dx, 0, dz}))
		}
	}
}

func (a alterGround) column(t *treeRun, r *Random, p Pos) {
	for dy := 2; dy >= -3; dy-- {
		q := p.Add(Pos{0, dy, 0})
		if st, ok := a.prov.pick(t.lv, r, q); ok {
			t.setDeco(q, st)
			return
		}
		if dy < 0 && !data.IsAir(t.lv.Block(q)) {
			return
		}
	}
}

func init() {
	trunkTypes["giant_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var g giantTrunk
		return g, json.Unmarshal(b, &g.trunkHeight)
	}
	foliageTypes["mega_pine_foliage_placer"] = func(b json.RawMessage) (foliagePlacer, error) {
		var p struct {
			Radius json.RawMessage `json:"radius"`
			Offset json.RawMessage `json:"offset"`
			Crown  json.RawMessage `json:"crown_height"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		var m megaPineFoliage
		var err error
		if m.radius, err = parseInt(p.Radius); err != nil {
			return nil, err
		}
		if m.offset, err = parseInt(p.Offset); err != nil {
			return nil, err
		}
		if m.crown, err = parseInt(p.Crown); err != nil {
			return nil, err
		}
		return m, nil
	}
	decoratorTypes["alter_ground"] = func(rg *registry, b json.RawMessage) (decorator, error) {
		var p struct {
			Provider json.RawMessage `json:"provider"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		sp, err := rg.parseStateProvider(p.Provider)
		if err != nil {
			return nil, err
		}
		rs, ok := sp.(*ruleState)
		if !ok {
			return nil, fmt.Errorf("alter_ground: provider is not rule_based")
		}
		return alterGround{rs}, nil
	}
}
