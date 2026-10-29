package server

import (
	"errors"
	"math"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

var chunkPacketID = (&packets.ClientboundLevelChunkWithLight{}).ID()

// chunkPacket encodes Chunk Data lazily on the connection's writer goroutine.
type chunkPacket struct{ c *world.Chunk }

func (chunkPacket) ID() int32 { return chunkPacketID }

func (p chunkPacket) Encode(w *proto.Writer) {
	w.I32(p.c.X)
	w.I32(p.c.Z)
	world.EncodeChunkData(w, p.c)
}

func (chunkPacket) Decode(*proto.Reader) error { return errors.New("chunk packet is encode-only") }

// inView mirrors vanilla ChunkTrackingView: a circle of radius vd with a
// two-chunk buffer around the centre.
func inView(c world.ChunkPos, vd int, x, z int32) bool {
	dx := max(0, abs32(x-c.X)-2)
	dz := max(0, abs32(z-c.Z)-2)
	return int64(dx)*int64(dx)+int64(dz)*int64(dz) < int64(vd)*int64(vd)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (p *Player) viewDistance() int {
	vd := p.s.viewDist
	if cv := p.c.ClientInfo().ViewDistance; cv > 0 {
		vd = min(vd, cv)
	}
	return max(vd, 2)
}

func (p *Player) streamChunks(w *World) {
	p.mu.Lock()
	pos := p.e.Pos
	p.mu.Unlock()
	center := world.ChunkAt(int(math.Floor(pos.X)), int(math.Floor(pos.Z)))
	vd := p.viewDistance()
	if !p.hasCenter || center != p.center || vd != p.vd {
		if !p.hasCenter || center != p.center {
			p.send(&packets.ClientboundSetChunkCacheCenter{ChunkX: center.X, ChunkZ: center.Z})
		}
		switch {
		case p.ticket == 0:
			p.ticket = w.w.AddTicket(center, vd+1, world.TicketPlayer)
		case vd != p.vd:
			old := p.ticket
			p.ticket = w.w.AddTicket(center, vd+1, world.TicketPlayer)
			w.w.RemoveTicket(old)
		default:
			w.w.MoveTicket(p.ticket, center)
		}
		p.center, p.vd, p.hasCenter, p.complete = center, vd, true, false
		var drop []uint64
		p.sentMu.RLock()
		for k := range p.sent {
			cp := world.PosFromKey(k)
			if !inView(center, vd, cp.X, cp.Z) {
				drop = append(drop, k)
			}
		}
		p.sentMu.RUnlock()
		if len(drop) > 0 {
			p.sentMu.Lock()
			for _, k := range drop {
				delete(p.sent, k)
			}
			p.sentMu.Unlock()
			for _, k := range drop {
				cp := world.PosFromKey(k)
				p.send(&packets.ClientboundForgetLevelChunk{ChunkX: cp.X, ChunkZ: cp.Z})
			}
		}
	}
	if p.complete || p.unacked >= p.maxUnacked {
		return
	}
	p.quota = min(p.quota+p.cpt, max(1, p.cpt))
	if p.quota < 1 {
		return
	}
	budget := int(p.quota)
	var batch []*world.Chunk
	missing := false
	r := int32(vd + 1)
	visit := func(x, z int32) bool {
		if !inView(center, vd, x, z) {
			return true
		}
		cp := world.ChunkPos{X: x, Z: z}
		if p.hasChunk(cp.Key()) {
			return true
		}
		c := w.w.Chunk(cp)
		if c == nil {
			missing = true
			return true
		}
		batch = append(batch, c)
		return len(batch) < budget
	}
	spiral(center, r, visit)
	if len(batch) == 0 {
		if !missing {
			p.complete = true
		}
		return
	}
	p.sentMu.Lock()
	for _, c := range batch {
		p.sent[c.Pos().Key()] = struct{}{}
	}
	p.sentMu.Unlock()
	p.send(&packets.ClientboundChunkBatchStart{})
	for _, c := range batch {
		p.send(chunkPacket{c})
	}
	p.send(&packets.ClientboundChunkBatchFinished{BatchSize: int32(len(batch))})
	p.unacked++
	p.quota -= float32(len(batch))
}

// spiral visits chunks in rings of increasing distance until fn returns false.
func spiral(c world.ChunkPos, r int32, fn func(x, z int32) bool) {
	if !fn(c.X, c.Z) {
		return
	}
	for d := int32(1); d <= r; d++ {
		for i := -d; i <= d; i++ {
			if !fn(c.X+i, c.Z-d) || !fn(c.X+i, c.Z+d) {
				return
			}
		}
		for i := -d + 1; i <= d-1; i++ {
			if !fn(c.X-d, c.Z+i) || !fn(c.X+d, c.Z+i) {
				return
			}
		}
	}
}
