package translate

import (
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
	"github.com/levitskyyy/KremenMC/internal/translate/v772"
)

func TestAddEntityVelocity772(t *testing.T) {
	zombie, _ := data.Canonical().EntityByName("zombie")
	p := &packets.ClientboundAddEntity{EntityID: 1, Type: int32(zombie.ID), Velocity: proto.LpVec3{X: 0.5, Y: -10}}
	q := encodeOne(t, lookup(t, 772), packets.Play, p, "add_entity").(*v772.ClientboundAddEntity)
	if q.Velocity.X < 3990 || q.Velocity.X > 4010 || q.Velocity.Y != -31200 || q.Velocity.Z != 0 { // LpVec3 is quantized
		t.Errorf("%+v", q.Velocity)
	}
}

func TestPlayerCommand770(t *testing.T) {
	c := lookup(t, 770)
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "player_command")
	for action, want := range map[int32]int32{0: -1, 1: -1, 3: 1, 8: 6} {
		var w proto.Writer
		w.VarInt(id)
		(&v770.ServerboundPlayerCommand{EntityID: 2, ActionID: action}).Encode(&w)
		out, err := c.Decode(packets.Play, w.B)
		if err != nil {
			t.Fatal(err)
		}
		if want < 0 {
			if len(out) != 0 {
				t.Errorf("sneak action %d kept", action)
			}
			continue
		}
		if len(out) != 1 || out[0].(*packets.ServerboundPlayerCommand).ActionID != want {
			t.Errorf("action %d -> %#v", action, out)
		}
	}
}

func TestTeams769(t *testing.T) {
	p := &packets.ClientboundSetPlayerTeam{Team: "t", Mode: 2, Parameters: packets.TeamParameters{NameTagVisibility: 2, CollisionRule: 3}}
	q := encodeOne(t, lookup(t, 769), packets.Play, p, "set_player_team").(*v769.ClientboundSetPlayerTeam)
	if q.NameTagVisibility != "hideForOtherTeams" || q.CollisionRule != "pushOwnTeam" || q.Formatting != 21 {
		t.Errorf("%+v", q)
	}
}

func TestExperienceOrb769(t *testing.T) {
	orb, _ := data.Canonical().EntityByName("experience_orb")
	q := encodeOne(t, lookup(t, 769), packets.Play, &packets.ClientboundAddEntity{EntityID: 4, Type: int32(orb.ID), X: 1, Y: 2, Z: 3}, "spawn_entity_experience_orb").(*v769.ClientboundSpawnEntityExperienceOrb)
	if q.EntityID != 4 || q.Y != 2 || q.Count != 1 {
		t.Errorf("%+v", q)
	}
}

func TestPlayerChat769(t *testing.T) {
	p := &packets.ClientboundPlayerChat{GlobalIndex: 9, Index: 2, PlainMessage: "hello", NetworkName: proto.Plain("a")}
	q := encodeOne(t, lookup(t, 769), packets.Play, p, "player_chat").(*v769.ClientboundPlayerChat)
	if q.PlainMessage != "hello" || q.Index != 2 {
		t.Errorf("%+v", q)
	}
}

