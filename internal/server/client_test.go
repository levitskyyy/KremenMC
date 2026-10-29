package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	stdnet "net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// fakeClient speaks protocol 777 with the generated packet structs.
type fakeClient struct {
	t     testing.TB
	name  string
	nc    stdnet.Conn
	fr    *proto.FrameReader
	fw    *proto.FrameWriter
	wmu   sync.Mutex
	state packets.State
	in    chan packets.Packet
	err   chan error

	eid       int32
	velocity  []byte
	profile   [16]byte
	registry  int
	tags      bool
	chunks    atomic.Int64
	joinStart time.Time
	view      int8
	joined    time.Duration
}

func startServer(t testing.TB, mod func(*config.ServerProperties)) *Server {
	t.Helper()
	return startServerWith(t, func(c *Config) {
		if mod != nil {
			mod(c.Props)
		}
	})
}

func startServerWith(t testing.TB, mod func(*Config)) *Server {
	t.Helper()
	dir := t.TempDir()
	props := config.DefaultServerProperties()
	props.OnlineMode = false
	props.LevelType = "minecraft:flat"
	props.ViewDistance = 4
	props.SimulationDistance = 4
	props.Gamemode = "creative"
	props.MaxPlayers = 1000
	cfg := Config{Dir: dir, Props: props, Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mod(&cfg)
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorld(s, WorldConfig{Name: "world", Dir: dir + "/world", Seed: 1, Flat: true})
	if err != nil {
		t.Fatal(err)
	}
	s.AddWorld(w)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Stop(api.Text("stop")); err != nil {
			t.Error(err)
		}
	})
	return s
}

func dial(t testing.TB, addr, name string) *fakeClient {
	t.Helper()
	nc, err := stdnet.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeClient{t: t, name: name, view: 4, nc: nc, fr: proto.NewFrameReader(nc), fw: proto.NewFrameWriter(nc), in: make(chan packets.Packet, 4096), err: make(chan error, 1)}
	t.Cleanup(func() { nc.Close() })
	return c
}

func (c *fakeClient) send(p packets.Packet) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	var w proto.Writer
	packets.Marshal(&w, p)
	if err := c.fw.WriteFrame(w.B); err != nil {
		c.t.Error(err)
		return
	}
	if err := c.fw.Flush(); err != nil {
		select {
		case c.err <- err:
		default:
		}
	}
}

// read decodes one frame synchronously (before play).
func (c *fakeClient) read() packets.Packet {
	c.t.Helper()
	c.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	frame, err := c.fr.ReadFrame()
	if err != nil {
		c.t.Fatalf("%s: read in state %d: %v", c.name, c.state, err)
	}
	p, err := packets.Unmarshal(c.state, packets.Clientbound, frame)
	if err != nil {
		c.t.Fatalf("%s: decode in state %d: %v", c.name, c.state, err)
	}
	return p
}

func hostPort(addr string) (string, uint16) {
	h, p, _ := stdnet.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return h, uint16(n)
}

// join runs handshake, login and configuration and starts the play reader.
func (c *fakeClient) join(addr string) {
	c.t.Helper()
	c.joinStart = time.Now()
	h, port := hostPort(addr)
	c.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: packets.Protocol, ServerHost: h, ServerPort: port, NextState: 2})
	c.state = packets.Login
	c.send(&packets.ServerboundLoginHello{Username: c.name, PlayerUUID: mcnet.OfflineUUID(c.name)})
login:
	for {
		switch p := c.read().(type) {
		case *packets.ClientboundLoginCompression:
			c.fr.SetCompression(int(p.Threshold))
			c.wmu.Lock()
			c.fw.SetCompression(int(p.Threshold), 6)
			c.wmu.Unlock()
		case *packets.ClientboundLoginHello:
			c.encrypt(p)
		case *packets.ClientboundLoginCustomQuery:
			c.forward(p)
		case *packets.ClientboundLoginFinished:
			want := mcnet.OfflineUUID(c.name)
			if c.profile != ([16]byte{}) {
				want = c.profile
			}
			if p.Username != c.name || p.UUID != want {
				c.t.Fatalf("login finished for %q", p.Username)
			}
			c.send(&packets.ServerboundLoginAcknowledged{})
			c.state = packets.Configuration
			break login
		case *packets.ClientboundLoginDisconnect:
			c.t.Fatalf("login disconnect: %s", p.Reason)
		default:
			c.t.Fatalf("unexpected login packet %T", p)
		}
	}
	c.send(&packets.ServerboundConfigClientInformation{Locale: "en_us", ViewDistance: c.view, ChatColors: true, SkinParts: 0x7f, MainHand: 1, EnableServerListing: true})
	for {
		switch p := c.read().(type) {
		case *packets.ClientboundConfigSelectKnownPacks:
			resp := &packets.ServerboundConfigSelectKnownPacks{}
			for _, k := range p.Packs {
				resp.Packs = append(resp.Packs, packets.ServerboundConfigSelectKnownPacksPack(k))
			}
			c.send(resp)
		case *packets.ClientboundConfigRegistryData:
			for _, e := range p.Entries {
				if e.Value.Ok {
					c.t.Fatalf("registry %s entry %s carries NBT", p.Registry, e.Key)
				}
			}
			c.registry++
		case *packets.ClientboundConfigUpdateTags:
			c.tags = true
		case *packets.ClientboundConfigKeepAlive:
			c.send(&packets.ServerboundConfigKeepAlive{KeepAliveID: p.KeepAliveID})
		case *packets.ClientboundConfigFinishConfiguration:
			c.send(&packets.ServerboundConfigFinishConfiguration{})
			c.state = packets.Play
			go c.reader()
			return
		case *packets.ClientboundConfigDisconnect:
			c.t.Fatalf("config disconnect: %s", p.Reason.String())
		}
	}
}

