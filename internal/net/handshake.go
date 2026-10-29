package net

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

func (l *Listener) readOne(c *Conn) (packets.Packet, error) {
	for {
		ps, err := c.readPackets(nil)
		if err != nil {
			return nil, err
		}
		if len(ps) == 0 {
			continue
		}
		if len(ps) > 1 {
			return nil, fmt.Errorf("%w: %d packets in one frame before play", errUnexpected, len(ps))
		}
		return ps[0], nil
	}
}

// serve runs handshake through configuration and returns the play session,
// or nil when the connection is done.
func (l *Listener) serve(c *Conn) Session {
	c.inState = packets.Handshake
	p, err := l.readOne(c)
	if err != nil {
		return nil
	}
	hs, ok := p.(*packets.ServerboundHandshakeIntention)
	if !ok {
		return nil
	}
	c.proto = hs.ProtocolVersion
	c.host = hs.ServerHost
	if i := strings.IndexByte(c.host, 0); i >= 0 {
		c.host = c.host[:i]
	}
	c.host = strings.TrimSuffix(c.host, ".")
	switch hs.NextState {
	case 1:
		c.inState = packets.Status
		c.setOutState(packets.Status)
		l.status(c)
		return nil
	case 2, 3:
		c.inState = packets.Login
		c.setOutState(packets.Login)
		if !l.allow(c.addr.Addr()) {
			c.Disconnect(proto.Plain("Connection throttled! Please wait before reconnecting."))
			return nil
		}
	default:
		return nil
	}
	timer := time.AfterFunc(l.cfg.LoginTimeout, func() { c.Close(fmt.Errorf("net: login timed out")) })
	defer timer.Stop()

	if hs.NextState == 3 && !l.cfg.AcceptTransfers {
		c.Disconnect(proto.Translatable("multiplayer.disconnect.transfers_disabled"))
		return nil
	}
	if c.proto != packets.Protocol {
		codec, ok := l.lookupCodec(c.proto)
		if !ok {
			key := "multiplayer.disconnect.outdated_client"
			if c.proto > packets.Protocol {
				key = "multiplayer.disconnect.incompatible"
			}
			c.Disconnect(proto.Translatable(key, proto.Plain(packets.Version)))
			return nil
		}
		c.codec = codec
	}
	if err := l.login(c); err != nil {
		if c.Err() == nil {
			l.log.Debug("login failed", "addr", c.addr, "err", err)
		}
		c.fail(err)
		return nil
	}
	if err := l.configure(c); err != nil {
		if c.Err() == nil {
			l.log.Debug("configuration failed", "addr", c.addr, "player", c.profile.Name, "err", err)
		}
		c.fail(err)
		return nil
	}
	timer.Stop()
	sess, err := l.h.Join(c)
	if err != nil {
		c.Disconnect(proto.Plain(err.Error()))
		return nil
	}
	return sess
}

func (l *Listener) lookupCodec(protocol int32) (translate.Codec, bool) {
	if l.cfg.Translations == nil {
		return nil, false
	}
	cd, ok := l.cfg.Translations.Lookup(protocol)
	if !ok || cd == nil {
		return nil, false
	}
	return cd, true
}

type statusJSON struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int32  `json:"protocol"`
	} `json:"version"`
	Players *struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"sample,omitempty"`
	} `json:"players,omitempty"`
	Description proto.Text `json:"description"`
	Favicon     string     `json:"favicon,omitempty"`
	Secure      bool       `json:"enforcesSecureChat"`
}

func (l *Listener) statusResponse(protocol int32) string {
	info := l.h.Status()
	var s statusJSON
	s.Version.Name, s.Version.Protocol = packets.Version, packets.Protocol
	if protocol != packets.Protocol && l.cfg.Translations != nil {
		if cd, ok := l.cfg.Translations.Lookup(protocol); ok && cd != nil {
			s.Version.Name, s.Version.Protocol = cd.Name(), protocol
		}
	}
	s.Players = &struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"sample,omitempty"`
	}{Max: info.MaxPlayers, Online: info.Online}
	for _, p := range info.Sample {
		s.Players.Sample = append(s.Players.Sample, struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		}{p.Name, uuidString(p.UUID)})
	}
	s.Description = info.Description
	s.Favicon = info.Favicon
	b, _ := json.Marshal(&s)
	return string(b)
}

func (l *Listener) status(c *Conn) {
	for range 2 {
		p, err := l.readOne(c)
		if err != nil {
			return
		}
		switch p := p.(type) {
		case *packets.ServerboundStatusRequest:
			c.Send(&packets.ClientboundStatusResponse{Response: l.statusResponse(c.proto)})
		case *packets.ServerboundStatusPingRequest:
			c.Send(&packets.ClientboundStatusPongResponse{Time: p.Time})
			c.control(item{ctl: ctlClose})
			<-c.done
			return
		}
	}
}