func TestSlots769Up(t *testing.T) {
	c := lookup(t, 769)
	tr, tm := c.t.last().reg, c.t.last().meta
	sword, _ := tr.ItemByName("diamond_sword")
	damage := tm.Index(meta.Component, "damage")
	slot := func(w *proto.Writer) {
		w.VarInt(1)
		w.VarInt(int32(sword))
		w.VarInt(1)
		w.VarInt(0)
		w.VarInt(damage)
		w.VarInt(300)
	}

	var w proto.Writer
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "set_creative_mode_slot")
	w.VarInt(id)
	w.I16(36)
	slot(&w)
	out, err := c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	r := proto.Reader{B: out[0].(*packets.ServerboundSetCreativeModeSlot).Raw}
	d := rd{r: &r}
	if cnt, item, add, rem := d.varint(), d.varint(), d.varint(), d.varint(); cnt != 1 || item != canonItem(t, "diamond_sword") || add != 1 || rem != 0 {
		t.Fatalf("creative slot %d %d %d %d", cnt, item, add, rem)
	}
	if typ := d.varint(); typ != canonEnum(t, meta.Component, "damage") {
		t.Errorf("component %d", typ)
	}
	if b, _ := r.ByteArray(10); len(b) != 2 {
		t.Errorf("component data % x", b)
	}

	w.B = w.B[:0]
	id, _ = c.t.last().id(packets.Play, packets.Serverbound, "container_click")
	w.VarInt(id)
	(&v769.ServerboundContainerClick{WindowID: 1, StateID: 2, Slot: 3}).Encode(&w)
	w.VarInt(1)
	w.I16(3)
	slot(&w)
	w.VarInt(0) // empty carried item
	out, err = c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	q := out[0].(*packets.ServerboundContainerClick)
	r = proto.Reader{B: q.Raw}
	d = rd{r: &r}
	n, s := d.varint(), int32(0)
	s16, _ := r.I16()
	s = int32(s16)
	present := d.bool()
	item, cnt, comps := d.varint(), d.varint(), d.varint()
	if n != 1 || s != 3 || !present || item != canonItem(t, "diamond_sword") || cnt != 1 || comps != 1 {
		t.Fatalf("click %d %d %v %d %d %d", n, s, present, item, cnt, comps)
	}
	if typ := d.varint(); typ != canonEnum(t, meta.Component, "damage") {
		t.Errorf("hashed component %d", typ)
	}
	r.I32()
	if rem, carried := d.varint(), d.bool(); rem != 0 || carried || r.Remaining() != 0 || d.err != nil {
		t.Errorf("click tail")
	}
}

func TestStep768(t *testing.T) {
	c := lookup(t, 768)
	q := encodeOne(t, c, packets.Play, &packets.ClientboundSetHeldSlot{Slot: 5}, "set_held_slot").(*v768.ClientboundSetHeldSlot)
	if q.Slot != 5 {
		t.Errorf("held slot %+v", q)
	}
	hat := &packets.ClientboundPlayerInfoUpdate{
		Action: packets.ClientboundPlayerInfoUpdateActionUpdateLatency | packets.ClientboundPlayerInfoUpdateActionUpdateHat,
		Data:   []packets.ClientboundPlayerInfoUpdateDataEntry{{UUID: [16]byte{1}, Latency: 40, ShowHat: true}},
	}
	pi := encodeOne(t, c, packets.Play, hat, "player_info_update").(*v768.ClientboundPlayerInfoUpdate)
	if pi.Action != v768.ClientboundPlayerInfoUpdateActionUpdateLatency || pi.Data[0].Latency != 40 {
		t.Errorf("player info %+v", pi)
	}
	hat.Action = packets.ClientboundPlayerInfoUpdateActionUpdateHat
	if n := len(encode(t, c, packets.Play, hat)); n != 0 {
		t.Errorf("hat-only update sent")
	}

	encode(t, c, packets.Play, samplePlayLogin())
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "accept_teleportation")
	body := []byte{byte(id), 1}
	out, err := c.Decode(packets.Play, body)
	if err != nil || len(out) != 2 {
		t.Fatalf("%#v %v", out, err)
	}
	if _, ok := out[1].(*packets.ServerboundPlayerLoaded); !ok {
		t.Errorf("second packet %T", out[1])
	}
	if out, _ := c.Decode(packets.Play, body); len(out) != 1 {
		t.Errorf("player loaded repeated")
	}
}

