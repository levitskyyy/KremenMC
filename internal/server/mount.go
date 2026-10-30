package server

import (
	"slices"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// mobRide links a mob to its vehicle and riders. A rider skips its own
// physics and sits on the vehicle; clients place riders themselves from
// Set Passengers.
type mobRide struct {
	mu      sync.Mutex
	vehicle *Mob
	riders  []*Mob
	synced  bool // spawn extras registered
}

func (m *Mob) passengersPacket() []byte {
	m.ride.mu.Lock()
	ids := make([]int32, 0, len(m.ride.riders))
	for _, r := range m.ride.riders {
		ids = append(ids, r.e.ID)
	}
	m.ride.mu.Unlock()
	return encode(&packets.ClientboundSetPassengers{EntityID: m.e.ID, Passengers: ids})
}

// syncRide makes new viewers of m receive the Set Passengers packets of m
// (as a vehicle) and of m's vehicle; either entity may reach a viewer
// first, and the client ignores ids it does not know yet.
func (m *Mob) syncRide() {
	m.ride.mu.Lock()
	done := m.ride.synced
	m.ride.synced = true
	m.ride.mu.Unlock()
	if done {
		return
	}
	m.trk.AddSpawnExtra(func() [][]byte {
		var out [][]byte
		m.ride.mu.Lock()
		v, n := m.ride.vehicle, len(m.ride.riders)
		m.ride.mu.Unlock()
		if n > 0 {
			out = append(out, m.passengersPacket())
		}
		if v != nil {
			out = append(out, v.passengersPacket())
		}
		return out
	})
}

// mount seats r on v. False when r already rides or would ride itself.
func (r *Mob) mount(v *Mob) bool {
	if r == v || v.removed.Load() || r.removed.Load() {
		return false
	}
	for x := v; x != nil; x = x.vehicleMob() {
		if x == r {
			return false
		}
	}
	r.ride.mu.Lock()
	if r.ride.vehicle != nil {
		r.ride.mu.Unlock()
		return false
	}
	r.ride.vehicle = v
	r.ride.mu.Unlock()
	v.ride.mu.Lock()
	v.ride.riders = append(v.ride.riders, r)
	v.ride.mu.Unlock()
	r.syncRide()
	v.syncRide()
	r.seat(v)
	v.trk.Push(v.passengersPacket())
	return true
}

// dismount takes r off its vehicle.
func (r *Mob) dismount() bool {
	r.ride.mu.Lock()
	v := r.ride.vehicle
	r.ride.vehicle = nil
	r.ride.mu.Unlock()
	if v == nil {
		return false
	}
	v.ride.mu.Lock()
	v.ride.riders = slices.DeleteFunc(v.ride.riders, func(x *Mob) bool { return x == r })
	v.ride.mu.Unlock()
	v.trk.Push(v.passengersPacket())
	return true
}

func (m *Mob) vehicleMob() *Mob {
	m.ride.mu.Lock()
	defer m.ride.mu.Unlock()
	return m.ride.vehicle
}

// seat moves r to its seat on v: the vehicle's top (the default passenger
// attachment), standing still.
func (r *Mob) seat(v *Mob) {
	v.mu.Lock()
	p, yaw := v.e.Pos, v.e.Yaw
	v.mu.Unlock()
	p.Y += float64(v.typ.Height)
	r.mu.Lock()
	r.e.Prev, r.e.Pos = r.e.Pos, p
	r.e.Vel = api.Vec3{}
	r.e.OnGround = false
	if r.e.Yaw == 0 {
		r.e.Yaw = yaw
	}
	r.mu.Unlock()
}

// tickRide drops removed riders and a removed vehicle, then seats a rider.
// True when m rides and so skips its own movement this tick.
func (m *Mob) tickRide() bool {
	m.ride.mu.Lock()
	n := len(m.ride.riders)
	m.ride.riders = slices.DeleteFunc(m.ride.riders, func(x *Mob) bool { return x.removed.Load() })
	pruned := len(m.ride.riders) != n
	v := m.ride.vehicle
	m.ride.mu.Unlock()
	if pruned {
		m.trk.Push(m.passengersPacket())
	}
	if v == nil {
		return false
	}
	if v.removed.Load() || m.isDeadMob() {
		m.dismount()
		return false
	}
	m.seat(v)
	return true
}

func (m *Mob) isDeadMob() bool {
	if m.liv.attrs == nil {
		return false
	}
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	return m.liv.dead
}

func (m *Mob) Passengers() []api.Entity {
	m.ride.mu.Lock()
	defer m.ride.mu.Unlock()
	out := make([]api.Entity, 0, len(m.ride.riders))
	for _, r := range m.ride.riders {
		out = append(out, r)
	}
	return out
}

func (m *Mob) AddPassenger(e api.Entity) bool {
	r, ok := e.(*Mob)
	return ok && r.mount(m)
}

func (m *Mob) RemovePassenger(e api.Entity) bool {
	r, ok := e.(*Mob)
	return ok && r.vehicleMob() == m && r.dismount()
}

func (m *Mob) Vehicle() (api.Entity, bool) {
	if v := m.vehicleMob(); v != nil {
		return v, true
	}
	return nil, false
}
