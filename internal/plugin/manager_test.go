package plugin

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

type fakeRuntime struct {
	id     string
	log    *[]string
	mu     *sync.Mutex
	failOn string
}

func (r *fakeRuntime) Kind() string { return "fake" }
func (r *fakeRuntime) Err() error   { return nil }
func (r *fakeRuntime) record(s string) error {
	r.mu.Lock()
	*r.log = append(*r.log, s+" "+r.id)
	r.mu.Unlock()
	if r.failOn == s {
		return errors.New("boom")
	}
	return nil
}
func (r *fakeRuntime) Enable(context.Context) error  { return r.record("enable") }
func (r *fakeRuntime) Disable(context.Context) error { return r.record("disable") }
func (r *fakeRuntime) Close(context.Context) error   { return nil }

func TestManagerOrdering(t *testing.T) {
	m, srv, bus, _ := newTestManager(t, DefaultLimits().CallTimeout)
	var log []string
	var mu sync.Mutex
	var enabledEvents []string
	Subscribe(bus, api.Monitor, false, func(e *api.PluginEnableEvent) { enabledEvents = append(enabledEvents, e.Plugin.ID) })
	reg := []struct {
		m      *Manifest
		failOn string
	}{
		{man("economy", dep("core")), ""},
		{man("shop", dep("economy"), soft("chat")), ""},
		{man("core"), ""},
		{man("chat"), ""},
		{man("broken", dep("core")), "enable"},
		{man("needsbroken", dep("broken")), ""},
	}
	for _, r := range reg {
		if err := m.Register(r.m, "", &fakeRuntime{id: r.m.ID, log: &log, mu: &mu, failOn: r.failOn}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	m.EnableAll(ctx)
	wantOrder := []string{"chat", "core", "broken", "economy", "needsbroken", "shop"}
	if got := m.Order(); !slices.Equal(got, wantOrder) {
		t.Fatalf("order = %v, want %v", got, wantOrder)
	}
	// broken fails its own enable and is disabled again; needsbroken never runs.
	wantLog := []string{"enable chat", "enable core", "enable broken", "disable broken", "enable economy", "enable shop"}
	if !slices.Equal(log, wantLog) {
		t.Fatalf("log = %v, want %v", log, wantLog)
	}
	if !slices.Equal(enabledEvents, []string{"chat", "core", "economy", "shop"}) {
		t.Errorf("PluginEnableEvents = %v", enabledEvents)
	}
	if f := m.Failed(); len(f) != 2 || f["broken"] == nil || f["needsbroken"] == nil {
		t.Errorf("failed = %v", f)
	}

	log = nil
	if err := m.Disable(ctx, "core"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"disable shop", "disable economy", "disable core"}; !slices.Equal(log, want) {
		t.Errorf("cascade disable = %v, want %v", log, want)
	}
	if m.IsEnabled("economy") || !m.IsEnabled("chat") {
		t.Error("wrong enabled set after cascade")
	}
	if err := m.Enable(ctx, "economy"); err == nil {
		t.Error("enable with disabled dependency should fail")
	}

	cmd := m.AdminCommand("kremenmc.plugins")
	srv.cmds.Register("kremenmc", cmd)
	console := srv.console
	n, err := srv.cmds.Dispatch(console, "plugins enable core")
	if err != nil || n != 1 || !m.IsEnabled("core") {
		t.Fatalf("admin enable: %d %v", n, err)
	}
	if _, err := srv.cmds.Dispatch(console, "plugins"); err != nil {
		t.Fatal(err)
	}
	msgs := console.messages()
	if last := msgs[len(msgs)-1]; !strings.HasPrefix(last, "Plugins (6): ") || !strings.Contains(last, "needsbroken") {
		t.Errorf("list = %q", last)
	}
	if _, err := srv.cmds.Dispatch(console, "plugins reload nope"); err == nil {
		t.Error("reload of unknown plugin should fail")
	}
}
