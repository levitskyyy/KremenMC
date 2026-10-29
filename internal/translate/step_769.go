package translate

import (
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
	"github.com/levitskyyy/KremenMC/internal/translate/v769"
	"github.com/levitskyyy/KremenMC/internal/translate/v770"
)

// heightmapNames are the 1.21.4 NBT keys of the 1.21.5+ heightmap type IDs.
var heightmapNames = [...]string{
	"WORLD_SURFACE_WG", "WORLD_SURFACE", "OCEAN_FLOOR_WG", "OCEAN_FLOOR", "MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES",
}

var (
	visibilityNames = [...]string{"always", "never", "hideForOtherTeams", "hideForOwnTeam"}
	collisionNames  = [...]string{"always", "never", "pushOtherTeams", "pushOwnTeam"}
)

// step769 converts between 1.21.5 (770) and 1.21.4 (769). Chunk sections get
// their long-array lengths back through the spec's chunk format.
func step769(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Play, cb, "test_instance_block_status")
	// item displays and trades are dropped before the chain (see rewriteCB)
	st.drop(packets.Play, cb, "merchant_offers", "update_advancements")
	st.drop(packets.Play, sb, "set_test_block", "test_instance_block_action")
	st.compatible(packets.Play, sb, "set_structure_block")

	st.clientbound(packets.Play, "level_chunk_with_light", func(x *xctx, r *proto.Reader) error {
		d := rd{r: r}
		cx, cz := d.i32(), d.i32()
		n := d.varint()
		if d.err != nil {
			return d.err
		}
		w := x.out()
		w.I32(cx)
		w.I32(cz)
		w.U8(nbt.TagCompound)
		for range n {
			typ := d.varint()
			l := d.varint()
			if d.err != nil {
				return d.err
			}
			raw, err := r.Raw(int(l) * 8)
			if err != nil || l < 0 {
				return errCount
			}
			if typ < 0 || int(typ) >= len(heightmapNames) {
				continue
			}
			w.U8(nbt.TagLongArray)
			w.U16(uint16(len(heightmapNames[typ])))
			w.Raw([]byte(heightmapNames[typ]))
			w.I32(l)
			w.Raw(raw)
		}
		w.U8(nbt.TagEnd)
		w.Raw(r.B[r.Off:])
		return x.emit("level_chunk_with_light", w)
	})
	st.clientbound(packets.Play, "player_chat", func(x *xctx, r *proto.Reader) error {
		if _, err := r.VarInt(); err != nil { // global index
			return err
		}
		return x.passRest("player_chat")
	})
	st.clientbound(packets.Play, "set_player_team", func(x *xctx, r *proto.Reader) error {
		var p v770.ClientboundSetPlayerTeam
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v769.ClientboundSetPlayerTeam{
			Team: p.Team, Mode: p.Mode, Name: p.Name, FriendlyFire: p.FriendlyFire,
			Formatting: p.Formatting, Prefix: p.Prefix, Suffix: p.Suffix, Players: p.Players,
		}
		if p.Mode == 0 || p.Mode == 2 {
			o.NameTagVisibility = enumName(visibilityNames[:], p.NameTagVisibility)
			o.CollisionRule = enumName(collisionNames[:], p.CollisionRule)
		}
		w := x.out()
		o.Encode(w)
		return x.emit("set_player_team", w)
	})
	// experience orbs had their own spawn packet; the value moved to metadata in 1.21.5
	st.clientbound(packets.Play, "add_entity", func(x *xctx, r *proto.Reader) error {
		var p v770.ClientboundAddEntity
		if err := p.Decode(r); err != nil {
			return err
		}
		if orb, ok := x.c.t.last().reg.EntityByName("experience_orb"); !ok || p.Type != int32(orb.ID) {
			return x.pass("add_entity")
		}
		o := v769.ClientboundSpawnEntityExperienceOrb{EntityID: p.EntityID, X: p.X, Y: p.Y, Z: p.Z, Count: 1}
		w := x.out()
		o.Encode(w)
		return x.emit("spawn_entity_experience_orb", w)
	})

	appendChecksum := func(name string) xform {
		return func(x *xctx, r *proto.Reader) error {
			w := x.out()
			w.Raw(r.B)
			w.U8(0)
			return x.emit(name, w)
		}
	}
	st.serverbound(packets.Play, "chat", appendChecksum("chat"))
	st.serverbound(packets.Play, "chat_command_signed", appendChecksum("chat_command_signed"))
	st.serverbound(packets.Play, "set_creative_mode_slot", func(x *xctx, r *proto.Reader) error {
		slot, err := r.I16()
		if err != nil {
			return err
		}
		w := x.out()
		w.I16(slot)
		if err := x.c.slotToUntrusted(r, w); err != nil {
			return err
		}
		return x.emit("set_creative_mode_slot", w)
	})
	st.serverbound(packets.Play, "container_click", func(x *xctx, r *proto.Reader) error {
		var p v769.ServerboundContainerClick
		if err := p.Decode(r); err != nil {
			return err
		}
		w := x.out()
		o := v770.ServerboundContainerClick{WindowID: p.WindowID, StateID: p.StateID, Slot: p.Slot, MouseButton: p.MouseButton, Mode: p.Mode}
		o.Encode(w)
		t := proto.Reader{B: p.Raw}
		n, err := t.VarInt()
		if err != nil || n < 0 || int(n) > t.Remaining() {
			return errCount
		}
		w.VarInt(n)
		for range n {
			s, err := t.I16()
			if err != nil {
				return err
			}
			w.I16(s)
			if err := x.c.slotToHashed(&t, w); err != nil {
				return err
			}
		}
		if err := x.c.slotToHashed(&t, w); err != nil {
			return err
		}
		return x.emit("container_click", w)
	})
}

