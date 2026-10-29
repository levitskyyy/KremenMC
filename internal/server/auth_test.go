package server

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func TestOnlineModeLogin(t *testing.T) {
	var gotHash string
	sess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/minecraft/hasJoined" || r.URL.Query().Get("username") != "Notch" {
			http.NotFound(w, r)
			return
		}
		gotHash = r.URL.Query().Get("serverId")
		fmt.Fprint(w, `{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch","properties":[{"name":"textures","value":"e30=","signature":"c2ln"}]}`)
	}))
	defer sess.Close()
	s := startServerWith(t, func(c *Config) {
		c.Props.OnlineMode = true
		c.SessionServer = sess.URL
	})
	c := dial(t, s.Addr(), "Notch")
	raw, _ := hex.DecodeString("069a79f444e94726a5befca90e38aaf5")
	copy(c.profile[:], raw)
	c.join(s.Addr())
	if gotHash == "" {
		t.Fatal("session server not queried")
	}
	info := expect[*packets.ClientboundPlayerInfoUpdate](c, func(p *packets.ClientboundPlayerInfoUpdate) bool {
		return p.Action&packets.ClientboundPlayerInfoUpdateActionAddPlayer != 0
	})
	if len(info.Data) != 1 || len(info.Data[0].Player.Properties) != 1 || !info.Data[0].Player.Properties[0].Signature.Ok {
		t.Fatalf("profile properties not forwarded: %+v", info.Data)
	}
}

func TestVelocityForwarding(t *testing.T) {
	secret := []byte("s3cret")
	s := startServerWith(t, func(c *Config) {
		c.Props.OnlineMode = true
		c.VelocitySecret = secret
	})
	c := dial(t, s.Addr(), "Proxied")
	c.velocity = secret
	c.profile = [16]byte{1, 2, 3, 4, 5, 6, 0x40, 8, 0x80, 10, 11, 12, 13, 14, 15, 16}
	c.joinPlay(s.Addr())
	p := mustPlayer(t, s, "Proxied")
	if p.Address().Addr().String() != "203.0.113.7" || p.UUID() != c.profile {
		t.Fatalf("forwarded data not applied: %v %v", p.Address(), p.UUID())
	}

	bad := dial(t, s.Addr(), "Spoofer")
	bad.velocity = []byte("wrong")
	h, port := hostPort(s.Addr())
	bad.send(&packets.ServerboundHandshakeIntention{ProtocolVersion: packets.Protocol, ServerHost: h, ServerPort: port, NextState: 2})
	bad.state = packets.Login
	bad.send(&packets.ServerboundLoginHello{Username: "Spoofer"})
	bad.forward(bad.read().(*packets.ClientboundLoginCustomQuery))
	if _, ok := bad.read().(*packets.ClientboundLoginDisconnect); !ok {
		t.Fatal("bad forwarding signature accepted")
	}
}
