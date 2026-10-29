package command

import (
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

type env struct {
	srv        *fakeServer
	alice, bob *fakePlayer
	guest      *fakePlayer
	pig        *fakeEntity
}

func newEnv() *env {
	srv := newServer()
	RegisterBuiltins(srv.cmds, srv)
	e := &env{srv: srv}
	e.alice = srv.addPlayer("Alice", api.Vec3{X: 0.5, Y: 64, Z: 0.5}, nil)
	e.bob = srv.addPlayer("Bob", api.Vec3{X: 20, Y: 70, Z: -3}, nil)
	e.guest = srv.addPlayer("Guest", api.Vec3{}, map[string]bool{
		"minecraft.command.help": true, "minecraft.command.list": true, "minecraft.command.msg": true,
	})
	e.pig = &fakeEntity{id: api.UUID{9}, kind: "minecraft:pig", loc: api.Location{World: "overworld", Pos: api.Vec3{X: 2, Y: 64}}}
	srv.worlds[0].entities = []api.Entity{e.pig}
	return e
}

func TestBuiltins(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		as    func(*env) *fakePlayer
		err   string
		check func(t *testing.T, e *env)
	}{
		{name: "gamemode self", lines: []string{"gamemode creative"}, check: func(t *testing.T, e *env) {
			if e.alice.mode != api.Creative || e.alice.fs.last() != "Set own game mode to Creative Mode" {
				t.Fatalf("mode %v msg %q", e.alice.mode, e.alice.fs.last())
			}
		}},
		{name: "gamemode other", lines: []string{"gamemode spectator Bob"}, check: func(t *testing.T, e *env) {
			if e.bob.mode != api.Spectator || e.bob.fs.messages()[0] != "Your game mode has been updated to Spectator Mode" {
				t.Fatalf("mode %v msgs %q", e.bob.mode, e.bob.fs.messages())
			}
		}},
		{name: "gamemode denied", lines: []string{"gamemode creative"}, as: func(e *env) *fakePlayer { return e.guest }, err: msgUnknownCommand},
		{name: "tp coords", lines: []string{"tp 10 65 -2"}, check: func(t *testing.T, e *env) {
			if e.alice.loc.Pos != (api.Vec3{X: 10.5, Y: 65, Z: -1.5}) {
				t.Fatalf("pos %+v", e.alice.loc.Pos)
			}
		}},
		{name: "tp relative targets", lines: []string{"teleport @a ~ ~10 ~"}, check: func(t *testing.T, e *env) {
			want := api.Vec3{X: 0.5, Y: 74, Z: 0.5}
			if e.alice.loc.Pos != want || e.bob.loc.Pos != want {
				t.Fatalf("alice %+v bob %+v", e.alice.loc.Pos, e.bob.loc.Pos)
			}
		}},
		{name: "tp to entity", lines: []string{"tp Bob"}, check: func(t *testing.T, e *env) {
			if e.alice.loc.Pos != e.bob.loc.Pos || e.alice.fs.last() != "Teleported Alice to Bob" {
				t.Fatalf("pos %+v msg %q", e.alice.loc.Pos, e.alice.fs.last())
			}
		}},
		{name: "tp entity to entity", lines: []string{"tp @e[type=pig] Bob"}, check: func(t *testing.T, e *env) {
			if e.pig.loc.Pos != e.bob.loc.Pos {
				t.Fatalf("pig at %+v", e.pig.loc.Pos)
			}
		}},
		{name: "tp rotation", lines: []string{"tp Alice 0 64 0 90 ~5"}, check: func(t *testing.T, e *env) {
			if e.alice.loc.Yaw != 90 || e.alice.loc.Pitch != 5 {
				t.Fatalf("rot %v %v", e.alice.loc.Yaw, e.alice.loc.Pitch)
			}
		}},
		{name: "tp facing", lines: []string{"tp Alice 0 64 0 facing 10 64 0"}, check: func(t *testing.T, e *env) {
			if e.alice.loc.Yaw != -90 || e.alice.loc.Pitch != 0 {
				t.Fatalf("rot %v %v", e.alice.loc.Yaw, e.alice.loc.Pitch)
			}
		}},
		{name: "tp no match", lines: []string{"tp @e[type=cow] 0 0 0"}, err: msgNoEntity},
		{name: "time", lines: []string{"time set noon", "time add 1d"}, check: func(t *testing.T, e *env) {
			if e.srv.worlds[0].time != 30000 || e.alice.fs.last() != "Set the time to 6000" {
				t.Fatalf("time %d msg %q", e.srv.worlds[0].time, e.alice.fs.last())
			}
		}},
		{name: "time query", lines: []string{"time set 25000", "time query daytime"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "The time is 1000" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "weather", lines: []string{"weather thunder 10s"}, check: func(t *testing.T, e *env) {
			if w := e.srv.worlds[0]; !w.storm || !w.thunder {
				t.Fatal("weather not set")
			}
		}},
		{name: "difficulty", lines: []string{"difficulty hard", "difficulty"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "The difficulty is Hard" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "difficulty unchanged", lines: []string{"difficulty peaceful"}, err: "The difficulty did not change; it is already set to Peaceful"},
		{name: "setblock", lines: []string{"setblock ~ ~-1 ~ oak_log[axis=y]"}, check: func(t *testing.T, e *env) {
			st := e.srv.worlds[0].BlockState(api.BlockPos{Y: 63})
			if st.String() != "minecraft:oak_log[axis=y]" || e.alice.fs.last() != "Changed the block at 0, 63, 0" {
				t.Fatalf("state %s msg %q", st, e.alice.fs.last())
			}
		}},
		{name: "setblock keep", lines: []string{"setblock 0 0 0 stone", "setblock 0 0 0 grass_block keep"}, check: func(t *testing.T, e *env) {
			if e.srv.worlds[0].BlockState(api.BlockPos{}).Type() != "minecraft:stone" || e.alice.fs.last() != "Could not set the block" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "setblock out of world", lines: []string{"setblock 0 400 0 stone"}, err: "That position is out of this world!"},
		{name: "fill", lines: []string{"fill 0 0 0 2 2 2 stone"}, check: func(t *testing.T, e *env) {
			if e.srv.worlds[0].sets != 27 || e.alice.fs.last() != "Successfully filled 27 block(s)" {
				t.Fatalf("sets %d msg %q", e.srv.worlds[0].sets, e.alice.fs.last())
			}
		}},
		{name: "fill hollow", lines: []string{"fill 0 0 0 2 2 2 stone", "fill 0 0 0 2 2 2 grass_block hollow"}, check: func(t *testing.T, e *env) {
			w := e.srv.worlds[0]
			if !w.BlockState(api.BlockPos{X: 1, Y: 1, Z: 1}).IsAir() || w.BlockState(api.BlockPos{}).Type() != "minecraft:grass_block" {
				t.Fatal("hollow fill wrong")
			}
		}},
		{name: "fill outline", lines: []string{"fill 0 0 0 2 2 2 stone", "fill 0 0 0 2 2 2 grass_block outline"}, check: func(t *testing.T, e *env) {
			w := e.srv.worlds[0]
			if w.BlockState(api.BlockPos{X: 1, Y: 1, Z: 1}).Type() != "minecraft:stone" || e.alice.fs.last() != "Successfully filled 26 block(s)" {
				t.Fatalf("outline wrong: %q", e.alice.fs.last())
			}
		}},
		{name: "fill replace filter", lines: []string{"setblock 1 1 1 stone", "fill 0 0 0 2 2 2 oak_log replace stone"}, check: func(t *testing.T, e *env) {
			w := e.srv.worlds[0]
			if w.BlockState(api.BlockPos{X: 1, Y: 1, Z: 1}).Type() != "minecraft:oak_log" || e.alice.fs.last() != "Successfully filled 1 block(s)" {
				t.Fatalf("replace wrong: %q", e.alice.fs.last())
			}
		}},
		{name: "fill keep", lines: []string{"setblock 0 0 0 stone", "fill 0 0 0 1 0 0 grass_block keep"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "Successfully filled 1 block(s)" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "fill too big", lines: []string{"fill 0 0 0 32 32 31 stone"}, err: "Too many blocks in the specified area (maximum 32768, specified 34848)"},
		{name: "give", lines: []string{"give Bob diamond 130"}, check: func(t *testing.T, e *env) {
			inv := e.bob.inv.slots
			if inv[0].Count != 64 || inv[1].Count != 64 || inv[2].Count != 2 || e.alice.fs.last() != "Gave 130 [Diamond] to Bob" {
				t.Fatalf("inv %v msg %q", inv[:3], e.alice.fs.last())
			}
		}},
		{name: "give limit", lines: []string{"give Bob diamond_sword 101"}, err: "Can't give more than 100 of Diamond Sword"},
		{name: "clear", lines: []string{"give Bob diamond 10", "give Bob stone 5", "clear Bob diamond 4"}, check: func(t *testing.T, e *env) {
			if e.bob.inv.slots[0].Count != 6 || e.alice.fs.last() != "Removed 4 item(s) from player Bob" {
				t.Fatalf("inv %v msg %q", e.bob.inv.slots[:2], e.alice.fs.last())
			}
		}},
		{name: "clear count", lines: []string{"give Bob diamond 10", "clear Bob * 0"}, check: func(t *testing.T, e *env) {
			if e.bob.inv.slots[0].Count != 10 || e.alice.fs.last() != "Found 10 matching item(s) on player Bob" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "clear empty", lines: []string{"clear Bob"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "No items were found on player Bob" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "kill", lines: []string{"kill @e[type=pig]", "kill Bob"}, check: func(t *testing.T, e *env) {
			if !e.pig.removed || !e.bob.dead || e.alice.fs.last() != "Killed Bob" {
				t.Fatalf("pig %v bob %v msg %q", e.pig.removed, e.bob.dead, e.alice.fs.last())
			}
		}},
		{name: "msg", lines: []string{"tell Bob hello there"}, as: func(e *env) *fakePlayer { return e.guest }, check: func(t *testing.T, e *env) {
			if e.bob.fs.last() != "Guest whispers to you: hello there" || e.guest.fs.last() != "You whisper to Bob: hello there" {
				t.Fatalf("bob %q guest %q", e.bob.fs.last(), e.guest.fs.last())
			}
		}},
		{name: "msg no selector", lines: []string{"msg @a hi"}, as: func(e *env) *fakePlayer { return e.guest }, err: msgSelectorNotAllowed},
		{name: "say", lines: []string{"say hi @a"}, check: func(t *testing.T, e *env) {
			if e.srv.broadcast[0] != "[Alice] hi Alice, Bob, Guest" {
				t.Fatalf("broadcast %q", e.srv.broadcast)
			}
		}},
		{name: "me", lines: []string{"me waves"}, as: func(e *env) *fakePlayer { return e.guest }, err: msgUnknownCommand},
		{name: "list", lines: []string{"list"}, as: func(e *env) *fakePlayer { return e.guest }, check: func(t *testing.T, e *env) {
			if e.guest.fs.last() != "There are 3 of a max of 20 players online: Alice, Bob, Guest" {
				t.Fatalf("msg %q", e.guest.fs.last())
			}
		}},
		{name: "help", lines: []string{"help"}, as: func(e *env) *fakePlayer { return e.guest }, check: func(t *testing.T, e *env) {
			got := strings.Join(e.guest.fs.messages(), "|")
			if got != "/help [<command>]|/list [uuids]|/msg <targets> <message>|/tell <targets> <message>|/w <targets> <message>" {
				t.Fatalf("help %q", got)
			}
		}},
		{name: "help command", lines: []string{"help gamemode"}, check: func(t *testing.T, e *env) {
			if got := strings.Join(e.alice.fs.messages(), "|"); got != "/gamemode <gamemode> [<target>]" {
				t.Fatalf("help %q", got)
			}
		}},
		{name: "ban pardon", lines: []string{"ban Bob griefing", "pardon Bob"}, check: func(t *testing.T, e *env) {
			msgs := e.alice.fs.messages()
			if e.bob.kicked != "You are banned from this server.\nReason: griefing" || msgs[0] != "Banned Bob: griefing" || msgs[1] != "Unbanned Bob" {
				t.Fatalf("kick %q msgs %q", e.bob.kicked, msgs)
			}
		}},
		{name: "ban twice", lines: []string{"ban Bob", "ban Bob"}, err: "Nothing changed. The player is already banned"},
		{name: "ban unknown", lines: []string{"ban Nobody"}, err: "That player does not exist"},
		{name: "ban-ip", lines: []string{"ban-ip Bob", "banlist ips"}, check: func(t *testing.T, e *env) {
			msgs := e.alice.fs.messages()
			if e.bob.kicked == "" || msgs[0] != "Banned IP 10.0.0.2: Banned by an operator." || msgs[len(msgs)-1] != "10.0.0.2 was banned by Alice: Banned by an operator." {
				t.Fatalf("msgs %q", msgs)
			}
		}},
		{name: "pardon-ip invalid", lines: []string{"pardon-ip nope"}, err: "Invalid IP address"},
		{name: "op deop", lines: []string{"op Bob", "deop Bob"}, check: func(t *testing.T, e *env) {
			msgs := e.alice.fs.messages()
			if e.bob.op || msgs[0] != "Made Bob a server operator" || msgs[1] != "Made Bob no longer a server operator" {
				t.Fatalf("msgs %q", msgs)
			}
		}},
		{name: "whitelist", lines: []string{"whitelist on", "whitelist add Bob", "whitelist list", "whitelist remove Bob"}, check: func(t *testing.T, e *env) {
			msgs := e.alice.fs.messages()
			if !e.srv.wl.on || len(e.srv.wl.entries) != 0 || msgs[2] != "There are 1 whitelisted player(s): Bob" {
				t.Fatalf("msgs %q", msgs)
			}
		}},
		{name: "whitelist already", lines: []string{"whitelist off"}, err: "Whitelist is already turned off"},
		{name: "kick", lines: []string{"kick Bob bye now"}, check: func(t *testing.T, e *env) {
			if e.bob.kicked != "bye now" {
				t.Fatalf("kick %q", e.bob.kicked)
			}
		}},
		{name: "gamerule", lines: []string{"gamerule keep_inventory true", "gamerule minecraft:keep_inventory"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "Gamerule keep_inventory is currently set to: true" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "gamerule invalid", lines: []string{"gamerule keep_inventory maybe"}, err: "Invalid value 'maybe' for game rule keep_inventory"},
		{name: "seed", lines: []string{"seed"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "Seed: [42]" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "setworldspawn", lines: []string{"setworldspawn 1 70 2"}, check: func(t *testing.T, e *env) {
			if e.srv.worlds[0].spawn.Pos != (api.Vec3{X: 1.5, Y: 70, Z: 2.5}) {
				t.Fatalf("spawn %+v", e.srv.worlds[0].spawn)
			}
		}},
		{name: "spawnpoint", lines: []string{"spawnpoint Bob ~ ~ ~ 45"}, check: func(t *testing.T, e *env) {
			if e.bob.respawn == nil || e.bob.respawn.Pos != (api.Vec3{X: 0.5, Y: 64, Z: 0.5}) || e.bob.respawn.Yaw != 45 {
				t.Fatalf("respawn %+v", e.bob.respawn)
			}
		}},
		{name: "defaultgamemode unsupported", lines: []string{"defaultgamemode creative"}, err: "Not supported by this server"},
		{name: "version", lines: []string{"version"}, as: func(e *env) *fakePlayer { return e.alice }, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "This server is running kremenmc version dev (Minecraft 26.3, protocol 777)" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "tps", lines: []string{"kremenmc:tps"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "TPS: 20.00" {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
		{name: "plugins fallback", lines: []string{"pl"}, check: func(t *testing.T, e *env) {
			if e.alice.fs.last() != "Plugins (0): " {
				t.Fatalf("msg %q", e.alice.fs.last())
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			sender := e.alice
			if tt.as != nil {
				sender = tt.as(e)
			}
			var err error
			for _, l := range tt.lines {
				if _, err = e.srv.cmds.Dispatch(sender, l); err != nil {
					break
				}
			}
			if tt.err != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, e)
			}
		})
	}
}

func TestBuiltinPermissions(t *testing.T) {
	e := newEnv()
	tests := map[string]api.PermissionDefault{
		"minecraft.command.help":     api.DefaultTrue,
		"minecraft.command.msg":      api.DefaultTrue,
		"minecraft.command.teleport": api.DefaultOp,
		"minecraft.command.stop":     api.DefaultOp,
		"kremenmc.command.version":   api.DefaultTrue,
		"kremenmc.command.plugins":   api.DefaultOp,
		SelectorPermission:           api.DefaultOp,
	}
	for name, def := range tests {
		p, ok := e.srv.perms.Get(name)
		if !ok || p.Default != def {
			t.Errorf("%s: %+v %v", name, p, ok)
		}
	}
	lvl4, _ := e.srv.perms.Get("minecraft.oplevel.4")
	if !lvl4.Children["minecraft.command.stop"] || !lvl4.Children["minecraft.oplevel.3"] {
		t.Errorf("oplevel.4 children: %v", lvl4.Children)
	}
	info, ok := e.srv.cmds.Lookup("tp")
	if !ok || info.Name != "teleport" || info.Permission != "minecraft.command.teleport" || info.Owner != "" {
		t.Errorf("tp info %+v", info)
	}
}

func TestFeedbackEcho(t *testing.T) {
	e := newEnv()
	e.srv.cmds.Dispatch(e.alice, "weather clear")
	if got := e.bob.fs.last(); got != "[Alice: Set the weather to clear]" {
		t.Fatalf("bob got %q", got)
	}
	if got := e.srv.console.last(); got != "[Alice: Set the weather to clear]" {
		t.Fatalf("console got %q", got)
	}
	if got := e.guest.fs.last(); got != "" {
		t.Fatalf("guest got %q", got)
	}
}
