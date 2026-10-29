package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// Runtime is one loaded plugin instance, independent of how it executes.
type Runtime interface {
	Kind() string
	Enable(ctx context.Context) error
	// Disable must be safe to call after the runtime has crashed.
	Disable(ctx context.Context) error
	Close(ctx context.Context) error
	// Err is non-nil once the runtime has crashed.
	Err() error
}

type State uint8

const (
	StateLoaded State = iota
	StateEnabled
	StateDisabled
	StateFailed
)

func (s State) String() string {
	return [...]string{"loaded", "enabled", "disabled", "failed"}[s]
}

type entry struct {
	m     *Manifest
	path  string // file or directory the plugin was loaded from
	rt    Runtime
	state State
	err   error
}

func (e *entry) effectiveState() State {
	if e.state == StateEnabled && e.rt != nil && e.rt.Err() != nil {
		return StateFailed
	}
	return e.state
}

type Options struct {
	Dir      string // plugins directory
	CacheDir string // compiled wasm cache; empty disables the disk cache
	Limits   Limits
	Logger   *slog.Logger
}

// Manager discovers, orders, loads, enables and reloads plugins.
type Manager struct {
	srv  api.Server
	bus  *Bus
	eng  *Engine
	opts Options
	log  *slog.Logger

	opMu sync.Mutex // serializes load/enable/disable/reload

	mu      sync.RWMutex
	entries map[string]*entry
	order   []string
	failed  map[string]error
	jars    []string
}

func NewManager(srv api.Server, bus *Bus, opts Options) (*Manager, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Limits == (Limits{}) {
		opts.Limits = DefaultLimits()
	}
	eng, err := NewEngine(opts.CacheDir, opts.Limits, opts.Logger)
	if err != nil {
		return nil, err
	}
	return &Manager{
		srv:     srv,
		bus:     bus,
		eng:     eng,
		opts:    opts,
		log:     opts.Logger,
		entries: map[string]*entry{},
		failed:  map[string]error{},
	}, nil
}

// Candidate is a discovered plugin before loading.
type Candidate struct {
	Manifest *Manifest
	Path     string // directory or .wasm file
	Wasm     string // wasm file to execute
}

// Discover scans the plugins directory. WASM plugins are either a directory
// with plugin.toml, a .wasm with a sibling <name>.toml, or a .wasm with an
// embedded manifest section. Jars are returned for the Java bridge.
func (m *Manager) Discover() ([]Candidate, []string, map[string]error) {
	errs := map[string]error{}
	ents, err := os.ReadDir(m.opts.Dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			errs[m.opts.Dir] = err
		}
		return nil, nil, errs
	}
	var cands []Candidate
	var jars []string
	for _, de := range ents {
		name := de.Name()
		path := filepath.Join(m.opts.Dir, name)
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case de.IsDir():
			c, ok, err := candidateFromDir(path)
			if err != nil {
				errs[path] = err
			} else if ok {
				cands = append(cands, c)
			}
		case strings.HasSuffix(name, ".wasm"):
			c, err := candidateFromFile(path)
			if err != nil {
				errs[path] = err
			} else {
				cands = append(cands, c)
			}
		case strings.HasSuffix(name, ".jar"):
			jars = append(jars, path)
		}
	}
	slices.Sort(jars)
	return cands, jars, errs
}

func candidateFromDir(dir string) (Candidate, bool, error) {
	mpath := filepath.Join(dir, "plugin.toml")
	if _, err := os.Stat(mpath); err != nil {
		return Candidate{}, false, nil
	}
	man, err := LoadManifest(mpath)
	if err != nil {
		return Candidate{}, false, err
	}
	main := man.Main
	if main == "" {
		if _, err := os.Stat(filepath.Join(dir, "plugin.wasm")); err == nil {
			main = "plugin.wasm"
		} else {
			matches, _ := filepath.Glob(filepath.Join(dir, "*.wasm"))
			if len(matches) != 1 {
				return Candidate{}, false, fmt.Errorf("%s: set main in plugin.toml (found %d .wasm files)", dir, len(matches))
			}
			main = filepath.Base(matches[0])
		}
	}
	if filepath.IsAbs(main) || strings.Contains(filepath.ToSlash(main), "..") {
		return Candidate{}, false, fmt.Errorf("%s: main must be a relative path inside the plugin directory", dir)
	}
	return Candidate{Manifest: man, Path: dir, Wasm: filepath.Join(dir, main)}, true, nil
}

