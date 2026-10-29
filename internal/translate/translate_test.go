package translate

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
	"github.com/levitskyyy/KremenMC/internal/translate/v766"
	"github.com/levitskyyy/KremenMC/internal/translate/v767"
	"github.com/levitskyyy/KremenMC/internal/translate/v768"
	"github.com/levitskyyy/KremenMC/internal/translate/v769"
	"github.com/levitskyyy/KremenMC/internal/translate/v770"
	"github.com/levitskyyy/KremenMC/internal/translate/v771"
	"github.com/levitskyyy/KremenMC/internal/translate/v772"
	"github.com/levitskyyy/KremenMC/internal/translate/v773"
	"github.com/levitskyyy/KremenMC/internal/translate/v774"
	"github.com/levitskyyy/KremenMC/internal/translate/v775"
	"github.com/levitskyyy/KremenMC/internal/translate/v776"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// pkt is satisfied by the packets of every generated package.
type pkt interface {
	ID() int32
	Encode(w *proto.Writer)
	Decode(r *proto.Reader) error
}

type vpkg struct {
	unmarshal func(s packets.State, d packets.Direction, b []byte) (pkt, error)
	name      func(s packets.State, d packets.Direction, id int32) string
}

func mk[S, D ~uint8, P pkt](unm func(S, D, []byte) (P, error), name func(S, D, int32) string) vpkg {
	return vpkg{
		func(s packets.State, d packets.Direction, b []byte) (pkt, error) {
			p, err := unm(S(s), D(d), b)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
		func(s packets.State, d packets.Direction, id int32) string { return name(S(s), D(d), id) },
	}
}

var vpkgs = map[int32]vpkg{
	776: mk(v776.Unmarshal, v776.Name),
	775: mk(v775.Unmarshal, v775.Name),
	774: mk(v774.Unmarshal, v774.Name),
	773: mk(v773.Unmarshal, v773.Name),
	772: mk(v772.Unmarshal, v772.Name),
	771: mk(v771.Unmarshal, v771.Name),
	770: mk(v770.Unmarshal, v770.Name),
	769: mk(v769.Unmarshal, v769.Name),
	768: mk(v768.Unmarshal, v768.Name),
	767: mk(v767.Unmarshal, v767.Name),
	766: mk(v766.Unmarshal, v766.Name),
}

func lookup(t testing.TB, protocol int32) *conn {
	t.Helper()
	c, ok := Default().Lookup(protocol)
	if !ok {
		t.Fatalf("protocol %d not supported", protocol)
	}
	return c.(*conn)
}

// encode returns the frame bodies emitted for p.
func encode(t testing.TB, c Codec, s packets.State, p packets.Packet) [][]byte {
	t.Helper()
	var out [][]byte
	err := c.Encode(s, p, func(b []byte) error {
		out = append(out, append([]byte(nil), b...))
		return nil
	})
	if err != nil {
		t.Fatalf("encode %T: %v", p, err)
	}
	return out
}

// encodeOne encodes p, expects one frame named name and decodes it with the
// target's generated package.
func encodeOne(t *testing.T, c *conn, s packets.State, p packets.Packet, name string) pkt {
	t.Helper()
	bodies := encode(t, c, s, p)
	if len(bodies) != 1 {
		t.Fatalf("%d: %T produced %d frames", c.Protocol(), p, len(bodies))
	}
	return checkFrame(t, c.Protocol(), s, packets.Clientbound, bodies[0], name)
}

func checkFrame(t *testing.T, protocol int32, s packets.State, d packets.Direction, body []byte, name string) pkt {
	t.Helper()
	vp := vpkgs[protocol]
	r := proto.Reader{B: body}
	id, err := r.VarInt()
	if err != nil {
		t.Fatal(err)
	}
	if got := vp.name(s, d, id); got != name {
		t.Fatalf("%d: id 0x%02X is %q, want %q", protocol, id, got, name)
	}
	q, err := vp.unmarshal(s, d, body)
	if err != nil {
		t.Fatalf("%d: %s does not decode: %v", protocol, name, err)
	}
	var w proto.Writer
	w.VarInt(q.ID())
	q.Encode(&w)
	if !bytes.Equal(w.B, body) {
		t.Fatalf("%d: %s re-encodes differently", protocol, name)
	}
	return q
}

func TestCanonicalMetaOrder(t *testing.T) {
	m, err := meta.Load(packets.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	for s := range packets.State(5) {
		for d := range packets.Direction(2) {
			order := m.Order[sdKeys[s][d]]
			if len(order) != packets.Count(s, d) {
				t.Fatalf("%s/%d: %d names, %d packets", stateNames[s], d, len(order), packets.Count(s, d))
			}
			for id, n := range order {
				if packets.Name(s, d, int32(id)) != n {
					t.Errorf("%s/%d 0x%02X: meta %s, packets %s", stateNames[s], d, id, n, packets.Name(s, d, int32(id)))
				}
			}
		}
	}
}

func TestRegistry(t *testing.T) {
	r := Default()
	if len(r.Supported()) < 2 {
		t.Fatal(r.Supported())
	}
	for _, p := range r.Supported() {
		c, ok := r.Lookup(p)
		if !ok || c.Protocol() != p || c.Name() == "" {
			t.Fatalf("lookup %d", p)
		}
		if _, ok := vpkgs[p]; !ok {
			t.Errorf("no test package for %d", p)
		}
		if names, ok := ParserNames(p); !ok || len(names) < 40 {
			t.Errorf("%d parser names: %d", p, len(names))
		}
	}
	if _, ok := r.Lookup(packets.Protocol); ok {
		t.Error("canonical protocol must not be translated")
	}
	if _, ok := r.Lookup(5); ok {
		t.Error("unknown protocol accepted")
	}
	a, _ := r.Lookup(776)
	b, _ := r.Lookup(776)
	if a == b {
		t.Error("Lookup must return a codec per connection")
	}
}

// clientbound samples whose payload passes every step unchanged.
func passthroughCB() []struct {
	s packets.State
	p packets.Packet
} {
	return []struct {
		s packets.State
		p packets.Packet
	}{
		{packets.Status, &packets.ClientboundStatusResponse{Response: `{"version":{}}`}},
		{packets.Status, &packets.ClientboundStatusPongResponse{Time: 42}},
		{packets.Login, &packets.ClientboundLoginCompression{Threshold: 256}},
		{packets.Login, &packets.ClientboundLoginHello{ServerID: "", PublicKey: []byte{1, 2}, VerifyToken: []byte{3}, ShouldAuthenticate: true}},
		{packets.Configuration, &packets.ClientboundConfigFinishConfiguration{}},
		{packets.Configuration, &packets.ClientboundConfigKeepAlive{KeepAliveID: 7}},
		{packets.Configuration, &packets.ClientboundConfigCustomPayload{Channel: "minecraft:brand", Data: []byte{4, 'g', 'o', 'm', 'c'}}},
		{packets.Play, &packets.ClientboundKeepAlive{KeepAliveID: 9}},
		{packets.Play, &packets.ClientboundSetChunkCacheCenter{ChunkX: 3, ChunkZ: -4}},
		{packets.Play, &packets.ClientboundGameEvent{Reason: 13, GameMode: 1}},
		{packets.Play, &packets.ClientboundSystemChat{Content: proto.Plain("hi")}},
		{packets.Play, &packets.ClientboundForgetLevelChunk{}},
		{packets.Play, &packets.ClientboundChunkBatchFinished{BatchSize: 4}},
		{packets.Play, &packets.ClientboundRemoveEntities{EntityIDs: []int32{1, 2}}},
		{packets.Play, &packets.ClientboundStartConfiguration{}},
	}
}

func TestPassthroughClientbound(t *testing.T) {
	for _, proto_ := range Default().Supported() {
		c := lookup(t, proto_)
		for _, tc := range passthroughCB() {
			name := packets.Name(tc.s, packets.Clientbound, tc.p.ID())
			q := encodeOne(t, c, tc.s, tc.p, name)
			var a, b proto.Writer
			tc.p.Encode(&a)
			q.Encode(&b)
			if !bytes.Equal(a.B, b.B) {
				t.Errorf("%d: %s payload changed", proto_, name)
			}
		}
	}
}

func TestPassthroughServerbound(t *testing.T) {
	samples := []struct {
		s packets.State
		p packets.Packet
	}{
		{packets.Handshake, &packets.ServerboundHandshakeIntention{ProtocolVersion: 776, ServerHost: "localhost", ServerPort: 25565, NextState: 2}},
		{packets.Status, &packets.ServerboundStatusRequest{}},
		{packets.Status, &packets.ServerboundStatusPingRequest{Time: 5}},
		{packets.Login, &packets.ServerboundLoginHello{Username: "alex", PlayerUUID: [16]byte{1}}},
		{packets.Login, &packets.ServerboundLoginAcknowledged{}},
		{packets.Configuration, &packets.ServerboundConfigFinishConfiguration{}},
		{packets.Configuration, &packets.ServerboundConfigKeepAlive{KeepAliveID: 3}},
		{packets.Play, &packets.ServerboundKeepAlive{KeepAliveID: 3}},
		{packets.Play, &packets.ServerboundChatCommand{Command: "help"}},
		{packets.Play, &packets.ServerboundMovePlayerPos{X: 1, Y: 64, Z: -3, Flags: 1}},
		{packets.Play, &packets.ServerboundSetCarriedItem{SlotID: 4}},
		{packets.Play, &packets.ServerboundClientTickEnd{}},
		{packets.Play, &packets.ServerboundConfigurationAcknowledged{}},
		{packets.Play, &packets.ServerboundUseItemOn{Hand: 1, Location: proto.BlockPos{X: 1, Y: 2, Z: 3}, Direction: 1, Sequence: 8}},
	}
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		tv := c.t.last()
		for _, tc := range samples {
			name := packets.Name(tc.s, packets.Serverbound, tc.p.ID())
			id, ok := tv.id(tc.s, packets.Serverbound, name)
			if !ok {
				continue // newer than the client
			}
			if c.t.sb[len(c.t.vers)-1][tc.s][id].fn != nil {
				continue // layout differs; covered by step tests
			}
			var w proto.Writer
			w.VarInt(id)
			tc.p.Encode(&w)
			checkFrame(t, p, tc.s, packets.Serverbound, w.B, name)
			out, err := c.Decode(tc.s, w.B)
			if err != nil {
				t.Fatalf("%d %s: %v", p, name, err)
			}
			if len(out) != 1 || !reflect.DeepEqual(out[0], tc.p) {
				t.Errorf("%d %s: got %#v", p, name, out)
			}
			if got := c.NextState(tc.s, out[0], true); got != NextState(tc.s, tc.p, true) {
				t.Errorf("%d %s: next state %d", p, name, got)
			}
		}
	}
}

func TestNextState(t *testing.T) {
	c := lookup(t, 776)
	tests := []struct {
		s    packets.State
		p    packets.Packet
		want packets.State
	}{
		{packets.Handshake, &packets.ServerboundHandshakeIntention{NextState: 1}, packets.Status},
		{packets.Handshake, &packets.ServerboundHandshakeIntention{NextState: 2}, packets.Login},
		{packets.Handshake, &packets.ServerboundHandshakeIntention{NextState: 3}, packets.Login},
		{packets.Login, &packets.ServerboundLoginAcknowledged{}, packets.Configuration},
		{packets.Configuration, &packets.ServerboundConfigFinishConfiguration{}, packets.Play},
		{packets.Play, &packets.ServerboundConfigurationAcknowledged{}, packets.Configuration},
		{packets.Play, &packets.ServerboundKeepAlive{}, packets.Play},
	}
	for _, tc := range tests {
		if got := c.NextState(tc.s, tc.p, true); got != tc.want {
			t.Errorf("%T: %d, want %d", tc.p, got, tc.want)
		}
	}
	if got := c.NextState(packets.Login, &packets.ClientboundLoginFinished{}, false); got != packets.Login {
		t.Errorf("clientbound login finished moved to %d", got)
	}
}

func TestLoginFinished(t *testing.T) {
	p := &packets.ClientboundLoginFinished{UUID: [16]byte{1, 2}, Username: "steve", SessionID: [16]byte{9}}
	q := encodeOne(t, lookup(t, 776), packets.Login, p, "login_finished").(*v776.ClientboundLoginFinished)
	if q.Username != "steve" || q.SessionID != p.SessionID {
		t.Errorf("776: %+v", q)
	}
	r := encodeOne(t, lookup(t, 775), packets.Login, p, "login_finished").(*v775.ClientboundLoginFinished)
	if r.Username != "steve" || r.UUID != p.UUID {
		t.Errorf("775: %+v", r)
	}
}

func samplePlayLogin() *packets.ClientboundLogin {
	return &packets.ClientboundLogin{
		EntityID: 5, WorldNames: []string{"minecraft:overworld"}, MaxPlayers: 20, ViewDistance: 10,
		SimulationDistance: 8, EnableRespawnScreen: true,
		WorldState: packets.SpawnInfo{
			Dimension: 0, Name: "minecraft:overworld", HashedSeed: 77,
			Gamemode: 1, PreviousGamemode: 0, SeaLevel: 63,
		},
		OnlineMode: true, EnforcesSecureChat: true,
	}
}

func TestPlayLogin(t *testing.T) {
	p := samplePlayLogin()
	q := encodeOne(t, lookup(t, 776), packets.Play, p, "login").(*v776.ClientboundLogin)
	if q.WorldState.Gamemode != 1 || q.WorldState.PreviousGamemode != 255 || !q.OnlineMode || !q.EnforcesSecureChat || q.WorldState.SeaLevel != 63 {
		t.Errorf("776: %+v", q)
	}
	p.WorldState.PreviousGamemode = 3 // adventure + 1
	r := encodeOne(t, lookup(t, 775), packets.Play, p, "login").(*v775.ClientboundLogin)
	if r.WorldState.PreviousGamemode != 2 || !r.EnforcesSecureChat || r.EntityID != 5 {
		t.Errorf("775: %+v", r)
	}
	rs := &packets.ClientboundRespawn{WorldState: p.WorldState, CopyMetadata: 1}
	q2 := encodeOne(t, lookup(t, 775), packets.Play, rs, "respawn").(*v775.ClientboundRespawn)
	if q2.WorldState.Gamemode != 1 || q2.CopyMetadata != 1 {
		t.Errorf("775 respawn: %+v", q2)
	}
}

func TestEntityMovement(t *testing.T) {
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		stepped := &packets.ClientboundMoveEntityPos{EntityID: 3, Delta: proto.MoveDelta{OnGround: true, Steps: []proto.MoveStep{{DX: 100}, {DX: 50, DY: -20}}}}
		q := encodeOne(t, c, packets.Play, stepped, "move_entity_pos")
		v := reflect.ValueOf(q).Elem()
		if v.FieldByName("DX").Int() != 150 || v.FieldByName("DY").Int() != -20 || !v.FieldByName("OnGround").Bool() {
			t.Errorf("%d move_entity_pos: %+v", p, q)
		}
		rot := &packets.ClientboundMoveEntityRot{EntityID: 3, OnGround: true, Yaw: 10, Pitch: -5}
		q = encodeOne(t, c, packets.Play, rot, "move_entity_rot")
		v = reflect.ValueOf(q).Elem()
		if v.FieldByName("Yaw").Int() != 10 || v.FieldByName("Pitch").Int() != -5 || !v.FieldByName("OnGround").Bool() {
			t.Errorf("%d move_entity_rot: %+v", p, q)
		}
		sync := &packets.ClientboundEntityPositionSync{EntityID: 3, PathType: 1, Steps: []packets.PositionStep{{X: 1}, {X: 2, Y: 70, Z: 3}}, Yaw: 90}
		syncName := "entity_position_sync"
		if _, ok := c.t.last().id(packets.Play, packets.Clientbound, syncName); !ok {
			syncName = "teleport_entity"
		}
		q = encodeOne(t, c, packets.Play, sync, syncName)
		v = reflect.ValueOf(q).Elem()
		yaw := v.FieldByName("Yaw")
		if v.FieldByName("X").Float() != 2 || v.FieldByName("Y").Float() != 70 || (yaw.CanFloat() && yaw.Float() != 90) || (yaw.CanInt() && yaw.Int() != 64) {
			t.Errorf("%d entity_position_sync: %+v", p, q)
		}
		sw := &packets.ClientboundSwingAnimation{EntityID: 3, Hand: 1}
		q = encodeOne(t, c, packets.Play, sw, "animate")
		if reflect.ValueOf(q).Elem().FieldByName("Animation").Uint() != 3 {
			t.Errorf("%d swing: %+v", p, q)
		}
	}
}

