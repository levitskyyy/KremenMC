package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	wapi "github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// Limits bound what a single WASM plugin may consume.
type Limits struct {
	MaxMemoryMB   int
	CallTimeout   time.Duration // per event/command/task call
	EnableTimeout time.Duration // instantiate + kremenmc_enable
	KVMaxBytes    int
}

func DefaultLimits() Limits {
	return Limits{MaxMemoryMB: 128, CallTimeout: 50 * time.Millisecond, EnableTimeout: 10 * time.Second, KVMaxBytes: 16 << 20}
}

// Engine holds state shared by all WASM plugins: the compilation cache and
// default limits. Each plugin gets its own wazero runtime so memory limits and
// failures are isolated.
type Engine struct {
	cache  wazero.CompilationCache
	limits Limits
	log    *slog.Logger
}

// NewEngine creates an engine. cacheDir empty keeps compiled code in memory only.
func NewEngine(cacheDir string, limits Limits, log *slog.Logger) (*Engine, error) {
	if log == nil {
		log = slog.Default()
	}
	var cache wazero.CompilationCache
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("wasm cache: %w", err)
		}
		c, err := wazero.NewCompilationCacheWithDir(cacheDir)
		if err != nil {
			return nil, fmt.Errorf("wasm cache: %w", err)
		}
		cache = c
	} else {
		cache = wazero.NewCompilationCache()
	}
	return &Engine{cache: cache, limits: limits, log: log}, nil
}

func (e *Engine) Close(ctx context.Context) error { return e.cache.Close(ctx) }

// Env is what a runtime needs from the host.
type Env struct {
	Server  api.Server
	Bus     *Bus
	Log     *slog.Logger
	DataDir string
}

var errDead = errors.New("plugin is not running")

type wasmPlugin struct {
	m       *Manifest
	env     Env
	log     *slog.Logger
	timeout time.Duration
	enableT time.Duration

	rt  wazero.Runtime
	mod wapi.Module

	fnAlloc, fnEnable, fnDisable, fnEvent, fnCommand, fnTask wapi.Function

	// mu serializes guest calls; a wasm instance is single-threaded.
	mu sync.Mutex
	// hostGID is the goroutine executing a host function that can re-enter
	// the server (teleport, set_block, dispatch_command); 0 otherwise.
	hostGID atomic.Uint64
	dead    atomic.Bool
	failed  atomic.Pointer[error]

	result []byte
	enc    wire.Writer

	stateMu  sync.Mutex
	enabled  bool
	subs     []Subscription
	tasks    map[uint32]api.Task
	kv       *FileKV
	commands bool
}

// LoadWASM compiles and instantiates a plugin module. Package initializers
// run here (via _initialize); kremenmc_enable runs in Enable.
func (e *Engine) LoadWASM(ctx context.Context, m *Manifest, bin []byte, env Env) (Runtime, error) {
	log := env.Log
	if log == nil {
		log = e.log
	}
	log = log.With("plugin", m.ID)
	p := &wasmPlugin{
		m:       m,
		env:     env,
		log:     log,
		timeout: m.Limits.callTimeout(e.limits.CallTimeout),
		enableT: e.limits.EnableTimeout,
		tasks:   map[uint32]api.Task{},
	}
	if p.enableT <= 0 {
		p.enableT = 10 * time.Second
	}
	cfg := wazero.NewRuntimeConfig().
		WithCompilationCache(e.cache).
		WithMemoryLimitPages(m.Limits.memoryPages(e.limits.MaxMemoryMB)).
		WithCloseOnContextDone(true)
	p.rt = wazero.NewRuntimeWithConfig(ctx, cfg)
	ok := false
	defer func() {
		if !ok {
			p.rt.Close(context.Background())
		}
	}()
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, p.rt); err != nil {
		return nil, fmt.Errorf("wasm %s: wasi: %w", m.ID, err)
	}
	if err := p.buildHost(ctx); err != nil {
		return nil, fmt.Errorf("wasm %s: host module: %w", m.ID, err)
	}
	compiled, err := p.rt.CompileModule(ctx, bin)
	if err != nil {
		return nil, fmt.Errorf("wasm %s: compile: %w", m.ID, err)
	}
	exports := compiled.ExportedFunctions()
	for _, name := range []string{wire.ExportAlloc, wire.ExportOnEvent} {
		if _, ok := exports[name]; !ok {
			return nil, fmt.Errorf("wasm %s: missing export %s", m.ID, name)
		}
	}
	if env.DataDir != "" {
		if err := os.MkdirAll(env.DataDir, 0o755); err != nil {
			return nil, fmt.Errorf("wasm %s: %w", m.ID, err)
		}
		p.kv, err = OpenFileKV(filepath.Join(env.DataDir, "kv.bin"), e.limits.KVMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("wasm %s: %w", m.ID, err)
		}
	}
	out := &logWriter{log: log, level: slog.LevelInfo}
	errOut := &logWriter{log: log, level: slog.LevelWarn}
	mc := wazero.NewModuleConfig().
		WithName(m.ID).
		WithStartFunctions("_initialize").
		WithStdout(out).
		WithStderr(errOut).
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader).
		WithArgs(m.ID)
	if env.DataDir != "" {
		mc = mc.WithFSConfig(wazero.NewFSConfig().WithDirMount(env.DataDir, "/data"))
	}
	// Compilation is not bounded by the guest deadline; it is host work and
	// is cached on disk after the first run.
	ctx, cancel := context.WithTimeout(ctx, p.enableT)
	defer cancel()
	p.mod, err = p.rt.InstantiateModule(ctx, compiled, mc)
	if err != nil {
		return nil, fmt.Errorf("wasm %s: instantiate: %w", m.ID, err)
	}
	if v := p.mod.ExportedFunction(wire.ExportABIVersion); v != nil {
		res, err := v.Call(ctx)
		if err != nil {
			return nil, fmt.Errorf("wasm %s: abi version: %w", m.ID, err)
		}
		if len(res) == 1 && uint32(res[0]) != wire.ABIVersion {
			return nil, fmt.Errorf("wasm %s: abi version %d, host supports %d", m.ID, uint32(res[0]), wire.ABIVersion)
		}
	}
	p.fnAlloc = p.mod.ExportedFunction(wire.ExportAlloc)
	p.fnEvent = p.mod.ExportedFunction(wire.ExportOnEvent)
	p.fnEnable = p.mod.ExportedFunction(wire.ExportEnable)
	p.fnDisable = p.mod.ExportedFunction(wire.ExportDisable)
	p.fnCommand = p.mod.ExportedFunction(wire.ExportOnCommand)
	p.fnTask = p.mod.ExportedFunction(wire.ExportOnTask)
	ok = true
	return p, nil
}

