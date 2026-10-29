package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v766"
	"github.com/levitskyyy/KremenMC/internal/translate/v767"
)

// step766 converts between 1.21/1.21.1 (767) and 1.20.5/1.20.6 (766).
func step766(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Configuration, cb, "custom_report_details", "server_links")
	st.drop(packets.Play, cb, "custom_report_details", "server_links")
	// 1.21 changed projectile acceleration from a vector to a scalar
	st.drop(packets.Play, cb, "projectile_power")
	st.compatible(packets.Play, cb, "update_attributes")
	// signed and unsigned bytes of small values
	st.compatible(packets.Play, sb, "container_button_click", "place_recipe")

	st.clientbound(packets.Play, "disguised_chat", func(x *xctx, r *proto.Reader) error {
		if err := skipNBT(r); err != nil {
			return err
		}
		return x.chatType("disguised_chat", r)
	})
	st.clientbound(packets.Play, "player_chat", func(x *xctx, r *proto.Reader) error {
		if err := skipPlayerChatHead(r); err != nil {
			return err
		}
		return x.chatType("player_chat", r)
	})
	st.serverbound(packets.Play, "use_item", func(x *xctx, r *proto.Reader) error {
		var p v766.ServerboundUseItem
		if err := p.Decode(r); err != nil {
			return err
		}
		yaw, pitch := x.c.rotation()
		o := v767.ServerboundUseItem{Hand: p.Hand, Sequence: p.Sequence, Rotation: v767.Vec2f{X: yaw, Y: pitch}}
		w := x.out()
		o.Encode(w)
		return x.emit("use_item", w)
	})
}

// chatType rewrites the chat type holder at r into the plain registry ID
// used before 1.21; inline chat types cannot be expressed and are dropped.
func (x *xctx) chatType(name string, r *proto.Reader) error {
	head := r.B[:r.Off]
	h, err := r.VarInt()
	if err != nil || h == 0 {
		return err
	}
	w := x.out()
	w.Raw(head)
	w.VarInt(h - 1)
	w.Raw(r.B[r.Off:])
	return x.emit(name, w)
}

func skipNBT(r *proto.Reader) error {
	n, err := proto.NBTLen(r.B[r.Off:])
	if err != nil {
		return err
	}
	r.Off += n
	return nil
}

// skipPlayerChatHead advances r to the chat type of a 1.21 player_chat.
func skipPlayerChatHead(r *proto.Reader) error {
	if _, err := r.Raw(16); err != nil { // sender
		return err
	}
	if _, err := r.VarInt(); err != nil { // index
		return err
	}
	if ok, err := r.Bool(); err != nil {
		return err
	} else if ok {
		if _, err := r.Raw(256); err != nil {
			return err
		}
	}
	if _, err := r.String(256); err != nil {
		return err
	}
	if _, err := r.Raw(16); err != nil { // timestamp, salt
		return err
	}
	n, err := r.VarInt()
	if err != nil || n < 0 || int(n) > r.Remaining() {
		return errCount
	}
	for range n {
		id, err := r.VarInt()
		if err != nil {
			return err
		}
		if id == 0 {
			if _, err := r.Raw(256); err != nil {
				return err
			}
		}
	}
	if ok, err := r.Bool(); err != nil {
		return err
	} else if ok {
		if err := skipNBT(r); err != nil {
			return err
		}
	}
	filter, err := r.VarInt()
	if err != nil {
		return err
	}
	if filter == 2 {
		n, err := r.VarInt()
		if err != nil || n < 0 || int(n)*8 > r.Remaining() {
			return errCount
		}
		_, err = r.Raw(int(n) * 8)
		return err
	}
	return nil
}
