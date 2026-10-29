package server

import (
	"math"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func TestFindPortalShape(t *testing.T) {
	ob := data.MustState("minecraft:obsidian")
	for _, tc := range []struct {
		w, h   int
		corner bool
		ok     bool
	}{{2, 3, false, true}, {2, 3, true, true}, {1, 3, true, false}, {2, 2, true, false}, {21, 21, false, true}, {22, 3, false, false}, {4, 22, false, false}} {
		m := map[api.BlockPos]data.StateID{}
		for i := -1; i <= tc.w; i++ {
			for h := -1; h <= tc.h; h++ {
				side, cap := i == -1 || i == tc.w, h == -1 || h == tc.h
				if side && cap && !tc.corner {
					continue
				}
				if side || cap {
					m[api.BlockPos{X: i, Y: 10 + h}] = ob
				}
			}
		}
		src := overlay{emptyBlocks{}, m}
		sh, ok := findEmptyPortalShape(src, api.BlockPos{X: 0, Y: 10}, 0)
		if ok != tc.ok || ok && (sh.width != tc.w || sh.height != tc.h || sh.axis != axisX) {
			t.Errorf("%dx%d corners=%v: got %+v %v", tc.w, tc.h, tc.corner, sh, ok)
		}
	}
}

type emptyBlocks struct{}

func (emptyBlocks) Block(int, int, int) data.StateID { return data.Air }

// TestNetherPortalTravel lights a frame with flint and steel, walks a
// creative player into it, checks the nether arrival portal and breaks the
// frame.
func TestNetherPortalTravel(t *testing.T) {
	s := startServer(t, nil)
	w := s.defaultWorld()
	if err := s.OpenDimensions(w, 1, 1); err != nil {
		t.Fatal(err)
	}
	c := dial(t, s.Addr(), "Traveller")
	pos := c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Traveller").(*Player)
	id, _ := item.IDOf("minecraft:flint_and_steel")
	p.setHand(api.MainHand, item.Stack{Item: id, Count: 1})

	ob := data.MustState("minecraft:obsidian")
	x0, y0, z0 := int(pos.X)+40, int(pos.Y), int(pos.Z)
	for i := -1; i <= 2; i++ {
		for h := -1; h <= 3; h++ {
			if i == -1 || i == 2 || h == -1 || h == 3 {
				w.w.SetBlock(x0+i, y0+h, z0, ob)
			} else {
				w.w.SetBlock(x0+i, y0+h, z0, data.Air)
			}
		}
	}
	waitFor(t, func() bool { return w.w.Block(x0+2, y0+3, z0) == ob })
	p.teleport(api.Location{Pos: api.Vec3{X: float64(x0) - 2.5, Y: float64(y0), Z: float64(z0) + 0.5}})
	c.send(&packets.ServerboundUseItemOn{
		Location: proto.BlockPos{X: int32(x0), Y: int32(y0 - 1), Z: int32(z0)}, Direction: 1,
		CursorX: 0.5, CursorY: 1, CursorZ: 0.5, Sequence: 1,
	})
	px := portalBlocks().portalX
	waitFor(t, func() bool { return w.w.Block(x0+1, y0+2, z0) == px && w.w.Block(x0, y0, z0) == px })

	p.teleport(api.Location{Pos: api.Vec3{X: float64(x0) + 1, Y: float64(y0), Z: float64(z0) + 0.5}})
	r := expect[*packets.ClientboundRespawn](c, nil)
	if r.WorldState.Name != "minecraft:the_nether" {
		t.Fatalf("respawn into %q", r.WorldState.Name)
	}
	nether := s.dimWorld(dimNether)
	waitFor(t, func() bool { return p.world.Load() == nether })
	at := p.Location().Pos
	if math.Abs(at.X-float64(x0+1)/8) > 2 || math.Abs(at.Z-(float64(z0)+0.5)/8) > 2 {
		t.Errorf("arrived at %v, want near %v,%v", at, float64(x0+1)/8, (float64(z0)+0.5)/8)
	}
	b := blockAt(at)
	waitFor(t, func() bool { return isNetherPortal(nether.w.Block(b.X, b.Y, b.Z)) })

	// Breaking a side of the overworld frame removes the portal.
	w.w.SetBlock(x0-1, y0+1, z0, data.Air)
	waitFor(t, func() bool { return data.IsAir(w.w.Block(x0, y0, z0)) && data.IsAir(w.w.Block(x0+1, y0+2, z0)) })
}

// TestEndPortalTravel fills the last frame of a ring with an eye, enters
// the end on the spawn platform and leaves through an end portal.
func TestEndPortalTravel(t *testing.T) {
	s := startServer(t, nil)
	w := s.defaultWorld()
	if err := s.OpenDimensions(w, 1, 1); err != nil {
		t.Fatal(err)
	}
	c := dial(t, s.Addr(), "Seeker")
	pos := c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Seeker").(*Player)
	id, _ := item.IDOf("minecraft:ender_eye")
	p.setHand(api.MainHand, item.Stack{Item: id, Count: 1})

	cx, cy, cz := int(pos.X)+20, int(pos.Y), int(pos.Z)
	frame := data.MustState("minecraft:end_portal_frame")
	last := api.BlockPos{}
	for i, f := range endRing {
		s, _ := data.With(frame, "facing", f.facing)
		if i > 0 {
			s, _ = data.With(s, "eye", "true")
		} else {
			last = api.BlockPos{X: cx + f.dx, Y: cy, Z: cz + f.dz}
		}
		w.w.SetBlock(cx+f.dx, cy, cz+f.dz, s)
	}
	waitFor(t, func() bool { return blockID(w.w.Block(last.X, last.Y, last.Z)) == portalBlocks().endFrame })
	p.teleport(api.Location{Pos: api.Vec3{X: float64(cx) + 0.5, Y: float64(cy), Z: float64(cz) - 4}})
	c.send(&packets.ServerboundUseItemOn{
		Location: proto.BlockPos{X: int32(last.X), Y: int32(last.Y), Z: int32(last.Z)}, Direction: 1,
		CursorX: 0.5, CursorY: 1, CursorZ: 0.5, Sequence: 1,
	})
	ep := portalBlocks().endPortal
	waitFor(t, func() bool { return blockID(w.w.Block(cx, cy, cz)) == ep && blockID(w.w.Block(cx+1, cy, cz-1)) == ep })

	p.teleport(api.Location{Pos: api.Vec3{X: float64(cx) + 0.5, Y: float64(cy), Z: float64(cz) + 0.5}})
	r := expect[*packets.ClientboundRespawn](c, nil)
	if r.WorldState.Name != "minecraft:the_end" {
		t.Fatalf("respawn into %q", r.WorldState.Name)
	}
	end := s.dimWorld(dimEnd)
	waitFor(t, func() bool { return p.world.Load() == end })
	if at := p.Location().Pos; at != (api.Vec3{X: 100.5, Y: 49, Z: 0.5}) {
		t.Errorf("end arrival %v", at)
	}
	waitFor(t, func() bool { return isObsidian(end.w.Block(102, 48, -2)) && data.IsAir(end.w.Block(100, 51, 0)) })

	// An end portal in the end returns the player to the overworld spawn.
	epState, _ := data.DefaultState("end_portal")
	end.w.SetBlock(101, 49, 0, epState)
	waitFor(t, func() bool { return blockID(end.w.Block(101, 49, 0)) == ep })
	time.Sleep(time.Second) // portal cooldown
	p.teleport(api.Location{Pos: api.Vec3{X: 101.5, Y: 49, Z: 0.5}})
	r = expect[*packets.ClientboundRespawn](c, func(r *packets.ClientboundRespawn) bool { return r.WorldState.Name == "minecraft:overworld" })
	waitFor(t, func() bool { return p.world.Load() == w })
}

// TestEndRingFacing pins the reference probe result: frames must face the
// centre (the north row faces south); an outward ring does not open.
func TestEndRingFacing(t *testing.T) {
	frame := data.MustState("minecraft:end_portal_frame")
	for _, inward := range []bool{true, false} {
		m := map[api.BlockPos]data.StateID{}
		opp := map[string]string{"north": "south", "south": "north", "east": "west", "west": "east"}
		for _, f := range endRing {
			fc := f.facing
			if !inward {
				fc = opp[fc]
			}
			s, _ := data.With(frame, "facing", fc)
			s, _ = data.With(s, "eye", "true")
			m[api.BlockPos{X: f.dx, Y: 5, Z: f.dz}] = s
		}
		_, ok := findEndRing(overlay{emptyBlocks{}, m}, api.BlockPos{X: 1, Y: 5, Z: 2})
		if ok != inward {
			t.Errorf("inward=%v: ring found=%v", inward, ok)
		}
	}
}
