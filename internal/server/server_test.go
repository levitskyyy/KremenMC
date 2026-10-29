package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

func TestJoinMoveChat(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Alice")
	c.join(s.Addr())
	if c.registry != len(regsync.Registries()) || !c.tags {
		t.Fatalf("got %d registries (want %d), tags=%v", c.registry, len(regsync.Registries()), c.tags)
	}
	login := expect[*packets.ClientboundLogin](c, nil)
	ow, _ := regsync.Index("minecraft:dimension_type", "minecraft:overworld")
	if login.WorldState.Dimension != ow || login.WorldState.Name != "minecraft:overworld" || len(login.WorldNames) != 1 {
		t.Fatalf("bad login: %+v", login)
	}
	if login.WorldState.Gamemode != int32(api.Creative) || login.ViewDistance != 4 {
		t.Fatalf("bad login gamemode/view: %+v", login)
	}
	expect[*packets.ClientboundCommands](c, nil)
	pos := expect[*packets.ClientboundPlayerPosition](c, nil)
	expect[*packets.ClientboundPlayerInfoUpdate](c, func(p *packets.ClientboundPlayerInfoUpdate) bool {
		return len(p.Data) == 1 && p.Data[0].Player.Name == "Alice"
	})
	expect[*packets.ClientboundGameEvent](c, func(p *packets.ClientboundGameEvent) bool { return p.Reason == 13 })
	expect[*packets.ClientboundSetChunkCacheCenter](c, nil)
	expect[*packets.ClientboundChunkBatchFinished](c, nil)
	c.send(&packets.ServerboundPlayerLoaded{})

	deadline := time.Now().Add(10 * time.Second)
	for c.chunks.Load() < 49 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := c.chunks.Load(); n < 49 {
		t.Fatalf("received %d chunks", n)
	}

	for i := 1; i <= 10; i++ {
		c.send(&packets.ServerboundMovePlayerPosRot{X: pos.X + float64(i)*0.2, Y: pos.Y, Z: pos.Z, Yaw: 90, Flags: packets.MovementFlagsOnGround})
	}
	waitFor(t, func() bool {
		p, _ := s.PlayerByName("alice")
		return p != nil && p.Location().Pos.X > pos.X+1.9
	})

	c.send(&packets.ServerboundMovePlayerPos{X: pos.X + 500, Y: pos.Y, Z: pos.Z})
	back := expect[*packets.ClientboundPlayerPosition](c, nil)
	if back.X > pos.X+3 {
		t.Fatalf("too-fast move was accepted: %+v", back)
	}

	pl := mustPlayer(t, s, "Alice")
	sp := pl.Location()
	pl.Teleport(api.Location{Pos: api.Vec3{X: sp.Pos.X + 400, Y: sp.Pos.Y, Z: sp.Pos.Z}}, api.TeleportPlugin)
	far := expect[*packets.ClientboundPlayerPosition](c, nil)
	if far.X < sp.Pos.X+399 {
		t.Fatalf("teleport %+v", far)
	}
	expect[*packets.ClientboundSetChunkCacheCenter](c, func(p *packets.ClientboundSetChunkCacheCenter) bool { return p.ChunkX > 20 })
	expect[*packets.ClientboundForgetLevelChunk](c, nil)

	c.send(&packets.ServerboundChat{Message: "hello world"})
	msg := expect[*packets.ClientboundSystemChat](c, func(m *packets.ClientboundSystemChat) bool {
		return strings.Contains(fromText(m.Content).Plain(), "hello world")
	})
	if msg.Content.Translate != "chat.type.text" {
		t.Fatalf("chat format: %+v", msg.Content)
	}

	c.send(&packets.ServerboundChatCommand{Command: "nosuchcommand"})
	expect[*packets.ClientboundSystemChat](c, nil)

	c.nc.Close()
	waitFor(t, func() bool { return len(s.Players()) == 0 })
}

