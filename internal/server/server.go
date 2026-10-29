// Package server glues the network layer, worlds, players and plugins
// together and runs the global tick.
package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/command"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/metrics"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/plugin"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

const EngineVersion = "0.1.0"

type Config struct {
	Dir          string
	Props        *config.ServerProperties
	Engine       *config.Engine
	Translations translate.Registry
	// VelocitySecret enables Velocity modern forwarding.
	VelocitySecret []byte
	// Addr overrides server-ip:server-port (tests use "127.0.0.1:0").
	Addr string
	// SessionServer overrides the Mojang session server URL.
	SessionServer string
	Logger        *slog.Logger
}

type Server struct {
	cfg   Config
	props *config.ServerProperties
	log   *slog.Logger
	dir   string

	ln     *mcnet.Listener
	bus    *plugin.Bus
	cmds   *command.Dispatcher
	sched  *scheduler
	perms  *permissions
	ops    *opList
	wl     *whitelist
	bans   *banList
	ipBans *banList

	bridge  atomic.Pointer[bridge.Bridge]
	plugins atomic.Pointer[pluginRegistry]

	worldsMu sync.RWMutex
	worlds   []*World
	def      *World

	playersMu sync.RWMutex
	players   map[api.UUID]*Player
	byName    map[string]*Player
	list      []*Player // tick-owned
	joins     chan *Player
	quits     chan *Player

	consoleQ chan string
	console  *console

	tick      atomic.Int64
	tpsBits   atomic.Uint64
	msptBits  atomic.Uint64
	tickTimes metrics.TickTimes
	stats     *statsSampler
	bar       *adminBar
	bossTick  chan struct{}
	startTime time.Time
	favicon   string

	gameMode   api.GameMode
	difficulty api.Difficulty
	viewDist   int
	simDist    int

	stop      chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
	stopErr   error
	done      chan struct{}
	shutdown  atomic.Pointer[api.Component]
	running   atomic.Bool
	autoSave  atomic.Bool
}

func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Props == nil {
		cfg.Props = config.DefaultServerProperties()
	}
	if cfg.Engine == nil {
		cfg.Engine = config.DefaultEngine()
	}
	if cfg.Dir == "" {
		cfg.Dir = "."
	}
	p := cfg.Props
	s := &Server{
		cfg:      cfg,
		props:    p,
		log:      cfg.Logger,
		dir:      cfg.Dir,
		bus:      plugin.NewBus(cfg.Logger),
		perms:    newPermissions(),
		players:  map[api.UUID]*Player{},
		byName:   map[string]*Player{},
		joins:    make(chan *Player, 1024),
		quits:    make(chan *Player, 1024),
		consoleQ: make(chan string, 64),
		bossTick: make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		viewDist: p.ViewDistance,
		simDist:  p.SimulationDistance,
	}
	s.gameMode = parseGameMode(p.GamemodeName())
	s.difficulty = parseDifficulty(p.DifficultyName())
	s.sched = newScheduler(s)
	s.console = newConsole(s)
	var errs []error
	var err error
	if s.ops, err = loadOps(s.path("ops.json"), p.OpPermissionLevel, s.log); err != nil {
		errs = append(errs, err)
	}
	if s.wl, err = loadWhitelist(s.path("whitelist.json"), p.WhiteList, s.log); err != nil {
		errs = append(errs, err)
	}
	if s.bans, err = loadBans(s.path("banned-players.json"), false, s.log); err != nil {
		errs = append(errs, err)
	}
	if s.ipBans, err = loadBans(s.path("banned-ips.json"), true, s.log); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	s.stats = newStatsSampler(s)
	s.bar = newAdminBar(s)
	s.cmds = command.New(s)
	command.RegisterBuiltins(s.cmds, s)
	s.autoSave.Store(true)
	s.cmds.SetResendHook(func(p api.Player) {
		if pl, ok := p.(*Player); ok {
			pl.sendCommands()
		}
	})
	if b, err := os.ReadFile(s.path("server-icon.png")); err == nil {
		s.favicon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
	}
	s.tpsBits.Store(f64bits(20))
	return s, nil
}

func (s *Server) path(name string) string { return filepath.Join(s.dir, name) }

// Bus returns the event bus shared with the plugin runtimes.
func (s *Server) Bus() *plugin.Bus { return s.bus }

// Dispatcher returns the command dispatcher.
func (s *Server) Dispatcher() *command.Dispatcher { return s.cmds }

// SetPlugins installs the plugin registry exposed through api.Server.
func (s *Server) SetPlugins(m *plugin.Manager) { s.plugins.Store(&pluginRegistry{m: m}) }