func (p *wasmPlugin) Kind() string { return "wasm" }

func (p *wasmPlugin) Err() error {
	if e := p.failed.Load(); e != nil {
		return *e
	}
	return nil
}

func (p *wasmPlugin) Enable(ctx context.Context) error {
	if p.dead.Load() {
		return errDead
	}
	p.stateMu.Lock()
	p.enabled = true
	p.stateMu.Unlock()
	if perms := p.env.Server.Permissions(); perms != nil {
		for _, perm := range p.m.APIPermissions() {
			if err := perms.Register(perm); err != nil {
				p.log.Warn("register permission", "permission", perm.Name, "err", err)
			}
		}
	}
	if p.fnEnable == nil {
		return nil
	}
	if !p.acquire() {
		return errors.New("enable called re-entrantly from the plugin itself")
	}
	res, err := p.callLocked(ctx, p.enableT, p.fnEnable)
	p.mu.Unlock()
	if err != nil {
		return err
	}
	if len(res) == 1 && int32(res[0]) != 0 {
		err := fmt.Errorf("kremenmc_enable returned %d", int32(res[0]))
		p.fail(err)
		return err
	}
	return nil
}

func (p *wasmPlugin) Disable(ctx context.Context) error {
	var err error
	if !p.dead.Load() && p.fnDisable != nil {
		if p.acquire() {
			_, err = p.callLocked(ctx, p.enableT, p.fnDisable)
			p.mu.Unlock()
		} else {
			p.log.Warn("skipping kremenmc_disable: plugin disabled itself from inside a call")
		}
	}
	p.teardown()
	return err
}

func (p *wasmPlugin) Close(ctx context.Context) error {
	p.teardown()
	p.dead.Store(true)
	return p.rt.Close(ctx)
}

// teardown removes everything the plugin registered with the server. It is
// idempotent and never calls into the guest.
func (p *wasmPlugin) teardown() {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if !p.enabled {
		return
	}
	p.enabled = false
	for _, s := range p.subs {
		p.env.Bus.Unsubscribe(s)
	}
	p.subs = nil
	for id, t := range p.tasks {
		t.Cancel()
		delete(p.tasks, id)
	}
	srv := p.env.Server
	if p.commands {
		srv.Commands().UnregisterAll(p.m.ID)
		p.commands = false
	}
	if perms := srv.Permissions(); perms != nil {
		for name := range p.m.Permissions {
			perms.Unregister(name)
		}
	}
}

// fail marks the plugin dead after a trap, timeout or guest panic. The module
// is closed and all registrations are dropped; the server keeps running.
func (p *wasmPlugin) fail(err error) {
	if !p.dead.CompareAndSwap(false, true) {
		return
	}
	p.failed.Store(&err)
	p.log.Error("plugin crashed, disabling", "err", err)
	// Asynchronous: the failing call may be running inside a server lock
	// (e.g. command dispatch) that teardown needs.
	go p.teardown()
}

