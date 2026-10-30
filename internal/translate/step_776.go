package translate

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v776"
)

// step776 converts between 26.3 (777) and 26.2 (776).
func step776(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Configuration, cb, "post_effects")
	st.drop(packets.Play, cb, "post_effects", "add_transient_block")
	// mapper value lists only (hand enum, attribute names from a stale data branch)
	st.compatible(packets.Play, cb, "open_book", "update_attributes")
	st.compatible(packets.Play, sb, "use_item", "use_item_on")

	st.clientbound(packets.Play, "level_chunk_with_light", chunkLight776)
	st.clientbound(packets.Play, "light_update", lightUpdate776)
	st.clientbound(packets.Play, "login", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundLogin
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v776.ClientboundLogin{
			EntityID: p.EntityID, IsHardcore: p.IsHardcore, WorldNames: p.WorldNames,
			MaxPlayers: p.MaxPlayers, ViewDistance: p.ViewDistance, SimulationDistance: p.SimulationDistance,
			ReducedDebugInfo: p.ReducedDebugInfo, EnableRespawnScreen: p.EnableRespawnScreen,
			DoLimitedCrafting: p.DoLimitedCrafting, WorldState: spawnInfo776(&p.WorldState),
			OnlineMode: p.OnlineMode, EnforcesSecureChat: p.EnforcesSecureChat,
		}
		w := x.out()
		o.Encode(w)
		return x.emit("login", w)
	})
	st.clientbound(packets.Play, "respawn", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundRespawn
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v776.ClientboundRespawn{WorldState: spawnInfo776(&p.WorldState), CopyMetadata: p.CopyMetadata}
		w := x.out()
		o.Encode(w)
		return x.emit("respawn", w)
	})
	st.clientbound(packets.Play, "entity_position_sync", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundEntityPositionSync
		if err := p.Decode(r); err != nil {
			return err
		}
		pos := p.Position
		if p.PathType == 1 {
			if len(p.Steps) == 0 {
				return nil
			}
			s := p.Steps[len(p.Steps)-1]
			pos = packets.Vec3f64{X: s.X, Y: s.Y, Z: s.Z}
		}
		o := v776.ClientboundEntityPositionSync{
			EntityID: p.EntityID, X: pos.X, Y: pos.Y, Z: pos.Z,
			Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.OnGround,
		}
		w := x.out()
		o.Encode(w)
		return x.emit("entity_position_sync", w)
	})
	st.clientbound(packets.Play, "move_entity_pos", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundMoveEntityPos
		if err := p.Decode(r); err != nil {
			return err
		}
		dx, dy, dz := sumDelta(&p.Delta)
		o := v776.ClientboundMoveEntityPos{EntityID: p.EntityID, DX: dx, DY: dy, DZ: dz, OnGround: p.Delta.OnGround}
		w := x.out()
		o.Encode(w)
		return x.emit("move_entity_pos", w)
	})
	st.clientbound(packets.Play, "move_entity_pos_rot", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundMoveEntityPosRot
		if err := p.Decode(r); err != nil {
			return err
		}
		dx, dy, dz := sumDelta(&p.Delta)
		o := v776.ClientboundMoveEntityPosRot{
			EntityID: p.EntityID, DX: dx, DY: dy, DZ: dz,
			Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.Delta.OnGround,
		}
		w := x.out()
		o.Encode(w)
		return x.emit("move_entity_pos_rot", w)
	})
	st.clientbound(packets.Play, "move_entity_rot", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundMoveEntityRot
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v776.ClientboundMoveEntityRot{EntityID: p.EntityID, Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.OnGround}
		w := x.out()
		o.Encode(w)
		return x.emit("move_entity_rot", w)
	})
	st.clientbound(packets.Play, "open_sign_editor", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundOpenSignEditor
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v776.ClientboundOpenSignEditor{Location: p.Location, IsFrontText: p.TextSlot == 1}
		w := x.out()
		o.Encode(w)
		return x.emit("open_sign_editor", w)
	})
	st.clientbound(packets.Play, "swing_animation", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundSwingAnimation
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v776.ClientboundAnimate{EntityID: p.EntityID}
		if p.Hand == 1 {
			o.Animation = 3
		}
		w := x.out()
		o.Encode(w)
		return x.emit("animate", w)
	})
	st.clientbound(packets.Play, "animate", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundAnimate
		if err := p.Decode(r); err != nil {
			return err
		}
		// 26.3 renumbered the remaining actions: wake up, crit, magic crit.
		o := v776.ClientboundAnimate{EntityID: p.EntityID, Animation: [...]uint8{2, 4, 5}[min(p.Animation, 2)]}
		w := x.out()
		o.Encode(w)
		return x.emit("animate", w)
	})
	st.clientbound(packets.Play, "explode", func(x *xctx, r *proto.Reader) error {
		b := r.B
		if len(b) == 0 {
			return errCount
		}
		w := x.out()
		w.Raw(b[:len(b)-1]) // play sound flag
		return x.emit("explode", w)
	})
	st.clientbound(packets.Play, "level_particles", func(x *xctx, r *proto.Reader) error {
		start := r.Off
		if err := x.c.skipTargetParticle(r); err != nil {
			return err
		}
		particle := r.B[start:r.Off]
		d := rd{r: r}
		override, always := d.bool(), d.bool()
		px, py, pz := d.f64(), d.f64(), d.f64()
		ox, oy, oz := d.f32(), d.f32(), d.f32()
		sx, sy, sz := d.f32(), d.f32(), d.f32()
		count := d.varint()
		d.varint() // randomization
		if d.err != nil {
			return d.err
		}
		w := x.out()
		w.Bool(override)
		w.Bool(always)
		w.F64(px)
		w.F64(py)
		w.F64(pz)
		w.F32(ox)
		w.F32(oy)
		w.F32(oz)
		w.F32(max(sx, sy, sz))
		w.I32(count)
		w.Raw(particle)
		return x.emit("level_particles", w)
	})

	st.serverbound(packets.Play, "accept_teleportation", func(x *xctx, r *proto.Reader) error {
		var p v776.ServerboundAcceptTeleportation
		if err := p.Decode(r); err != nil {
			return err
		}
		o := packets.ServerboundAcceptTeleportation{TeleportID: p.TeleportID}
		if t, ok := x.c.teleport(p.TeleportID); ok {
			o.X, o.Y, o.Z, o.Yaw, o.Pitch = t.x, t.y, t.z, t.yaw, t.pitch
		}
		w := x.out()
		o.Encode(w)
		return x.emit("accept_teleportation", w)
	})
	st.serverbound(packets.Play, "sign_update", func(x *xctx, r *proto.Reader) error {
		var p v776.ServerboundSignUpdate
		if err := p.Decode(r); err != nil {
			return err
		}
		o := packets.ServerboundSignUpdate{Location: p.Location, Text1: p.Text1, Text2: p.Text2, Text3: p.Text3, Text4: p.Text4}
		if p.IsFrontText {
			o.TextSlot = 1
		}
		w := x.out()
		o.Encode(w)
		return x.emit("sign_update", w)
	})
	// 26.3 dropped the serverbound arm swing; a main-hand swing is the
	// closest thing to the new punch packet.
	st.serverbound(packets.Play, "swing", func(x *xctx, r *proto.Reader) error {
		var p v776.ServerboundSwing
		if err := p.Decode(r); err != nil {
			return err
		}
		if p.Hand != 0 {
			return nil
		}
		return x.emit("punch", x.out())
	})
}