// SetBridge connects the Java plugin bridge.
func (s *Server) SetBridge(b *bridge.Bridge) { s.bridge.Store(b) }

func (s *Server) javaBridge() *bridge.Bridge { return s.bridge.Load() }

// AddWorld registers a world; the first one becomes the default.
func (s *Server) AddWorld(w *World) {
	s.worldsMu.Lock()
	s.worlds = append(s.worlds, w)
	if s.def == nil {
		s.def = w
	}
	s.worldsMu.Unlock()
	if s.bus.HasHandlers(api.KindWorldLoad) {
		s.bus.Dispatch(&api.WorldLoadEvent{World: w})
	}
}

// Start binds the listener and starts the tick loop.
func (s *Server) Start() error {
	if s.def == nil {
		return errors.New("server: no world")
	}
	addr := s.cfg.Addr
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", s.props.ServerIP, s.props.ServerPort)
	}
	ln, err := mcnet.NewListener(mcnet.Config{
		Addr:                    addr,
		OnlineMode:              s.props.OnlineMode,
		PreventProxyConnections: s.props.PreventProxyConnections,
		CompressionThreshold:    s.props.NetworkCompressionThreshold,
		VelocitySecret:          s.cfg.VelocitySecret,
		AcceptTransfers:         s.props.AcceptsTransfers,
		ConnThrottle:            time.Second,
		MaxPacketsPerSecond:     s.props.RateLimit,
		Translations:            s.cfg.Translations,
		SessionServer:           s.cfg.SessionServer,
		Logger:                  s.log,
	}, handler{s})
	if err != nil {
		return err
	}
	if err := ln.Listen(); err != nil {
		return err
	}
	s.ln = ln
	s.startTime = time.Now()
	s.running.Store(true)
	s.stats.sample(s.startTime)
	go s.statsLoop()
	go func() {
		if err := ln.Serve(); err != nil {
			s.log.Error("listener stopped", "err", err)
		}
	}()
	go s.run()
	s.log.Info("listening", "addr", ln.Addr().String(), "version", packets.Version, "protocol", packets.Protocol, "online-mode", s.props.OnlineMode)
	return nil
}

// Addr is the bound listener address.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Stop kicks everyone, stops ticking and saves all worlds.
func (s *Server) Stop(reason api.Component) error {
	s.shutdown.CompareAndSwap(nil, &reason)
	s.stopOnce.Do(func() { close(s.stop) })
	s.closeOnce.Do(func() {
		if s.running.Load() {
			<-s.done
		}
		if s.ln != nil {
			s.ln.Close()
		}
		var errs []error
		for _, w := range s.worldList() {
			if err := w.close(); err != nil {
				errs = append(errs, err)
			}
		}
		s.stopErr = errors.Join(errs...)
		s.log.Info("server stopped")
	})
	return s.stopErr
}

// Done is closed after the tick loop exited.
func (s *Server) Done() <-chan struct{} { return s.done }

// ConsoleCommand queues a console command for the next tick.
func (s *Server) ConsoleCommand(line string) {
	select {
	case s.consoleQ <- line:
	default:
	}
}

func (s *Server) Name() string        { return "KremenMC" }
func (s *Server) Version() string     { return EngineVersion }
func (s *Server) GameVersion() string { return packets.Version }
func (s *Server) Protocol() int32     { return packets.Protocol }
func (s *Server) MaxPlayers() int     { return s.props.MaxPlayers }

func (s *Server) Players() []api.Player {
	s.playersMu.RLock()
	defer s.playersMu.RUnlock()
	out := make([]api.Player, 0, len(s.players))
	for _, p := range s.players {
		out = append(out, p)
	}
	return out
}

func (s *Server) onlinePlayers() []*Player {
	s.playersMu.RLock()
	defer s.playersMu.RUnlock()
	out := make([]*Player, 0, len(s.players))
	for _, p := range s.players {
		out = append(out, p)
	}
	return out
}

func (s *Server) Player(id api.UUID) (api.Player, bool) {
	s.playersMu.RLock()
	defer s.playersMu.RUnlock()
	p, ok := s.players[id]
	if !ok {
		return nil, false
	}
	return p, true
}

