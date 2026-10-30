package net

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// ErrTapOverflow closes a connection whose routed serverbound packets pile up.
var ErrTapOverflow = errors.New("net: routed packet queue full")

// A Tap routes a play connection's packet bodies (VarInt ID + payload in
// protocol 777, after decryption and decompression) through an external
// pipeline, the Java bridge's Netty channel. The pipeline hands bodies back
// through Conn.PipeSend and Conn.PipeRecv, possibly modified, dropped or
// joined by new ones. Both methods must not block for long and must not
// retain body after returning.
type Tap interface {
	// Outbound passes a clientbound body, or a marker (body nil) that the
	// pipeline returns in order with the packets around it.
	Outbound(body []byte, marker int64)
	// Inbound passes a serverbound body.
	Inbound(body []byte)
}

type tapState struct {
	t       Tap
	in      chan []byte
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]item
}

// HistRecord is one packet of a connection's handshake, login and
// configuration, in canonical form.
type HistRecord struct {
	Clientbound bool
	State       packets.State
	Body        []byte
}

type history struct {
	mu   sync.Mutex
	recs []HistRecord
	off  bool
}

func (h *history) add(clientbound bool, s packets.State, body []byte) {
	h.mu.Lock()
	if !h.off {
		h.recs = append(h.recs, HistRecord{Clientbound: clientbound, State: s, Body: append([]byte(nil), body...)})
	}
	h.mu.Unlock()
}

// TakeHistory returns the packets recorded before play and stops recording.
// It is empty unless Config.RecordHistory asked for it at handshake.
func (c *Conn) TakeHistory() []HistRecord {
	h := c.hist.Load()
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.off = true
	r := h.recs
	h.recs = nil
	return r
}

func (c *Conn) recordIn(p packets.Packet) {
	h := c.hist.Load()
	if h == nil || c.inState == packets.Play {
		return
	}
	if hs, ok := p.(*packets.ServerboundHandshakeIntention); ok && hs.ProtocolVersion != packets.Protocol {
		cp := *hs
		cp.ProtocolVersion = packets.Protocol
		p = &cp
	}
	var w proto.Writer
	packets.Marshal(&w, p)
	h.add(false, c.inState, w.B)
}

// SetTap routes the connection's play packets through t. Call it from
// Handler.Join, before the play session starts.
func (c *Conn) SetTap(t Tap) {
	c.tap.Store(&tapState{t: t, in: make(chan []byte, 4096), pending: map[int64]item{}})
}

// PipeSend queues a clientbound body that left the pipeline, or applies the
// deferred action of a returned marker. It never blocks.
func (c *Conn) PipeSend(body []byte, marker int64) {
	if marker != 0 {
		ts := c.tap.Load()
		if ts == nil {
			return
		}
		ts.mu.Lock()
		it, ok := ts.pending[marker]
		delete(ts.pending, marker)
		ts.mu.Unlock()
		if ok {
			it.direct = true
			c.enqueue(it)
		}
		return
	}
	if body != nil {
		c.enqueue(item{raw: body, state: c.State(), direct: true})
	}
}

// PipeRecv queues a serverbound body that left the pipeline for the play
// session. It never blocks; a full queue closes the connection.
func (c *Conn) PipeRecv(body []byte) {
	ts := c.tap.Load()
	if ts == nil {
		return
	}
	select {
	case ts.in <- body:
	default:
		c.Close(ErrTapOverflow)
	}
}

// PipeDetach stops routing, for example because the pipeline went away.
// Deferred actions still waiting for their marker run now.
func (c *Conn) PipeDetach() {
	ts := c.tap.Load()
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.t = nil
	its := make([]item, 0, len(ts.pending))
	for id, it := range ts.pending {
		it.direct = true
		its = append(its, it)
		delete(ts.pending, id)
	}
	ts.mu.Unlock()
	for _, it := range its {
		c.enqueue(it)
	}
}

func (ts *tapState) tap() Tap {
	ts.mu.Lock()
	t := ts.t
	ts.mu.Unlock()
	return t
}

// tapOut routes a writer item through the tap; it reports false when the
// item must be written directly. A control item waits, under a marker, for
// the packets queued before it to come back.
func (c *Conn) tapOut(it item, w *proto.Writer) bool {
	ts := c.tap.Load()
	if ts == nil || it.direct || it.ctl == ctlNone && it.state != packets.Play {
		return false
	}
	if it.ctl != ctlNone {
		ts.mu.Lock()
		t := ts.t
		var id int64
		if t != nil {
			id = ts.nextID.Add(1)
			ts.pending[id] = it
		}
		ts.mu.Unlock()
		if t == nil {
			return false
		}
		t.Outbound(nil, id)
		return true
	}
	t := ts.tap()
	if t == nil {
		return false
	}
	switch {
	case it.raws != nil:
		for _, r := range it.raws {
			t.Outbound(r, 0)
		}
	case it.raw != nil:
		t.Outbound(it.raw, 0)
	default:
		w.Reset()
		packets.Marshal(w, it.p)
		t.Outbound(w.B, 0)
	}
	return true
}

// pump reads play frames and passes them to the tap, or straight to the
// session queue once the tap is detached.
func (c *Conn) pump(ts *tapState) {
	var buf []packets.Packet
	var w proto.Writer
	for {
		var err error
		buf, err = c.readPackets(buf[:0])
		if err != nil {
			c.Close(err)
			return
		}
		for _, p := range buf {
			w.Reset()
			packets.Marshal(&w, p)
			if t := ts.tap(); t != nil {
				t.Inbound(w.B)
				continue
			}
			select {
			case ts.in <- append([]byte(nil), w.B...):
			case <-c.done:
				return
			}
		}
		clear(buf)
	}
}

// playTapped runs the play session on bodies coming back from the pipeline.
func (l *Listener) playTapped(c *Conn, sess Session, ts *tapState) {
	go c.pump(ts)
	for {
		select {
		case <-c.done:
			return
		case body := <-ts.in:
			p, err := packets.Unmarshal(packets.Play, packets.Serverbound, body)
			if err != nil {
				c.log.Debug("dropping malformed routed packet", "addr", c.addr, "err", err)
				continue
			}
			if err := l.play(c, sess, p); err != nil {
				c.Close(err)
				return
			}
		}
	}
}
