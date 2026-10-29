package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

const vanillaTime = "2006-01-02 15:04:05 -0700"

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type opEntry struct {
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Level               int    `json:"level"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit"`
}

// opList is ops.json.
type opList struct {
	path  string
	level int
	log   *slog.Logger
	mu    sync.RWMutex
	ops   map[api.UUID]opEntry
}

func loadOps(path string, level int, log *slog.Logger) (*opList, error) {
	l := &opList{path: path, level: level, log: log, ops: map[api.UUID]opEntry{}}
	var list []opEntry
	if err := readJSON(path, &list); err != nil {
		return l, err
	}
	for _, e := range list {
		if u, err := api.ParseUUID(e.UUID); err == nil {
			l.ops[u] = e
		}
	}
	return l, nil
}

func (l *opList) get(u api.UUID) (opEntry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	e, ok := l.ops[u]
	return e, ok
}

func (l *opList) set(u api.UUID, name string, op bool) {
	l.mu.Lock()
	if op {
		l.ops[u] = opEntry{UUID: u.String(), Name: name, Level: l.level}
	} else {
		delete(l.ops, u)
	}
	list := make([]opEntry, 0, len(l.ops))
	for _, e := range l.ops {
		list = append(list, e)
	}
	l.mu.Unlock()
	slices.SortFunc(list, func(a, b opEntry) int { return strings.Compare(a.Name, b.Name) })
	if err := writeJSON(l.path, list); err != nil {
		l.log.Error("save ops", "err", err)
	}
}

type wlEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// whitelist is whitelist.json; it implements api.Whitelist.
type whitelist struct {
	path    string
	log     *slog.Logger
	mu      sync.RWMutex
	enabled bool
	m       map[api.UUID]string
	onState func(bool)
}

func loadWhitelist(path string, enabled bool, log *slog.Logger) (*whitelist, error) {
	w := &whitelist{path: path, log: log, enabled: enabled, m: map[api.UUID]string{}}
	var list []wlEntry
	err := readJSON(path, &list)
	for _, e := range list {
		if u, err := api.ParseUUID(e.UUID); err == nil {
			w.m[u] = e.Name
		}
	}
	return w, err
}

func (w *whitelist) Enabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled
}

func (w *whitelist) SetEnabled(v bool) {
	w.mu.Lock()
	w.enabled = v
	f := w.onState
	w.mu.Unlock()
	if f != nil {
		f(v)
	}
}

func (w *whitelist) Add(p api.GameProfile) {
	w.mu.Lock()
	w.m[p.UUID] = p.Name
	w.mu.Unlock()
	w.save()
}

func (w *whitelist) Remove(id api.UUID) bool {
	w.mu.Lock()
	_, ok := w.m[id]
	delete(w.m, id)
	w.mu.Unlock()
	if ok {
		w.save()
	}
	return ok
}

func (w *whitelist) Contains(id api.UUID) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.m[id]
	return ok
}

func (w *whitelist) Entries() []api.GameProfile {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]api.GameProfile, 0, len(w.m))
	for u, n := range w.m {
		out = append(out, api.GameProfile{UUID: u, Name: n})
	}
	return out
}

func (w *whitelist) save() {
	w.mu.RLock()
	list := make([]wlEntry, 0, len(w.m))
	for u, n := range w.m {
		list = append(list, wlEntry{u.String(), n})
	}
	w.mu.RUnlock()
	slices.SortFunc(list, func(a, b wlEntry) int { return strings.Compare(a.Name, b.Name) })
	if err := writeJSON(w.path, list); err != nil {
		w.log.Error("save whitelist", "err", err)
	}
}

type banJSON struct {
	UUID    string `json:"uuid,omitempty"`
	IP      string `json:"ip,omitempty"`
	Name    string `json:"name,omitempty"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason"`
}

// banList is banned-players.json or banned-ips.json; it implements api.BanList.
type banList struct {
	path string
	ip   bool
	log  *slog.Logger
	mu   sync.RWMutex
	m    map[string]api.BanEntry
}

func loadBans(path string, ip bool, log *slog.Logger) (*banList, error) {
	b := &banList{path: path, ip: ip, log: log, m: map[string]api.BanEntry{}}
	var list []banJSON
	err := readJSON(path, &list)
	for _, j := range list {
		e := api.BanEntry{Target: j.UUID, Name: j.Name, Reason: j.Reason, Source: j.Source}
		if ip {
			e.Target = j.IP
		}
		e.Created, _ = time.Parse(vanillaTime, j.Created)
		if j.Expires != "forever" {
			e.Expires, _ = time.Parse(vanillaTime, j.Expires)
		}
		b.m[strings.ToLower(e.Target)] = e
	}
	return b, err
}

func (b *banList) Ban(e api.BanEntry) {
	if e.Created.IsZero() {
		e.Created = time.Now()
	}
	if e.Source == "" {
		e.Source = "Server"
	}
	if e.Reason == "" {
		e.Reason = "Banned by an operator."
	}
	b.mu.Lock()
	b.m[strings.ToLower(e.Target)] = e
	b.mu.Unlock()
	b.save()
}

func (b *banList) Pardon(target string) bool {
	b.mu.Lock()
	k := strings.ToLower(target)
	_, ok := b.m[k]
	delete(b.m, k)
	b.mu.Unlock()
	if ok {
		b.save()
	}
	return ok
}

func (b *banList) Get(target string) (api.BanEntry, bool) {
	b.mu.RLock()
	e, ok := b.m[strings.ToLower(target)]
	b.mu.RUnlock()
	if ok && !e.Expires.IsZero() && time.Now().After(e.Expires) {
		b.Pardon(target)
		return api.BanEntry{}, false
	}
	return e, ok
}

func (b *banList) Entries() []api.BanEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]api.BanEntry, 0, len(b.m))
	for _, e := range b.m {
		out = append(out, e)
	}
	return out
}

func (b *banList) save() {
	b.mu.RLock()
	list := make([]banJSON, 0, len(b.m))
	for _, e := range b.m {
		j := banJSON{Name: e.Name, Created: e.Created.Format(vanillaTime), Source: e.Source, Reason: e.Reason, Expires: "forever"}
		if b.ip {
			j.IP = e.Target
		} else {
			j.UUID = e.Target
		}
		if !e.Expires.IsZero() {
			j.Expires = e.Expires.Format(vanillaTime)
		}
		list = append(list, j)
	}
	b.mu.RUnlock()
	if err := writeJSON(b.path, list); err != nil {
		b.log.Error("save bans", "path", b.path, "err", err)
	}
}
