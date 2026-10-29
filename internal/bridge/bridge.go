// Package bridge runs Bukkit/Spigot/Paper/Folia plugin jars in a sidecar JVM.
// The Go server stays authoritative for the world; the JVM hosts plugins on
// top of an API shim (bridge/java) and talks to Go over the protocol in
// protocol.go.
package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/internal/bridge/embed"
	"github.com/levitskyyy/KremenMC/internal/bridge/jre"
)

var ErrNoBridgeJar = errors.New("bridge: kremenmc-bridge.jar not embedded (run `go run ./tools/bridgebuild`)")

type Config struct {
	PluginsDir string // default "plugins"
	RuntimeDir string // default "runtime"; holds the JRE, libs and the extracted bridge jar
	LibsDir    string // default <RuntimeDir>/libs
	JavaPath   string // use this java binary instead of provisioning one
	BridgeJar  string // use this jar instead of the embedded one
	Xms, Xmx   string // heap sizes, default 128M / 1G
	GC         string // "zgc" (default) or "g1"
	JVMArgs    []string

	EventBudget  time.Duration // max wait for a sync event, default 50ms
	AsyncBudget  time.Duration // max wait for an async event, default 5s
	CallTimeout  time.Duration // other Go->Java calls, default 10s
	StartTimeout time.Duration // JVM start until plugins are loaded, default 2m

	HTTPClient *http.Client
	Logger     *slog.Logger
}

func (c *Config) defaults() {
	if c.PluginsDir == "" {
		c.PluginsDir = "plugins"
	}
	if c.RuntimeDir == "" {
		c.RuntimeDir = "runtime"
	}
	if c.LibsDir == "" {
		c.LibsDir = filepath.Join(c.RuntimeDir, "libs")
	}
	if c.Xms == "" {
		c.Xms = "128M"
	}
	if c.Xmx == "" {
		c.Xmx = "1G"
	}
	if c.EventBudget == 0 {
		c.EventBudget = 50 * time.Millisecond
	}
	if c.AsyncBudget == 0 {
		c.AsyncBudget = 5 * time.Second
	}
	if c.CallTimeout == 0 {
		c.CallTimeout = 10 * time.Second
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = 2 * time.Minute
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

type PluginStatus struct {
	PluginDesc
	State string // discovered, loaded, enabled, disabled, failed
}

type Bridge struct {
	cfg  Config
	host Host
	log  *slog.Logger

	mu      sync.Mutex
	plugins []PluginDesc
	states  map[string]string
	sup     *supervisor

	conn      atomic.Pointer[Conn]
	listeners atomic.Pointer[map[string]bool]
	slowWarn  atomic.Int64
}

func New(cfg Config, host Host) *Bridge {
	cfg.defaults()
	b := &Bridge{cfg: cfg, host: host, log: cfg.Logger.With("component", "bridge"), states: map[string]string{}}
	empty := map[string]bool{}
	b.listeners.Store(&empty)
	return b
}

// Discover scans the plugins directory without starting anything.
func (b *Bridge) Discover() []PluginDesc {
	ps, errs := DiscoverPlugins(b.cfg.PluginsDir)
	for _, err := range errs {
		b.log.Warn("skipping plugin", "err", err)
	}
	b.mu.Lock()
	b.plugins = ps
	for _, p := range ps {
		if _, ok := b.states[p.Name]; !ok {
			b.states[p.Name] = "discovered"
		}
	}
	b.mu.Unlock()
	return ps
}

// Plugins lists the discovered Java plugins and their current state.
func (b *Bridge) Plugins() []PluginStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]PluginStatus, len(b.plugins))
	for i, p := range b.plugins {
		out[i] = PluginStatus{PluginDesc: p, State: b.states[p.Name]}
	}
	return out
}

