package bridge

import (
	"context"
	"sync"
)

// PipeEnd is the server side of a routed connection: it receives what the
// player's Java pipeline lets through. Methods are called on the IPC read
// goroutine and must not block.
type PipeEnd interface {
	// PipeSend gets a clientbound body to send, or a marker passed to
	// Pipe.Outbound (body nil) once everything before it went through.
	PipeSend(body []byte, marker int64)
	// PipeRecv gets a serverbound body to process.
	PipeRecv(body []byte)
	// PipeDetach is called when the JVM goes away; routing must stop.
	PipeDetach()
}

// PipeRecord is one packet of a connection's history, replayed into a new
// pipeline so plugin handlers see the handshake, login and configuration.
type PipeRecord struct {
	Clientbound bool
	Body        []byte
}

// Pipe routes one player's packet bodies through the Java side's Netty
// channel. It satisfies net.Tap.
type Pipe struct {
	b   *Bridge
	c   *Conn
	id  UUID
	end PipeEnd
}

type pipes struct {
	mu sync.Mutex
	m  map[UUID]*Pipe
}

// PipelineActive reports whether a Java plugin hooked new connections (as
// PacketEvents does). Only then do players get a routed pipeline.
func (b *Bridge) PipelineActive() bool { return b.pipelineActive.Load() && b.conn.Load() != nil }

// OpenPipe creates the player's Java pipeline and replays hist through it.
// It returns nil when no plugin handler works on packet bytes, in which case
// nothing is routed.
func (b *Bridge) OpenPipe(ctx context.Context, id UUID, name, addr string, hist []PipeRecord, end PipeEnd) (*Pipe, error) {
	c := b.conn.Load()
	if c == nil {
		return nil, ErrClosed
	}
	recs := make([]any, len(hist))
	for i, r := range hist {
		recs[i] = []any{r.Clientbound, r.Body}
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.CallTimeout)
	defer cancel()
	res, err := c.Call(ctx, OpPipeOpen, id, name, addr, recs)
	if err != nil {
		return nil, err
	}
	if !ArgBool(res, 0) {
		return nil, nil
	}
	p := &Pipe{b: b, c: c, id: id, end: end}
	b.pipes.mu.Lock()
	if b.pipes.m == nil {
		b.pipes.m = map[UUID]*Pipe{}
	}
	old := b.pipes.m[id]
	b.pipes.m[id] = p
	b.pipes.mu.Unlock()
	if old != nil {
		old.end.PipeDetach()
	}
	return p, nil
}

func (p *Pipe) Outbound(body []byte, marker int64) {
	if body == nil { // a marker travels as nil, not as an empty body
		_ = p.c.NotifyBuffered(OpPipeOut, p.id, nil, marker)
		return
	}
	_ = p.c.NotifyBuffered(OpPipeOut, p.id, body, marker)
}

func (p *Pipe) Inbound(body []byte) { _ = p.c.NotifyBuffered(OpPipeIn, p.id, body) }

// Close tears down the Java pipeline once the connection ended.
func (p *Pipe) Close() {
	b := p.b
	b.pipes.mu.Lock()
	if b.pipes.m[p.id] == p {
		delete(b.pipes.m, p.id)
	}
	b.pipes.mu.Unlock()
	_ = p.c.Notify(OpPipeClose, p.id)
}

func (b *Bridge) pipe(id UUID) *Pipe {
	b.pipes.mu.Lock()
	p := b.pipes.m[id]
	b.pipes.mu.Unlock()
	return p
}

// detachPipes stops routing for every player of the lost connection c.
func (b *Bridge) detachPipes(c *Conn) {
	b.pipelineActive.Store(false)
	b.pipes.mu.Lock()
	var gone []*Pipe
	for id, p := range b.pipes.m {
		if p.c == c {
			gone = append(gone, p)
			delete(b.pipes.m, id)
		}
	}
	b.pipes.mu.Unlock()
	for _, p := range gone {
		p.end.PipeDetach()
	}
}

func (b *Bridge) onPipeSend(args []any) {
	id, ok := ArgUUID(args, 0)
	if !ok {
		return
	}
	body, _ := argBytes(args, 1)
	marker := ArgInt(args, 2)
	if p := b.pipe(id); p != nil {
		p.end.PipeSend(body, marker)
		return
	}
	if body != nil {
		b.host.SendRawPacket(id, body)
	}
}

func (b *Bridge) onPipeRecv(args []any) {
	id, ok := ArgUUID(args, 0)
	if !ok {
		return
	}
	body, ok := argBytes(args, 1)
	if !ok {
		return
	}
	if p := b.pipe(id); p != nil {
		p.end.PipeRecv(body)
	}
}

func argBytes(args []any, i int) ([]byte, bool) {
	if i >= len(args) {
		return nil, false
	}
	b, ok := args[i].([]byte)
	return b, ok
}
