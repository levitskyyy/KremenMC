package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// modCountOnEveryLayer is count_on_every_layer. Inferred from
// kremenmc.probe.feat_nether.Place read logs: for layer 0, 1, 2, ... it
// makes count attempts, each drawing x then z (nextInt(16) from the
// origin, the chunk corner in generation) and scanning down from the
// MOTION_BLOCKING height for the layer-th empty block (air, water or lava)
// standing on a non-empty, non-bedrock block; it stops after the first
// layer where no attempt found one. Positions are emitted after all draws.
type modCountOnEveryLayer struct{ n IntProvider }

var bedrockState, _ = data.DefaultState("bedrock")

func (m modCountOnEveryLayer) Apply(c *placeCtx, r *Random, p Pos, emit func(Pos)) {
	lv := c.lv
	x0, z0 := p.X, p.Z
	var out []Pos
	for layer := 0; ; layer++ {
		found := false
		for range m.n.Sample(r) {
			x, z := x0+int(r.NextIntn(16)), z0+int(r.NextIntn(16))
			y := lv.Height(world.MotionBlocking, x, z)
			if ly, ok := findLayer(lv, x, y, z, layer); ok {
				out = append(out, Pos{x, ly, z})
				found = true
			}
		}
		if !found {
			break
		}
	}
	for _, q := range out {
		emit(q)
	}
}

func findLayer(lv *Level, x, y, z, target int) (int, bool) {
	cur := lv.Block(Pos{x, y, z})
	n := 0
	for j := y; j >= lv.MinY()+1; j-- {
		next := lv.Block(Pos{x, j - 1, z})
		if !layerEmpty(next) && layerEmpty(cur) && next != bedrockState {
			if n == target {
				return j, true
			}
			n++
		}
		cur = next
	}
	return 0, false
}

func init() {
	modifierTypes["count_on_every_layer"] = func(_ *registry, b json.RawMessage) (Modifier, error) {
		var p struct {
			Count json.RawMessage `json:"count"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("count_on_every_layer: %w", err)
		}
		n, err := parseInt(p.Count)
		return modCountOnEveryLayer{n}, err
	}
}

var waterBlock, lavaBlock = func() (data.BlockID, data.BlockID) {
	w, _ := data.Canonical().BlockByName("water")
	l, _ := data.Canonical().BlockByName("lava")
	return w.ID, l.ID
}()

func layerEmpty(s data.StateID) bool {
	if data.IsAir(s) {
		return true
	}
	b := data.Canonical().BlockID(s)
	return b == waterBlock || b == lavaBlock
}