// Start discovers plugins and, if there are any, provisions Java, starts the
// JVM and waits until the plugins are enabled. With no plugins it is a no-op.
func (b *Bridge) Start(ctx context.Context) error {
	plugins := b.Discover()
	if len(plugins) == 0 {
		b.log.Debug("no java plugins found", "dir", b.cfg.PluginsDir)
		return nil
	}
	for _, p := range plugins {
		if p.Bootstrapper != "" || p.Loader != "" {
			b.log.Warn("paper plugin bootstrapper/loader is not supported; loading main class only", "plugin", p.Name)
		}
	}
	if err := os.MkdirAll(b.cfg.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("bridge: runtime dir: %w", err)
	}
	javaPath := b.cfg.JavaPath
	if javaPath == "" {
		rt, err := jre.Ensure(ctx, jre.Options{RuntimeDir: b.cfg.RuntimeDir, MinVersion: jre.DefaultMinVersion, HTTPClient: b.cfg.HTTPClient, Logger: b.log})
		if err != nil {
			return fmt.Errorf("bridge: java runtime: %w", err)
		}
		b.log.Info("using java", "path", rt.JavaPath, "version", rt.FullVersion, "source", rt.Source)
		javaPath = rt.JavaPath
	}
	if err := EnsureLibs(ctx, b.cfg.HTTPClient, b.cfg.LibsDir, Libs, b.log); err != nil {
		return fmt.Errorf("bridge: %w", err)
	}
	jar, err := b.bridgeJar()
	if err != nil {
		return err
	}
	ln, addr, unlisten, err := listen(b.cfg.RuntimeDir)
	if err != nil {
		return fmt.Errorf("bridge: listen: %w", err)
	}
	var tok [32]byte
	_, _ = rand.Read(tok[:])
	jars := make([]string, len(plugins))
	for i, p := range plugins {
		jars[i], _ = filepath.Abs(p.File)
	}
	pluginsDir, _ := filepath.Abs(b.cfg.PluginsDir)
	classpath := []string{jar}
	for _, l := range Libs {
		classpath = append(classpath, filepath.Join(absPath(b.cfg.LibsDir), l.FileName()))
	}
	s := &supervisor{
		b:          b,
		ln:         ln,
		addr:       addr,
		unlisten:   unlisten,
		token:      hex.EncodeToString(tok[:]),
		java:       javaPath,
		classpath:  classpath,
		jars:       jars,
		pluginsDir: pluginsDir,
		conns:      make(chan *Conn),
		ready:      make(chan error, 1),
		stop:       make(chan struct{}),
		exited:     make(chan struct{}),
	}
	b.mu.Lock()
	b.sup = s
	b.mu.Unlock()
	go s.run()
	select {
	case err = <-s.ready:
	case <-ctx.Done():
		err = ctx.Err()
	case <-time.After(b.cfg.StartTimeout):
		err = errors.New("timed out waiting for the JVM to load plugins")
	}
	if err != nil {
		_ = b.Close(context.Background())
		return fmt.Errorf("bridge: start: %w", err)
	}
	return nil
}

