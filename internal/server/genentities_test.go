package server

import (
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// TestGenEntitiesSpawnOnce gives a live chunk generated entities and checks
// that the server adds the mob and the end crystal once, and keeps the kind
// it cannot run (a chest minecart) with the chunk.
func TestGenEntitiesSpawnOnce(t *testing.T) {
	s := startServerIn(t, t.TempDir())
	defer s.Stop(api.Text("stop"))
	w := s.defaultWorld()
	var c *world.Chunk
	for range 200 {
		if c = w.w.Chunk(world.ChunkPos{}); c != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("chunk 0,0 not loaded")
	}
	pos := func(x, y, z float64) nbt.List { return nbt.List{Type: nbt.TagDouble, Items: []any{x, y, z}} }
	rot := nbt.List{Type: nbt.TagFloat, Items: []any{float32(90), float32(0)}}
	c.Lock()
	c.GenEntities = []nbt.Compound{
		{"id": "minecraft:end_crystal", "Pos": pos(3.5, 70, 4.5), "Rotation": rot, "ShowBottom": int8(1)},
		{"id": "minecraft:witch", "Pos": pos(5.5, 70, 5.5), "Rotation": rot, "PersistenceRequired": int8(1)},
		{"id": "minecraft:chest_minecart", "Pos": pos(6.5, 70, 6.5), "Rotation": rot},
	}
	c.Unlock()
	for range 2 {
		w.queueGenEntities(c)
		w.spawnGenEntities()
	}
	got := map[string]int{}
	for _, m := range w.mobList() {
		l := m.Location()
		switch m.typ.Name {
		case "end_crystal":
			if l.Pos != (api.Vec3{X: 3.5, Y: 70, Z: 4.5}) || l.Yaw != 90 || !m.e.NoGravity {
				t.Errorf("crystal at %v yaw %v gravity %v", l.Pos, l.Yaw, !m.e.NoGravity)
			}
		case "witch":
			if m.sp == nil || !m.sp.persistent {
				t.Error("witch not persistent")
			}
		}
		got[m.typ.Name]++
	}
	if got["end_crystal"] != 1 || got["witch"] != 1 || got["chest_minecart"] != 0 {
		t.Fatalf("spawned %v", got)
	}
	c.RLock()
	defer c.RUnlock()
	if len(c.GenEntities) != 1 || c.GenEntities[0]["id"] != "minecraft:chest_minecart" {
		t.Fatalf("left in chunk: %v", c.GenEntities)
	}
}

// TestGenEntitiesPassengers spawns generated vehicles with Passengers (an
// ocean ruin drowned on a zombie nautilus, a chicken jockey) and checks the
// riders are seated on them and follow them.
func TestGenEntitiesPassengers(t *testing.T) {
	s := startServerIn(t, t.TempDir())
	defer s.Stop(api.Text("stop"))
	w := s.defaultWorld()
	var c *world.Chunk
	for range 200 {
		if c = w.w.Chunk(world.ChunkPos{}); c != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("chunk 0,0 not loaded")
	}
	pos := func(x, y, z float64) nbt.List { return nbt.List{Type: nbt.TagDouble, Items: []any{x, y, z}} }
	rider := func(id string, x, y, z float64) nbt.List {
		return nbt.List{Type: nbt.TagCompound, Items: []any{nbt.Compound{"id": id, "Pos": pos(x, y, z), "PersistenceRequired": int8(1)}}}
	}
	c.Lock()
	c.GenEntities = []nbt.Compound{
		{"id": "minecraft:zombie_nautilus", "Pos": pos(3.5, 60, 4.5), "PersistenceRequired": int8(1), "Passengers": rider("minecraft:drowned", 3.5, 60, 4.5)},
		{"id": "minecraft:chicken", "Pos": pos(8.5, 70, 8.5), "Passengers": rider("minecraft:zombie", 8.5, 70, 8.5)},
	}
	c.Unlock()
	w.queueGenEntities(c)
	w.spawnGenEntities()
	byType := map[string]*Mob{}
	for _, m := range w.mobList() {
		byType[m.typ.Name] = m
	}
	for _, p := range [][2]string{{"zombie_nautilus", "drowned"}, {"chicken", "zombie"}} {
		v, r := byType[p[0]], byType[p[1]]
		if v == nil || r == nil {
			t.Fatalf("%s/%s not spawned: %v", p[0], p[1], byType)
		}
		if got, ok := r.Vehicle(); !ok || got != v {
			t.Errorf("%s does not ride the %s", p[1], p[0])
		}
		if ps := v.Passengers(); len(ps) != 1 || ps[0] != r {
			t.Errorf("%s passengers %v", p[0], ps)
		}
		v.mu.Lock()
		v.e.Pos.X += 2
		vp := v.e.Pos
		v.mu.Unlock()
		r.tickRide()
		if l := r.Location().Pos; l.X != vp.X || l.Z != vp.Z || l.Y != vp.Y+float64(v.typ.Height) {
			t.Errorf("%s at %v, vehicle at %v", p[1], l, vp)
		}
	}
	c.RLock()
	defer c.RUnlock()
	if len(c.GenEntities) != 0 {
		t.Fatalf("left in chunk: %v", c.GenEntities)
	}
}