func TestStep767(t *testing.T) {
	c := lookup(t, 767)
	lf := encodeOne(t, c, packets.Login, &packets.ClientboundLoginFinished{Username: "x"}, "login_finished").(*v767.ClientboundLoginFinished)
	if lf.Username != "x" || lf.StrictErrorHandling {
		t.Errorf("%+v", lf)
	}
	var w proto.Writer
	w.VarInt(0)
	cs := encodeOne(t, c, packets.Play, &packets.ClientboundSetCursorItem{Raw: w.B}, "container_set_slot").(*v767.ClientboundContainerSetSlot)
	if cs.WindowID != containerCarried || cs.Slot != -1 {
		t.Errorf("cursor %+v", cs)
	}
	cd := encodeOne(t, c, packets.Play, &packets.ClientboundCooldown{CooldownGroup: "minecraft:ender_pearl", CooldownTicks: 20}, "cooldown").(*v767.ClientboundCooldown)
	if c.t.last().reg.ItemName(data.ItemID(cd.ItemID)) != "ender_pearl" || cd.CooldownTicks != 20 {
		t.Errorf("cooldown %+v", cd)
	}
	pp := encodeOne(t, c, packets.Play, &packets.ClientboundPlayerPosition{TeleportID: 3, X: 1, Y: 2, Z: 3, Yaw: 90, Flags: 0x1 | 0x20}, "player_position").(*v767.ClientboundPlayerPosition)
	if pp.TeleportID != 3 || pp.Y != 2 || pp.Yaw != 90 || pp.Flags != 1 {
		t.Errorf("position %+v", pp)
	}
	st := encodeOne(t, c, packets.Play, &packets.ClientboundSetTime{Age: 5}, "set_time").(*v767.ClientboundSetTime)
	if st.Age != 5 || st.Time >= 0 {
		t.Errorf("frozen time %+v", st)
	}
	login := encodeOne(t, c, packets.Play, samplePlayLogin(), "login").(*v767.ClientboundLogin)
	if login.WorldState.Gamemode != 1 || !login.EnforcesSecureChat {
		t.Errorf("login %+v", login)
	}

	dec := func(name string, p pkt) []packets.Packet {
		id, ok := c.t.last().id(packets.Play, packets.Serverbound, name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		var w proto.Writer
		w.VarInt(id)
		p.Encode(&w)
		out, err := c.Decode(packets.Play, w.B)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := dec("player_command", &v767.ServerboundPlayerCommand{EntityID: 1, ActionID: 0})
	if len(out) != 1 || out[0].(*packets.ServerboundPlayerInput).Inputs != packets.ServerboundPlayerInputInputsShift {
		t.Errorf("sneak %#v", out)
	}
	out = dec("steer_vehicle", &v767.ServerboundSteerVehicle{Forward: 1, Sideways: -1, Jump: 1})
	want := packets.ServerboundPlayerInputInputsShift | packets.ServerboundPlayerInputInputsForward | packets.ServerboundPlayerInputInputsRight | packets.ServerboundPlayerInputInputsJump
	if len(out) != 1 || out[0].(*packets.ServerboundPlayerInput).Inputs != want {
		t.Errorf("steer %#v", out)
	}
	out = dec("container_close", &v767.ServerboundContainerClose{WindowID: 200})
	if len(out) != 1 || out[0].(*packets.ServerboundContainerClose).WindowID != 200 {
		t.Errorf("close %#v", out)
	}
}

func TestStep766(t *testing.T) {
	c := lookup(t, 766)
	dc := &packets.ClientboundDisguisedChat{Message: proto.Plain("hi"), Type: proto.Holder[packets.ChatTypes]{ID: 2}, Name: proto.Plain("a")}
	q := encodeOne(t, c, packets.Play, dc, "disguised_chat").(*v766.ClientboundDisguisedChat)
	if q.Type != 2 || q.Message.Text != "hi" {
		t.Errorf("%+v", q)
	}
	pc := &packets.ClientboundPlayerChat{PlainMessage: "yo", Type: proto.Holder[packets.ChatTypes]{ID: 1}, NetworkName: proto.Plain("n"),
		PreviousMessages: []packets.PreviousMessage{{ID: 3}}, FilterType: 2, FilterTypeMask: []int64{5}}
	qc := encodeOne(t, c, packets.Play, pc, "player_chat").(*v766.ClientboundPlayerChat)
	if qc.Type != 1 || qc.PlainMessage != "yo" || len(qc.FilterTypeMask) != 1 {
		t.Errorf("%+v", qc)
	}

	rot, _ := c.t.last().id(packets.Play, packets.Serverbound, "move_player_rot")
	var w proto.Writer
	w.VarInt(rot)
	w.F32(45)
	w.F32(10)
	w.U8(1)
	if _, err := c.Decode(packets.Play, w.B); err != nil {
		t.Fatal(err)
	}
	use, _ := c.t.last().id(packets.Play, packets.Serverbound, "use_item")
	w.B = w.B[:0]
	w.VarInt(use)
	(&v766.ServerboundUseItem{Hand: 1, Sequence: 4}).Encode(&w)
	out, err := c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	if u := out[0].(*packets.ServerboundUseItem); u.Hand != 1 || u.Sequence != 4 || u.Rotation.X != 45 || u.Rotation.Y != 10 {
		t.Errorf("%+v", u)
	}
}
