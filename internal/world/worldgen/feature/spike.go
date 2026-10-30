package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// spike is the ice spike feature. Fitted to kremenmc.probe.feat_ow.Place
// draw and write logs (snow scene):
//   - the origin moves down through air (not below min y + 2); the block
//     there must match can_place_on;
//   - draws: rise nextInt(4), height 7 + nextInt(4), radius height/4 +
//     nextInt(2); a radius above 1 draws nextInt(60), and 0 makes a huge
//     spike (rise 10 + nextInt(30) more);
//   - layer y of height h has f = (1 - y/h)·radius; cells of the square of
//     ceil(f) with (|x|-1/4)² + (|z|-1/4)² <= f² (and the centre) are
//     filled; cells on the square's edge draw nextFloat and are dropped
//     above 0.75; a layer above the first is mirrored below the base when
//     ceil(f) > 1. x is the outer loop, z the inner.
//   - a stem (3x3 when radius > 1, else the centre) runs down from below
//     the base through air and can_replace blocks; a corner column starts
//     with a budget of nextInt(5), others with 50; each placed block spends
//     one, and an empty budget skips nextInt(5)+1 blocks down and refills
//     with nextInt(5).
type spike struct {
	state         data.StateID
	placeOn, repl Predicate
}

func parseSpike(rg *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		State   json.RawMessage `json:"state"`
		PlaceOn json.RawMessage `json:"can_place_on"`
		Replace json.RawMessage `json:"can_replace"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("spike: %w", err)
	}
	st, err := parseBlockState(c.State)
	if err != nil {
		return nil, fmt.Errorf("spike: %w", err)
	}
	f := spike{state: st}
	if f.placeOn, err = rg.parsePredicate(c.PlaceOn); err != nil {
		return nil, fmt.Errorf("spike: %w", err)
	}
	if f.repl, err = rg.parsePredicate(c.Replace); err != nil {
		return nil, fmt.Errorf("spike: %w", err)
	}
	return f, nil
}

func (f spike) put(lv *Level, p Pos) {
	if data.IsAir(lv.Block(p)) || f.repl.Test(lv, p) {
		lv.SetBlock(p, f.state)
	}
}

func (f spike) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	for data.IsAir(lv.Block(p)) && p.Y > lv.MinY()+2 {
		p.Y--
	}
	if !f.placeOn.Test(lv, p) {
		return false
	}
	p.Y += int(r.NextIntn(4))
	h := int(r.NextIntn(4)) + 7
	rad := h/4 + int(r.NextIntn(2))
	if rad > 1 && r.NextIntn(60) == 0 {
		p.Y += 10 + int(r.NextIntn(30))
	}
	for y := 0; y < h; y++ {
		fr := (1 - float32(y)/float32(h)) * float32(rad)
		n := int(math.Ceil(float64(fr)))
		for x := -n; x <= n; x++ {
			dx := float32(abs(x)) - 0.25
			for z := -n; z <= n; z++ {
				dz := float32(abs(z)) - 0.25
				if (x != 0 || z != 0) && dx*dx+dz*dz > fr*fr {
					continue
				}
				if (x == -n || x == n || z == -n || z == n) && r.NextFloat() > 0.75 {
					continue
				}
				f.put(lv, p.Add(Pos{x, y, z}))
				if y != 0 && n > 1 {
					f.put(lv, p.Add(Pos{x, -y, z}))
				}
			}
		}
	}
	stem := 0
	if rad > 1 {
		stem = 1
	}
	for x := -stem; x <= stem; x++ {
		for z := -stem; z <= stem; z++ {
			q := p.Add(Pos{x, -1, z})
			budget := 50
			if abs(x) == 1 && abs(z) == 1 {
				budget = int(r.NextIntn(5))
			}
			for q.Y > lv.MinY() {
				st := lv.Block(q)
				if !data.IsAir(st) && !f.repl.Test(lv, q) && st != f.state {
					break
				}
				lv.SetBlock(q, f.state)
				q.Y--
				if budget--; budget <= 0 {
					q.Y -= int(r.NextIntn(5)) + 1
					budget = int(r.NextIntn(5))
				}
			}
		}
	}
	return true
}

func init() { featureTypes["spike"] = parseSpike }