func TestDroppedPackets(t *testing.T) {
	c := lookup(t, 776)
	for _, tc := range []struct {
		s packets.State
		p packets.Packet
	}{
		{packets.Configuration, &packets.ClientboundConfigPostEffects{}},
		{packets.Play, &packets.ClientboundPostEffects{}},
		{packets.Play, &packets.ClientboundAddTransientBlock{}},
		{packets.Play, &packets.ClientboundAddEntity{Type: 1 << 20}},
	} {
		if n := len(encode(t, c, tc.s, tc.p)); n != 0 {
			t.Errorf("%T: %d frames", tc.p, n)
		}
	}
}

func TestServerboundTransforms(t *testing.T) {
	c := lookup(t, 776)
	encode(t, c, packets.Play, &packets.ClientboundPlayerPosition{TeleportID: 12, X: 1.5, Y: 70, Z: -8, Yaw: 45})
	var w proto.Writer
	w.VarInt(0x00)
	(&v776.ServerboundAcceptTeleportation{TeleportID: 12}).Encode(&w)
	out, err := c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	want := &packets.ServerboundAcceptTeleportation{TeleportID: 12, X: 1.5, Y: 70, Z: -8, Yaw: 45}
	if !reflect.DeepEqual(out[0], want) {
		t.Errorf("accept teleportation: %+v", out[0])
	}

	sign := &v776.ServerboundSignUpdate{Location: proto.BlockPos{X: 1}, IsFrontText: true, Text1: "a", Text4: "d"}
	w.B = w.B[:0]
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "sign_update")
	w.VarInt(id)
	sign.Encode(&w)
	out, err = c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	if s := out[0].(*packets.ServerboundSignUpdate); s.TextSlot != 1 || s.Text1 != "a" || s.Text4 != "d" {
		t.Errorf("sign: %+v", s)
	}

	for _, p := range Default().Supported() {
		c := lookup(t, p)
		id, _ := c.t.last().id(packets.Play, packets.Serverbound, "swing")
		out, err := c.Decode(packets.Play, []byte{byte(id), 0})
		if err != nil || len(out) != 1 {
			t.Fatalf("%d swing: %v %v", p, out, err)
		}
		if _, ok := out[0].(*packets.ServerboundPunch); !ok {
			t.Errorf("%d swing -> %T", p, out[0])
		}
		if out, _ := c.Decode(packets.Play, []byte{byte(id), 1}); len(out) != 0 {
			t.Errorf("%d off-hand swing kept", p)
		}
	}
}

