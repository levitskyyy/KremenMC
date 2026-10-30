package server

import (
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// BridgeHost adapts the server to the Java bridge. Mutations are queued to
// the owning tick context so calls made while Go waits in FireEvent never
// block on the tick goroutine.
type BridgeHost struct{ s *Server }

func (s *Server) BridgeHost() bridge.Host { return BridgeHost{s} }

func (h BridgeHost) ServerInfo() bridge.ServerInfo {
	s := h.s
	return bridge.ServerInfo{
		Name: "KremenMC", Version: EngineVersion, MinecraftVersion: packets.Version, Protocol: packets.Protocol,
		DataVersion: int(data.Canonical().DataVersion), Motd: textJSON(fromText(s.status().Description)),
		MaxPlayers: s.props.MaxPlayers, Port: s.props.ServerPort, OnlineMode: s.props.OnlineMode,
	}
}

func gameModeName(g api.GameMode) string { return strings.ToUpper(g.String()) }

func (h BridgeHost) info(p *Player) bridge.PlayerInfo {
	l := p.Location()
	dn := ""
	if c := p.DisplayName(); c.Text != p.prof.Name || len(c.Extra) > 0 {
		dn = textJSON(c)
	}
	props := make([]bridge.ProfileProperty, len(p.prof.Properties))
	for i, pr := range p.prof.Properties {
		props[i] = bridge.ProfileProperty(pr)
	}
	return bridge.PlayerInfo{
		UUID: bridge.UUID(p.prof.UUID), EntityID: int(p.e.ID), Name: p.prof.Name, DisplayName: dn, Location: bridgeLoc(l),
		Op: p.IsOp(), GameMode: gameModeName(p.GameMode()), Address: p.c.Addr().String(),
		Ping: int(p.c.Ping().Milliseconds()), Properties: props,
	}
}

func (h BridgeHost) Players() []bridge.PlayerInfo {
	ps := h.s.onlinePlayers()
	out := make([]bridge.PlayerInfo, len(ps))
	for i, p := range ps {
		out[i] = h.info(p)
	}
	return out
}

func (h BridgeHost) Player(id bridge.UUID) (bridge.PlayerInfo, bool) {
	p, ok := h.s.Player(api.UUID(id))
	if !ok {
		return bridge.PlayerInfo{}, false
	}
	return h.info(p.(*Player)), true
}

func (h BridgeHost) PlayerByName(name string) (bridge.PlayerInfo, bool) {
	p, ok := h.s.PlayerByName(name)
	if !ok {
		return bridge.PlayerInfo{}, false
	}
	return h.info(p.(*Player)), true
}

func (h BridgeHost) SendMessage(target bridge.UUID, j string) {
	msg := componentFromJSON(j)
	if target.IsZero() {
		h.s.console.SendMessage(msg)
		return
	}
	if p, ok := h.s.Player(api.UUID(target)); ok {
		p.SendMessage(msg)
	}
}

func (h BridgeHost) Broadcast(j, perm string) int {
	msg := componentFromJSON(j)
	n := 0
	for _, p := range h.s.onlinePlayers() {
		if perm == "" || p.HasPermission(perm) {
			p.SendMessage(msg)
			n++
		}
	}
	return n
}

func (h BridgeHost) Kick(id bridge.UUID, j string) {
	if p, ok := h.s.Player(api.UUID(id)); ok {
		p.(*Player).c.Disconnect(toText(componentFromJSON(j)))
	}
}

func (h BridgeHost) Teleport(id bridge.UUID, to bridge.Location) bool {
	p, ok := h.s.Player(api.UUID(id))
	if !ok {
		return false
	}
	pl := p.(*Player)
	if w := pl.world.Load(); w == nil || to.World != "" && to.World != w.name {
		return false
	}
	pl.tasks.add(pending{t: h.s.sched.newTask("bridge", false), run: func(*task) {
		pl.teleport(api.Location{World: to.World, Pos: api.Vec3{X: to.X, Y: to.Y, Z: to.Z}, Yaw: to.Yaw, Pitch: to.Pitch})
	}})
	return true
}

func (h BridgeHost) HasPermission(id bridge.UUID, perm, def string) bool {
	if id.IsZero() {
		return true
	}
	p, ok := h.s.Player(api.UUID(id))
	if !ok {
		return false
	}
	if d, known := api.ParsePermissionDefault(def); known {
		return p.(*Player).permissible.hasPermissionOr(perm, d)
	}
	return p.HasPermission(perm)
}

func (h BridgeHost) Worlds() []bridge.WorldInfo {
	ws := h.s.worldList()
	out := make([]bridge.WorldInfo, len(ws))
	for i, w := range ws {
		out[i] = bridge.WorldInfo{
			Name: w.name, UUID: bridge.UUID(w.uuid), Environment: "NORMAL",
			MinY: w.MinY(), MaxY: w.MaxY(), Seed: w.seed, Spawn: bridgeLoc(w.Spawn()),
		}
	}
	return out
}

func (h BridgeHost) Block(world string, x, y, z int) (string, bool) {
	w := h.s.world(world)
	if w == nil {
		return "", false
	}
	return data.StateString(w.w.Block(x, y, z)), true
}

func (h BridgeHost) SetBlock(world string, x, y, z int, state string, _ bool) {
	w := h.s.world(world)
	if w == nil {
		return
	}
	if st, ok := data.ParseState(state); ok && w.w.Dim().Contains(y) {
		w.w.SetBlock(x, y, z, st)
	}
}

func (h BridgeHost) DispatchCommand(sender bridge.UUID, line string) bool {
	var snd api.CommandSender = h.s.console
	if !sender.IsZero() {
		p, ok := h.s.Player(api.UUID(sender))
		if !ok {
			return false
		}
		snd = p
	}
	_, err := h.s.cmds.Dispatch(snd, line)
	return err == nil
}
