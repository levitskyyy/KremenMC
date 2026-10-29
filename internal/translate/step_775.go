package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v775"
)

// step775 converts between 26.2 (776) and 26.1 (775).
func step775(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.compatible(packets.Play, cb, "open_book", "update_attributes")
	st.compatible(packets.Play, sb, "swing", "use_item", "use_item_on")

	st.clientbound(packets.Login, "login_finished", func(x *xctx, r *proto.Reader) error {
		b := r.B
		if len(b) < 16 {
			return errCount
		}
		w := x.out()
		w.Raw(b[:len(b)-16]) // session ID
		return x.emit("login_finished", w)
	})
	st.clientbound(packets.Play, "login", func(x *xctx, r *proto.Reader) error {
		b := r.B
		if len(b) < 2 {
			return errCount
		}
		w := x.out()
		w.Raw(b[:len(b)-2]) // online mode
		w.U8(b[len(b)-1])
		return x.emit("login", w)
	})
	// the 26.2 team layout is the canonical one
	st.clientbound(packets.Play, "set_player_team", func(x *xctx, r *proto.Reader) error {
		var p packets.ClientboundSetPlayerTeam
		if err := p.Decode(r); err != nil {
			return err
		}
		o := v775.ClientboundSetPlayerTeam{Team: p.Team, Mode: p.Mode, Players: p.Players}
		if p.Mode == 0 || p.Mode == 2 {
			t := &p.Parameters
			o.Name, o.Prefix, o.Suffix = t.Name, t.Prefix, t.Suffix
			o.Flags = v775.ClientboundSetPlayerTeamFlags(t.Flags)
			o.NameTagVisibility, o.CollisionRule = t.NameTagVisibility, t.CollisionRule
			o.Formatting = 21 // reset
			if t.Color.Ok {
				o.Formatting = t.Color.V
			}
		}
		w := x.out()
		o.Encode(w)
		return x.emit("set_player_team", w)
	})
}