func candidateFromFile(path string) (Candidate, error) {
	sidecar := strings.TrimSuffix(path, ".wasm") + ".toml"
	if _, err := os.Stat(sidecar); err == nil {
		man, err := LoadManifest(sidecar)
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{Manifest: man, Path: path, Wasm: path}, nil
	}
	bin, err := os.ReadFile(path)
	if err != nil {
		return Candidate{}, err
	}
	raw, ok, err := wire.ExtractManifest(bin)
	if err != nil {
		return Candidate{}, fmt.Errorf("%s: %w", path, err)
	}
	if !ok {
		return Candidate{}, fmt.Errorf("%s: no manifest (add %s or embed one with kremenmc-pack)", path, filepath.Base(sidecar))
	}
	man, err := ParseManifest(raw)
	if err != nil {
		return Candidate{}, fmt.Errorf("%s: %w", path, err)
	}
	return Candidate{Manifest: man, Path: path, Wasm: path}, nil
}

// LoadAll discovers and instantiates every plugin in dependency order.
// Plugins that fail are recorded and skipped; the error is only for fatal
// problems.
func (m *Manager) LoadAll(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	cands, jars, errs := m.Discover()
	m.mu.Lock()
	m.jars = jars
	m.mu.Unlock()
	for path, err := range errs {
		m.log.Error("plugin discovery", "path", path, "err", err)
	}
	byID := map[string]Candidate{}
	mans := make([]*Manifest, 0, len(cands))
	for _, c := range cands {
		if _, loaded := m.lookup(c.Manifest.ID); loaded {
			continue
		}
		byID[c.Manifest.ID] = c
		mans = append(mans, c.Manifest)
	}
	for _, e := range m.snapshot() {
		mans = append(mans, e.m)
	}
	res := Resolve(mans)
	for id, err := range res.Failed {
		m.log.Error("plugin not loadable", "plugin", id, "err", err)
		m.setFailed(id, err)
	}
	for _, id := range res.Order {
		c, ok := byID[id]
		if !ok {
			continue
		}
		if dep, err := m.failedDependency(c.Manifest); err != nil {
			m.log.Error("plugin not loadable", "plugin", id, "dependency", dep, "err", err)
			m.setFailed(id, fmt.Errorf("dependency %s failed: %w", dep, err))
			continue
		}
		rt, err := m.loadRuntime(ctx, c)
		if err != nil {
			m.log.Error("plugin load failed", "plugin", id, "err", err)
			m.setFailed(id, err)
			continue
		}
		m.mu.Lock()
		m.entries[id] = &entry{m: c.Manifest, path: c.Path, rt: rt, state: StateLoaded}
		m.order = append(m.order, id)
		delete(m.failed, id)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.reorderLocked()
	m.mu.Unlock()
	return nil
}

// Register adds a plugin with an externally provided runtime, e.g. one hosted
// by the Java bridge. It is enabled by EnableAll like any other plugin.
func (m *Manager) Register(man *Manifest, path string, rt Runtime) error {
	if err := man.Validate(); err != nil {
		return err
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.entries[man.ID]; dup {
		return fmt.Errorf("%w: %s", ErrDuplicateID, man.ID)
	}
	m.entries[man.ID] = &entry{m: man, path: path, rt: rt, state: StateLoaded}
	m.order = append(m.order, man.ID)
	m.reorderLocked()
	return nil
}

func (m *Manager) reorderLocked() {
	mans := make([]*Manifest, 0, len(m.entries))
	for _, e := range m.entries {
		mans = append(mans, e.m)
	}
	m.order = Resolve(mans).Order
}

func (m *Manager) failedDependency(man *Manifest) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, d := range man.Depends {
		if err, ok := m.failed[d]; ok {
			return d, err
		}
	}
	return "", nil
}

func (m *Manager) loadRuntime(ctx context.Context, c Candidate) (Runtime, error) {
	bin, err := os.ReadFile(c.Wasm)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.Wasm, err)
	}
	return m.eng.LoadWASM(ctx, c.Manifest, bin, Env{
		Server:  m.srv,
		Bus:     m.bus,
		Log:     m.log,
		DataDir: m.DataDir(c.Manifest.ID),
	})
}