func absPath(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func (b *Bridge) bridgeJar() (string, error) {
	if b.cfg.BridgeJar != "" {
		return absPath(b.cfg.BridgeJar), nil
	}
	data := embed.BridgeJar()
	if data == nil {
		return "", ErrNoBridgeJar
	}
	sum := sha256.Sum256(data)
	path := absPath(filepath.Join(b.cfg.RuntimeDir, "kremenmc-bridge-"+hex.EncodeToString(sum[:6])+".jar"))
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return path, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("bridge: extract jar: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("bridge: extract jar: %w", err)
	}
	return path, nil
}

// Running reports whether a JVM is connected.
func (b *Bridge) Running() bool { return b.conn.Load() != nil }

// HasListeners reports whether any Java plugin listens for the event.
func (b *Bridge) HasListeners(event string) bool { return (*b.listeners.Load())[event] }

// FireEvent forwards an event to Java plugins and returns whether it was
// cancelled plus the fields plugins changed. Events nobody listens for return
// immediately. A sync event that exceeds EventBudget is treated as not
// cancelled and unmodified; the late result is dropped.
func (b *Bridge) FireEvent(ctx context.Context, ev Event) (cancelled bool, mutated map[string]any) {
	c := b.conn.Load()
	if c == nil || !b.HasListeners(ev.Name) {
		return false, nil
	}
	budget := b.cfg.EventBudget
	if ev.Async {
		budget = b.cfg.AsyncBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	fields := ev.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	start := time.Now()
	res, err := c.Call(ctx, OpEvent, ev.Name, fields, ev.Async)
	if err != nil {
		b.warnSlow("event dispatch failed", "event", ev.Name, "elapsed", time.Since(start), "err", err)
		return false, nil
	}
	return ArgBool(res, 0), ArgMap(res, 1)
}

func (b *Bridge) warnSlow(msg string, args ...any) {
	now := time.Now().UnixNano()
	last := b.slowWarn.Load()
	if now-last < int64(5*time.Second) || !b.slowWarn.CompareAndSwap(last, now) {
		return
	}
	b.log.Warn(msg, args...)
}

// Tick drives the Bukkit/Folia schedulers on the Java side; call once per server tick.
func (b *Bridge) Tick(tick int64) {
	if c := b.conn.Load(); c != nil {
		_ = c.Notify(OpTick, tick)
	}
}

// Command runs a Java plugin command. line has no leading slash.
func (b *Bridge) Command(ctx context.Context, sender UUID, line string) (bool, error) {
	c := b.conn.Load()
	if c == nil {
		return false, ErrClosed
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.CallTimeout)
	defer cancel()
	res, err := c.Call(ctx, OpCommand, uuidOrNil(sender), line)
	if err != nil {
		return false, err
	}
	return ArgBool(res, 0), nil
}

func (b *Bridge) TabComplete(ctx context.Context, sender UUID, line string) ([]string, error) {
	c := b.conn.Load()
	if c == nil {
		return nil, ErrClosed
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.CallTimeout)
	defer cancel()
	res, err := c.Call(ctx, OpTabComplete, uuidOrNil(sender), line)
	if err != nil {
		return nil, err
	}
	return ArgStrings(res, 0), nil
}

// Close disables plugins and stops the JVM, killing it if ctx expires first.
func (b *Bridge) Close(ctx context.Context) error {
	b.mu.Lock()
	s := b.sup
	b.sup = nil
	b.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.shutdown(ctx)
}

func uuidOrNil(u UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}

func (b *Bridge) setState(name, state string) {
	b.mu.Lock()
	b.states[name] = state
	b.mu.Unlock()
}

func (b *Bridge) onNotify(op uint16, args []any) {
	h := b.host
	switch op {
	case OpListeners:
		m := map[string]bool{}
		for _, n := range ArgStrings(args, 0) {
			m[n] = true
		}
		b.listeners.Store(&m)
	case OpPluginState:
		b.setState(ArgString(args, 0), ArgString(args, 1))
	case OpSendMessage:
		u, _ := ArgUUID(args, 0)
		h.SendMessage(u, ArgString(args, 1))
	case OpKick:
		if u, ok := ArgUUID(args, 0); ok {
			h.Kick(u, ArgString(args, 1))
		}
	case OpSetBlock:
		h.SetBlock(ArgString(args, 0), int(ArgInt(args, 1)), int(ArgInt(args, 2)), int(ArgInt(args, 3)), ArgString(args, 4), ArgBool(args, 5))
	case OpRegisterCommand:
		h.RegisterCommand(CommandSpec{
			Plugin: ArgString(args, 0), Name: ArgString(args, 1), Aliases: ArgStrings(args, 2),
			Description: ArgString(args, 3), Usage: ArgString(args, 4), Permission: ArgString(args, 5),
		})
	default:
		b.log.Debug("unknown notify from jvm", "op", op)
	}
}

func (b *Bridge) onCall(_ context.Context, op uint16, args []any) ([]any, error) {
	h := b.host
	switch op {
	case OpPlayers:
		ps := h.Players()
		out := make([]any, len(ps))
		for i, p := range ps {
			out[i] = p.Map()
		}
		return []any{out}, nil
	case OpPlayer:
		var p PlayerInfo
		var ok bool
		if u, isUUID := ArgUUID(args, 0); isUUID {
			p, ok = h.Player(u)
		} else {
			p, ok = h.PlayerByName(ArgString(args, 0))
		}
		if !ok {
			return []any{nil}, nil
		}
		return []any{p.Map()}, nil
	case OpBroadcast:
		return []any{h.Broadcast(ArgString(args, 0), ArgString(args, 1))}, nil
	case OpTeleport:
		u, _ := ArgUUID(args, 0)
		return []any{h.Teleport(u, LocationFromMap(ArgMap(args, 1)))}, nil
	case OpHasPermission:
		u, _ := ArgUUID(args, 0)
		return []any{h.HasPermission(u, ArgString(args, 1))}, nil
	case OpWorlds:
		ws := h.Worlds()
		out := make([]any, len(ws))
		for i, w := range ws {
			out[i] = w.Map()
		}
		return []any{out}, nil
	case OpGetBlock:
		st, ok := h.Block(ArgString(args, 0), int(ArgInt(args, 1)), int(ArgInt(args, 2)), int(ArgInt(args, 3)))
		if !ok {
			return []any{nil}, nil
		}
		return []any{st}, nil
	case OpDispatchCommand:
		u, _ := ArgUUID(args, 0)
		return []any{h.DispatchCommand(u, ArgString(args, 1))}, nil
	}
	return nil, fmt.Errorf("unknown op %d", op)
}

func (b *Bridge) serverInfo() map[string]any {
	si := b.host.ServerInfo()
	return map[string]any{
		"name": si.Name, "version": si.Version, "minecraftVersion": si.MinecraftVersion,
		"protocol": si.Protocol, "dataVersion": si.DataVersion,
		"motd": si.Motd, "maxPlayers": si.MaxPlayers, "port": si.Port, "onlineMode": si.OnlineMode,
		"os": runtime.GOOS,
	}
}
