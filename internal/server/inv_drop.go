package server

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
)

const (
	itemMetaIndex = 8
	metaItemStack = 7
)

// droppedItem is the item entity state (itement_entity.go).
type droppedItem = itemEnt

type itemRegistry2 struct {
	mu    sync.Mutex
	items map[int32]*droppedItem
}

// dropped indexes live item entities by entity id.
var dropped = itemRegistry2{items: map[int32]*droppedItem{}}

func itemMeta(st item.Stack) []byte {
	b := append([]byte{itemMetaIndex, metaItemStack}, item.AppendStack(nil, st)...)
	return entity.EndMeta(b)
}

// dropItem is Player.drop: spawns st at eye height minus 0.3 with vanilla
// throw velocity. throw spreads it randomly (inventory throws); otherwise
// it flies where the player looks (Q key).
func (p *Player) dropItem(st item.Stack, throw bool) {
	if st.IsEmpty() {
		return
	}
	w := p.world.Load()
	if w == nil {
		return
	}
	loc := p.Location()
	pos := loc.Pos
	pos.Y += 1.62 - 0.3
	var v api.Vec3
	if throw {
		f := rand.Float64() * 0.5
		a := rand.Float64() * math.Pi * 2
		v = api.Vec3{X: -math.Sin(a) * f, Y: 0.2, Z: math.Cos(a) * f}
	} else {
		pitch := float64(loc.Pitch) * math.Pi / 180
		yaw := float64(loc.Yaw) * math.Pi / 180
		sp, cp := math.Sin(pitch), math.Cos(pitch)
		sy, cy := math.Sin(yaw), math.Cos(yaw)
		a := rand.Float64() * math.Pi * 2
		f := 0.02 * rand.Float64()
		v = api.Vec3{X: -sy*cp*0.3 + math.Cos(a)*f, Y: -sp*0.3 + 0.1 + (rand.Float64()-rand.Float64())*0.1, Z: cy*cp*0.3 + math.Sin(a)*f}
	}
	if d := w.spawnItem(pos, v, st, itemPickupThrown); d != nil {
		d.thrower = p.prof.UUID
	}
}

// pickupItems is the item part of Player.aiStep.
func (p *Player) pickupItems() { p.pickupNearby() }

func (p *Player) isTracking(id int32) bool {
	_, ok := p.tracking[id]
	return ok
}
