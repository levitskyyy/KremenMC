// Package net accepts client connections and drives them through the
// handshake, status, login and configuration states, then hands them to the
// server in the play state.
package net

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdnet "net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

type Config struct {
	Addr                    string
	OnlineMode              bool
	PreventProxyConnections bool
	// CompressionThreshold < 0 disables compression.
	CompressionThreshold int
	CompressionLevel     int
	// VelocitySecret enables Velocity modern forwarding when non-empty.
	VelocitySecret  []byte
	AcceptTransfers bool
	// ConnThrottle is the minimum interval between logins from one IP
	// (loopback exempt); 0 disables. Status pings are not throttled.
	ConnThrottle time.Duration
	// MaxPacketsPerSecond kicks a client sending more; 0 means 1000.
	MaxPacketsPerSecond int
	SendQueue           int
	LoginTimeout        time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	KeepAliveInterval   time.Duration
	KeepAliveTimeout    time.Duration
	Brand               string
	SessionServer       string
	HTTPClient          *http.Client
	Translations        translate.Registry
	Logger              *slog.Logger
}

func (c *Config) defaults() {
	if c.CompressionLevel == 0 {
		c.CompressionLevel = 4
	}
	if c.SendQueue <= 0 {
		c.SendQueue = 8192
	}
	if c.MaxPacketsPerSecond == 0 {
		c.MaxPacketsPerSecond = 1000
	}
	if c.LoginTimeout <= 0 {
		c.LoginTimeout = 30 * time.Second
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 15 * time.Second
	}
	if c.KeepAliveInterval <= 0 {
		c.KeepAliveInterval = 15 * time.Second
	}
	if c.KeepAliveTimeout <= 0 {
		c.KeepAliveTimeout = 30 * time.Second
	}
	if c.Brand == "" {
		c.Brand = "kremenmc"
	}
	if c.SessionServer == "" {
		c.SessionServer = "https://sessionserver.mojang.com"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// StatusInfo is what the server list shows.
type StatusInfo struct {
	Description proto.Text
	MaxPlayers  int
	Online      int
	Sample      []Profile
	// Favicon is a data:image/png;base64 URL or empty.
	Favicon string
}

// Handler is implemented by the server.
type Handler interface {
	Status() StatusInfo
	// Login runs on the connection goroutine after authentication and may
	// block (async pre-login events). A non-nil result denies the login.
	Login(c *Conn) *proto.Text
	// Join is called once the client entered the play state. The returned
	// session receives every play packet on the connection goroutine.
	Join(c *Conn) (Session, error)
}

type Session interface {
	Handle(p packets.Packet)
	// Closed is called exactly once after the connection ended.
	Closed(err error)
}

type Listener struct {
	cfg       Config
	h         Handler
	log       *slog.Logger
	key       *rsa.PrivateKey
	pubDER    []byte
	sessionID [16]byte

	ln stdnet.Listener

	throttleMu sync.Mutex
	throttle   map[netip.Addr]time.Time

	connsMu sync.Mutex
	conns   map[*Conn]struct{}
	wg      sync.WaitGroup
	closed  chan struct{}

	bytesIn, bytesOut     atomic.Uint64
	packetsIn, packetsOut atomic.Uint64
}

// Traffic is cumulative wire traffic over all connections since start.
// Bytes are counted after compression and encryption.
type Traffic struct {
	BytesIn, BytesOut     uint64
	PacketsIn, PacketsOut uint64
}

func (l *Listener) Traffic() Traffic {
	return Traffic{
		BytesIn: l.bytesIn.Load(), BytesOut: l.bytesOut.Load(),
		PacketsIn: l.packetsIn.Load(), PacketsOut: l.packetsOut.Load(),
	}
}

func NewListener(cfg Config, h Handler) (*Listener, error) {
	cfg.defaults()
	l := &Listener{cfg: cfg, h: h, log: cfg.Logger.With("component", "net"), throttle: map[netip.Addr]time.Time{}, conns: map[*Conn]struct{}{}, closed: make(chan struct{})}
	if cfg.OnlineMode && len(cfg.VelocitySecret) == 0 {
		key, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			return nil, fmt.Errorf("net: rsa key: %w", err)
		}
		der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("net: rsa key: %w", err)
		}
		l.key, l.pubDER = key, der
	}
	rand.Read(l.sessionID[:])
	l.sessionID[6] = l.sessionID[6]&0x0f | 0x40
	l.sessionID[8] = l.sessionID[8]&0x3f | 0x80
	return l, nil
}

// Listen binds the configured address with SO_REUSEADDR.
func (l *Listener) Listen() error {
	lc := stdnet.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
		var serr error
		err := rc.Control(func(fd uintptr) { serr = setReuseAddr(fd) })
		if err != nil {
			return err
		}
		return serr
	}}
	ln, err := lc.Listen(context.Background(), "tcp", l.cfg.Addr)
	if err != nil {
		return fmt.Errorf("net: listen %s: %w", l.cfg.Addr, err)
	}
	l.ln = ln
	return nil
}

