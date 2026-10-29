package net

import (
	"errors"
	"fmt"
	"log/slog"
	stdnet "net"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

var (
	ErrSlowClient   = errors.New("net: send queue full")
	ErrKeepAlive    = errors.New("net: keepalive timeout")
	ErrPacketRate   = errors.New("net: packet rate exceeded")
	ErrClosed       = errors.New("net: connection closed")
	errUnexpected   = errors.New("net: unexpected packet")
	errBadKeepAlive = errors.New("net: bad keepalive id")
)

type ctl uint8

const (
	ctlNone ctl = iota
	ctlCompress
	ctlEncrypt
	ctlClose
)

type item struct {
	p      packets.Packet
	raw    []byte
	raws   [][]byte
	state  packets.State
	ctl    ctl
	secret []byte
}

// Profile is an authenticated game profile.
type Profile struct {
	UUID       [16]byte
	Name       string
	Properties []packets.GameProfileProperty
}

// ClientInfo is the last Client Information the client sent.
type ClientInfo struct {
	Locale       string
	ViewDistance int
	ChatMode     int32
	ChatColors   bool
	SkinParts    uint8
	MainHand     int32
	AllowListing bool
}

// Conn is one client connection. Send and the accessors are safe from any
// goroutine; packet reading happens on the connection's own goroutine.
type Conn struct {
	l     *Listener
	nc    stdnet.Conn
	addr  netip.AddrPort
	host  string
	codec translate.Codec
	proto int32
	log   *slog.Logger

	fr      *proto.FrameReader
	inState packets.State

	out      chan item
	outState atomic.Uint32
	done     chan struct{}
	closeErr error
	once     sync.Once
	closing  atomic.Bool
	writerWG sync.WaitGroup

	profile Profile
	brand   atomic.Pointer[string]
	info    atomic.Pointer[ClientInfo]

	kaID     atomic.Int64
	kaSent   atomic.Int64
	pingNS   atomic.Int64
	lastAck  atomic.Int64
	rateTick int64
	rateN    int
}

func newConn(l *Listener, nc stdnet.Conn) *Conn {
	c := &Conn{
		l:    l,
		nc:   nc,
		out:  make(chan item, l.cfg.SendQueue),
		done: make(chan struct{}),
		log:  l.log,
	}
	if ap, err := netip.ParseAddrPort(nc.RemoteAddr().String()); err == nil {
		c.addr = netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	c.lastAck.Store(time.Now().UnixNano())
	return c
}

func (c *Conn) Addr() netip.AddrPort { return c.addr }

// Host is the server address the client connected to (handshake), minus any forwarding data.
func (c *Conn) Host() string     { return c.host }
func (c *Conn) Protocol() int32  { return c.proto }
func (c *Conn) Profile() Profile { return c.profile }
func (c *Conn) Ping() time.Duration {
	return time.Duration(c.pingNS.Load())
}

func (c *Conn) Brand() string {
	if b := c.brand.Load(); b != nil {
		return *b
	}
	return ""
}

func (c *Conn) ClientInfo() ClientInfo {
	if i := c.info.Load(); i != nil {
		return *i
	}
	return ClientInfo{Locale: "en_us", ViewDistance: 10, ChatColors: true, SkinParts: 0x7f, MainHand: 1}
}

// Done is closed when the connection is closed.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection closed, once Done is closed.
func (c *Conn) Err() error {
	select {
	case <-c.done:
		return c.closeErr
	default:
		return nil
	}
}

func (c *Conn) State() packets.State { return packets.State(c.outState.Load()) }

func (c *Conn) setOutState(s packets.State) { c.outState.Store(uint32(s)) }

// Send queues a packet; the caller must not modify p afterwards. It returns
// false if the connection is closed or was dropped for a full queue.
func (c *Conn) Send(p packets.Packet) bool {
	s := c.State()
	if c.codec != nil {
		if ns := c.codec.NextState(s, p, false); ns != s {
			c.setOutState(ns)
		}
	}
	return c.enqueue(item{p: p, state: s})
}

// SendRaw queues a pre-encoded packet body (VarInt ID + payload) for the
// current state. raw is shared and must stay immutable.
func (c *Conn) SendRaw(raw []byte) bool {
	return c.enqueue(item{raw: raw, state: c.State()})
}

// SendRaws queues several pre-encoded bodies as one queue entry.
func (c *Conn) SendRaws(raws [][]byte) bool {
	if len(raws) == 0 {
		return true
	}
	return c.enqueue(item{raws: raws, state: c.State()})
}

func (c *Conn) enqueue(it item) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- it:
		return true
	case <-c.done:
		return false
	default:
		c.Close(ErrSlowClient)
		return false
	}
}