// reader decodes play packets and answers the protocol-level ones itself,
// like a vanilla client does.
func (c *fakeClient) reader() {
	defer close(c.in)
	for {
		c.nc.SetReadDeadline(time.Now().Add(30 * time.Second))
		frame, err := c.fr.ReadFrame()
		if err != nil {
			select {
			case c.err <- err:
			default:
			}
			return
		}
		p, err := packets.Unmarshal(packets.Play, packets.Clientbound, frame)
		if err != nil {
			select {
			case c.err <- fmt.Errorf("decode: %w", err):
			default:
			}
			return
		}
		switch m := p.(type) {
		case *packets.ClientboundKeepAlive:
			c.send(&packets.ServerboundKeepAlive{KeepAliveID: m.KeepAliveID})
		case *packets.ClientboundPlayerPosition:
			c.send(&packets.ServerboundAcceptTeleportation{TeleportID: m.TeleportID, X: m.X, Y: m.Y, Z: m.Z})
		case *packets.ClientboundChunkBatchFinished:
			c.send(&packets.ServerboundChunkBatchReceived{ChunksPerTick: 64})
		case *packets.ClientboundLevelChunkWithLight:
			c.chunks.Add(1)
			continue
		}
		c.in <- p
	}
}

// expect waits for the first packet matching fn, discarding others.
func expect[T packets.Packet](c *fakeClient, fn func(T) bool) T {
	c.t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case p, ok := <-c.in:
			if !ok {
				var err error
				select {
				case err = <-c.err:
				default:
				}
				c.t.Fatalf("%s: connection closed waiting for %T: %v", c.name, *new(T), err)
			}
			if m, ok := p.(T); ok && (fn == nil || fn(m)) {
				return m
			}
		case <-timeout:
			c.t.Fatalf("%s: timed out waiting for %T", c.name, *new(T))
		}
	}
}

// joinPlay waits until the client is in the world: Login, position and chunks.
func (c *fakeClient) joinPlay(addr string) *packets.ClientboundPlayerPosition {
	c.join(addr)
	login := expect[*packets.ClientboundLogin](c, nil)
	c.eid = login.EntityID
	pos := expect[*packets.ClientboundPlayerPosition](c, nil)
	expect[*packets.ClientboundChunkBatchFinished](c, nil)
	c.send(&packets.ServerboundPlayerLoaded{})
	c.joined = time.Since(c.joinStart)
	return pos
}

func (c *fakeClient) encrypt(p *packets.ClientboundLoginHello) {
	c.t.Helper()
	pub, err := x509.ParsePKIXPublicKey(p.PublicKey)
	if err != nil {
		c.t.Fatal(err)
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	es, err1 := rsa.EncryptPKCS1v15(rand.Reader, pub.(*rsa.PublicKey), secret)
	et, err2 := rsa.EncryptPKCS1v15(rand.Reader, pub.(*rsa.PublicKey), p.VerifyToken)
	if err1 != nil || err2 != nil {
		c.t.Fatal(err1, err2)
	}
	c.send(&packets.ServerboundLoginKey{SharedSecret: es, VerifyToken: et})
	c.wmu.Lock()
	c.fw.EnableEncryption(secret)
	c.wmu.Unlock()
	c.fr.EnableEncryption(secret)
}

func (c *fakeClient) forward(p *packets.ClientboundLoginCustomQuery) {
	if p.Channel != "velocity:player_info" || c.velocity == nil {
		c.send(&packets.ServerboundLoginCustomQueryAnswer{MessageID: p.MessageID})
		return
	}
	var w proto.Writer
	w.VarInt(1)
	w.String("203.0.113.7")
	w.UUID(c.profile)
	w.String(c.name)
	w.VarInt(0)
	m := hmac.New(sha256.New, c.velocity)
	m.Write(w.B)
	c.send(&packets.ServerboundLoginCustomQueryAnswer{MessageID: p.MessageID, Data: proto.Some(append(m.Sum(nil), w.B...))})
}
