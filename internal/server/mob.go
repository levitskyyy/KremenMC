package server

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
)

// Mob is a non-player entity driven by generic physics.
type Mob struct {
	w       *World
	typ     *data.EntityType
	mu      sync.Mutex
	e       *entity.Entity
	trk     entity.Tracked
	removed atomic.Bool
	tasks   tickQueue
	onTick  func(*Mob)

	liv     living
	proj    *projectile
	ticks   int64
	ai      *mobBrain // mob_ai.go
	name    *api.Component
	silent  bool
	glowing bool
	invuln  bool
	tags    []string
	pdata   *dataContainer
	sp      *mobSpawn // tick goroutine only
	itm     *itemEnt  // item entities only (itement_entity.go)
	life    mobLife   // mob_life.go
	ride    mobRide   // mount.go
}

func newMob(w *World, t *data.EntityType, at api.Location) *Mob {
	e := entity.New(t)
	e.Pos, e.Prev = at.Pos, at.Pos
	e.Yaw, e.Pitch, e.HeadYaw = at.Yaw, at.Pitch, at.Yaw
	m := &Mob{w: w, typ: t, e: e, pdata: newDataContainer()}
	m.liv.init(t.Name)
	m.trk = entity.Tracked{ID: e.ID, UUID: e.UUID, Type: int32(t.ID), Range: 80}
	m.initCombat()
	return m
}

func (m *Mob) EntityID() int32  { return m.e.ID }
func (m *Mob) UUID() api.UUID   { return m.e.UUID }
func (m *Mob) Type() api.Key    { return api.Key("minecraft:" + m.typ.Name) }
func (m *Mob) World() api.World { return m.w }

func (m *Mob) Location() api.Location {
	m.mu.Lock()
	defer m.mu.Unlock()
	return api.Location{World: m.w.name, Pos: m.e.Pos, Yaw: m.e.Yaw, Pitch: m.e.Pitch}
}

func (m *Mob) Velocity() api.Vec3 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.e.Vel
}

func (m *Mob) SetVelocity(v api.Vec3) {
	m.mu.Lock()
	m.e.Vel = v
	m.mu.Unlock()
}

func (m *Mob) OnGround() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.e.OnGround
}

func (m *Mob) BoundingBox() (api.Vec3, api.Vec3) {
	m.mu.Lock()
	b := m.e.Box()
	m.mu.Unlock()
	return api.Vec3{X: b.MinX, Y: b.MinY, Z: b.MinZ}, api.Vec3{X: b.MaxX, Y: b.MaxY, Z: b.MaxZ}
}

func (m *Mob) Teleport(to api.Location, _ api.TeleportCause) bool {
	if to.World != "" && to.World != m.w.name {
		return false
	}
	m.mu.Lock()
	m.e.Pos, m.e.Prev, m.e.Yaw, m.e.Pitch = to.Pos, to.Pos, to.Yaw, to.Pitch
	m.mu.Unlock()
	return true
}

func (m *Mob) CustomName() (api.Component, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.name == nil {
		return api.Component{}, false
	}
	return *m.name, true
}

func (m *Mob) SetCustomName(c *api.Component) {
	m.mu.Lock()
	m.name = c
	m.mu.Unlock()
}

func (m *Mob) Silent() bool           { m.mu.Lock(); defer m.mu.Unlock(); return m.silent }
func (m *Mob) SetSilent(v bool)       { m.mu.Lock(); m.silent = v; m.mu.Unlock() }
func (m *Mob) Glowing() bool          { m.mu.Lock(); defer m.mu.Unlock(); return m.glowing }
func (m *Mob) SetGlowing(v bool)      { m.mu.Lock(); m.glowing = v; m.mu.Unlock() }
func (m *Mob) Invulnerable() bool     { m.mu.Lock(); defer m.mu.Unlock(); return m.invuln }
func (m *Mob) SetInvulnerable(v bool) { m.mu.Lock(); m.invuln = v; m.mu.Unlock() }
func (m *Mob) Gravity() bool          { m.mu.Lock(); defer m.mu.Unlock(); return !m.e.NoGravity }
func (m *Mob) SetGravity(v bool)      { m.mu.Lock(); m.e.NoGravity = !v; m.mu.Unlock() }

func (m *Mob) Tags() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.tags)
}

func (m *Mob) AddTag(t string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.Contains(m.tags, t) {
		return false
	}
	m.tags = append(m.tags, t)
	return true
}

func (m *Mob) RemoveTag(t string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.tags)
	m.tags = slices.DeleteFunc(m.tags, func(x string) bool { return x == t })
	return len(m.tags) != n
}

func (m *Mob) TicksLived() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ticks
}

func (m *Mob) IsValid() bool { return !m.removed.Load() }

func (m *Mob) Remove() {
	if !m.removed.Swap(true) {
		m.tasks.drop(nil)
	}
}

func (m *Mob) PersistentData() api.DataContainer { return m.pdata }

func (m *Mob) Scheduler() api.EntityScheduler {
	return entitySched{queueSched: queueSched{s: m.w.s.sched, q: &m.tasks}, valid: m.IsValid}
}

var _ api.Entity = (*Mob)(nil)