func (c *Conn) control(it item) {
	select {
	case c.out <- it:
	case <-c.done:
	}
}

// Disconnect sends the state-appropriate disconnect packet and closes the
// connection once it has been flushed.
func (c *Conn) Disconnect(reason proto.Text) {
	switch c.State() {
	case packets.Login:
		b, _ := reason.MarshalJSON()
		c.Send(&packets.ClientboundLoginDisconnect{Reason: string(b)})
	case packets.Configuration:
		c.Send(&packets.ClientboundConfigDisconnect{Reason: reason})
	case packets.Play:
		c.Send(&packets.ClientboundDisconnect{Reason: reason})
	}
	c.closing.Store(true)
	c.control(item{ctl: ctlClose})
	go func() {
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			c.Close(nil)
		}
	}()
}

// fail closes the connection unless a disconnect packet is still being flushed.
func (c *Conn) fail(err error) {
	if !c.closing.Load() {
		c.Close(err)
	}
}

// Close drops the connection without a disconnect packet.
func (c *Conn) Close(err error) {
	c.once.Do(func() {
		c.closeErr = err
		close(c.done)
		c.nc.Close()
	})
}

func (c *Conn) writer() {
	defer c.writerWG.Done()
	fw := proto.NewFrameWriter(c.nc)
	var w proto.Writer
	ka := time.NewTicker(c.l.cfg.KeepAliveInterval)
	defer ka.Stop()
	closing := false
	frames := 0
	frame := func(b []byte) error {
		frames++
		return fw.WriteFrame(b)
	}

	var write func(it item) error
	write = func(it item) error {
		switch it.ctl {
		case ctlCompress:
			fw.SetCompression(c.l.cfg.CompressionThreshold, c.l.cfg.CompressionLevel)
			return nil
		case ctlEncrypt:
			return fw.EnableEncryption(it.secret)
		case ctlClose:
			closing = true
			return nil
		}
		if it.raws != nil {
			for _, r := range it.raws {
				if err := write(item{raw: r, state: it.state}); err != nil {
					return err
				}
			}
			return nil
		}
		if c.codec == nil {
			if it.raw != nil {
				return frame(it.raw)
			}
			w.Reset()
			packets.Marshal(&w, it.p)
			return frame(w.B)
		}
		p := it.p
		if it.raw != nil || !canonical(it.state, p) {
			body := it.raw
			if body == nil {
				w.Reset()
				packets.Marshal(&w, p)
				body = w.B
			}
			var err error
			if p, err = packets.Unmarshal(it.state, packets.Clientbound, body); err != nil {
				return fmt.Errorf("translate: %w", err)
			}
		}
		return c.codec.Encode(it.state, p, frame)
	}

	flush := func() bool {
		n := fw.Buffered()
		if n == 0 {
			return true
		}
		c.nc.SetWriteDeadline(time.Now().Add(c.l.cfg.WriteTimeout))
		if err := fw.Flush(); err != nil {
			c.Close(fmt.Errorf("write: %w", err))
			return false
		}
		c.l.bytesOut.Add(uint64(n))
		c.l.packetsOut.Add(uint64(frames))
		frames = 0
		return true
	}

	for {
		select {
		case <-c.done:
			return
		case <-ka.C:
			if !c.keepAlive() {
				return
			}
			continue
		case it := <-c.out:
			if err := write(it); err != nil {
				c.Close(err)
				return
			}
		}
	drain:
		for n := 0; n < 512 && fw.Buffered() < 256<<10 && !closing; n++ {
			select {
			case it := <-c.out:
				if err := write(it); err != nil {
					c.Close(err)
					return
				}
			default:
				break drain
			}
		}
		if !flush() {
			return
		}
		if closing {
			c.Close(nil)
			return
		}
	}
}

