package translate

import (
	"math"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v772"
	"github.com/levitskyyy/KremenMC/internal/translate/v773"
)

// step772 converts between 1.21.9/1.21.10 (773) and 1.21.7/1.21.8 (772).
func step772(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Configuration, cb, "code_of_conduct")
	st.drop(packets.Play, cb, "debug_block_value", "debug_chunk_value", "debug_entity_value", "debug_event", "game_test_highlight_pos")
	st.drop(packets.Play, sb, "debug_sample_subscription")

	st.clientbound(packets.Play, "add_entity", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundAddEntity
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v772.ClientboundAddEntity{
			EntityID: p.EntityID, ObjectUUID: p.ObjectUUID, Type: p.Type, X: p.X, Y: p.Y, Z: p.Z,
			Pitch: p.Pitch, Yaw: p.Yaw, HeadPitch: p.HeadPitch, ObjectData: p.ObjectData,
			Velocity: velocity16(p.Velocity),
		}
		w := x.out()
		o.Encode(w)
		return x.emit("add_entity", w)
	})
	st.clientbound(packets.Play, "set_entity_motion", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundSetEntityMotion
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v772.ClientboundSetEntityMotion{EntityID: p.EntityID, Velocity: velocity16(p.Velocity)}
		w := x.out()
		o.Encode(w)
		return x.emit("set_entity_motion", w)
	})
	st.clientbound(packets.Play, "explode", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundExplode
		if err := p.Decode(r); err != nil {
			return err
		}
		tail := proto.Reader{B: p.Raw}
		if err := x.c.skipTargetParticle(&tail); err != nil {
			return err
		}
		if err := skipSoundHolder(&tail); err != nil {
			return err
		}
		o := v772.ClientboundExplode{X: p.Center.X, Y: p.Center.Y, Z: p.Center.Z, Raw: p.Raw[:tail.Off]}
		if p.PlayerKnockback.Ok {
			k := p.PlayerKnockback.V
			o.PlayerKnockback = proto.Some(v772.Vec3f64{X: k.X, Y: k.Y, Z: k.Z})
		}
		w := x.out()
		o.Encode(w)
		return x.emit("explode", w)
	})
	st.clientbound(packets.Play, "move_minecart_along_track", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundMoveMinecartAlongTrack
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v772.ClientboundMoveMinecartAlongTrack{EntityID: p.EntityID, Steps: make([]v772.ClientboundMoveMinecartAlongTrackStep, len(p.Steps))}
		for i, s := range p.Steps {
			o.Steps[i] = v772.ClientboundMoveMinecartAlongTrackStep{
				Position: v772.Vec3f{X: float32(s.Position.X), Y: float32(s.Position.Y), Z: float32(s.Position.Z)},
				Movement: v772.Vec3f{X: float32(s.Velocity.X), Y: float32(s.Velocity.Y), Z: float32(s.Velocity.Z)},
				Yaw:      float32(s.Yaw) * 360 / 256, Pitch: float32(s.Pitch) * 360 / 256, Weight: s.Weight,
			}
		}
		w := x.out()
		o.Encode(w)
		return x.emit("move_minecart_along_track", w)
	})
	// relative rotation has no older form; values are sent as absolute
	st.clientbound(packets.Play, "player_rotation", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundPlayerRotation
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v772.ClientboundPlayerRotation{Yaw: p.Yaw, Pitch: p.Pitch}
		w := x.out()
		o.Encode(w)
		return x.emit("player_rotation", w)
	})
	st.clientbound(packets.Play, "set_default_spawn_position", func(x *xctx, r *proto.Reader) error {
		var p v773.ClientboundSetDefaultSpawnPosition
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v772.ClientboundSetDefaultSpawnPosition{Location: p.GlobalPos.Location, Angle: p.Yaw}
		w := x.out()
		o.Encode(w)
		return x.emit("set_default_spawn_position", w)
	})
}

// velocity16 converts blocks/tick to the pre-1.21.9 1/8000 units.
func velocity16(v proto.LpVec3) v772.Vec3i16 {
	f := func(c float64) int16 {
		return int16(math.Round(max(-3.9, min(3.9, c)) * 8000))
	}
	return v772.Vec3i16{X: f(v.X), Y: f(v.Y), Z: f(v.Z)}
}

func skipSoundHolder(r *proto.Reader) error {
	id, err := r.VarInt()
	if err != nil || id != 0 {
		return err
	}
	if _, err := r.String(32767); err != nil {
		return err
	}
	ok, err := r.Bool()
	if err != nil || !ok {
		return err
	}
	_, err = r.F32()
	return err
}
