package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func startServerIn(t *testing.T, dir string) *Server {
	t.Helper()
	props := config.DefaultServerProperties()
	props.OnlineMode = false
	props.LevelType = "minecraft:flat"
	props.ViewDistance = 4
	props.SimulationDistance = 4
	props.Gamemode = "survival"
	s, err := New(Config{Dir: dir, Props: props, Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorld(s, WorldConfig{Name: "world", Dir: dir + "/world", Seed: 1, Flat: true})
	if err != nil {
		t.Fatal(err)
	}
	s.AddWorld(w)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestPlayerDataSurvivesRestart saves a player on quit and stop, then loads
// position, rotation, inventory with components, selected slot, vitals,
// effects, game mode and experience after a restart.
func TestPlayerDataSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s := startServerIn(t, dir)
	c := dial(t, s.Addr(), "Keeper")
	pos := c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Keeper").(*Player)

	pick, _ := item.IDOf("diamond_pickaxe")
	sharp, _ := regsync.Index("minecraft:enchantment", "minecraft:efficiency")
	tool := item.Stack{Item: pick, Count: 1}.WithDamage(17).Set(item.Enchantments, item.EncodeEnchantments([]item.Enchantment{{ID: sharp, Level: 4}}))
	dirt, _ := item.IDOf("dirt")
	helmet, _ := item.IDOf("iron_helmet")
	haste, _ := item.BuiltinID("mob_effect", "haste")
	to := api.Vec3{X: pos.X + 3.25, Y: pos.Y, Z: pos.Z - 1.5}
	done := make(chan struct{})
	p.tasks.add(pending{t: &task{sched: s.sched}, run: func(*task) {
		p.inv.mu.Lock()
		p.inv.set(4, tool)
		p.inv.set(20, item.Stack{Item: dirt, Count: 37})
		p.inv.set(invArmorFeet+3, item.Stack{Item: helmet, Count: 1})
		p.inv.mu.Unlock()
		p.held.Store(4)
		p.mu.Lock()
		p.e.Pos, p.e.Yaw, p.e.Pitch = to, 123.5, -12.25
		p.health, p.food, p.saturation = 13.5, 11, 2.5
		p.expLevel, p.expProgress = 7, 0.25
		p.mu.Unlock()
		p.use.hunger.Exhaustion = 1.75
		p.addEffect(item.Effect{ID: haste, Amplifier: 1, Duration: 2400, ShowParticles: true, ShowIcon: true})
		p.SetGameMode(api.Adventure)
		close(done)
	}})
	<-done
	c.nc.Close()
	waitFor(t, func() bool { return len(s.Players()) == 0 })
	if err := s.Stop(api.Text("stop")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "world", "playerdata", p.prof.UUID.String()+".dat")); err != nil {
		t.Fatal(err)
	}

	s2 := startServerIn(t, dir)
	t.Cleanup(func() { s2.Stop(api.Text("stop")) })
	c2 := dial(t, s2.Addr(), "Keeper")
	c2.join(s2.Addr())
	login := expect[*packets.ClientboundLogin](c2, nil)
	if login.WorldState.Gamemode != int32(api.Adventure) {
		t.Fatalf("game mode %d", login.WorldState.Gamemode)
	}
	at := expect[*packets.ClientboundPlayerPosition](c2, nil)
	if at.X != to.X || at.Y != to.Y || at.Z != to.Z || at.Yaw != 123.5 || at.Pitch != -12.25 {
		t.Fatalf("position %+v, want %+v", at, to)
	}
	expect[*packets.ClientboundUpdateMobEffect](c2, func(m *packets.ClientboundUpdateMobEffect) bool {
		return m.EffectID == haste && m.Amplifier == 1 && m.Duration > 2300 && m.Duration < 2400
	})
	xp := expect[*packets.ClientboundSetExperience](c2, nil)
	if xp.Level != 7 || xp.ExperienceBar != 0.25 {
		t.Fatalf("xp %+v", xp)
	}
	q := mustPlayer(t, s2, "Keeper").(*Player)
	q.inv.mu.Lock()
	got4, got20, gotHead := q.inv.get(4), q.inv.get(20), q.inv.get(invArmorFeet+3)
	q.inv.mu.Unlock()
	if !item.Matches(got4, tool) || got4.Damage() != 17 || got4.EnchantmentLevel("minecraft:efficiency") != 4 {
		t.Fatalf("tool %+v", got4)
	}
	if got20.Item != dirt || got20.Count != 37 || gotHead.Item != helmet {
		t.Fatalf("inventory %+v %+v", got20, gotHead)
	}
	if q.held.Load() != 4 || q.Health() != 13.5 || q.Food() != 11 || q.Saturation() != 2.5 {
		t.Fatalf("vitals held=%d health=%v food=%d sat=%v", q.held.Load(), q.Health(), q.Food(), q.Saturation())
	}
}
