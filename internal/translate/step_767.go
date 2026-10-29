package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v767"
	"github.com/levitskyyy/KremenMC/internal/translate/v768"
)

// Container IDs that address the player directly in 1.21.1 container_set_slot.
const (
	containerCarried   = 0xff
	containerInventory = 0xfe
)

// step767 converts between 1.21.2/1.21.3 (768) and 1.21/1.21.1 (767).
func step767(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	// the recipe book was rebuilt in 1.21.2; recipes are dropped before the chain
	st.drop(packets.Play, cb, "place_ghost_recipe", "recipe_book_add", "recipe_book_remove", "recipe_book_settings",
		"update_recipes", "merchant_offers", "player_rotation", "move_minecart_along_track")
	st.drop(packets.Play, sb, "place_recipe", "recipe_book_seen_recipe")
	// attribute IDs are remapped before the chain; move flags: onGround is bit 0
	st.compatible(packets.Play, cb, "update_attributes")
	st.compatible(packets.Play, sb, "move_player_pos", "move_player_pos_rot", "move_player_rot", "move_player_status_only")

	st.clientbound(packets.Login, "login_finished", func(x *xctx, r *proto.Reader) error {
		w := x.out()
		w.Raw(r.B)
		w.Bool(false) // strict error handling
		return x.emit("login_finished", w)
	})
	for _, name := range []string{"container_close", "container_set_content", "container_set_data", "container_set_slot", "mount_screen_open"} {
		st.clientbound(packets.Play, name, func(x *xctx, r *proto.Reader) error {
			id, err := r.VarInt()
			if err != nil {
				return err
			}
			w := x.out()
			w.U8(uint8(id))
			w.Raw(r.B[r.Off:])
			return x.emit(name, w)
		})
	}
	st.clientbound(packets.Play, "set_cursor_item", func(x *xctx, r *proto.Reader) error {
		w := x.out()
		w.U8(containerCarried)
		w.VarInt(0)
		w.I16(-1)
		w.Raw(r.B)
		return x.emit("container_set_slot", w)
	})
	st.clientbound(packets.Play, "set_player_inventory", func(x *xctx, r *proto.Reader) error {
		slot, err := r.VarInt()
		if err != nil {
			return err
		}
		w := x.out()
		w.U8(containerInventory)
		w.VarInt(0)
		w.I16(int16(slot))
		w.Raw(r.B[r.Off:])
		return x.emit("container_set_slot", w)
	})
	st.clientbound(packets.Play, "cooldown", func(x *xctx, r *proto.Reader) error {
		group, err := r.String(32767)
		if err != nil {
			return err
		}
		ticks, err := r.VarInt()
		if err != nil {
			return err
		}
		item, ok := x.c.t.last().reg.ItemByName(trimNS(group))
		if !ok {
			return nil
		}
		o := v767.ClientboundCooldown{ItemID: int32(item), CooldownTicks: ticks}
		w := x.out()
		o.Encode(w)
		return x.emit("cooldown", w)
	})
	st.clientbound(packets.Play, "explode", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundExplode
		if err := p.Decode(r); err != nil {
			return err
		}
		t := proto.Reader{B: p.Raw}
		if err := x.c.skipTargetParticle(&t); err != nil {
			return err
		}
		particle := p.Raw[:t.Off]
		o := v767.ClientboundExplode{X: p.X, Y: p.Y, Z: p.Z}
		if k := p.PlayerKnockback; k.Ok {
			o.PlayerMotionX, o.PlayerMotionY, o.PlayerMotionZ = k.V.X, k.V.Y, k.V.Z
		}
		w := x.out()
		o.Encode(w)
		w.Raw(particle) // small and large particle
		w.Raw(p.Raw)
		return x.emit("explode", w)
	})
	st.clientbound(packets.Play, "login", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundLogin
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v767.ClientboundLogin{
			EntityID: p.EntityID, IsHardcore: p.IsHardcore, WorldNames: p.WorldNames,
			MaxPlayers: p.MaxPlayers, ViewDistance: p.ViewDistance, SimulationDistance: p.SimulationDistance,
			ReducedDebugInfo: p.ReducedDebugInfo, EnableRespawnScreen: p.EnableRespawnScreen,
			DoLimitedCrafting: p.DoLimitedCrafting, WorldState: spawnInfo767(&p.WorldState),
			EnforcesSecureChat: p.EnforcesSecureChat,
		}
		w := x.out()
		o.Encode(w)
		return x.emit("login", w)
	})
	st.clientbound(packets.Play, "respawn", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundRespawn
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v767.ClientboundRespawn{WorldState: spawnInfo767(&p.WorldState), CopyMetadata: p.CopyMetadata}
		w := x.out()
		o.Encode(w)
		return x.emit("respawn", w)
	})
	st.clientbound(packets.Play, "player_info_update", func(x *xctx, r *proto.Reader) error {
		const listOrder = v768.ClientboundPlayerInfoUpdateActionUpdateListOrder
		if len(r.B) == 0 || r.B[0]&uint8(listOrder) == 0 {
			return x.pass("player_info_update")
		}
		var p v768.ClientboundPlayerInfoUpdate
		if err := p.Decode(r); err != nil {
			return err
		}
		if p.Action &^= listOrder; p.Action == 0 {
			return nil
		}
		w := x.out()
		p.Encode(w)
		return x.emit("player_info_update", w)
	})
	// velocity and relative velocity flags have no 1.21.1 form
	st.clientbound(packets.Play, "player_position", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundPlayerPosition
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v767.ClientboundPlayerPosition{X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw, Pitch: p.Pitch,
			Flags: v767.PositionUpdateRelatives(p.Flags & 0x1f), TeleportID: p.TeleportID}
		w := x.out()
		o.Encode(w)
		return x.emit("player_position", w)
	})
	st.clientbound(packets.Play, "teleport_entity", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundTeleportEntity
		if err := p.Decode(r); err != nil {
			return err
		}
		return x.emitTeleport(p.EntityID, p.X, p.Y, p.Z, p.Yaw, p.Pitch, p.OnGround)
	})
	st.clientbound(packets.Play, "entity_position_sync", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundEntityPositionSync
		if err := p.Decode(r); err != nil {
			return err
		}
		return x.emitTeleport(p.EntityID, p.X, p.Y, p.Z, p.Yaw, p.Pitch, p.OnGround)
	})
	st.clientbound(packets.Play, "set_time", func(x *xctx, r *proto.Reader) error {
		var p v768.ClientboundSetTime
		if err := p.Decode(r); err != nil {
			return err
		}
		// a negative day time stops the daylight cycle
		t := p.Time
		if !p.TickDayTime {
			t = -max(t, 1)
		}
		o := v767.ClientboundSetTime{Age: p.Age, Time: t}
		w := x.out()
		o.Encode(w)
		return x.emit("set_time", w)
	})

	st.serverbound(packets.Configuration, "client_information", appendVarInt("client_information", 0))
	st.serverbound(packets.Play, "client_information", appendVarInt("client_information", 0))
	for _, name := range []string{"container_button_click", "container_click", "container_close"} {
		st.serverbound(packets.Play, name, func(x *xctx, r *proto.Reader) error {
			id, err := r.U8()
			if err != nil {
				return err
			}
			w := x.out()
			w.VarInt(int32(id))
			w.Raw(r.B[r.Off:])
			return x.emit(name, w)
		})
	}
	st.serverbound(packets.Play, "use_item_on", func(x *xctx, r *proto.Reader) error {
		var p v767.ServerboundUseItemOn
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v768.ServerboundUseItemOn{Hand: p.Hand, Location: p.Location, Direction: p.Direction,
			CursorX: p.CursorX, CursorY: p.CursorY, CursorZ: p.CursorZ, InsideBlock: p.InsideBlock, Sequence: p.Sequence}
		w := x.out()
		o.Encode(w)
		return x.emit("use_item_on", w)
	})
	// before 1.21.2 sneaking came through player_command and movement
	// keys only while riding; both feed the newer player_input state
	st.serverbound(packets.Play, "steer_vehicle", func(x *xctx, r *proto.Reader) error {
		var p v767.ServerboundSteerVehicle
		if err := p.Decode(r); err != nil {
			return err
		}
		in := x.c.input & v768.ServerboundPlayerInputInputsShift
		switch {
		case p.Forward > 0:
			in |= v768.ServerboundPlayerInputInputsForward
		case p.Forward < 0:
			in |= v768.ServerboundPlayerInputInputsBackward
		}
		switch {
		case p.Sideways > 0:
			in |= v768.ServerboundPlayerInputInputsLeft
		case p.Sideways < 0:
			in |= v768.ServerboundPlayerInputInputsRight
		}
		if p.Jump&1 != 0 {
			in |= v768.ServerboundPlayerInputInputsJump
		}
		if p.Jump&2 != 0 {
			in |= v768.ServerboundPlayerInputInputsShift
		}
		return x.emitInput(in)
	})
	st.serverbound(packets.Play, "player_command", func(x *xctx, r *proto.Reader) error {
		action := int32(-1)
		if len(r.B) > 0 {
			t := proto.Reader{B: r.B}
			t.VarInt()
			action, _ = t.VarInt()
		}
		if err := x.pass("player_command"); err != nil || action > 1 || action < 0 {
			return err
		}
		in := x.c.input &^ v768.ServerboundPlayerInputInputsShift
		if action == 0 {
			in |= v768.ServerboundPlayerInputInputsShift
		}
		return x.emitInput(in)
	})
}