func TestTeams(t *testing.T) {
	p := &packets.ClientboundSetPlayerTeam{
		Team: "red", Mode: 0, Players: []string{"a"},
		Parameters: packets.TeamParameters{
			Name: proto.Plain("Red"), Prefix: proto.Plain("["), NameTagVisibility: 2, CollisionRule: 1,
			Color: proto.Some[int32](12), Flags: packets.TeamParametersFlagsFriendlyFire,
		},
	}
	q := encodeOne(t, lookup(t, 775), packets.Play, p, "set_player_team").(*v775.ClientboundSetPlayerTeam)
	if q.Formatting != 12 || q.NameTagVisibility != 2 || q.CollisionRule != 1 || q.Flags != 1 ||
		q.Prefix.Text != "[" || len(q.Players) != 1 {
		t.Errorf("%+v", q)
	}
	encodeOne(t, lookup(t, 776), packets.Play, p, "set_player_team")
}

func TestBlockUpdateRemap(t *testing.T) {
	canon := data.Canonical()
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		tr := c.t.last().reg
		for _, tc := range []struct{ in, want string }{
			{"oak_stairs[facing=east,half=top,shape=straight,waterlogged=true]", "oak_stairs[facing=east,half=top,shape=straight,waterlogged=true]"},
			{"redstone_wire[east=side,north=none,power=7,south=up,west=none]", "redstone_wire[east=side,north=none,power=7,south=up,west=none]"},
			{"poplar_planks", "stone"},
			{"air", "air"},
		} {
			s, ok := canon.ParseState(tc.in)
			if !ok {
				t.Fatalf("parse %s", tc.in)
			}
			bu := &packets.ClientboundBlockUpdate{Location: proto.BlockPos{X: 1, Y: 2, Z: 3}, Type: int32(s)}
			q := encodeOne(t, c, packets.Play, bu, "block_update")
			got := sname(tr, data.StateID(reflect.ValueOf(q).Elem().FieldByName("Type").Int()))
			if got != tc.want {
				t.Errorf("%d: %s -> %s, want %s", p, tc.in, got, tc.want)
			}
			if bu.Type != int32(s) {
				t.Fatal("canonical packet mutated")
			}
		}
		// out-of-range IDs fall back to air instead of failing
		q := encodeOne(t, c, packets.Play, &packets.ClientboundBlockUpdate{Type: 1 << 30}, "block_update")
		if reflect.ValueOf(q).Elem().FieldByName("Type").Int() != 0 {
			t.Errorf("%d: unknown state not air", p)
		}

		lamp, _ := canon.ParseState("redstone_lamp[lit=true]")
		poplar, _ := canon.DefaultState("poplar_planks")
		su := &packets.ClientboundSectionBlocksUpdate{Records: []int64{int64(lamp)<<12 | 0x123, int64(poplar)<<12 | 0x7}}
		q = encodeOne(t, c, packets.Play, su, "section_blocks_update")
		recs := reflect.ValueOf(q).Elem().FieldByName("Records").Interface().([]int64)
		if sname(tr, data.StateID(recs[0]>>12)) != "redstone_lamp[lit=true]" || recs[0]&0xfff != 0x123 ||
			sname(tr, data.StateID(recs[1]>>12)) != "stone" {
			t.Errorf("%d: section update %v", p, recs)
		}
	}
}

