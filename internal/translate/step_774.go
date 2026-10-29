package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v774"
	"github.com/levitskyyy/KremenMC/internal/translate/v775"
)

// step774 converts between 26.1 (775) and 1.21.11 (774). The chunk section
// fluid count is handled by the chunk format of the spec.
func step774(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Play, cb, "game_rule_values", "low_disk_space_warning")
	// item displays are dropped before the chain (see rewriteCB)
	st.drop(packets.Play, cb, "update_advancements")
	st.compatible(packets.Play, cb, "open_book")
	st.compatible(packets.Play, sb, "chat_command_signed", "client_command", "swing", "use_item", "use_item_on")

	st.clientbound(packets.Play, "set_time", func(x *xctx, r *proto.Reader) error {
		var p v775.ClientboundSetTime
		if err := p.Decode(r); err != nil {
			return err
		}
		c := x.c
		for _, u := range p.ClockUpdates {
			if name, ok := c.regName("world_clock", u.ID); !ok || name == "overworld" {
				c.dayTime, c.ticking = u.TotalTicks, u.Rate != 0
				break
			}
		}
		o := v774.ClientboundSetTime{Age: p.Age, Time: c.dayTime, TickDayTime: c.ticking}
		w := x.out()
		o.Encode(w)
		return x.emit("set_time", w)
	})

	st.serverbound(packets.Play, "interact", func(x *xctx, r *proto.Reader) error {
		var p v774.ServerboundInteract
		if err := p.Decode(r); err != nil {
			return err
		}
		w := x.out()
		switch p.Mouse {
		case 1:
			o := v775.ServerboundAttack{EntityID: p.Target}
			o.Encode(w)
			return x.emit("attack", w)
		case 2:
			// interact_at precedes a plain interact for the same click; the
			// newer single interact carries its location
			o := v775.ServerboundInteract{
				Target: p.Target, Hand: p.Hand, Sneaking: p.Sneaking,
				Location: proto.LpVec3{X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)},
			}
			o.Encode(w)
			return x.emit("interact", w)
		}
		return nil
	})
	// minecraft-data has an i8 button for 1.21.11 only; a VarInt reads both
	st.serverbound(packets.Play, "container_button_click", func(x *xctx, r *proto.Reader) error {
		d := rd{r: r}
		o := v775.ServerboundContainerButtonClick{WindowID: d.varint(), Enchantment: d.varint()}
		if d.err != nil {
			return d.err
		}
		w := x.out()
		o.Encode(w)
		return x.emit("container_button_click", w)
	})
}