func (m *Manager) setFailed(id string, err error) {
	m.mu.Lock()
	m.failed[id] = err
	m.mu.Unlock()
}

func (m *Manager) lookup(id string) (*entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[id]
	return e, ok
}

func (m *Manager) snapshot() []*entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*entry, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.entries[id])
	}
	return out
}

// EnableAll enables loaded plugins in dependency order.
func (m *Manager) EnableAll(ctx context.Context) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	for _, e := range m.snapshot() {
		if e.state == StateLoaded || e.state == StateDisabled {
			if err := m.enableLocked(ctx, e); err != nil {
				m.log.Error("plugin enable failed", "plugin", e.m.ID, "err", err)
			}
		}
	}
}

// DisableAll disables plugins in reverse dependency order.
func (m *Manager) DisableAll(ctx context.Context) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	entries := m.snapshot()
	for i := len(entries) - 1; i >= 0; i-- {
		m.disableLocked(ctx, entries[i])
	}
}

func (m *Manager) Enable(ctx context.Context, id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	e, ok := m.lookup(id)
	if !ok {
		return fmt.Errorf("plugin %s not loaded", id)
	}
	return m.enableLocked(ctx, e)
}

// Disable disables id and every enabled plugin that depends on it.
func (m *Manager) Disable(ctx context.Context, id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if _, ok := m.lookup(id); !ok {
		return fmt.Errorf("plugin %s not loaded", id)
	}
	for _, e := range m.dependentsOf(id, true) {
		m.disableLocked(ctx, e)
	}
	return nil
}

func (m *Manager) enableLocked(ctx context.Context, e *entry) error {
	if e.effectiveState() == StateEnabled {
		return nil
	}
	if e.rt == nil || e.rt.Err() != nil {
		return fmt.Errorf("plugin %s has crashed; reload it", e.m.ID)
	}
	for _, d := range e.m.Depends {
		de, ok := m.lookup(d)
		if !ok || de.effectiveState() != StateEnabled {
			err := fmt.Errorf("dependency %s is not enabled", d)
			m.setState(e, StateFailed, err)
			return err
		}
	}
	m.log.Info("enabling plugin", "plugin", e.m.ID, "version", e.m.Version)
	if err := e.rt.Enable(ctx); err != nil {
		e.rt.Disable(ctx)
		m.setState(e, StateFailed, err)
		return err
	}
	m.setState(e, StateEnabled, nil)
	m.bus.Dispatch(&api.PluginEnableEvent{Plugin: e.m.Info(e.rt.Kind())})
	return nil
}

// disableLocked tears down a plugin that was enabled, including one whose
// runtime has since crashed. Plugins that never enabled are left alone.
func (m *Manager) disableLocked(ctx context.Context, e *entry) {
	if e.state != StateEnabled {
		return
	}
	crashErr := e.rt.Err()
	m.log.Info("disabling plugin", "plugin", e.m.ID)
	if crashErr == nil {
		m.bus.Dispatch(&api.PluginDisableEvent{Plugin: e.m.Info(e.rt.Kind())})
	}
	if err := e.rt.Disable(ctx); err != nil && !errors.Is(err, errDead) {
		m.log.Warn("plugin disable error", "plugin", e.m.ID, "err", err)
	}
	m.bus.UnsubscribeAll(e.m.ID)
	m.srv.Scheduler().CancelAll(e.m.ID)
	if crashErr != nil {
		m.setState(e, StateFailed, crashErr)
	} else {
		m.setState(e, StateDisabled, nil)
	}
}

func (m *Manager) setState(e *entry, s State, err error) {
	m.mu.Lock()
	e.state, e.err = s, err
	m.mu.Unlock()
}