func TestUnknownFallbacks(t *testing.T) {
	c := lookup(t, 775)
	m := c.t.maps
	poplar, _ := data.Canonical().ItemByName("poplar_planks")
	stone, _ := c.t.last().reg.ItemByName("stone")
	if m.item(int32(poplar)) != int32(stone) || m.item(-1) != int32(stone) || m.item(1<<20) != int32(stone) {
		t.Error("unknown items must become stone")
	}
	if m.entity(1<<20) != -1 || m.block(-5) != -1 {
		t.Error("unknown entity/block must be -1")
	}
	// a truncated payload is a per-connection error, not a panic
	if err := c.Encode(packets.Play, &packets.ClientboundLevelChunkWithLight{ChunkData: []byte{0, 1, 0, 0, 9}}, func([]byte) error { return nil }); err == nil {
		t.Error("corrupt chunk accepted")
	}
	if _, err := c.Decode(packets.Play, []byte{0x7f, 1, 2}); err != nil {
		t.Logf("unknown serverbound id: %v", err)
	}
	if _, err := c.Decode(packets.Play, nil); err == nil {
		t.Error("empty frame accepted")
	}
}

func TestAddEntity(t *testing.T) {
	canon := data.Canonical()
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		for _, name := range []string{"zombie", "player", "item", "armor_stand"} {
			e, _ := canon.EntityByName(name)
			q := encodeOne(t, c, packets.Play, &packets.ClientboundAddEntity{EntityID: 9, Type: int32(e.ID), Y: 64}, "add_entity")
			te := c.t.last().reg.Entity(data.EntityID(reflect.ValueOf(q).Elem().FieldByName("Type").Int()))
			if te == nil || te.Name != name {
				t.Errorf("%d: %s -> %v", p, name, te)
			}
		}
	}
}

