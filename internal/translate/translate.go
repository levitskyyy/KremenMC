package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// Codec converts between a client's wire protocol and canonical (packets.Protocol) packets.
// The native protocol never goes through a Codec.
type Codec interface {
	Protocol() int32
	Name() string
	// Decode turns one serverbound frame body (VarInt id + payload) into zero or more canonical packets.
	Decode(s packets.State, body []byte) ([]packets.Packet, error)
	// Encode emits zero or more client-version frame bodies (VarInt id + payload) for a canonical packet.
	// emit must not retain body after returning.
	Encode(s packets.State, p packets.Packet, emit func(body []byte) error) error
	// NextState reports the state the client expects after a canonical packet in state s was sent or received.
	// Codecs for protocols without a configuration phase map Configuration onto Play here.
	NextState(s packets.State, p packets.Packet, serverbound bool) packets.State
}

type Registry interface {
	Lookup(protocol int32) (Codec, bool)
	Supported() []int32
}
