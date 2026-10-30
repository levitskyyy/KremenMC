package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// endGateway is end_gateway. From kremenmc.probe.feat_nether.Place (air
// and end scenes, exact and inexact, with and without exit; no draws):
// over dx, dz in [-1, 1] and dy in [-2, 2] around the origin, bedrock at
// dy = ±2 above and below the origin and on the five-block crosses at
// dy = ±1, the gateway at the origin, air everywhere else. The block
// entity holds Age 0, ExactTeleport 1b when exact and exit_portal when an
// exit is configured.
type endGateway struct {
	exact bool
	exit  []int32
}

var endGatewayState, _ = data.DefaultState("end_gateway")

func (f *endGateway) Place(c *placeCtx, _ *Random, p Pos) bool {
	lv := c.lv
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for dy := -2; dy <= 2; dy++ {
				q := p.Add(Pos{dx, dy, dz})
				switch {
				case dx == 0 && dy == 0 && dz == 0:
				case (dy == 2 || dy == -2) && dx == 0 && dz == 0,
					(dy == 1 || dy == -1) && abs(dx)+abs(dz) <= 1:
					lv.SetBlock(q, bedrockState)
				default:
					lv.SetBlock(q, airState)
				}
			}
		}
	}
	lv.SetBlock(p, endGatewayState)
	be := nbt.Compound{"Age": int64(0)}
	if f.exact {
		be["ExactTeleport"] = int8(1)
	}
	if f.exit != nil {
		be["exit_portal"] = append([]int32(nil), f.exit...)
	}
	lv.SetBlockEntity(p, "minecraft:end_gateway", be)
	return true
}

func init() {
	featureTypes["end_gateway"] = func(_ *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Exact bool    `json:"exact"`
			Exit  []int32 `json:"exit"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("end_gateway: %w", err)
		}
		if raw.Exit != nil && len(raw.Exit) != 3 {
			return nil, fmt.Errorf("end_gateway: exit %v", raw.Exit)
		}
		return &endGateway{exact: raw.Exact, exit: raw.Exit}, nil
	}
}
