package net

import (
	"io"
	"log/slog"
	stdnet "net"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// loopTap stands in for the Java pipeline: it hands bodies back in order on
// its own goroutine, after rewrite had a chance to change, drop or add some.
type loopTap struct {
	c       *Conn
	q       chan func()
	rewrite func(clientbound bool, p packets.Packet) []packets.Packet
}

func newLoopTap(c *Conn, rewrite func(bool, packets.Packet) []packets.Packet) *loopTap {
	t := &loopTap{c: c, q: make(chan func(), 1024), rewrite: rewrite}
	go func() {
		for f := range t.q {
			f()
		}
	}()
	return t
}

func (t *loopTap) Outbound(body []byte, marker int64) {
	if body == nil {
		t.q <- func() { t.c.PipeSend(nil, marker) }
		return
	}
	p, err := packets.Unmarshal(packets.Play, packets.Clientbound, body)
	if err != nil {
		panic(err)
	}
	t.q <- func() {
		for _, q := range t.rewrite(true, p) {
			t.c.PipeSend(marshal(q), 0)
		}
	}
}

func (t *loopTap) Inbound(body []byte) {
	p, err := packets.Unmarshal(packets.Play, packets.Serverbound, body)
	if err != nil {
		panic(err)
	}
	t.q <- func() {
		for _, q := range t.rewrite(false, p) {
			t.c.PipeRecv(marshal(q))
		}
	}
}

func marshal(p packets.Packet) []byte {
	var w proto.Writer
	packets.Marshal(&w, p)
	return w.B
}

type tapSession struct {
	c    *Conn
	mu   sync.Mutex
	chat []string
}

func (s *tapSession) Handle(p packets.Packet) {
	m, ok := p.(*packets.ServerboundChat)
	if !ok {
		return
	}
	s.mu.Lock()
	s.chat = append(s.chat, m.Message)
	s.mu.Unlock()
	s.c.Send(&packets.ClientboundSystemChat{Content: proto.Plain("a")})
	s.c.Send(&packets.ClientboundSystemChat{Content: proto.Plain("b")})
	s.c.Disconnect(proto.Plain("bye"))
}

func (s *tapSession) Closed(error) {}

type tapHandler struct {
	mu   sync.Mutex
	hist []HistRecord
	sess *tapSession
	tap  func(*Conn) Tap
}

func (h *tapHandler) Status() StatusInfo      { return StatusInfo{} }
func (h *tapHandler) Login(*Conn) *proto.Text { return nil }

func (h *tapHandler) Join(c *Conn) (Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hist = c.TakeHistory()
	c.SetTap(h.tap(c))
	h.sess = &tapSession{c: c}
	return h.sess, nil
}

func text(p packets.Packet) string {
	if m, ok := p.(*packets.ClientboundSystemChat); ok {
		return m.Content.Text
	}
	return ""
}

func TestTapRoutesPlayPackets(t *testing.T) {
	rewrite := func(clientbound bool, p packets.Packet) []packets.Packet {
		switch m := p.(type) {
		case *packets.ServerboundChat:
			if m.Message == "ping" {
				cp := *m
				cp.Message = "pong"
				return []packets.Packet{&cp}
			}
		case *packets.ClientboundSystemChat:
			switch m.Content.Text {
			case "a":
				return nil
			case "b":
				return []packets.Packet{&packets.ClientboundSystemChat{Content: proto.Plain("injected")}, m}
			}
		}
		return []packets.Packet{p}
	}
	h := &tapHandler{}
	h.tap = func(c *Conn) Tap { return newLoopTap(c, rewrite) }
	l, err := NewListener(Config{
		Addr: "127.0.0.1:0", CompressionThreshold: -1,
		RecordHistory: func() bool { return true },
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	go l.Serve()
	defer l.Close()

	got := joinAndChat(t, l.Addr().String(), "ping")
	want := []string{"injected", "b", "disconnect:bye"}
	if !slices.Equal(got, want) {
		t.Fatalf("client saw %q, want %q", got, want)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sess.mu.Lock()
	chat := slices.Clone(h.sess.chat)
	h.sess.mu.Unlock()
	if !slices.Equal(chat, []string{"pong"}) {
		t.Fatalf("session got chat %q, want rewritten [pong]", chat)
	}
	tests := []struct {
		name        string
		clientbound bool
		state       packets.State
		id          int32
	}{
		{"handshake", false, packets.Handshake, (&packets.ServerboundHandshakeIntention{}).ID()},
		{"login finished", true, packets.Login, (&packets.ClientboundLoginFinished{}).ID()},
		{"login acknowledged", false, packets.Login, (&packets.ServerboundLoginAcknowledged{}).ID()},
		{"finish configuration", true, packets.Configuration, (&packets.ClientboundConfigFinishConfiguration{}).ID()},
		{"finish configuration ack", false, packets.Configuration, (&packets.ServerboundConfigFinishConfiguration{}).ID()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i := slices.IndexFunc(h.hist, func(r HistRecord) bool {
				return r.Clientbound == tc.clientbound && r.State == tc.state && len(r.Body) > 0 && int32(r.Body[0]) == tc.id
			})
			if i < 0 {
				t.Fatalf("history lacks %s", tc.name)
			}
		})
	}
	if hs, err := packets.Unmarshal(packets.Handshake, packets.Serverbound, h.hist[0].Body); err != nil || hs.(*packets.ServerboundHandshakeIntention).ProtocolVersion != packets.Protocol {
		t.Fatalf("first history record is not a canonical handshake: %v %v", hs, err)
	}
}

// joinAndChat logs in, sends one chat message and returns what the client
// saw in play until the server closed the connection.
func joinAndChat(t *testing.T, addr, msg string) []string {
	t.Helper()
	nc, err := stdnet.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(10 * time.Second))
	fr, fw := proto.NewFrameReader(nc), proto.NewFrameWriter(nc)
	send := func(p packets.Packet) {
		if err := fw.WriteFrame(marshal(p)); err != nil {
			t.Fatal(err)
		}
		if err := fw.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	state := packets.Login
	read := func() (packets.Packet, error) {
		frame, err := fr.ReadFrame()
		if err != nil {
			return nil, err
		}
		return packets.Unmarshal(state, packets.Clientbound, frame)
	}
	host, ps, _ := stdnet.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	send(&packets.ServerboundHandshakeIntention{ProtocolVersion: packets.Protocol, ServerHost: host, ServerPort: uint16(port), NextState: 2})
	send(&packets.ServerboundLoginHello{Username: "Tapped", PlayerUUID: OfflineUUID("Tapped")})
	for state != packets.Play {
		p, err := read()
		if err != nil {
			t.Fatalf("state %d: %v", state, err)
		}
		switch p := p.(type) {
		case *packets.ClientboundLoginFinished:
			send(&packets.ServerboundLoginAcknowledged{})
			state = packets.Configuration
		case *packets.ClientboundConfigSelectKnownPacks:
			resp := &packets.ServerboundConfigSelectKnownPacks{}
			for _, k := range p.Packs {
				resp.Packs = append(resp.Packs, packets.ServerboundConfigSelectKnownPacksPack(k))
			}
			send(resp)
		case *packets.ClientboundConfigFinishConfiguration:
			send(&packets.ServerboundConfigFinishConfiguration{})
			state = packets.Play
		}
	}
	send(&packets.ServerboundChat{Message: msg, Timestamp: time.Now().UnixMilli()})
	var seen []string
	for {
		p, err := read()
		if err != nil {
			return seen
		}
		switch p := p.(type) {
		case *packets.ClientboundSystemChat:
			seen = append(seen, text(p))
		case *packets.ClientboundDisconnect:
			seen = append(seen, "disconnect:"+p.Reason.Text)
		}
	}
}
