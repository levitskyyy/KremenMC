package net

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const velocityChannel = "velocity:player_info"

var (
	errBadName     = errors.New("invalid username")
	errVerifyToken = errors.New("verify token mismatch")
	errNotVerified = errors.New("failed to verify username")
)

func validName(s string) bool {
	if len(s) == 0 || len(s) > 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// OfflineUUID is the version 3 UUID vanilla assigns to "OfflinePlayer:<name>".
func OfflineUUID(name string) [16]byte {
	u := md5.Sum([]byte("OfflinePlayer:" + name))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return u
}

func (l *Listener) login(c *Conn) error {
	p, err := l.readOne(c)
	if err != nil {
		return err
	}
	hello, ok := p.(*packets.ServerboundLoginHello)
	if !ok {
		return errUnexpected
	}
	if !validName(hello.Username) {
		c.Disconnect(proto.Translatable("multiplayer.disconnect.invalid_player_data"))
		return errBadName
	}
	switch {
	case len(l.cfg.VelocitySecret) > 0:
		if err := l.velocity(c); err != nil {
			return err
		}
	case l.cfg.OnlineMode:
		if err := l.authenticate(c, hello.Username); err != nil {
			return err
		}
	default:
		c.profile = Profile{UUID: OfflineUUID(hello.Username), Name: hello.Username}
	}
	if t := l.cfg.CompressionThreshold; t >= 0 {
		c.Send(&packets.ClientboundLoginCompression{Threshold: int32(t)})
		c.control(item{ctl: ctlCompress})
		c.fr.SetCompression(t)
	}
	if reason := l.h.Login(c); reason != nil {
		c.Disconnect(*reason)
		return errors.New("login denied")
	}
	fin := &packets.ClientboundLoginFinished{UUID: c.profile.UUID, Username: c.profile.Name, Properties: c.profile.Properties, SessionID: l.sessionID}
	c.Send(fin)
	if c.codec != nil {
		if ns := c.State(); ns != packets.Login {
			c.inState = ns
			return nil
		}
	}
	for {
		p, err := l.readOne(c)
		if err != nil {
			return err
		}
		switch p := p.(type) {
		case *packets.ServerboundLoginAcknowledged:
			c.advance(p)
			c.setOutState(c.inState)
			return nil
		case *packets.ServerboundLoginCustomQueryAnswer, *packets.ServerboundLoginCookieResponse:
		default:
			return errUnexpected
		}
	}
}

func (l *Listener) authenticate(c *Conn, name string) error {
	var token [4]byte
	rand.Read(token[:])
	c.Send(&packets.ClientboundLoginHello{PublicKey: l.pubDER, VerifyToken: token[:], ShouldAuthenticate: true})
	p, err := l.readOne(c)
	if err != nil {
		return err
	}
	key, ok := p.(*packets.ServerboundLoginKey)
	if !ok {
		return errUnexpected
	}
	secret, err := rsa.DecryptPKCS1v15(nil, l.key, key.SharedSecret)
	if err != nil || len(secret) != 16 {
		return fmt.Errorf("shared secret: %w", err)
	}
	vt, err := rsa.DecryptPKCS1v15(nil, l.key, key.VerifyToken)
	if err != nil || !bytes.Equal(vt, token[:]) {
		return errVerifyToken
	}
	if err := c.fr.EnableEncryption(secret); err != nil {
		return err
	}
	c.control(item{ctl: ctlEncrypt, secret: secret})

	prof, err := l.hasJoined(name, serverHash(secret, l.pubDER), c.addr.Addr())
	if err != nil {
		c.Disconnect(proto.Translatable("multiplayer.disconnect.unverified_username"))
		return err
	}
	c.profile = prof
	return nil
}

// serverHash is Minecraft's signed hex SHA-1 of server ID, secret and key.
func serverHash(secret, pub []byte) string {
	h := sha1.New()
	h.Write(secret)
	h.Write(pub)
	sum := h.Sum(nil)
	n := new(big.Int).SetBytes(sum)
	if sum[0]&0x80 != 0 {
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(len(sum)*8)))
	}
	return n.Text(16)
}

func (l *Listener) hasJoined(name, hash string, ip netip.Addr) (Profile, error) {
	q := url.Values{"username": {name}, "serverId": {hash}}
	if l.cfg.PreventProxyConnections {
		q.Set("ip", ip.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.cfg.SessionServer+"/session/minecraft/hasJoined?"+q.Encode(), nil)
	if err != nil {
		return Profile{}, err
	}
	resp, err := l.cfg.HTTPClient.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("session server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Profile{}, errNotVerified
	}
	var body struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Properties []struct {
			Name      string `json:"name"`
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return Profile{}, fmt.Errorf("session server: %w", err)
	}
	raw, err := hex.DecodeString(body.ID)
	if err != nil || len(raw) != 16 || !validName(body.Name) {
		return Profile{}, errNotVerified
	}
	var p Profile
	copy(p.UUID[:], raw)
	p.Name = body.Name
	for _, pr := range body.Properties {
		gp := packets.GameProfileProperty{Name: pr.Name, Value: pr.Value}
		if pr.Signature != "" {
			gp.Signature = proto.Some(pr.Signature)
		}
		p.Properties = append(p.Properties, gp)
	}
	return p, nil
}

func (l *Listener) velocity(c *Conn) error {
	const msgID = 0x6D63
	c.Send(&packets.ClientboundLoginCustomQuery{MessageID: msgID, Channel: velocityChannel, Data: []byte{1}})
	for {
		p, err := l.readOne(c)
		if err != nil {
			return err
		}
		ans, ok := p.(*packets.ServerboundLoginCustomQueryAnswer)
		if !ok {
			return errUnexpected
		}
		if ans.MessageID != msgID {
			continue
		}
		if !ans.Data.Ok || len(ans.Data.V) < 32 {
			c.Disconnect(proto.Plain("This server requires you to connect with Velocity."))
			return errors.New("velocity: no forwarding data")
		}
		sig, payload := ans.Data.V[:32], ans.Data.V[32:]
		m := hmac.New(sha256.New, l.cfg.VelocitySecret)
		m.Write(payload)
		if !hmac.Equal(sig, m.Sum(nil)) {
			c.Disconnect(proto.Plain("Unable to verify player details."))
			return errors.New("velocity: bad signature")
		}
		return c.parseVelocity(payload)
	}
}

func (c *Conn) parseVelocity(b []byte) error {
	r := proto.Reader{B: b}
	if _, err := r.VarInt(); err != nil {
		return err
	}
	addr, err := r.String(255)
	if err != nil {
		return err
	}
	if a, err := netip.ParseAddr(addr); err == nil {
		c.addr = netip.AddrPortFrom(a.Unmap(), c.addr.Port())
	}
	if c.profile.UUID, err = r.UUID(); err != nil {
		return err
	}
	if c.profile.Name, err = r.String(16); err != nil {
		return err
	}
	n, err := r.Count(3)
	if err != nil {
		return err
	}
	for range n {
		var gp packets.GameProfileProperty
		if err := gp.Decode(&r); err != nil {
			return err
		}
		c.profile.Properties = append(c.profile.Properties, gp)
	}
	return nil
}
