package kremenmc

import (
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/plugin"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

type stubPlayer struct {
	api.Player
	name string
	id   api.UUID
}

func (p stubPlayer) Name() string    { return p.name }
func (p stubPlayer) UUID() api.UUID  { return p.id }
func (p stubPlayer) EntityID() int32 { return 7 }
func (p stubPlayer) Location() api.Location {
	return api.Location{World: "w", Pos: api.Vec3{X: 1, Y: 2, Z: 3}, Yaw: 90}
}
func (p stubPlayer) GameMode() api.GameMode { return api.Creative }
func (p stubPlayer) Health() float64        { return 17.5 }
func (p stubPlayer) IsOp() bool             { return true }
func (p stubPlayer) Type() api.Key          { return "minecraft:player" }

type stubServer struct {
	api.Server
	players map[api.UUID]api.Player
}

func (s stubServer) Player(id api.UUID) (api.Player, bool) { p, ok := s.players[id]; return p, ok }

// TestEventLayoutsMatchHost encodes every built-in event on the host side and
// decodes it with the SDK, requiring the payload to be consumed exactly.
func TestEventLayoutsMatchHost(t *testing.T) {
	steve := stubPlayer{name: "Steve", id: api.UUID{1}}
	msg := api.Text("m")
	item := api.ItemStack{Type: "minecraft:stone", Count: 3, Name: &msg, Lore: []api.Component{msg}}
	cases := []struct {
		host  api.Event
		guest Event
		check func(Event) bool
	}{
		{&api.PlayerPreLoginEvent{Name: "Steve", UUID: steve.id, Address: "1.2.3.4"}, &PlayerPreLoginEvent{}, func(e Event) bool { return e.(*PlayerPreLoginEvent).Address == "1.2.3.4" }},
		{&api.PlayerLoginEvent{Player: steve, Hostname: "h"}, &PlayerLoginEvent{}, func(e Event) bool { return e.(*PlayerLoginEvent).Hostname == "h" }},
		{&api.PlayerJoinEvent{Player: steve, Message: &msg}, &PlayerJoinEvent{}, func(e Event) bool {
			j := e.(*PlayerJoinEvent)
			return j.Player.Name == "Steve" && j.Player.Health == 17.5 && j.Player.Location.Yaw == 90 && j.Message.Text == "m"
		}},
		{&api.PlayerQuitEvent{Player: steve, Reason: api.QuitKicked}, &PlayerQuitEvent{}, func(e Event) bool { return e.(*PlayerQuitEvent).Reason == api.QuitKicked }},
		{&api.PlayerKickEvent{Player: steve, Reason: msg}, &PlayerKickEvent{}, nil},
		{&api.PlayerChatEvent{Player: steve, Message: "hi", Recipients: []api.Player{steve}}, &PlayerChatEvent{}, func(e Event) bool {
			c := e.(*PlayerChatEvent)
			return c.Message == "hi" && len(c.Recipients) == 1 && c.Recipients[0] == steve.id
		}},
		{&api.PlayerCommandEvent{Player: steve, Line: "tp"}, &PlayerCommandEvent{}, nil},
		{&api.ServerCommandEvent{Line: "stop"}, &ServerCommandEvent{}, nil},
		{&api.PlayerMoveEvent{Player: steve, To: api.Location{World: "w", Pos: api.Vec3{Y: 5}}}, &PlayerMoveEvent{}, func(e Event) bool { return e.(*PlayerMoveEvent).To.Pos.Y == 5 }},
		{&api.PlayerTeleportEvent{Player: steve, Cause: api.TeleportCommand}, &PlayerTeleportEvent{}, func(e Event) bool { return e.(*PlayerTeleportEvent).Cause == api.TeleportCommand }},
		{&api.PlayerChangeWorldEvent{Player: steve}, &PlayerChangeWorldEvent{}, nil},
		{&api.PlayerRespawnEvent{Player: steve, BedSpawn: true}, &PlayerRespawnEvent{}, func(e Event) bool { return e.(*PlayerRespawnEvent).BedSpawn }},
		{&api.BlockBreakEvent{Player: steve, DropItems: true, Exp: 4}, &BlockBreakEvent{}, func(e Event) bool { return e.(*BlockBreakEvent).Exp == 4 }},
		{&api.BlockPlaceEvent{Player: steve, Item: item, CanBuild: true, Against: api.BlockPos{X: -1, Y: 2, Z: 3}}, &BlockPlaceEvent{}, func(e Event) bool {
			b := e.(*BlockPlaceEvent)
			return b.Item.Count == 3 && b.Item.Name.Text == "m" && b.Against.X == -1 && b.CanBuild
		}},
		{&api.PlayerInteractEvent{Player: steve, Action: api.RightClickAir, Item: item}, &PlayerInteractEvent{}, func(e Event) bool { return e.(*PlayerInteractEvent).Block == nil }},
		{&api.PlayerInteractEntityEvent{Player: steve, Target: steve, Attack: true}, &PlayerInteractEntityEvent{}, func(e Event) bool { return e.(*PlayerInteractEntityEvent).Target.EntityID == 7 }},
		{&api.EntityDamageEvent{Entity: steve, Damage: 3.5, Source: api.DamageSource{Type: "minecraft:fall", Cause: api.DamageFall, Direct: steve}}, &EntityDamageEvent{}, func(e Event) bool {
			d := e.(*EntityDamageEvent)
			return d.Damage == 3.5 && d.Damager != nil && d.DamageType == "minecraft:fall"
		}},
		{&api.EntityDeathEvent{Entity: steve, Drops: []api.ItemStack{item, item}, Exp: 9}, &EntityDeathEvent{}, func(e Event) bool { return len(e.(*EntityDeathEvent).Drops) == 2 }},
		{&api.EntitySpawnEvent{Entity: steve, Reason: api.SpawnEgg}, &EntitySpawnEvent{}, nil},
		{&api.WorldLoadEvent{}, &WorldLoadEvent{}, nil},
		{&api.WorldUnloadEvent{}, &WorldUnloadEvent{}, nil},
		{&api.ChunkLoadEvent{X: -3, Z: 4, New: true}, &ChunkLoadEvent{}, func(e Event) bool { c := e.(*ChunkLoadEvent); return c.X == -3 && c.Z == 4 && c.New }},
		{&api.ChunkUnloadEvent{X: 1, Save: true}, &ChunkUnloadEvent{}, nil},
		{&api.ServerTickStartEvent{Tick: 99}, &ServerTickStartEvent{}, func(e Event) bool { return e.(*ServerTickStartEvent).Tick == 99 }},
		{&api.ServerTickEndEvent{Tick: 1, DurationNanos: 2, RemainingNanos: -3}, &ServerTickEndEvent{}, func(e Event) bool { return e.(*ServerTickEndEvent).RemainingNanos == -3 }},
		{&api.PluginEnableEvent{Plugin: api.PluginInfo{ID: "x"}}, &PluginEnableEvent{}, func(e Event) bool { return e.(*PluginEnableEvent).Plugin.ID == "x" }},
		{&api.PluginDisableEvent{}, &PluginDisableEvent{}, nil},
		{&api.InventoryClickEvent{Player: steve, Slot: -999, Current: item}, &InventoryClickEvent{}, func(e Event) bool { return e.(*InventoryClickEvent).Slot == -999 }},
		{&api.InventoryCloseEvent{Player: steve}, &InventoryCloseEvent{}, nil},
		{&api.PluginMessageEvent{Player: steve, Channel: "bungeecord:main", Data: []byte{1, 2}}, &PluginMessageEvent{}, func(e Event) bool {
			m := e.(*PluginMessageEvent)
			return m.Channel == "bungeecord:main" && len(m.Data) == 2
		}},
	}
	seen := map[api.EventKind]bool{}
	for _, c := range cases {
		t.Run(c.host.Kind().String(), func(t *testing.T) {
			if c.host.Kind() != c.guest.Kind() {
				t.Fatalf("kind mismatch %v vs %v", c.host.Kind(), c.guest.Kind())
			}
			seen[c.host.Kind()] = true
			var w wire.Writer
			if !plugin.EncodeEvent(&w, c.host) {
				t.Fatal("host cannot encode")
			}
			r := wire.NewReader(w.B)
			c.guest.decode(r)
			if r.Err != nil || r.Len() != 0 {
				t.Fatalf("decode err=%v leftover=%d", r.Err, r.Len())
			}
			if c.check != nil && !c.check(c.guest) {
				t.Errorf("decoded fields wrong: %+v", c.guest)
			}
		})
	}
	for k := api.KindPlayerPreLogin; k < api.BuiltinKinds; k++ {
		if !seen[k] {
			t.Errorf("no layout test for %v", k)
		}
	}
}

func response(e Event) []byte {
	b := e.base()
	var w wire.Writer
	w.U8(b.cancel)
	w.Raw(b.muts.B)
	return w.B
}

func TestEventMutationsApplyOnHost(t *testing.T) {
	steve := stubPlayer{name: "Steve", id: api.UUID{1}}
	alex := stubPlayer{name: "Alex", id: api.UUID{2}}
	srv := stubServer{players: map[api.UUID]api.Player{steve.id: steve, alex.id: alex}}
	msg := api.Text("bye")

	chat := &api.PlayerChatEvent{Player: steve, Message: "a", Recipients: []api.Player{steve, alex}}
	g := &PlayerChatEvent{}
	g.SetMessage("b")
	g.SetRecipients([]api.UUID{alex.id})
	g.SetCancelled(true)
	if err := plugin.ApplyResponse(chat, response(g), srv); err != nil {
		t.Fatal(err)
	}
	if chat.Message != "b" || !chat.Cancelled() || len(chat.Recipients) != 1 || chat.Recipients[0].Name() != "Alex" {
		t.Errorf("chat = %+v", chat)
	}

	quit := &api.PlayerQuitEvent{Message: &msg}
	gq := &PlayerQuitEvent{}
	gq.SetMessage(nil)
	plugin.ApplyResponse(quit, response(gq), srv)
	if quit.Message != nil {
		t.Error("quit message not cleared")
	}

	pre := &api.PlayerPreLoginEvent{}
	gp := &PlayerPreLoginEvent{}
	gp.Deny(api.LoginKickWhitelist, api.Text("no"))
	plugin.ApplyResponse(pre, response(gp), srv)
	if pre.Result != api.LoginKickWhitelist || pre.Message.Text != "no" {
		t.Errorf("prelogin = %+v", pre)
	}

	dmg := &api.EntityDamageEvent{Damage: 5}
	gd := &EntityDamageEvent{}
	gd.SetDamage(1.25)
	plugin.ApplyResponse(dmg, response(gd), srv)
	if dmg.Damage != 1.25 {
		t.Errorf("damage = %v", dmg.Damage)
	}

	death := &api.EntityDeathEvent{}
	gx := &EntityDeathEvent{}
	gx.SetExp(0)
	gx.SetKeepInventory(true)
	gx.SetDeathMessage(&msg)
	plugin.ApplyResponse(death, response(gx), srv)
	if !death.KeepInventory || death.DeathMessage == nil || death.DeathMessage.Text != "bye" {
		t.Errorf("death = %+v", death)
	}

	brk := &api.BlockBreakEvent{DropItems: true, Exp: 3}
	gb := &BlockBreakEvent{}
	gb.SetDropItems(false)
	gb.SetExp(0)
	gb.SetCancelled(false)
	brk.SetCancelled(true)
	plugin.ApplyResponse(brk, response(gb), srv)
	if brk.DropItems || brk.Exp != 0 || brk.Cancelled() {
		t.Errorf("break = %+v", brk)
	}
}

func TestDecodeCommand(t *testing.T) {
	var w wire.Writer
	w.U8(wire.SenderConsole)
	w.String("CONSOLE")
	w.String("give x 5")
	w.String("give")
	w.Uvarint(3)
	w.String("n")
	w.U8(wire.ValInt)
	w.Varint(5)
	w.String("who")
	w.U8(wire.ValUUIDs)
	w.Uvarint(1)
	w.UUID(api.UUID{9})
	w.String("at")
	w.U8(wire.ValBlockPos)
	w.BlockPos(api.BlockPos{X: 1, Y: -2, Z: 3})
	c, err := decodeCommand(w.B)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsPlayer() || c.Int("n") != 5 || c.Float("n") != 5 || c.Targets("who")[0] != (api.UUID{9}) || c.BlockPos("at").Y != -2 || c.Has("missing") {
		t.Errorf("ctx = %+v", c)
	}
}