func enumName(names []string, v int32) string {
	if v < 0 || int(v) >= len(names) {
		return names[0]
	}
	return names[v]
}

// targetComponent returns the schema of a component type in the client's format.
func (c *conn) targetComponent(t int32) (*pdef.Node, error) {
	cs := c.sbItems.compSrc
	if t < 0 || int(t) >= len(cs) || cs[t] == nil {
		return nil, errUnknownValue
	}
	return cs[t], nil
}

// slotToUntrusted converts a pre-1.21.5 Slot into the length-prefixed
// UntrustedSlot form, keeping the client's IDs.
func (c *conn) slotToUntrusted(r *proto.Reader, w *proto.Writer) error {
	d := rd{r: r}
	cnt := d.varint()
	w.VarInt(cnt)
	if d.err != nil || cnt <= 0 {
		return d.err
	}
	id, add, rem := d.varint(), d.varint(), d.varint()
	if d.err != nil {
		return d.err
	}
	w.VarInt(id)
	w.VarInt(add)
	w.VarInt(rem)
	for range add {
		t := d.varint()
		if d.err != nil {
			return d.err
		}
		node, err := c.targetComponent(t)
		if err != nil {
			return err
		}
		start := r.Off
		if err := pdef.Skip(node, r); err != nil {
			return err
		}
		w.VarInt(t)
		w.ByteArray(r.B[start:r.Off])
	}
	for range rem {
		w.VarInt(d.varint())
	}
	return d.err
}

// slotToHashed converts a pre-1.21.5 Slot into an optional HashedSlot. The
// component hashes are unknown; the server resynchronizes such slots.
func (c *conn) slotToHashed(r *proto.Reader, w *proto.Writer) error {
	d := rd{r: r}
	cnt := d.varint()
	if d.err != nil {
		return d.err
	}
	if cnt <= 0 {
		w.Bool(false)
		return nil
	}
	id, add, rem := d.varint(), d.varint(), d.varint()
	if d.err != nil {
		return d.err
	}
	w.Bool(true)
	w.VarInt(id)
	w.VarInt(cnt)
	w.VarInt(add)
	for range add {
		t := d.varint()
		if d.err != nil {
			return d.err
		}
		node, err := c.targetComponent(t)
		if err != nil {
			return err
		}
		if err := pdef.Skip(node, r); err != nil {
			return err
		}
		w.VarInt(t)
		w.I32(0)
	}
	w.VarInt(rem)
	for range rem {
		w.VarInt(d.varint())
	}
	return d.err
}

// passRest re-emits the unread part of the input as packet name.
func (x *xctx) passRest(name string) error {
	w := x.out()
	w.Raw(x.r.B[x.r.Off:])
	return x.emit(name, w)
}
