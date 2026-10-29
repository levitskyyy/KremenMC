package translate

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/command"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
)

// registry looks up translating codecs. Lookup returns a new Codec per call
// because codecs keep per-connection state.
type registry struct{}

// Default returns the registry of every built-in protocol translation.
func Default() Registry { return registry{} }

func (registry) Lookup(protocol int32) (Codec, bool) {
	t, err := getTarget(protocol)
	if err != nil {
		return nil, false
	}
	return newConn(t), true
}

func (registry) Supported() []int32 {
	out := make([]int32, 0, len(specs)-1)
	for _, s := range specs[1:] {
		out = append(out, s.protocol)
	}
	return out
}

// ParserNames returns the command argument parser registry of a supported
// protocol in network ID order, for command.NewParserTable and
// (*command.Dispatcher).EncodeTreeWith.
func ParserNames(protocol int32) ([]api.ParserID, bool) {
	if protocol == packets.Protocol {
		return command.Parsers777, true
	}
	i := specIndex(protocol)
	if i < 0 {
		return nil, false
	}
	m, err := meta.Load(protocol)
	if err != nil {
		return nil, false
	}
	l := m.Enums[meta.Parser]
	out := make([]api.ParserID, len(l))
	for j, n := range l {
		out[j] = api.ParserID(n)
	}
	return out, true
}