func (p *wasmPlugin) callLocked(ctx context.Context, timeout time.Duration, fn wapi.Function, params ...uint64) ([]uint64, error) {
	if p.dead.Load() {
		return nil, errDead
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := fn.Call(ctx, params...)
	if err != nil {
		err = fmt.Errorf("%s: %w", fn.Definition().Name(), err)
		p.fail(err)
		return nil, err
	}
	return res, nil
}

// acquire locks the instance. It returns false for a re-entrant call from a
// host function on the same goroutine, which would deadlock otherwise.
func (p *wasmPlugin) acquire() bool {
	if p.mu.TryLock() {
		return true
	}
	if g := p.hostGID.Load(); g != 0 && g == goid() {
		return false
	}
	p.mu.Lock()
	return true
}

// writeGuest copies data into guest memory obtained from kremenmc_alloc.
func (p *wasmPlugin) writeGuest(ctx context.Context, data []byte) (uint32, error) {
	res, err := p.callLocked(ctx, p.timeout, p.fnAlloc, uint64(len(data)))
	if err != nil {
		return 0, err
	}
	ptr := uint32(res[0])
	if !p.mod.Memory().Write(ptr, data) {
		err := fmt.Errorf("kremenmc_alloc returned out-of-bounds pointer %#x", ptr)
		p.fail(err)
		return 0, err
	}
	return ptr, nil
}

func (p *wasmPlugin) handleEvent(e api.Event, tag uint32, priority api.Priority) {
	if p.dead.Load() {
		return
	}
	if !p.acquire() {
		p.log.Debug("skipping event fired re-entrantly by the plugin itself", "event", e.Kind().String())
		return
	}
	defer p.mu.Unlock()
	p.enc.Reset()
	if !EncodeEvent(&p.enc, e) {
		return
	}
	ctx := context.Background()
	ptr, err := p.writeGuest(ctx, p.enc.B)
	if err != nil {
		return
	}
	res, err := p.callLocked(ctx, p.timeout, p.fnEvent, uint64(uint32(e.Kind())|tag), uint64(ptr), uint64(len(p.enc.B)))
	if err != nil || len(res) == 0 || res[0] == 0 || priority == api.Monitor {
		return
	}
	rptr, rlen := uint32(res[0]>>32), uint32(res[0])
	data, ok := p.mod.Memory().Read(rptr, rlen)
	if !ok {
		p.log.Warn("event response out of bounds", "event", e.Kind().String())
		return
	}
	if err := ApplyResponse(e, data, p.env.Server); err != nil {
		p.log.Warn("malformed event response", "event", e.Kind().String(), "err", err)
	}
}

func (p *wasmPlugin) runCommand(handler uint32, payload []byte) (int, error) {
	if p.dead.Load() {
		return 0, api.Errorf("plugin " + p.m.ID + " is not running")
	}
	if p.fnCommand == nil {
		return 0, api.Errorf("plugin " + p.m.ID + " cannot handle commands")
	}
	if !p.acquire() {
		return 0, api.Errorf("plugin " + p.m.ID + " is busy")
	}
	defer p.mu.Unlock()
	ctx := context.Background()
	ptr, err := p.writeGuest(ctx, payload)
	if err != nil {
		return 0, api.Errorf("plugin " + p.m.ID + " failed")
	}
	res, err := p.callLocked(ctx, p.timeout, p.fnCommand, uint64(handler), uint64(ptr), uint64(len(payload)))
	if err != nil {
		return 0, api.Errorf("plugin " + p.m.ID + " failed")
	}
	return int(int32(res[0])), nil
}

func (p *wasmPlugin) runTask(id uint32, repeating bool) {
	if !repeating {
		p.stateMu.Lock()
		delete(p.tasks, id)
		p.stateMu.Unlock()
	}
	if p.dead.Load() || p.fnTask == nil || !p.acquire() {
		return
	}
	defer p.mu.Unlock()
	p.callLocked(context.Background(), p.timeout, p.fnTask, uint64(id))
}

// goid returns the current goroutine id. It is only used on the contended
// path of acquire and inside re-entrant host functions.
func goid() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	id, _ := strconv.ParseUint(string(b), 10, 64)
	return id
}

type logWriter struct {
	log   *slog.Logger
	level slog.Level
	mu    sync.Mutex
	buf   []byte
}

func (w *logWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.log.Log(context.Background(), w.level, string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 4096 {
		w.log.Log(context.Background(), w.level, string(w.buf))
		w.buf = w.buf[:0]
	}
	return len(b), nil
}