func (l *Listener) Addr() stdnet.Addr { return l.ln.Addr() }

// Serve accepts connections until Close.
func (l *Listener) Serve() error {
	var delay time.Duration
	for {
		nc, err := l.ln.Accept()
		if err != nil {
			select {
			case <-l.closed:
				return nil
			default:
			}
			var ne stdnet.Error
			if errors.As(err, &ne) && ne.Timeout() {
				delay = min(max(delay*2, 5*time.Millisecond), time.Second)
				time.Sleep(delay)
				continue
			}
			return fmt.Errorf("net: accept: %w", err)
		}
		delay = 0
		if tc, ok := nc.(*stdnet.TCPConn); ok {
			tc.SetNoDelay(true)
			tc.SetKeepAlive(true)
		}
		c := newConn(l, nc)
		l.connsMu.Lock()
		l.conns[c] = struct{}{}
		l.connsMu.Unlock()
		l.wg.Add(1)
		go l.handle(c)
	}
}

func (l *Listener) allow(a netip.Addr) bool {
	if l.cfg.ConnThrottle <= 0 || a.IsLoopback() {
		return true
	}
	now := time.Now()
	l.throttleMu.Lock()
	defer l.throttleMu.Unlock()
	if last, ok := l.throttle[a]; ok && now.Sub(last) < l.cfg.ConnThrottle {
		l.throttle[a] = now
		return false
	}
	l.throttle[a] = now
	if len(l.throttle) > 4096 {
		for k, t := range l.throttle {
			if now.Sub(t) > l.cfg.ConnThrottle {
				delete(l.throttle, k)
			}
		}
	}
	return true
}

// Close stops accepting and drops every connection.
func (l *Listener) Close() error {
	select {
	case <-l.closed:
		return nil
	default:
	}
	close(l.closed)
	var err error
	if l.ln != nil {
		err = l.ln.Close()
	}
	l.connsMu.Lock()
	for c := range l.conns {
		c.Close(ErrClosed)
	}
	l.connsMu.Unlock()
	l.wg.Wait()
	return err
}

// Conns returns a snapshot of all open connections.
func (l *Listener) Conns() []*Conn {
	l.connsMu.Lock()
	defer l.connsMu.Unlock()
	out := make([]*Conn, 0, len(l.conns))
	for c := range l.conns {
		out = append(out, c)
	}
	return out
}

// prefixReader replays the sniffed first byte and counts inbound bytes.
type prefixReader struct {
	b  byte
	ok bool
	r  io.Reader
	n  *atomic.Uint64
}

func (p *prefixReader) Read(b []byte) (int, error) {
	if p.ok && len(b) > 0 {
		p.ok = false
		b[0] = p.b
		p.n.Add(1)
		return 1, nil
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.n.Add(uint64(n))
	}
	return n, err
}

func (l *Listener) handle(c *Conn) {
	var sess Session
	defer func() {
		if v := recover(); v != nil {
			l.log.Error("connection panic", "addr", c.addr, "panic", v, "stack", string(debug.Stack()))
			c.Close(fmt.Errorf("panic: %v", v))
		}
		if c.closing.Load() {
			<-c.done
		}
		c.Close(nil)
		c.writerWG.Wait()
		if sess != nil {
			sess.Closed(c.closeErr)
		}
		l.connsMu.Lock()
		delete(l.conns, c)
		l.connsMu.Unlock()
		l.wg.Done()
	}()

	c.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	var first [1]byte
	if _, err := io.ReadFull(c.nc, first[:]); err != nil {
		return
	}
	if first[0] == 0xFE {
		l.legacyPing(c)
		return
	}
	c.fr = proto.NewFrameReader(&prefixReader{b: first[0], ok: true, r: c.nc, n: &l.bytesIn})
	c.writerWG.Add(1)
	go c.writer()

	sess = l.serve(c)
	if sess == nil {
		return
	}
	var buf []packets.Packet
	var err error
	for {
		buf, err = c.readPackets(buf[:0])
		if err != nil {
			c.Close(err)
			return
		}
		for _, p := range buf {
			if err = l.play(c, sess, p); err != nil {
				c.Close(err)
				return
			}
		}
		clear(buf)
	}
}

func (l *Listener) play(c *Conn, s Session, p packets.Packet) error {
	switch p := p.(type) {
	case *packets.ServerboundKeepAlive:
		return c.gotKeepAlive(p.KeepAliveID)
	case *packets.ServerboundConfigurationAcknowledged:
		return errUnexpected
	case *packets.ServerboundClientInformation:
		c.setInfo(p.Locale, p.ViewDistance, p.ChatFlags, p.ChatColors, p.SkinParts, p.MainHand, p.EnableServerListing)
	case *packets.ServerboundCustomPayload:
		c.customPayload(p.Channel, p.Data)
	}
	s.Handle(p)
	return nil
}
