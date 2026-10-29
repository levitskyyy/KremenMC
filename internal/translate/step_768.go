package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v768"
	"github.com/levitskyyy/KremenMC/internal/translate/v769"
)

// step768 converts between 1.21.4 (769) and 1.21.2/1.21.3 (768).
func step768(st *step) {
	const sb = packets.Serverbound
	// picking from an inventory slot has no newer counterpart
	st.drop(packets.Play, sb, "pick_item")

	st.clientbound(packets.Play, "explode", func(x *xctx, r *proto.Reader) error {
		var p v769.ClientboundExplode
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v768.ClientboundExplode{X: p.X, Y: p.Y, Z: p.Z, Raw: p.Raw}
		if k := p.PlayerKnockback; k.Ok {
			o.PlayerKnockback = proto.Some(v768.Vec3f{X: float32(k.V.X), Y: float32(k.V.Y), Z: float32(k.V.Z)})
		}
		w := x.out()
		o.Encode(w)
		return x.emit("explode", w)
	})
	st.clientbound(packets.Play, "level_particles", func(x *xctx, r *proto.Reader) error {
		b := r.B
		if len(b) < 2 {
			return errCount
		}
		w := x.out()
		w.U8(b[0]) // long distance; always-show was added in 1.21.4
		w.Raw(b[2:])
		return x.emit("level_particles", w)
	})
	st.clientbound(packets.Play, "player_info_update", func(x *xctx, r *proto.Reader) error {
		if len(r.B) == 0 || r.B[0]&0x80 == 0 {
			return x.pass("player_info_update")
		}
		var p v769.ClientboundPlayerInfoUpdate
		if err := p.Decode(r); err != nil {
			return err
		}
		p.Action &^= v769.ClientboundPlayerInfoUpdateActionUpdateHat
		if p.Action == 0 {
			return nil
		}
		w := x.out()
		p.Encode(w)
		return x.emit("player_info_update", w)
	})
	st.clientbound(packets.Play, "set_held_slot", func(x *xctx, r *proto.Reader) error {
		s, err := r.VarInt()
		if err != nil {
			return err
		}
		w := x.out()
		w.I8(int8(s))
		return x.emit("set_held_slot", w)
	})

	st.serverbound(packets.Play, "move_vehicle", func(x *xctx, r *proto.Reader) error {
		var p v768.ServerboundMoveVehicle
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v769.ServerboundMoveVehicle{X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw, Pitch: p.Pitch}
		w := x.out()
		o.Encode(w)
		return x.emit("move_vehicle", w)
	})
	// 1.21.4 clients report when the world is loaded; older clients are
	// taken as loaded once they confirm the spawn teleport.
	st.serverbound(packets.Play, "accept_teleportation", func(x *xctx, r *proto.Reader) error {
		if err := x.pass("accept_teleportation"); err != nil {
			return err
		}
		if x.c.loaded.Swap(true) {
			return nil
		}
		return x.emit("player_loaded", x.out())
	})
}