// dependentsOf returns id plus every loaded plugin that transitively depends
// on it (hard or soft), in reverse load order when reverse is set.
func (m *Manager) dependentsOf(id string, reverse bool) []*entry {
	entries := m.snapshot()
	set := map[string]bool{id: true}
	var out []*entry
	for _, e := range entries {
		if set[e.m.ID] {
			out = append(out, e)
			continue
		}
		for _, d := range slices.Concat(e.m.Depends, e.m.SoftDepends) {
			if set[d] {
				set[e.m.ID] = true
				out = append(out, e)
				break
			}
		}
	}
	if reverse {
		slices.Reverse(out)
	}
	return out
}

// Reload disables id and its dependents, re-reads id from disk, and enables
// everything that was enabled before.
func (m *Manager) Reload(ctx context.Context, id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	target, ok := m.lookup(id)
	if !ok {
		return fmt.Errorf("plugin %s not loaded", id)
	}
	affected := m.dependentsOf(id, true)
	wasEnabled := map[string]bool{}
	for _, e := range affected {
		if st := e.effectiveState(); st == StateEnabled || st == StateFailed {
			wasEnabled[e.m.ID] = true
		}
		m.disableLocked(ctx, e)
	}
	wasEnabled[id] = true

	var cand Candidate
	var err error
	if info, statErr := os.Stat(target.path); statErr != nil {
		err = statErr
	} else if info.IsDir() {
		var ok bool
		cand, ok, err = candidateFromDir(target.path)
		if err == nil && !ok {
			err = fmt.Errorf("%s: plugin.toml missing", target.path)
		}
	} else {
		cand, err = candidateFromFile(target.path)
	}
	if err == nil && cand.Manifest.ID != id {
		err = fmt.Errorf("plugin id changed from %s to %s; restart instead", id, cand.Manifest.ID)
	}
	var rt Runtime
	if err == nil {
		rt, err = m.loadRuntime(ctx, cand)
	}
	if err != nil {
		m.setState(target, StateFailed, err)
		return fmt.Errorf("reload %s: %w", id, err)
	}
	if target.rt != nil {
		target.rt.Close(ctx)
	}
	m.mu.Lock()
	target.rt, target.m, target.state, target.err = rt, cand.Manifest, StateLoaded, nil
	m.reorderLocked()
	m.mu.Unlock()

	slices.Reverse(affected)
	var firstErr error
	for _, e := range affected {
		if !wasEnabled[e.m.ID] {
			continue
		}
		if err := m.enableLocked(ctx, e); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("reload %s: enable %s: %w", id, e.m.ID, err)
		}
	}
	return firstErr
}

// Close disables everything and releases runtimes.
func (m *Manager) Close(ctx context.Context) error {
	m.DisableAll(ctx)
	m.opMu.Lock()
	defer m.opMu.Unlock()
	for _, e := range m.snapshot() {
		if e.rt != nil {
			e.rt.Close(ctx)
		}
	}
	return m.eng.Close(ctx)
}

// JavaPlugins lists jar files found in the plugins directory for the bridge.
func (m *Manager) JavaPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.jars)
}

// Failed reports plugins that could not be loaded or enabled.
func (m *Manager) Failed() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]error, len(m.failed))
	for k, v := range m.failed {
		out[k] = v
	}
	for id, e := range m.entries {
		if e.effectiveState() == StateFailed {
			err := e.err
			if err == nil && e.rt != nil {
				err = e.rt.Err()
			}
			out[id] = err
		}
	}
	return out
}

func (m *Manager) State(id string) (State, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[id]
	if !ok {
		return 0, false
	}
	return e.effectiveState(), true
}

// Order returns plugin ids in enable order.
func (m *Manager) Order() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.order)
}

func (m *Manager) Plugins() []api.PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]api.PluginInfo, 0, len(m.order))
	for _, id := range m.order {
		e := m.entries[id]
		out = append(out, e.m.Info(e.rt.Kind()))
	}
	return out
}

func (m *Manager) Plugin(id string) (api.PluginInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[id]
	if !ok {
		return api.PluginInfo{}, false
	}
	return e.m.Info(e.rt.Kind()), true
}

func (m *Manager) IsEnabled(id string) bool {
	st, ok := m.State(id)
	return ok && st == StateEnabled
}

func (m *Manager) DataDir(id string) string { return filepath.Join(m.opts.Dir, id) }

var _ api.PluginRegistry = (*Manager)(nil)
