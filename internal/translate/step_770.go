package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/v770"
	"github.com/levitskyyy/KremenMC/internal/translate/v771"
)

// step770 converts between 1.21.6 (771) and 1.21.5 (770).
func step770(st *step) {
	const (
		cb = packets.Clientbound
		sb = packets.Serverbound
	)
	st.drop(packets.Configuration, cb, "clear_dialog", "show_dialog")
	st.drop(packets.Play, cb, "waypoint", "clear_dialog", "show_dialog")
	// minecraft-data lists two clientbound packets as serverbound for 1.21.5
	st.drop(packets.Configuration, sb, "custom_report_details", "server_links")
	// VarInt/u8 difficulty, heightmap and sound category enums, team and
	// recipe book settings schema representation, attribute names (remapped
	// before the chain)
	st.compatible(packets.Play, cb, "change_difficulty", "level_chunk_with_light", "recipe_book_settings",
		"set_player_team", "sound", "sound_entity", "update_attributes")
	st.compatible(packets.Play, sb, "change_difficulty")

	// 1.21.6 moved sneaking into player input
	st.serverbound(packets.Play, "player_command", func(x *xctx, r *proto.Reader) error {
		var p v770.ServerboundPlayerCommand
		if err := p.Decode(r); err != nil {
			return err
		}
		if p.ActionID < 2 {
			return nil
		}
		o := v771.ServerboundPlayerCommand{EntityID: p.EntityID, ActionID: p.ActionID - 2, JumpBoost: p.JumpBoost}
		w := x.out()
		o.Encode(w)
		return x.emit("player_command", w)
	})
}
