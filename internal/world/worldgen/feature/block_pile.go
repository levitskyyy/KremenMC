package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// blockPile is block_pile (village hay, melon, pumpkin, snow and ice
// piles). Derived black box from kremenmc.probe.jigsaw.PilePlace (logged draws
// and call lines of the reference BlockPileFeature, 139 runs with flat
// floors of 20 block types, single obstacles and hay floors, all
// reproduced draw for draw):
//   - rx = 2 + nextInt(2), rz = 2 + nextInt(2);
//   - for z in [-rz,rz], y in [0,1], x in [-rx,rx]: a = nextFloat(),
//     b = nextFloat(); the position is tried when x²+z² < 10a - 6b, else
//     when a third nextFloat() < 0.031;
//   - a try needs an air position and below it a block with a sturdy top
//     face, or dirt_path with nextBoolean() true; then the provider's state
//     is placed.
type blockPile struct{ state StateProvider }

func (f *blockPile) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if p.Y < lv.MinY()+5 {
		return false
	}
	rx, rz := 2+int(r.NextIntn(2)), 2+int(r.NextIntn(2))
	path, _ := data.Canonical().BlockByName("dirt_path")
	for z := -rz; z <= rz; z++ {
		for y := range 2 {
			for x := -rx; x <= rx; x++ {
				a, b := r.NextFloat(), r.NextFloat()
				if float32(x*x+z*z) >= 10*a-6*b && r.NextFloat() >= 0.031 {
					continue
				}
				q := p.Add(Pos{x, y, z})
				if !data.IsAir(lv.Block(q)) {
					continue
				}
				below := lv.Block(q.Add(Pos{0, -1, 0}))
				if !faceSturdy(below, 1) {
					if bb := data.Canonical().BlockOf(below); path == nil || bb != path || !r.NextBool() {
						continue
					}
				}
				lv.SetBlock(q, f.state.State(lv, r, q))
			}
		}
	}
	return true
}

// randomAxis is the direction-less rotated provider: a random direction
// (nextInt(6): down, up, north, south, west, east) sets the axis.
type randomAxis struct{ inner StateProvider }

var dirAxis = [6]string{"y", "y", "z", "z", "x", "x"}

func (s randomAxis) State(lv *Level, r *Random, p Pos) data.StateID {
	st := s.inner.State(lv, r, p)
	a := dirAxis[r.NextIntn(6)]
	if out, ok := data.Canonical().With(st, "axis", a); ok {
		return out
	}
	return st
}

func init() {
	featureTypes["block_pile"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Provider json.RawMessage `json:"state_provider"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("block_pile: %w", err)
		}
		var rot struct {
			Type      string          `json:"type"`
			Direction *string         `json:"direction"`
			State     json.RawMessage `json:"state"`
		}
		if json.Unmarshal(raw.Provider, &rot) == nil && rot.Type == "minecraft:rotated" && rot.Direction == nil {
			in, err := rg.parseStateProvider(rot.State)
			if err != nil {
				return nil, fmt.Errorf("block_pile: %w", err)
			}
			return &blockPile{randomAxis{in}}, nil
		}
		sp, err := rg.parseStateProvider(raw.Provider)
		if err != nil {
			return nil, fmt.Errorf("block_pile: %w", err)
		}
		return &blockPile{sp}, nil
	}
}
