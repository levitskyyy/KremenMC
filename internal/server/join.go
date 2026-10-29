package server

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/entity"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

type handler struct{ s *Server }

func (h handler) Status() mcnet.StatusInfo { return h.s.status() }

// Login runs on the connection goroutine before Login Finished.
func (h handler) Login(c *mcnet.Conn) *proto.Text {
	s := h.s
	prof := c.Profile()
	id := api.UUID(prof.UUID)
	deny := func(t proto.Text) *proto.Text { return &t }
	if e, ok := s.bans.Get(id.String()); ok {
		return deny(proto.Translatable("multiplayer.disconnect.banned.reason", proto.Plain(e.Reason)))
	}
	if e, ok := s.ipBans.Get(c.Addr().Addr().String()); ok {
		return deny(proto.Translatable("multiplayer.disconnect.banned_ip.reason", proto.Plain(e.Reason)))
	}
	op, isOp := s.ops.get(id)
	if s.wl.Enabled() && !s.wl.Contains(id) && !isOp {
		return deny(proto.Translatable("multiplayer.disconnect.not_whitelisted"))
	}
	if s.props.MaxPlayers > 0 && len(s.onlinePlayers()) >= s.props.MaxPlayers && !(isOp && op.BypassesPlayerLimit) {
		return deny(proto.Translatable("multiplayer.disconnect.server_full"))
	}
	if s.bus.HasHandlers(api.KindPlayerPreLogin) {
		ev := &api.PlayerPreLoginEvent{Name: prof.Name, UUID: id, Address: c.Addr().Addr().String()}
		s.bus.Dispatch(ev)
		if ev.Result != api.LoginAllowed {
			return deny(toText(ev.Message))
		}
	}
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventAsyncPreLogin) {
		cancelled, mut := b.FireEvent(context.Background(), bridge.Event{
			Name: bridge.EventAsyncPreLogin, Async: true,
			Fields: map[string]any{"name": prof.Name, "uuid": bridge.UUID(id), "address": c.Addr().Addr().String()},
		})
		if cancelled {
			msg := api.Text("You are not allowed to join.")
			if j, ok := mut["kickMessage"].(string); ok && j != "" {
				msg = componentFromJSON(j)
			}
			return deny(toText(msg))
		}
	}
	if old, ok := s.Player(id); ok {
		old.(*Player).c.Disconnect(proto.Translatable("multiplayer.disconnect.duplicate_login"))
	}
	return nil
}

func (h handler) Join(c *mcnet.Conn) (mcnet.Session, error) {
	p := newPlayer(h.s, c)
	select {
	case h.s.joins <- p:
	case <-h.s.stop:
		return nil, errors.New("server is stopping")
	}
	return p, nil
}

