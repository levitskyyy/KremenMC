package server

import (
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func TestFallDamageFromMoves(t *testing.T) {
	tests := []struct {
		name   string
		height float64
		want   float32
	}{
		{"safe", 3, 20},
		{"ten blocks", 10, 13},
	}
	s := startServer(t, survival)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := dial(t, s.Addr(), "Faller")
			pos := c.joinPlay(s.Addr())
			defer c.nc.Close()
			p := mustPlayer(t, s, "Faller").(*Player)
			p.teleport(api.Location{Pos: api.Vec3{X: pos.X, Y: pos.Y + tt.height, Z: pos.Z}})
			waitFor(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.pendingTP == 0 })
			for y := pos.Y + tt.height - 0.5; y > pos.Y; y -= 0.5 {
				c.send(&packets.ServerboundMovePlayerPos{X: pos.X, Y: y, Z: pos.Z})
			}
			c.send(&packets.ServerboundMovePlayerPos{X: pos.X, Y: pos.Y, Z: pos.Z, Flags: packets.MovementFlagsOnGround})
			if tt.want < 20 {
				expect(c, func(h *packets.ClientboundSetHealth) bool { return h.Health == tt.want })
			} else {
				c.send(&packets.ServerboundChatCommand{Command: "nosuchcommand"})
				expect[*packets.ClientboundSystemChat](c, nil)
				if h := p.Health(); h != 20 {
					t.Fatalf("health %v after a safe fall", h)
				}
			}
			waitFor(t, func() bool { return len(s.Players()) == 1 })
		})
	}
}

func TestDeathAndRespawn(t *testing.T) {
	s := startServer(t, survival)
	c := dial(t, s.Addr(), "Doomed")
	c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Doomed").(*Player)
	waitFor(t, func() bool { return p.liv.synced })
	if !p.Hurt(NewDamage("minecraft:generic"), 100) {
		t.Fatal("not hurt")
	}
	kill := expect(c, func(k *packets.ClientboundPlayerCombatKill) bool { return k.PlayerID == p.e.ID })
	if kill.Message.Translate != "death.attack.generic" {
		t.Fatalf("death message %+v", kill.Message)
	}
	if p.Hurt(NewDamage("minecraft:generic"), 1) {
		t.Fatal("dead player hurt again")
	}
	c.send(&packets.ServerboundClientCommand{ActionID: 0})
	expect[*packets.ClientboundRespawn](c, nil)
	expect(c, func(h *packets.ClientboundSetHealth) bool { return h.Health == 20 })
	if p.IsDead() {
		t.Fatal("still dead")
	}
}

func TestMobDiesAndIsRemoved(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Viewer")
	pos := c.joinPlay(s.Addr())
	w := s.defaultWorld()
	e, err := w.SpawnEntity("minecraft:zombie", api.Location{Pos: api.Vec3{X: pos.X + 2, Y: pos.Y, Z: pos.Z}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	z := e.(*Mob)
	expect(c, func(a *packets.ClientboundUpdateAttributes) bool {
		return a.EntityID == z.e.ID && len(a.Properties) > 0
	})
	if h := z.Health(); h != 20 {
		t.Fatalf("zombie health %v", h)
	}
	if !z.Hurt(EntityDamage("minecraft:player_attack", mustPlayer(t, s, "Viewer")), 30) {
		t.Fatal("not hurt")
	}
	expect(c, func(d *packets.ClientboundDamageEvent) bool { return d.EntityID == z.e.ID })
	expect(c, func(d *packets.ClientboundEntityEvent) bool { return d.EntityID == z.e.ID && d.EntityStatus == 3 })
	expect(c, func(r *packets.ClientboundRemoveEntities) bool {
		return len(r.EntityIDs) == 1 && r.EntityIDs[0] == z.e.ID
	})
}

func TestMobDeathLoot(t *testing.T) {
	s := startServer(t, nil)
	w := s.defaultWorld()
	for _, tc := range []struct {
		fire bool
		want string
	}{{false, "minecraft:chicken"}, {true, "minecraft:cooked_chicken"}} {
		e, err := w.SpawnEntity("minecraft:chicken", api.Location{Pos: api.Vec3{Y: 100}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := e.(*Mob)
		if tc.fire {
			m.liv.mu.Lock()
			m.liv.fire = 100
			m.liv.mu.Unlock()
		}
		got := 0
		for _, d := range mobDeathLoot(m, NewDamage("minecraft:generic"), nil) {
			if item.NameOf(d.Item) == tc.want {
				got += int(d.Count)
			}
		}
		if got != 1 {
			t.Errorf("fire=%v: %d × %s", tc.fire, got, tc.want)
		}
	}
}
