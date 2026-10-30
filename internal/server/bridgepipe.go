package server

import (
	"context"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const pipeOpenTimeout = 5 * time.Second

// openPipe routes the player's packets through their Java Netty pipeline
// when a Java plugin hooked new connections (PacketEvents and the like).
// Runs on the connection goroutine before play starts.
func (s *Server) openPipe(p *Player) {
	hist := p.c.TakeHistory()
	b := s.javaBridge()
	if b == nil || !b.PipelineActive() {
		return
	}
	recs := make([]bridge.PipeRecord, 0, len(hist))
	for _, h := range hist {
		recs = append(recs, bridge.PipeRecord{Clientbound: h.Clientbound, Body: h.Body})
	}
	if len(recs) == 0 {
		recs = syntheticHistory(p.c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pipeOpenTimeout)
	defer cancel()
	pipe, err := b.OpenPipe(ctx, bridge.UUID(p.prof.UUID), p.prof.Name, p.c.Addr().String(), recs, p.c)
	if err != nil {
		s.log.Warn("java pipeline unavailable; packets are not routed", "player", p.prof.Name, "err", err)
		return
	}
	if pipe == nil {
		s.log.Debug("java pipeline has no packet hooks; packets are not routed", "player", p.prof.Name)
		return
	}
	s.log.Debug("routing packets through the java pipeline", "player", p.prof.Name, "history", len(recs))
	p.pipe = pipe
	p.c.SetTap(pipe)
}

// syntheticHistory stands in for a connection that was not recorded: the
// state transitions a pipeline needs to reach play.
func syntheticHistory(c *mcnet.Conn) []bridge.PipeRecord {
	prof := c.Profile()
	lf := &packets.ClientboundLoginFinished{UUID: prof.UUID, Username: prof.Name, Properties: prof.Properties}
	ps := []struct {
		cb bool
		p  packets.Packet
	}{
		{false, &packets.ServerboundHandshakeIntention{ProtocolVersion: packets.Protocol, ServerHost: c.Host(), ServerPort: c.Addr().Port(), NextState: 2}},
		{false, &packets.ServerboundLoginHello{Username: prof.Name, PlayerUUID: prof.UUID}},
		{true, lf},
		{false, &packets.ServerboundLoginAcknowledged{}},
		{true, &packets.ClientboundConfigFinishConfiguration{}},
		{false, &packets.ServerboundConfigFinishConfiguration{}},
	}
	out := make([]bridge.PipeRecord, len(ps))
	for i, x := range ps {
		out[i] = bridge.PipeRecord{Clientbound: x.cb, Body: encode(x.p)}
	}
	return out
}

// SendRawPacket sends a packet body that a Java plugin wrote to the
// pipeline of a player whose packets are not routed.
func (h BridgeHost) SendRawPacket(to bridge.UUID, body []byte) {
	if p, ok := h.s.Player(api.UUID(to)); ok {
		p.(*Player).c.SendRaw(body)
	}
}