func TestTwoPlayersSeeEachOther(t *testing.T) {
	s := startServer(t, nil)
	a := dial(t, s.Addr(), "Alpha")
	a.joinPlay(s.Addr())
	b := dial(t, s.Addr(), "Bravo")
	posB := b.joinPlay(s.Addr())

	expect[*packets.ClientboundPlayerInfoUpdate](a, func(p *packets.ClientboundPlayerInfoUpdate) bool {
		return len(p.Data) == 1 && p.Data[0].Player.Name == "Bravo"
	})
	pt, _ := data.Canonical().EntityByName("player")
	spawnB := expect[*packets.ClientboundAddEntity](a, func(p *packets.ClientboundAddEntity) bool { return p.EntityID == b.eid })
	if spawnB.Type != int32(pt.ID) {
		t.Fatalf("spawned type %d, want player %d", spawnB.Type, pt.ID)
	}
	spawnA := expect[*packets.ClientboundAddEntity](b, func(p *packets.ClientboundAddEntity) bool { return p.EntityID == a.eid })
	if spawnA.ObjectUUID != [16]byte(mustPlayer(t, s, "Alpha").UUID()) {
		t.Fatal("wrong uuid for Alpha")
	}

	b.send(&packets.ServerboundMovePlayerPosRot{X: posB.X + 1, Y: posB.Y, Z: posB.Z, Yaw: 45, Flags: packets.MovementFlagsOnGround})
	mv := expect[*packets.ClientboundMoveEntityPosRot](a, func(p *packets.ClientboundMoveEntityPosRot) bool { return p.EntityID == b.eid })
	if mv.Delta.DX != 4096 {
		t.Fatalf("delta %+v", mv.Delta)
	}

	b.send(&packets.ServerboundPlayerInput{Inputs: packets.ServerboundPlayerInputInputsShift})
	expect[*packets.ClientboundSetEntityData](a, func(p *packets.ClientboundSetEntityData) bool { return p.EntityID == b.eid })

	b.nc.Close()
	expect[*packets.ClientboundPlayerInfoRemove](a, nil)
	expect[*packets.ClientboundRemoveEntities](a, func(p *packets.ClientboundRemoveEntities) bool {
		return len(p.EntityIDs) == 1 && p.EntityIDs[0] == b.eid
	})
}

func TestBuild(t *testing.T) {
	s := startServer(t, nil)
	a := dial(t, s.Addr(), "Builder")
	pos := a.joinPlay(s.Addr())
	b := dial(t, s.Addr(), "Watcher")
	b.joinPlay(s.Addr())

	stone, _ := data.Canonical().ItemByName("stone")
	slot := proto.AppendVarInt(nil, 1)
	slot = proto.AppendVarInt(slot, int32(stone))
	slot = append(slot, 0, 0)
	a.send(&packets.ServerboundSetCreativeModeSlot{Slot: 36, Raw: slot})
	a.send(&packets.ServerboundSetCarriedItem{SlotID: 0})

	floor := int32(pos.Y) - 1
	target := proto.BlockPos{X: int32(pos.X) + 2, Y: floor, Z: int32(pos.Z)}
	a.send(&packets.ServerboundUseItemOn{Location: target, Direction: 1, CursorX: 0.5, CursorY: 1, CursorZ: 0.5, Sequence: 7})
	want, _ := data.DefaultState("stone")
	placed := proto.BlockPos{X: target.X, Y: floor + 1, Z: target.Z}
	match := func(p *packets.ClientboundBlockUpdate) bool { return p.Location == placed && p.Type == int32(want) }
	expect(a, match)
	expect[*packets.ClientboundBlockChangedAck](a, func(p *packets.ClientboundBlockChangedAck) bool { return p.SequenceID == 7 })
	expect(b, match)

	a.send(&packets.ServerboundPlayerAction{Status: 0, Location: placed, Face: 1, Sequence: 8})
	expect[*packets.ClientboundBlockUpdate](b, func(p *packets.ClientboundBlockUpdate) bool { return p.Location == placed && p.Type == 0 })
	expect[*packets.ClientboundBlockChangedAck](a, func(p *packets.ClientboundBlockChangedAck) bool { return p.SequenceID == 8 })
	w := s.defaultWorld()
	if st := w.w.Block(int(placed.X), int(placed.Y), int(placed.Z)); st != data.Air {
		t.Fatalf("block not broken: %s", data.StateString(st))
	}
}

func TestStatusAndLegacyPing(t *testing.T) {
	s := startServerWith(t, func(c *Config) {
		c.Props.MOTD = "§aHello"
		c.Translations = translate.Default()
	})
	c := dial(t, s.Addr(), "")
	h, port := hostPort(s.Addr())
	c.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: 4, ServerHost: h, ServerPort: port, NextState: 1})
	c.state = packets.Status
	c.send(&packets.ServerboundStatusRequest{})
	resp := c.read().(*packets.ClientboundStatusResponse)
	var st struct {
		Version struct {
			Name     string
			Protocol int32
		}
		Players     struct{ Max, Online int }
		Description json.RawMessage
	}
	if err := json.Unmarshal([]byte(resp.Response), &st); err != nil {
		t.Fatal(err)
	}
	if st.Version.Protocol != packets.Protocol || st.Players.Max != 1000 || !strings.Contains(string(st.Description), "Hello") {
		t.Fatalf("status: %s", resp.Response)
	}
	c.send(&packets.ServerboundStatusPingRequest{Time: 42})
	if pong := c.read().(*packets.ClientboundStatusPongResponse); pong.Time != 42 {
		t.Fatalf("pong %d", pong.Time)
	}

	c2 := dial(t, s.Addr(), "")
	c2.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: 776, ServerHost: h, ServerPort: port, NextState: 1})
	c2.state = packets.Status
	c2.send(&packets.ServerboundStatusRequest{})
	if r := c2.read().(*packets.ClientboundStatusResponse).Response; !strings.Contains(r, `"protocol":776`) {
		t.Fatalf("translated status: %s", r)
	}

	lc := dial(t, s.Addr(), "")
	lc.nc.Write([]byte{0xFE, 0x01, 0xFA})
	lc.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	n, _ := lc.nc.Read(buf)
	if n < 3 || buf[0] != 0xFF {
		t.Fatalf("legacy ping reply %x", buf[:n])
	}
}