// join adds a queued player to the server; runs on the tick goroutine.
func (s *Server) join(p *Player) {
	if p.quit.Load() {
		return
	}
	w := s.defaultWorld()
	sp := w.Spawn()
	if l, ok := s.loadPlayerData(p); ok {
		sp = l
		if lw := s.world(l.World); lw != nil {
			w = lw
		}
	}
	p.world.Store(w)
	p.mu.Lock()
	p.e.Pos, p.e.Prev, p.e.Yaw, p.e.HeadYaw = sp.Pos, sp.Pos, sp.Yaw, sp.Yaw
	p.mu.Unlock()

	if s.bus.HasHandlers(api.KindPlayerLogin) {
		ev := &api.PlayerLoginEvent{Player: p, Hostname: p.c.Host()}
		s.bus.Dispatch(ev)
		if ev.Result != api.LoginAllowed {
			p.c.Disconnect(toText(ev.Message))
			return
		}
	}

	s.playersMu.Lock()
	if old := s.players[p.prof.UUID]; old != nil && old != p {
		s.playersMu.Unlock()
		p.c.Disconnect(proto.Translatable("multiplayer.disconnect.duplicate_login"))
		return
	}
	s.players[p.prof.UUID] = p
	s.byName[strings.ToLower(p.prof.Name)] = p
	s.playersMu.Unlock()
	s.list = append(s.list, p)
	p.joined.Store(true)

	gm := p.GameMode()
	p.send(&packets.ClientboundLogin{
		EntityID:            p.e.ID,
		IsHardcore:          s.props.Hardcore,
		WorldNames:          s.worldKeys(),
		MaxPlayers:          int32(s.props.MaxPlayers),
		ViewDistance:        int32(s.viewDist),
		SimulationDistance:  int32(s.simDist),
		EnableRespawnScreen: true,
		WorldState: packets.SpawnInfo{
			Dimension:  w.dimID,
			Name:       string(w.key),
			HashedSeed: w.hashedSeed(),
			Gamemode:   int32(gm),
			IsFlat:     w.flat,
			SeaLevel:   w.dim.seaLevel(),
		},
		OnlineMode: s.props.OnlineMode,
	})
	p.send(&packets.ClientboundChangeDifficulty{Difficulty: int32(w.Difficulty())})
	p.sendAbilities()
	p.send(&packets.ClientboundSetHeldSlot{Slot: p.held.Load()})
	p.sendOpLevel()
	p.sendCommands()
	p.teleport(sp)
	sd := &packets.ClientboundServerData{Motd: proto.ParseLegacy(s.props.MOTD, proto.LegacyChar)}
	if s.favicon != "" {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s.favicon, "data:image/png;base64,")); err == nil {
			sd.IconBytes = proto.Some(b)
		}
	}
	p.send(sd)

	all := s.onlinePlayers()
	info := &packets.ClientboundPlayerInfoUpdate{Action: infoAddActions}
	for _, o := range all {
		info.Data = append(info.Data, o.infoEntry())
	}
	p.send(info)
	self := encode(&packets.ClientboundPlayerInfoUpdate{Action: infoAddActions, Data: []packets.ClientboundPlayerInfoUpdateDataEntry{p.infoEntry()}})
	for _, o := range all {
		if o != p {
			o.c.SendRaw(self)
		}
	}

	p.send(&packets.ClientboundInitializeBorder{
		OldDiameter: 59999968, NewDiameter: 59999968,
		PortalTeleportBoundary: 29999984, WarningBlocks: 5, WarningTime: 15,
	})
	p.send(w.timePacket())
	p.send(w.spawnPacket())
	if w.Storming() {
		p.send(&packets.ClientboundGameEvent{Reason: 2})
		p.send(&packets.ClientboundGameEvent{Reason: 7, GameMode: 1})
	}
	p.send(&packets.ClientboundGameEvent{Reason: 13})
	p.send(&packets.ClientboundTickingState{TickRate: 20})
	p.send(&packets.ClientboundTickingStep{})
	p.send(p.inventoryPacket())
	p.sendHealth()
	p.sendLoadedData()

	go p.chatLoop()

	msg := api.Translatable("multiplayer.player.joined", api.Text(p.prof.Name)).WithColor("yellow")
	joinMsg := &msg
	if s.bus.HasHandlers(api.KindPlayerJoin) {
		ev := &api.PlayerJoinEvent{Player: p, Message: joinMsg}
		s.bus.Dispatch(ev)
		joinMsg = ev.Message
	}
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventJoin) {
		j := ""
		if joinMsg != nil {
			j = textJSON(*joinMsg)
		}
		_, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventJoin, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "name": p.prof.Name, "joinMessage": j,
		}})
		if v, ok := mut["joinMessage"].(string); ok {
			if v == "" {
				joinMsg = nil
			} else {
				c := componentFromJSON(v)
				joinMsg = &c
			}
		}
	}
	if joinMsg != nil {
		s.Broadcast(*joinMsg)
	}
	s.bar.joined(p)
	s.log.Info("player joined", "name", p.prof.Name, "uuid", p.prof.UUID.String(), "addr", p.c.Addr().String(), "protocol", p.c.Protocol())
}

const infoAddActions = packets.ClientboundPlayerInfoUpdateActionAddPlayer |
	packets.ClientboundPlayerInfoUpdateActionUpdateGameMode |
	packets.ClientboundPlayerInfoUpdateActionUpdateListed |
	packets.ClientboundPlayerInfoUpdateActionUpdateLatency

func (p *Player) infoEntry() packets.ClientboundPlayerInfoUpdateDataEntry {
	e := packets.ClientboundPlayerInfoUpdateDataEntry{
		UUID:     p.prof.UUID,
		Player:   packets.GameProfileNameProp{Name: p.prof.Name},
		Gamemode: int32(p.GameMode()),
		Listed:   true,
		Latency:  int32(p.c.Ping().Milliseconds()),
	}
	for _, pr := range p.prof.Properties {
		gp := packets.GameProfileProperty{Name: pr.Name, Value: pr.Value}
		if pr.Signature != "" {
			gp.Signature = proto.Some(pr.Signature)
		}
		e.Player.Properties = append(e.Player.Properties, gp)
	}
	return e
}