func spawnInfo776(s *packets.SpawnInfo) v776.SpawnInfo {
	prev := uint8(255)
	if s.PreviousGamemode > 0 {
		prev = uint8(s.PreviousGamemode - 1)
	}
	o := v776.SpawnInfo{
		Dimension: s.Dimension, Name: s.Name, HashedSeed: s.HashedSeed,
		Gamemode: int8(s.Gamemode), PreviousGamemode: prev,
		IsDebug: s.IsDebug, IsFlat: s.IsFlat,
		PortalCooldown: s.PortalCooldown, SeaLevel: s.SeaLevel,
	}
	if s.Death.Ok {
		o.Death = proto.Some(v776.GlobalPos{DimensionName: s.Death.V.DimensionName, Location: s.Death.V.Location})
	}
	return o
}

func clamp16(v int32) int16 {
	return int16(max(math.MinInt16, min(math.MaxInt16, v)))
}

// sumDelta collapses a stepped 26.3 movement into one relative move.
func sumDelta(d *proto.MoveDelta) (int16, int16, int16) {
	if len(d.Steps) == 0 {
		return d.DX, d.DY, d.DZ
	}
	var x, y, z int32
	for _, s := range d.Steps {
		x += int32(s.DX)
		y += int32(s.DY)
		z += int32(s.DZ)
	}
	return clamp16(x), clamp16(y), clamp16(z)
}

// 26.3 writes light masks with ByteBufCodecs.BIT_SET (length-prefixed byte
// array); 26.2 and older read FriendlyByteBuf.readBitSet (long array).
func lightMasks776(r *proto.Reader, w *proto.Writer) error {
	for range 4 {
		b, err := r.ByteArray(1 << 16)
		if err != nil {
			return err
		}
		n := (len(b) + 7) / 8
		w.VarInt(int32(n))
		for i := range n {
			var l uint64
			for j := 0; j < 8 && i*8+j < len(b); j++ {
				l |= uint64(b[i*8+j]) << (8 * j)
			}
			w.U64(l)
		}
	}
	w.Raw(r.Rest())
	return nil
}

func chunkLight776(x *xctx, r *proto.Reader) error {
	start := r.Off
	if _, err := r.Raw(8); err != nil {
		return err
	}
	n, err := r.Count(2)
	if err != nil {
		return err
	}
	for range n {
		if _, err := r.VarInt(); err != nil {
			return err
		}
		l, err := r.Count(8)
		if err != nil {
			return err
		}
		if _, err := r.Raw(l * 8); err != nil {
			return err
		}
	}
	if _, err := r.ByteArray(2097152); err != nil {
		return err
	}
	if n, err = r.Count(4); err != nil {
		return err
	}
	for range n {
		if _, err := r.Raw(3); err != nil {
			return err
		}
		if _, err := r.VarInt(); err != nil {
			return err
		}
		l, err := proto.NBTLen(r.B[r.Off:])
		if err != nil {
			return err
		}
		r.Off += l
	}
	w := x.out()
	w.Raw(r.B[start:r.Off])
	if err := lightMasks776(r, w); err != nil {
		return err
	}
	return x.emit("level_chunk_with_light", w)
}

func lightUpdate776(x *xctx, r *proto.Reader) error {
	start := r.Off
	if _, err := r.VarInt(); err != nil {
		return err
	}
	if _, err := r.VarInt(); err != nil {
		return err
	}
	w := x.out()
	w.Raw(r.B[start:r.Off])
	if err := lightMasks776(r, w); err != nil {
		return err
	}
	return x.emit("light_update", w)
}