func TestOutdatedClient(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Old")
	h, port := hostPort(s.Addr())
	c.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: 47, ServerHost: h, ServerPort: port, NextState: 2})
	c.state = packets.Login
	c.send(&packets.ServerboundLoginHello{Username: "Old"})
	d, ok := c.read().(*packets.ClientboundLoginDisconnect)
	if !ok || !strings.Contains(d.Reason, "outdated_client") {
		t.Fatalf("got %+v", d)
	}
}

func TestWhitelistDenies(t *testing.T) {
	s := startServer(t, func(p *config.ServerProperties) { p.WhiteList = true })
	c := dial(t, s.Addr(), "Stranger")
	h, port := hostPort(s.Addr())
	c.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: packets.Protocol, ServerHost: h, ServerPort: port, NextState: 2})
	c.state = packets.Login
	c.send(&packets.ServerboundLoginHello{Username: "Stranger"})
	for {
		switch p := c.read().(type) {
		case *packets.ClientboundLoginCompression:
			c.fr.SetCompression(int(p.Threshold))
			c.fw.SetCompression(int(p.Threshold), 6)
		case *packets.ClientboundLoginDisconnect:
			if !strings.Contains(p.Reason, "not_whitelisted") {
				t.Fatalf("reason %s", p.Reason)
			}
			return
		default:
			t.Fatalf("unexpected %T", p)
		}
	}
}

func mustPlayer(t *testing.T, s *Server, name string) api.Player {
	t.Helper()
	p, ok := s.PlayerByName(name)
	if !ok {
		t.Fatalf("player %s not online", name)
	}
	return p
}

func waitFor(t testing.TB, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSpawnedEntityIsTrackedAndFalls(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Viewer")
	pos := c.joinPlay(s.Addr())
	w := s.defaultWorld()
	e, err := w.SpawnEntity("minecraft:pig", api.Location{Pos: api.Vec3{X: pos.X + 3, Y: pos.Y + 5, Z: pos.Z}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pig, _ := data.Canonical().EntityByName("pig")
	expect[*packets.ClientboundAddEntity](c, func(p *packets.ClientboundAddEntity) bool {
		return p.EntityID == e.EntityID() && p.Type == int32(pig.ID)
	})
	expect[*packets.ClientboundMoveEntityPos](c, func(p *packets.ClientboundMoveEntityPos) bool {
		return p.EntityID == e.EntityID() && p.Delta.DY < 0
	})
	waitFor(t, e.OnGround)
	if y := e.Location().Pos.Y; y != pos.Y {
		t.Fatalf("pig landed at y=%v, want %v", y, pos.Y)
	}
	e.Remove()
	expect[*packets.ClientboundRemoveEntities](c, func(p *packets.ClientboundRemoveEntities) bool {
		return len(p.EntityIDs) == 1 && p.EntityIDs[0] == e.EntityID()
	})
}

// TestTranslatedJoin drives a 26.2 (776) client through login and
// configuration with raw frames and checks it reaches play.
func TestTranslatedJoin(t *testing.T) {
	s := startServerWith(t, func(c *Config) { c.Translations = translate.Default() })
	c := dial(t, s.Addr(), "Old")
	h, port := hostPort(s.Addr())
	c.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: 776, ServerHost: h, ServerPort: port, NextState: 2})
	c.send(&packets.ServerboundLoginHello{Username: "Old", PlayerUUID: [16]byte{1}})
	raw := func() (int32, []byte) {
		c.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
		f, err := c.fr.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		r := proto.Reader{B: f}
		id, _ := r.VarInt()
		return id, r.Rest()
	}
	sendRaw := func(id int32, body ...byte) {
		w := proto.Writer{}
		w.VarInt(id)
		w.Raw(body)
		c.fw.WriteFrame(w.B)
		c.fw.Flush()
	}
	for {
		id, body := raw()
		if id == 0x03 {
			r := proto.Reader{B: body}
			th, _ := r.VarInt()
			c.fr.SetCompression(int(th))
			c.fw.SetCompression(int(th), 6)
			continue
		}
		if id != 0x02 {
			t.Fatalf("login packet 0x%02x", id)
		}
		break
	}
	sendRaw(0x03)
	registries := 0
	for {
		id, body := raw()
		switch id {
		case 0x0E:
			sendRaw(0x07, body...)
		case 0x07:
			registries++
		case 0x03:
			if registries == 0 {
				t.Fatal("no registry data before finish")
			}
			sendRaw(0x03)
			c.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
			if _, err := c.fr.ReadFrame(); err != nil {
				t.Fatalf("no play packet: %v", err)
			}
			waitFor(t, func() bool { _, ok := s.PlayerByName("Old"); return ok })
			return
		case 0x02:
			t.Fatal("disconnected in configuration")
		}
	}
}