// keepAlive runs on the writer goroutine.
func (c *Conn) keepAlive() bool {
	s := c.State()
	if s != packets.Configuration && s != packets.Play {
		return true
	}
	now := time.Now().UnixNano()
	if c.kaID.Load() != 0 {
		if time.Duration(now-c.kaSent.Load()) > c.l.cfg.KeepAliveTimeout {
			c.Close(ErrKeepAlive)
			return false
		}
		return true
	}
	c.kaSent.Store(now)
	c.kaID.Store(now)
	if s == packets.Configuration {
		return c.enqueue(item{p: &packets.ClientboundConfigKeepAlive{KeepAliveID: now}, state: s})
	}
	return c.enqueue(item{p: &packets.ClientboundKeepAlive{KeepAliveID: now}, state: s})
}

func (c *Conn) gotKeepAlive(id int64) error {
	want := c.kaID.Load()
	if want == 0 || id != want {
		return errBadKeepAlive
	}
	now := time.Now().UnixNano()
	ping := now - c.kaSent.Load()
	if old := c.pingNS.Load(); old != 0 {
		ping = (old*3 + ping) / 4
	}
	c.pingNS.Store(ping)
	c.kaID.Store(0)
	return nil
}

var typeCache sync.Map

// canonical reports whether p is the generated struct for its packet ID,
// as opposed to a custom Packet implementation that must be re-decoded
// before a codec can translate it.
func canonical(s packets.State, p packets.Packet) bool {
	t := reflect.TypeOf(p)
	key := [2]int64{int64(s), int64(p.ID())}
	if v, ok := typeCache.Load(key); ok {
		return v.(reflect.Type) == t
	}
	ref := packets.New(s, packets.Clientbound, p.ID())
	if ref == nil {
		return false
	}
	rt := reflect.TypeOf(ref)
	typeCache.Store(key, rt)
	return rt == t
}

// readPacket reads and decodes the next frame for the current inbound state.
// A translated frame may yield several canonical packets.
func (c *Conn) readPackets(dst []packets.Packet) ([]packets.Packet, error) {
	c.nc.SetReadDeadline(time.Now().Add(c.l.cfg.ReadTimeout))
	frame, err := c.fr.ReadFrame()
	if err != nil {
		return dst, err
	}
	c.l.packetsIn.Add(1)
	if lim := c.l.cfg.MaxPacketsPerSecond; lim > 0 {
		sec := time.Now().Unix()
		if sec != c.rateTick {
			c.rateTick, c.rateN = sec, 0
		}
		c.rateN++
		if c.rateN > lim {
			return dst, ErrPacketRate
		}
	}
	if c.codec != nil {
		ps, err := c.codec.Decode(c.inState, frame)
		if err != nil {
			return dst, fmt.Errorf("translate decode: %w", err)
		}
		return append(dst, ps...), nil
	}
	p, err := packets.Unmarshal(c.inState, packets.Serverbound, frame)
	if err != nil {
		return dst, err
	}
	return append(dst, p), nil
}

// advance applies the inbound state transition caused by p.
func (c *Conn) advance(p packets.Packet) {
	if c.codec != nil {
		c.inState = c.codec.NextState(c.inState, p, true)
		return
	}
	switch p.(type) {
	case *packets.ServerboundLoginAcknowledged:
		c.inState = packets.Configuration
	case *packets.ServerboundConfigFinishConfiguration:
		c.inState = packets.Play
	case *packets.ServerboundConfigurationAcknowledged:
		c.inState = packets.Configuration
	}
}