// legacyPing answers the pre-Netty (1.4-1.6) server list ping.
func (l *Listener) legacyPing(c *Conn) {
	c.nc.SetReadDeadline(time.Now().Add(time.Second))
	var buf [256]byte
	n, _ := c.nc.Read(buf[:])
	info := l.h.Status()
	motd := info.Description.String()
	var s string
	if n > 0 && buf[0] == 0x01 {
		s = fmt.Sprintf("§1\x00127\x00%s\x00%s\x00%d\x00%d", packets.Version, motd, info.Online, info.MaxPlayers)
	} else {
		s = fmt.Sprintf("%s§%d§%d", strings.ReplaceAll(motd, "§", ""), info.Online, info.MaxPlayers)
	}
	units := utf16Encode(s)
	out := make([]byte, 0, 3+2*len(units))
	out = append(out, 0xFF, byte(len(units)>>8), byte(len(units)))
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u))
	}
	c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	c.nc.Write(out)
}

func utf16Encode(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

var (
	syncOnce       sync.Once
	registryBodies [][]byte
	tagsBody       []byte
)

func encodeSync() {
	for _, r := range regsync.Registries() {
		p := &packets.ClientboundConfigRegistryData{Registry: r.Name, Entries: make([]packets.ClientboundConfigRegistryDataEntry, len(r.Entries))}
		for i, e := range r.Entries {
			p.Entries[i].Key = e
		}
		var w proto.Writer
		packets.Marshal(&w, p)
		registryBodies = append(registryBodies, w.B)
	}
	groups := regsync.Tags()
	ut := &packets.ClientboundConfigUpdateTags{Tags: make([]packets.ClientboundConfigUpdateTagsTag, len(groups))}
	for i, g := range groups {
		ut.Tags[i].TagType = g.Registry
		ut.Tags[i].Tags = make([]packets.Tag, len(g.Tags))
		for j, t := range g.Tags {
			ut.Tags[i].Tags[j] = packets.Tag{TagName: t.Name, Entries: t.IDs}
		}
	}
	var w proto.Writer
	packets.Marshal(&w, ut)
	tagsBody = w.B
}

func (l *Listener) sendRegistries(c *Conn) {
	syncOnce.Do(encodeSync)
	for _, b := range registryBodies {
		c.SendRaw(b)
	}
	c.SendRaw(tagsBody)
}

const corePackNS, corePackID = "minecraft", "core"

func (l *Listener) configure(c *Conn) error {
	var w proto.Writer
	w.String(l.cfg.Brand)
	c.Send(&packets.ClientboundConfigCustomPayload{Channel: "minecraft:brand", Data: w.B})
	c.Send(&packets.ClientboundConfigUpdateEnabledFeatures{Features: []string{"minecraft:vanilla"}})
	c.Send(&packets.ClientboundConfigSelectKnownPacks{Packs: []packets.ClientboundConfigSelectKnownPacksPack{{Namespace: corePackNS, ID: corePackID, Version: packets.Version}}})
	finish := func() bool {
		l.sendRegistries(c)
		fin := &packets.ClientboundConfigFinishConfiguration{}
		c.Send(fin)
		if c.codec != nil {
			if ns := c.codec.NextState(packets.Configuration, fin, false); ns != packets.Configuration {
				c.inState = ns
				return true
			}
		}
		return false
	}
	if c.codec != nil && finish() {
		return nil
	}
	for {
		p, err := l.readOne(c)
		if err != nil {
			return err
		}
		switch p := p.(type) {
		case *packets.ServerboundConfigClientInformation:
			c.setInfo(p.Locale, p.ViewDistance, p.ChatFlags, p.ChatColors, p.SkinParts, p.MainHand, p.EnableServerListing)
		case *packets.ServerboundConfigCustomPayload:
			c.customPayload(p.Channel, p.Data)
		case *packets.ServerboundConfigSelectKnownPacks:
			if c.codec != nil {
				continue
			}
			known := false
			for _, k := range p.Packs {
				if k.Namespace == corePackNS && k.ID == corePackID && k.Version == packets.Version {
					known = true
				}
			}
			if !known {
				c.Disconnect(proto.Plain("This server requires the vanilla " + packets.Version + " data pack (minecraft:core)."))
				return fmt.Errorf("client lacks minecraft:core %s", packets.Version)
			}
			finish()
		case *packets.ServerboundConfigKeepAlive:
			if err := c.gotKeepAlive(p.KeepAliveID); err != nil {
				return err
			}
		case *packets.ServerboundConfigFinishConfiguration:
			c.advance(p)
			c.setOutState(c.inState)
			return nil
		}
	}
}

func (c *Conn) setInfo(locale string, vd int8, chat int32, colors bool, skin uint8, hand int32, listing bool) {
	c.info.Store(&ClientInfo{Locale: locale, ViewDistance: int(vd), ChatMode: chat, ChatColors: colors, SkinParts: skin, MainHand: hand, AllowListing: listing})
}

func (c *Conn) customPayload(channel string, data []byte) {
	if channel != "minecraft:brand" {
		return
	}
	r := proto.Reader{B: data}
	if s, err := r.String(256); err == nil {
		c.brand.Store(&s)
	}
}

func uuidString(u [16]byte) string {
	const hx = "0123456789abcdef"
	b := make([]byte, 0, 36)
	for i, x := range u {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			b = append(b, '-')
		}
		b = append(b, hx[x>>4], hx[x&15])
	}
	return string(b)
}