func TestConcurrentEncodeDecode(t *testing.T) {
	c := lookup(t, 775)
	keep := &packets.ClientboundKeepAlive{KeepAliveID: 1}
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "keep_alive")
	var w proto.Writer
	w.VarInt(id)
	w.I64(5)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			_ = c.Encode(packets.Play, keep, func([]byte) error { return nil })
			_ = c.Encode(packets.Play, &packets.ClientboundPlayerPosition{TeleportID: 1}, func([]byte) error { return nil })
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			if _, err := c.Decode(packets.Play, w.B); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}

func TestChunkSectionFormats(t *testing.T) {
	sizes := func(r *data.Registry) (blocks, biomes *world.Strategy) {
		return world.NewStrategy(4096, 4, 8, r.NumStates()), world.NewStrategy(64, 1, 3, len(r.Biomes))
	}
	canon := data.Canonical()
	cb, _ := sizes(canon)
	_ = cb
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		tr := c.t.last().reg
		bs, ms := sizes(tr)
		// the client numbers biomes in the order of the (filtered) registry data
		clientBiomes := map[uint32]string{}
		for i, v := range c.biomes.table {
			if v >= 0 {
				clientBiomes[uint32(v)] = canon.Biomes[i].Name
			}
		}
		ms = world.NewStrategy(64, 1, 3, len(clientBiomes))
		ch, states := testChunk(t)
		raw := chunkDataOf(t, c, ch)
		got := decodeSections(t, raw, c.t.last().chunk, bs, ms)
		if len(got) != len(states) {
			t.Fatalf("%d: %d sections, want %d", p, len(got), len(states))
		}
		for si := range states {
			for i, s := range states[si] {
				want := sname(canon, data.StateID(s))
				if ts, ok := tr.ParseState(want); ok && sname(tr, ts) != want {
					continue // a property value the target lacks; defaults apply
				} else if !ok {
					if _, exists := tr.BlockByName(canon.BlockOf(data.StateID(s)).Name); exists {
						continue // property values changed; defaults apply
					}
					want = "air"
					if canon.IsSolid(data.StateID(s)) {
						want = "stone"
					}
				}
				if g := sname(tr, data.StateID(got[si].blocks[i])); g != want {
					t.Fatalf("%d: section %d index %d: %s, want %s", p, si, i, g, want)
				}
			}
			for _, b := range got[si].biomes {
				if n := clientBiomes[b]; n != "plains" && n != "desert" {
					t.Fatalf("%d: section %d biome %q", p, si, n)
				}
			}
		}
	}
}

