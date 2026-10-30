package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// hugeFungus is huge_fungus (crimson and warped fungi). Inferred from
// kremenmc.probe.feat_nether.Place draw and read logs (130 placements):
//   - the block below the origin must be valid_base_block, else false
//     without draws;
//   - height h = 4 + nextInt(10), doubled when nextInt(12) == 0; thick
//     (3x3 stem) when nextFloat() < 0.06;
//   - the origin is set to air; the stem fills x, z, y (y innermost)
//     0..h-1 where the position is replaceable (#replaceable, e.g. lava,
//     fire and roots, or replaceable_blocks); thick corners only when
//     nextFloat() < 0.1;
//   - the hat: d = nextInt(h/3 + 1), layer count L = d + 6, layers from
//     max(0, h-L+1) to h; per layer a draw e = nextInt(3), radius 2 (3
//     thick), -1 when h-y <= e, +1 for the four lowest layers when L >= 10;
//   - the three lowest layers place only on the square's edge: when the
//     block below is the hat block, or nextFloat() < 0.15; crimson then
//     tries weeping vines when nextInt(11) == 0;
//   - higher layers place on every replaceable position: corners
//     (|x| = |z| = r), edges (or the top layer) and the interior draw a
//     shroomlight chance a, then a hat chance b, then a vine chance c;
//     see fungusChance;
//   - weeping vines need air below; nextInt(5)+1 blocks, doubled when
//     nextInt(7) == 0, one more, stopping above the first non-air block;
//     the tip has age 23 + nextInt(3).
type hugeFungus struct {
	base, stem, hat, decor data.StateID
	replace                Predicate
	soft                   blockSet // #replaceable
	vines                  bool
	air                    data.StateID
}

// fungusChance is decor, hat and vine chance for corner, edge and interior
// positions of the upper hat layers (fitted thresholds; each lies inside
// the gap between the observed draws on either side).
var fungusChance = [3][3]float32{
	{0.01, 0.7, 0.083},
	{0.0005, 0.98, 0.07},
	{0.1, 0.2, 0.1},
}

func (f *hugeFungus) replaceable(lv *Level, p Pos) bool {
	return f.soft.has(lv.Block(p)) || f.replace.Test(lv, p)
}

func (f *hugeFungus) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if lv.Block(p.Add(Pos{0, -1, 0})) != f.base {
		return false
	}
	h := 4 + int(r.NextIntn(10))
	if r.NextIntn(12) == 0 {
		h *= 2
	}
	thick := r.NextFloat() < 0.06
	lv.SetBlock(p, f.air)
	sr := 0
	if thick {
		sr = 1
	}
	for x := -sr; x <= sr; x++ {
		for z := -sr; z <= sr; z++ {
			corner := thick && x*x == 1 && z*z == 1
			for y := 0; y < h; y++ {
				q := p.Add(Pos{x, y, z})
				if !f.replaceable(lv, q) {
					continue
				}
				if corner && r.NextFloat() >= 0.1 {
					continue
				}
				lv.SetBlock(q, f.stem)
			}
		}
	}
	d := int(r.NextIntn(int32(h/3 + 1)))
	n := d + 6
	start := max(0, h-n+1)
	for y, i := start, 0; y <= h; y, i = y+1, i+1 {
		e := int(r.NextIntn(3))
		rad := 2
		if thick {
			rad = 3
		}
		if h-y <= e {
			rad--
		}
		if n >= 10 && i < 4 {
			rad++
		}
		for x := -rad; x <= rad; x++ {
			for z := -rad; z <= rad; z++ {
				q := p.Add(Pos{x, y, z})
				if !f.replaceable(lv, q) {
					continue
				}
				ex, ez := x == rad || x == -rad, z == rad || z == -rad
				if i < 3 {
					if ex || ez {
						f.dropBlock(lv, r, q)
					}
					continue
				}
				k := 2
				switch {
				case ex && ez:
					k = 0
				case ex || ez || y == h:
					k = 1
				}
				f.hatBlock(lv, r, q, fungusChance[k])
			}
		}
	}
	return true
}

func (f *hugeFungus) dropBlock(lv *Level, r *Random, q Pos) {
	if lv.Block(q.Add(Pos{0, -1, 0})) == f.hat {
		lv.SetBlock(q, f.hat)
		return
	}
	if r.NextFloat() < 0.15 {
		lv.SetBlock(q, f.hat)
		if f.vines && r.NextIntn(11) == 0 {
			f.weepingVines(lv, r, q)
		}
	}
}

func (f *hugeFungus) hatBlock(lv *Level, r *Random, q Pos, ch [3]float32) {
	if r.NextFloat() < ch[0] {
		lv.SetBlock(q, f.decor)
		return
	}
	if r.NextFloat() < ch[1] {
		lv.SetBlock(q, f.hat)
		if r.NextFloat() < ch[2] && f.vines {
			f.weepingVines(lv, r, q)
		}
	}
}

func (f *hugeFungus) weepingVines(lv *Level, r *Random, q Pos) {
	below := q.Add(Pos{0, -1, 0})
	if !data.IsAir(lv.Block(below)) {
		return
	}
	n := int(r.NextIntn(5)) + 1
	if r.NextIntn(7) == 0 {
		n *= 2
	}
	n++
	for i := 1; i <= n; i++ {
		v := q.Add(Pos{0, -i, 0})
		if !data.IsAir(lv.Block(v)) {
			return
		}
		if i == n || !data.IsAir(lv.Block(v.Add(Pos{0, -1, 0}))) {
			lv.SetBlock(v, weepingTip(int(r.NextIntn(3))+23))
			return
		}
		lv.SetBlock(v, weepingPlant)
	}
}

var weepingPlant, _ = data.DefaultState("weeping_vines_plant")

func weepingTip(age int) data.StateID {
	s, _ := data.Canonical().StateFrom("weeping_vines", func(k string) (string, bool) {
		if k == "age" {
			return fmt.Sprint(age), true
		}
		return "", false
	})
	return s
}

func init() {
	featureTypes["huge_fungus"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Base    json.RawMessage `json:"valid_base_block"`
			Stem    json.RawMessage `json:"stem_state"`
			Hat     json.RawMessage `json:"hat_state"`
			Decor   json.RawMessage `json:"decor_state"`
			Replace json.RawMessage `json:"replaceable_blocks"`
			Planted bool            `json:"planted"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("huge_fungus: %w", err)
		}
		f := &hugeFungus{}
		var err error
		for _, x := range []struct {
			dst *data.StateID
			src json.RawMessage
		}{{&f.base, raw.Base}, {&f.stem, raw.Stem}, {&f.hat, raw.Hat}, {&f.decor, raw.Decor}} {
			if *x.dst, err = parseBlockState(x.src); err != nil {
				return nil, fmt.Errorf("huge_fungus: %w", err)
			}
		}
		if raw.Planted {
			return nil, unsupported("feature", "huge_fungus (planted)")
		}
		if f.replace, err = rg.parsePredicate(raw.Replace); err != nil {
			return nil, fmt.Errorf("huge_fungus: %w", err)
		}
		if f.soft, err = rg.tag("replaceable"); err != nil {
			return nil, fmt.Errorf("huge_fungus: %w", err)
		}
		f.air, _ = data.DefaultState("air")
		wart, _ := data.DefaultState("nether_wart_block")
		f.vines = f.hat == wart
		return f, nil
	}
}
