package plugin

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

var (
	wasmCacheDir string
	buildMu      sync.Mutex
	built        = map[string][]byte{}
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kremenmc-wasm-cache")
	if err != nil {
		panic(err)
	}
	wasmCacheDir = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// buildWASM compiles a Go package to a wasip1 reactor once per test binary.
func buildWASM(t *testing.T, pkg string) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("wasm build skipped in -short mode")
	}
	buildMu.Lock()
	defer buildMu.Unlock()
	if b, ok := built[pkg]; ok {
		return b
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not in PATH")
	}
	out := filepath.Join(t.TempDir(), "p.wasm")
	cmd := exec.Command(gobin, "build", "-buildmode=c-shared", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "GOFLAGS=")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, msg)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	built[pkg] = b
	return b
}

// installDir lays a plugin out as plugins/<id>/{plugin.toml,<main>}.
func installDir(t *testing.T, pluginsDir, manifestPath string, bin []byte) {
	t.Helper()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(pluginsDir, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, m.Main), bin, 0o644); err != nil {
		t.Fatal(err)
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestManager(t *testing.T, timeout time.Duration) (*Manager, *fakeServer, *Bus, string) {
	t.Helper()
	srv := newFakeServer()
	bus := NewBus(quietLogger())
	dir := t.TempDir()
	lim := DefaultLimits()
	lim.CallTimeout = timeout
	m, err := NewManager(srv, bus, Options{Dir: dir, CacheDir: wasmCacheDir, Limits: lim, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m, srv, bus, dir
}

func loadEnable(t *testing.T, m *Manager) {
	t.Helper()
	ctx := context.Background()
	if err := m.LoadAll(ctx); err != nil {
		t.Fatal(err)
	}
	m.EnableAll(ctx)
	if f := m.Failed(); len(f) > 0 {
		t.Fatalf("failed plugins: %v", f)
	}
}

func TestWASMHelloPlugin(t *testing.T) {
	bin := buildWASM(t, "github.com/levitskyyy/KremenMC/examples/plugins/hello")
	m, srv, bus, dir := newTestManager(t, 5*time.Second)
	installDir(t, dir, "../../examples/plugins/hello/plugin.toml", bin)
	loadEnable(t, m)
	ctx := context.Background()

	if !m.IsEnabled("hello") {
		t.Fatal("hello not enabled")
	}
	if !srv.perms.has("hello.use") {
		t.Error("manifest permission not registered")
	}
	for _, name := range []string{"hello", "hi"} {
		if !srv.cmds.has(name) {
			t.Errorf("command %q not registered", name)
		}
	}

	steve := srv.addPlayer("Steve")
	bus.Dispatch(&api.PlayerJoinEvent{Player: steve})
	bus.Dispatch(&api.PlayerJoinEvent{Player: steve})
	want := []string{"Hello, Steve!", "Welcome back, Steve (visit #2)"}
	if got := steve.messages(); !slices.Equal(got, want) {
		t.Fatalf("join messages = %q, want %q", got, want)
	}

	cmds := []struct {
		line string
		want string
	}{
		{"hello", "Hello, Steve!"},
		{"hello world", "Hello, world!"},
		{"hi server", "Hello, server!"},
	}
	for _, c := range cmds {
		n, err := srv.cmds.Dispatch(steve, c.line)
		if err != nil || n != 1 {
			t.Fatalf("dispatch %q = %d, %v", c.line, n, err)
		}
		msgs := steve.messages()
		if got := msgs[len(msgs)-1]; got != c.want {
			t.Errorf("%q replied %q, want %q", c.line, got, c.want)
		}
	}

	if err := m.Reload(ctx, "hello"); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !m.IsEnabled("hello") || !srv.cmds.has("hello") {
		t.Fatal("hello not re-enabled after reload")
	}
	bus.Dispatch(&api.PlayerJoinEvent{Player: steve})
	if msgs := steve.messages(); msgs[len(msgs)-1] != "Welcome back, Steve (visit #3)" {
		t.Errorf("after reload got %q; kv should persist across reloads", msgs[len(msgs)-1])
	}

	if err := m.Disable(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if srv.cmds.has("hello") || srv.perms.has("hello.use") {
		t.Error("commands/permissions not removed on disable")
	}
	before := len(steve.messages())
	bus.Dispatch(&api.PlayerJoinEvent{Player: steve})
	if len(steve.messages()) != before {
		t.Error("disabled plugin still receives events")
	}

	if err := m.Enable(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	bus.Dispatch(&api.PlayerJoinEvent{Player: steve})
	if len(steve.messages()) != before+1 || !srv.cmds.has("hi") {
		t.Error("re-enable did not restore listeners and commands")
	}
}

func TestWASMEmbeddedManifest(t *testing.T) {
	bin := buildWASM(t, "github.com/levitskyyy/KremenMC/examples/plugins/hello")
	raw, err := os.ReadFile("../../examples/plugins/hello/plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	packed, err := wire.EmbedManifest(bin, raw)
	if err != nil {
		t.Fatal(err)
	}
	m, srv, bus, dir := newTestManager(t, 5*time.Second)
	if err := os.WriteFile(filepath.Join(dir, "hello.wasm"), packed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SomeBukkitPlugin.jar"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	loadEnable(t, m)
	p := srv.addPlayer("Alex")
	bus.Dispatch(&api.PlayerJoinEvent{Player: p})
	if got := p.messages(); len(got) != 1 || got[0] != "Hello, Alex!" {
		t.Fatalf("messages = %q", got)
	}
	if jars := m.JavaPlugins(); len(jars) != 1 || filepath.Base(jars[0]) != "SomeBukkitPlugin.jar" {
		t.Errorf("java plugins = %v", jars)
	}
}

func TestWASMEventMutation(t *testing.T) {
	bin := buildWASM(t, "github.com/levitskyyy/KremenMC/internal/plugin/testdata/testplug")
	m, srv, bus, dir := newTestManager(t, 5*time.Second)
	installDir(t, dir, "testdata/testplug/plugin.toml", bin)
	loadEnable(t, m)
	p := srv.addPlayer("Steve")

	chat := []struct {
		in, want  string
		cancelled bool
	}{
		{"hi", "HI!", false},
		{"please block this", "please block this", true},
	}
	for _, c := range chat {
		e := Fire(bus, &api.PlayerChatEvent{Player: p, Message: c.in})
		if e.Message != c.want || e.Cancelled() != c.cancelled {
			t.Errorf("chat %q -> %q cancelled=%v, want %q cancelled=%v", c.in, e.Message, e.Cancelled(), c.want, c.cancelled)
		}
	}

	mv := Fire(bus, &api.PlayerMoveEvent{Player: p, To: api.Location{World: "world", Pos: api.Vec3{Y: -5}}})
	if mv.To.Pos.Y != 64 || mv.To.World != "world" {
		t.Errorf("move To = %+v", mv.To)
	}

	msg := api.Text("joined")
	j := Fire(bus, &api.PlayerJoinEvent{Player: p, Message: &msg})
	if j.Message != nil {
		t.Errorf("join message not cleared: %v", j.Message)
	}
	Fire(bus, &api.PlayerJoinEvent{Player: srv.addPlayer("panic"), Message: &msg})
	if !m.IsEnabled("testplug") {
		t.Fatal("guest panic in a listener must not disable the plugin")
	}

	srv.sched.tick()
	srv.sched.tick()
	kv, err := OpenFileKV(filepath.Join(dir, "testplug", "kv.bin"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := kv.Get("ticks"); !ok || len(v) != 1 || v[0] != 2 {
		t.Errorf("ticks = %v %v, want [2]", v, ok)
	}
}

func TestWASMCrashIsolation(t *testing.T) {
	bin := buildWASM(t, "github.com/levitskyyy/KremenMC/internal/plugin/testdata/testplug")
	cases := []struct {
		name string
		cmd  string
	}{
		{"timeout", "spin"},
		{"exit", "die"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, srv, bus, dir := newTestManager(t, 300*time.Millisecond)
			installDir(t, dir, "testdata/testplug/plugin.toml", bin)
			loadEnable(t, m)
			p := srv.addPlayer("Steve")

			if _, err := srv.cmds.Dispatch(p, c.cmd); err == nil {
				t.Fatal("expected error from crashed command")
			}
			if m.IsEnabled("testplug") {
				t.Fatal("plugin still enabled after crash")
			}
			if _, ok := m.Failed()["testplug"]; !ok {
				t.Error("crash not reported in Failed()")
			}
			waitFor(t, func() bool { return !srv.cmds.has("spin") && !bus.HasHandlers(api.KindPlayerChat) })
			e := Fire(bus, &api.PlayerChatEvent{Player: p, Message: "hi"})
			if e.Message != "hi" {
				t.Error("crashed plugin still handled events")
			}

			if err := m.Reload(context.Background(), "testplug"); err != nil {
				t.Fatalf("reload after crash: %v", err)
			}
			e = Fire(bus, &api.PlayerChatEvent{Player: p, Message: "hi"})
			if e.Message != "HI!" {
				t.Errorf("after reload chat = %q", e.Message)
			}
		})
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