type section struct{ blocks, biomes []uint32 }

func testChunk(t testing.TB) (*packets.ClientboundLevelChunkWithLight, [][]uint32) {
	canon := data.Canonical()
	plains, _ := canon.BiomeByName("plains")
	desert, _ := canon.BiomeByName("desert")
	ch := world.NewChunk(0, 0, world.Overworld, plains)
	pick := []string{"stone", "dirt", "grass_block[snowy=false]", "oak_log[axis=x]", "poplar_planks",
		"water[level=0]", "redstone_wire[east=side,north=none,power=3,south=up,west=none]", "oak_slab[type=top,waterlogged=true]"}
	ids := make([]data.StateID, len(pick))
	for i, s := range pick {
		var ok bool
		if ids[i], ok = canon.ParseState(s); !ok {
			t.Fatalf("parse %s", s)
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := -64; y < 40; y++ {
				ch.SetBlock(x, y, z, ids[(x*7+(y+64)*3+z)%len(ids)])
			}
		}
	}
	// a direct-palette section: more distinct states than an indirect palette holds
	for i := range 4096 {
		ch.Sections[10].Blocks.Set(i, uint32(1+i*7%20000))
	}
	ch.SetBiome(0, 0, 0, desert)
	ch.Finish()
	var w proto.Writer
	w.I32(0)
	w.I32(0)
	world.EncodeChunkData(&w, ch)
	var p packets.ClientboundLevelChunkWithLight
	if err := p.Decode(&proto.Reader{B: w.B}); err != nil {
		t.Fatal(err)
	}
	states := make([][]uint32, len(ch.Sections))
	for i := range ch.Sections {
		states[i] = ch.Sections[i].Blocks.Values(make([]uint32, 0, 4096))
	}
	return &p, states
}