func (x *xctx) emitInput(in v768.ServerboundPlayerInputInputs) error {
	x.c.input = in
	w := x.out()
	(&v768.ServerboundPlayerInput{Inputs: in}).Encode(w)
	return x.emit("player_input", w)
}

func (x *xctx) emitTeleport(id int32, px, py, pz float64, yaw, pitch float32, onGround bool) error {
	o := v767.ClientboundTeleportEntity{EntityID: id, X: px, Y: py, Z: pz,
		Yaw: angleByte(yaw), Pitch: angleByte(pitch), OnGround: onGround}
	w := x.out()
	o.Encode(w)
	return x.emit("teleport_entity", w)
}

func angleByte(deg float32) int8 {
	return int8(int32(deg * 256 / 360))
}

func spawnInfo767(s *v768.SpawnInfo) v767.SpawnInfo {
	o := v767.SpawnInfo{
		Dimension: s.Dimension, Name: s.Name, HashedSeed: s.HashedSeed, Gamemode: s.Gamemode,
		PreviousGamemode: s.PreviousGamemode, IsDebug: s.IsDebug, IsFlat: s.IsFlat, PortalCooldown: s.PortalCooldown,
	}
	if s.Death.Ok {
		o.Death = proto.Some(v767.SpawnInfoDeath{DimensionName: s.Death.V.DimensionName, Location: s.Death.V.Location})
	}
	return o
}

func appendVarInt(name string, v int32) xform {
	return func(x *xctx, r *proto.Reader) error {
		w := x.out()
		w.Raw(r.B)
		w.VarInt(v)
		return x.emit(name, w)
	}
}
