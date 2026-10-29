package translate

import "github.com/levitskyyy/KremenMC/internal/proto/packets"

// step773 converts between 1.21.11 (774) and 1.21.9/1.21.10 (773). Their
// packet layouts match; registries, components and metadata differ and are
// remapped before the chain.
func step773(st *step) {
	// u8/i8 signature count, i8/VarInt button: same bytes for valid values
	st.compatible(packets.Play, packets.Serverbound, "chat_command_signed", "container_button_click")
}