// decodeSections parses chunk section data in the target's format with the
// world package's container decoder (after stripping length prefixes).
func decodeSections(t *testing.T, b []byte, f chunkFormat, blocks, biomes *world.Strategy) []section {
	t.Helper()
	r := proto.Reader{B: b}
	var out []section
	for r.Remaining() > 0 {
		if _, err := r.I16(); err != nil {
			t.Fatal(err)
		}
		if f&fmtFluidCount != 0 {
			if _, err := r.I16(); err != nil {
				t.Fatal(err)
			}
		}
		var s section
		for k, st := range []*world.Strategy{blocks, biomes} {
			c := world.NewContainer(st, 0)
			cr := &r
			if f&fmtLongArrayLength != 0 {
				cr = &proto.Reader{B: stripLength(t, &r, st)}
			}
			if err := c.Decode(cr); err != nil {
				t.Fatalf("section %d container %d: %v", len(out), k, err)
			}
			v := c.Values(make([]uint32, 0, 4096))
			if k == 0 {
				s.blocks = v
			} else {
				s.biomes = v
			}
		}
		out = append(out, s)
	}
	return out
}

func BenchmarkChunkTranslate(b *testing.B) {
	for _, p := range []int32{776, 774, 766} {
		b.Run(fmt.Sprint(p), func(b *testing.B) {
			c := lookup(b, p)
			ch, _ := testChunk(b)
			emit := func([]byte) error { return nil }
			b.ReportAllocs()
			for b.Loop() {
				if err := c.Encode(packets.Play, ch, emit); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMoveEntity(b *testing.B) {
	c := lookup(b, 775)
	p := &packets.ClientboundMoveEntityPosRot{EntityID: 3, Delta: proto.MoveDelta{DX: 5, OnGround: true}, Yaw: 3}
	emit := func([]byte) error { return nil }
	b.ReportAllocs()
	for b.Loop() {
		if err := c.Encode(packets.Play, p, emit); err != nil {
			b.Fatal(err)
		}
	}
}

func sname(r *data.Registry, s data.StateID) string { return trimNS(r.StateString(s)) }

// chunkDataOf encodes a chunk packet and returns its section data, checking
// the heightmap encoding of the target.
func chunkDataOf(t *testing.T, c *conn, ch *packets.ClientboundLevelChunkWithLight) []byte {
	t.Helper()
	bodies := encode(t, c, packets.Play, ch)
	if len(bodies) != 1 {
		t.Fatalf("%d frames", len(bodies))
	}
	r := proto.Reader{B: bodies[0]}
	id, _ := r.VarInt()
	if n := vpkgs[c.Protocol()].name(packets.Play, packets.Clientbound, id); n != "level_chunk_with_light" {
		t.Fatalf("%d: packet %s", c.Protocol(), n)
	}
	r.I32()
	r.I32()
	if c.Protocol() <= 769 {
		l, err := proto.NBTLen(r.B[r.Off:])
		if err != nil {
			t.Fatal(err)
		}
		hm, err := proto.RawNBT(r.B[r.Off : r.Off+l]).Compound()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := hm["MOTION_BLOCKING"]; !ok || len(hm) != len(ch.Heightmaps) {
			t.Fatalf("heightmaps %v", hm)
		}
		r.Off += l
	} else {
		n, _ := r.VarInt()
		for range n {
			r.VarInt()
			k, _ := r.VarInt()
			r.Raw(int(k) * 8)
		}
	}
	data, err := r.ByteArray(1 << 21)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// stripLength rewrites one length-prefixed container into the 1.21.5+ form.
func stripLength(t *testing.T, r *proto.Reader, st *world.Strategy) []byte {
	t.Helper()
	var w proto.Writer
	bits, _ := r.U8()
	w.U8(bits)
	switch {
	case bits == 0:
		v, _ := r.VarInt()
		w.VarInt(v)
	case bits <= st.MaxBits:
		n, _ := r.VarInt()
		w.VarInt(n)
		for range n {
			v, _ := r.VarInt()
			w.VarInt(v)
		}
	}
	l, err := r.VarInt()
	if err != nil {
		t.Fatal(err)
	}
	b := max(bits, st.MinBits)
	if bits > st.MaxBits {
		b = st.GlobalBits
	}
	want := 0
	if bits != 0 {
		want = (st.Size + 64/int(b) - 1) / (64 / int(b))
	}
	if int(l) != want {
		t.Fatalf("long array length %d, want %d (bits %d)", l, want, bits)
	}
	raw, err := r.Raw(int(l) * 8)
	if err != nil {
		t.Fatal(err)
	}
	w.Raw(raw)
	return w.B
}