func (s *Server) worldKeys() []string {
	ws := s.worldList()
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = string(w.key)
	}
	return out
}

func (p *Player) sendOpLevel() {
	lvl := 0
	if e, ok := p.s.ops.get(p.prof.UUID); ok {
		lvl = min(max(e.Level, 0), 4)
	}
	p.send(&packets.ClientboundEntityEvent{EntityID: p.e.ID, EntityStatus: int8(24 + lvl)})
}

var commandsID = (&packets.ClientboundCommands{}).ID()

func (p *Player) sendCommands() {
	var w proto.Writer
	w.VarInt(commandsID)
	p.s.cmds.EncodeTree(&w, p)
	p.c.SendRaw(w.B)
}

// leave removes a disconnected player; runs on the tick goroutine.
func (s *Server) leave(p *Player) {
	if !p.joined.Load() {
		return
	}
	s.playersMu.Lock()
	if s.players[p.prof.UUID] != p {
		s.playersMu.Unlock()
		p.joined.Store(false)
		return
	}
	delete(s.players, p.prof.UUID)
	delete(s.byName, strings.ToLower(p.prof.Name))
	s.playersMu.Unlock()
	for i, x := range s.list {
		if x == p {
			s.list = append(s.list[:i], s.list[i+1:]...)
			break
		}
	}
	close(p.chatCh)
	p.tasks.drop(nil)
	s.bar.left(p)
	if err := p.savePlayerData(); err != nil {
		s.log.Error("save playerdata", "player", p.prof.Name, "err", err)
	}
	if w := p.world.Load(); w != nil && p.ticket != 0 {
		w.w.RemoveTicket(p.ticket)
	}

	reason := api.QuitDisconnected
	switch {
	case errors.Is(p.c.Err(), mcnet.ErrKeepAlive):
		reason = api.QuitTimedOut
	case p.c.Err() != nil && !errors.Is(p.c.Err(), mcnet.ErrClosed):
		reason = api.QuitErroneous
	}
	msg := api.Translatable("multiplayer.player.left", api.Text(p.prof.Name)).WithColor("yellow")
	quitMsg := &msg
	if s.bus.HasHandlers(api.KindPlayerQuit) {
		ev := &api.PlayerQuitEvent{Player: p, Reason: reason, Message: quitMsg}
		s.bus.Dispatch(ev)
		quitMsg = ev.Message
	}
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventQuit) {
		reasons := [...]string{"DISCONNECTED", "KICKED", "TIMED_OUT", "ERRONEOUS_STATE"}
		_, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventQuit, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "name": p.prof.Name, "quitMessage": textJSON(msg), "reason": reasons[reason],
		}})
		if v, ok := mut["quitMessage"].(string); ok {
			if v == "" {
				quitMsg = nil
			} else {
				c := componentFromJSON(v)
				quitMsg = &c
			}
		}
	}
	p.joined.Store(false)
	raw := encode(&packets.ClientboundPlayerInfoRemove{Players: [][16]byte{p.prof.UUID}})
	for _, o := range s.onlinePlayers() {
		o.c.SendRaw(raw)
	}
	if quitMsg != nil {
		s.Broadcast(*quitMsg)
	}
	s.log.Info("player left", "name", p.prof.Name, "reason", p.c.Err())
}

// playerMeta builds the metadata other clients need for p.
func (p *Player) playerMeta() []byte {
	p.mu.Lock()
	var flags byte
	pose := int32(entity.PoseStanding)
	if p.sneaking {
		flags |= entity.FlagCrouching
		if !p.flying {
			pose = entity.PoseCrouching
		}
	}
	if p.sprinting {
		flags |= entity.FlagSprinting
	}
	p.mu.Unlock()
	b := entity.AppendMetaByte(nil, entity.IndexFlags, flags)
	b = entity.AppendMetaPose(b, pose)
	b = entity.AppendMetaByte(b, entity.IndexSkinParts, p.c.ClientInfo().SkinParts)
	return entity.EndMeta(b)
}