func (s *Server) PlayerByName(name string) (api.Player, bool) {
	s.playersMu.RLock()
	defer s.playersMu.RUnlock()
	p, ok := s.byName[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	return p, true
}

func (s *Server) Worlds() []api.World {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	out := make([]api.World, len(s.worlds))
	for i, w := range s.worlds {
		out[i] = w
	}
	return out
}

func (s *Server) worldList() []*World {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	return slices.Clone(s.worlds)
}

func (s *Server) World(name string) (api.World, bool) {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	for _, w := range s.worlds {
		if w.name == name || string(w.key) == name {
			return w, true
		}
	}
	return nil, false
}

func (s *Server) world(name string) *World {
	w, ok := s.World(name)
	if !ok {
		return nil
	}
	return w.(*World)
}

func (s *Server) DefaultWorld() api.World {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	return s.def
}

func (s *Server) defaultWorld() *World {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	return s.def
}

func (s *Server) CreateWorld(opts api.WorldOptions) (api.World, error) {
	if w, ok := s.World(opts.Name); ok {
		return w, nil
	}
	if opts.Name == "" || strings.ContainsAny(opts.Name, `/\`) {
		return nil, fmt.Errorf("invalid world name %q", opts.Name)
	}
	seed := opts.Seed
	if opts.RandomSeed {
		seed = time.Now().UnixNano()
	}
	w, err := OpenWorld(s, WorldConfig{
		Name:      opts.Name,
		Dir:       s.path(opts.Name),
		Seed:      seed,
		Flat:      strings.Contains(strings.ToLower(opts.Generator), "flat"),
		FlatSpec:  opts.GeneratorSettings,
		Workers:   s.cfg.Engine.Workers.ChunkIO,
		TickProcs: s.cfg.Engine.Workers.Tick,
	})
	if err != nil {
		return nil, err
	}
	s.AddWorld(w)
	return w, nil
}

func (s *Server) UnloadWorld(name string, save bool) bool {
	w := s.world(name)
	if w == nil || w == s.defaultWorld() || len(w.Players()) > 0 {
		return false
	}
	if s.bus.HasHandlers(api.KindWorldUnload) {
		if s.bus.Dispatch(&api.WorldUnloadEvent{World: w}).(*api.WorldUnloadEvent).Cancelled() {
			return false
		}
	}
	s.worldsMu.Lock()
	s.worlds = slices.DeleteFunc(s.worlds, func(x *World) bool { return x == w })
	s.worldsMu.Unlock()
	if save {
		w.close()
	}
	return true
}

func (s *Server) Broadcast(msg api.Component) {
	raw := systemChat(msg, false)
	for _, p := range s.onlinePlayers() {
		p.c.SendRaw(raw)
	}
	s.log.Info(msg.Plain())
}

func (s *Server) BroadcastPermission(msg api.Component, perm string) {
	raw := systemChat(msg, false)
	for _, p := range s.onlinePlayers() {
		if p.HasPermission(perm) {
			p.c.SendRaw(raw)
		}
	}
	s.log.Info(msg.Plain())
}

func (s *Server) Console() api.CommandSender    { return s.console }
func (s *Server) Scheduler() api.Scheduler      { return s.sched }
func (s *Server) Commands() api.CommandRegistry { return s.cmds }
func (s *Server) Permissions() api.Permissions  { return s.perms }
func (s *Server) Blocks() api.BlockRegistry     { return blockRegistry{} }
func (s *Server) Items() api.ItemRegistry       { return itemRegistry{} }
func (s *Server) ProfileBans() api.BanList      { return s.bans }
func (s *Server) IPBans() api.BanList           { return s.ipBans }
func (s *Server) Whitelist() api.Whitelist      { return s.wl }
func (s *Server) Logger() *slog.Logger          { return s.log }
func (s *Server) CurrentTick() int64            { return s.tick.Load() }
func (s *Server) StartTime() time.Time          { return s.startTime }
func (s *Server) TPS() float64                  { return f64(s.tpsBits.Load()) }
func (s *Server) MSPT() float64                 { return f64(s.msptBits.Load()) }
func (s *Server) Shutdown(reason api.Component) { go s.Stop(reason) }
func (s *Server) Plugins() api.PluginRegistry {
	if r := s.plugins.Load(); r != nil {
		return r
	}
	return &pluginRegistry{}
}

func (s *Server) CreateInventory(kind api.InventoryType, title api.Component, size int) api.Inventory {
	return newInventory(kind, title, size, nil)
}

func (s *Server) OfflinePlayer(id api.UUID) (api.OfflinePlayer, bool) {
	if p, ok := s.Player(id); ok {
		return offline{s: s, id: id, name: p.Name()}, true
	}
	if e, ok := s.ops.get(id); ok {
		return offline{s: s, id: id, name: e.Name}, true
	}
	return nil, false
}

type offline struct {
	s    *Server
	id   api.UUID
	name string
}

func (o offline) UUID() api.UUID { return o.id }
func (o offline) Name() string   { return o.name }
func (o offline) IsOnline() bool {
	_, ok := o.s.Player(o.id)
	return ok
}
func (o offline) IsOp() bool {
	_, ok := o.s.ops.get(o.id)
	return ok
}
func (o offline) SetOp(v bool) { o.s.setOp(o.id, o.name, v) }
func (o offline) IsBanned() bool {
	_, ok := o.s.bans.Get(o.id.String())
	return ok
}
func (o offline) IsWhitelisted() bool { return o.s.wl.Contains(o.id) }
func (o offline) SetWhitelisted(v bool) {
	if v {
		o.s.wl.Add(api.GameProfile{UUID: o.id, Name: o.name})
	} else {
		o.s.wl.Remove(o.id)
	}
}
func (o offline) LastSeen() time.Time               { return time.Time{} }
func (o offline) PersistentData() api.DataContainer { return newDataContainer() }

func (s *Server) setOp(id api.UUID, name string, v bool) {
	s.ops.set(id, name, v)
	if p, ok := s.Player(id); ok {
		pl := p.(*Player)
		pl.sendOpLevel()
		pl.sendCommands()
	}
}

type pluginRegistry struct{ m *plugin.Manager }

func (r *pluginRegistry) Plugins() []api.PluginInfo {
	if r.m == nil {
		return nil
	}
	return r.m.Plugins()
}

func (r *pluginRegistry) Plugin(id string) (api.PluginInfo, bool) {
	if r.m == nil {
		return api.PluginInfo{}, false
	}
	return r.m.Plugin(id)
}

func (r *pluginRegistry) IsEnabled(id string) bool { return r.m != nil && r.m.IsEnabled(id) }
func (r *pluginRegistry) DataDir(id string) string {
	if r.m == nil {
		return filepath.Join("plugins", id)
	}
	return r.m.DataDir(id)
}

func parseGameMode(s string) api.GameMode {
	switch s {
	case "creative":
		return api.Creative
	case "adventure":
		return api.Adventure
	case "spectator":
		return api.Spectator
	}
	return api.Survival
}

func parseDifficulty(s string) api.Difficulty {
	switch s {
	case "peaceful":
		return api.DifficultyPeaceful
	case "normal":
		return api.DifficultyNormal
	case "hard":
		return api.DifficultyHard
	}
	return api.DifficultyEasy
}

// systemChat pre-encodes a System Chat packet body.
func systemChat(msg api.Component, actionBar bool) []byte {
	var w proto.Writer
	packets.Marshal(&w, &packets.ClientboundSystemChat{Content: toText(msg), IsActionBar: actionBar})
	return w.B
}

func encode(p packets.Packet) []byte {
	var w proto.Writer
	packets.Marshal(&w, p)
	return w.B
}

func (s *Server) status() mcnet.StatusInfo {
	ps := s.onlinePlayers()
	info := mcnet.StatusInfo{
		Description: proto.ParseLegacy(s.props.MOTD, proto.LegacyChar),
		MaxPlayers:  s.props.MaxPlayers,
		Online:      len(ps),
		Favicon:     s.favicon,
	}
	if !s.props.HideOnlinePlayers {
		for i, p := range ps {
			if i == 12 {
				break
			}
			info.Sample = append(info.Sample, mcnet.Profile{UUID: p.prof.UUID, Name: p.prof.Name})
		}
	}
	return info
}

func (s *Server) workers() int {
	if n := s.cfg.Engine.Workers.Tick; n > 0 {
		return n
	}
	return runtime.GOMAXPROCS(0)
}

var _ api.Server = (*Server)(nil)

// RunUntil blocks until ctx is done or the server stops itself.
func (s *Server) RunUntil(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-s.stop:
	}
}

func (s *Server) DefaultGameMode() api.GameMode {
	s.worldsMu.RLock()
	defer s.worldsMu.RUnlock()
	return s.gameMode
}

func (s *Server) SetDefaultGameMode(g api.GameMode) {
	s.worldsMu.Lock()
	s.gameMode = g
	s.worldsMu.Unlock()
}

func (s *Server) AutoSave() bool     { return s.autoSave.Load() }
func (s *Server) SetAutoSave(v bool) { s.autoSave.Store(v) }

func (s *Server) SaveAll(bool) error {
	var errs []error
	for _, w := range s.worldList() {
		errs = append(errs, w.Save())
	}
	return errors.Join(errs...)
}

func (s *Server) ReloadWhitelist() error {
	w, err := loadWhitelist(s.path("whitelist.json"), s.wl.Enabled(), s.log)
	if err != nil {
		return err
	}
	s.wl.mu.Lock()
	s.wl.m = w.m
	s.wl.mu.Unlock()
	return nil
}
