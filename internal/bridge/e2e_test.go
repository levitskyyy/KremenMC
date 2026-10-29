package bridge

import (
	"archive/zip"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/bridge/embed"
	"github.com/levitskyyy/KremenMC/internal/bridge/jre"
)

const testPluginJava = `package testplugin;

import io.papermc.paper.event.player.AsyncChatEvent;
import net.kyori.adventure.text.Component;
import org.bukkit.Bukkit;
import org.bukkit.Material;
import org.bukkit.event.EventHandler;
import org.bukkit.event.EventPriority;
import org.bukkit.event.Listener;
import org.bukkit.event.block.BlockBreakEvent;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.plugin.java.JavaPlugin;

public final class TestPlugin extends JavaPlugin implements Listener {
    @Override
    public void onEnable() {
        getServer().getPluginManager().registerEvents(this, this);
        getCommand("hello").setExecutor((sender, cmd, label, args) -> {
            sender.sendMessage("hello " + String.join(",", args));
            return true;
        });
        Bukkit.getScheduler().runTaskLater(this, () -> Bukkit.broadcastMessage("tick task"), 2);
        Bukkit.getGlobalRegionScheduler().runDelayed(this, t -> Bukkit.broadcast(Component.text("folia task")), 3);
        getLogger().info("test plugin enabled on " + Bukkit.getName());
    }

    @EventHandler
    public void onJoin(PlayerJoinEvent e) {
        e.getPlayer().sendMessage(Component.text("Welcome " + e.getPlayer().getName()));
        e.joinMessage(Component.text(e.getPlayer().getName() + " arrived"));
    }

    @EventHandler(priority = EventPriority.HIGH, ignoreCancelled = true)
    public void onBreak(BlockBreakEvent e) {
        if (e.getBlock().getType() == Material.BEDROCK) e.setCancelled(true);
    }

    @EventHandler
    public void onChat(AsyncChatEvent e) {
        e.message(Component.text("[filtered]"));
    }
}
`

const testPluginYML = `name: TestPlugin
main: testplugin.TestPlugin
version: 1.0
api-version: '26.3'
commands:
  hello:
    description: say hello
    aliases: [hi]
`

type fakeHost struct {
	mu       sync.Mutex
	players  map[UUID]PlayerInfo
	messages chan [2]string
	commands chan CommandSpec
	bcast    chan string
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		players:  map[UUID]PlayerInfo{},
		messages: make(chan [2]string, 64),
		commands: make(chan CommandSpec, 16),
		bcast:    make(chan string, 16),
	}
}

func (h *fakeHost) ServerInfo() ServerInfo {
	return ServerInfo{Name: "kremenmc", Version: "test", MinecraftVersion: "26.3", MaxPlayers: 10, Port: 25565}
}

func (h *fakeHost) Players() []PlayerInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []PlayerInfo
	for _, p := range h.players {
		out = append(out, p)
	}
	return out
}

func (h *fakeHost) Player(id UUID) (PlayerInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.players[id]
	return p, ok
}

func (h *fakeHost) PlayerByName(name string) (PlayerInfo, bool) {
	for _, p := range h.Players() {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return PlayerInfo{}, false
}

func (h *fakeHost) SendMessage(target UUID, json string) {
	h.messages <- [2]string{target.String(), json}
}
func (h *fakeHost) Broadcast(json, perm string) int { h.bcast <- json; return len(h.Players()) }
func (h *fakeHost) Kick(UUID, string)               {}
func (h *fakeHost) Teleport(UUID, Location) bool    { return true }
func (h *fakeHost) HasPermission(UUID, string) bool { return true }
func (h *fakeHost) Worlds() []WorldInfo {
	return []WorldInfo{{Name: "world", Environment: "NORMAL", MinY: -64, MaxY: 320}}
}
func (h *fakeHost) Block(string, int, int, int) (string, bool)   { return "minecraft:stone", true }
func (h *fakeHost) SetBlock(string, int, int, int, string, bool) {}
func (h *fakeHost) RegisterCommand(c CommandSpec)                { h.commands <- c }
func (h *fakeHost) DispatchCommand(UUID, string) bool            { return false }

func e2ePrereqs(t *testing.T) (java, javac, libsDir string) {
	t.Helper()
	if testing.Short() {
		t.Skip("short mode")
	}
	if embed.BridgeJar() == nil {
		t.Skip("kremenmc-bridge.jar not built (go run ./tools/bridgebuild)")
	}
	var err error
	if java, err = exec.LookPath("java"); err != nil {
		t.Skip("no java on PATH")
	}
	if javac, err = exec.LookPath("javac"); err != nil {
		t.Skip("no javac on PATH")
	}
	out, _ := exec.Command(java, "-version").CombinedOutput()
	if v, _, err := jre.ParseVersion(string(out)); err != nil || v < jre.DefaultMinVersion {
		t.Skipf("java %d < %d", v, jre.DefaultMinVersion)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	libsDir = filepath.Join(cache, "kremenmc-bridgebuild", "libs")
	for _, l := range Libs {
		if ok, _ := FileHasSHA256(filepath.Join(libsDir, l.FileName()), l.SHA256); !ok {
			t.Skipf("library %s not cached in %s (go run ./tools/bridgebuild -fetch)", l.Coord(), libsDir)
		}
	}
	return java, javac, libsDir
}

func buildTestPlugin(t *testing.T, javac, libsDir, out string) {
	t.Helper()
	src := t.TempDir()
	srcFile := filepath.Join(src, "testplugin", "TestPlugin.java")
	os.MkdirAll(filepath.Dir(srcFile), 0o755)
	os.WriteFile(srcFile, []byte(testPluginJava), 0o644)
	classes := filepath.Join(src, "classes")
	var cp []string
	for _, l := range Libs {
		cp = append(cp, filepath.Join(libsDir, l.FileName()))
	}
	cmd := exec.Command(javac, "--release", "25", "-nowarn", "-cp", strings.Join(cp, string(os.PathListSeparator)), "-d", classes, srcFile)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, b)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, _ := zw.Create("plugin.yml")
	w.Write([]byte(testPluginYML))
	filepath.WalkDir(classes, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(classes, p)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, _ := os.ReadFile(p)
		_, err = w.Write(b)
		return err
	})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitFor[T any](t *testing.T, ch <-chan T, what string, ok func(T) bool) T {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case v := <-ch:
			if ok(v) {
				return v
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestEndToEnd(t *testing.T) {
	java, javac, libsDir := e2ePrereqs(t)
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	os.MkdirAll(plugins, 0o755)
	buildTestPlugin(t, javac, libsDir, filepath.Join(plugins, "test.jar"))

	host := newFakeHost()
	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b := New(Config{
		PluginsDir:  plugins,
		RuntimeDir:  filepath.Join(root, "runtime"),
		LibsDir:     libsDir,
		JavaPath:    java,
		Xmx:         "256M",
		EventBudget: 5 * time.Second,
		Logger:      logger,
	}, host)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if err := b.Close(cctx); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	ps := b.Plugins()
	if len(ps) != 1 || ps[0].Name != "TestPlugin" || ps[0].State != "enabled" {
		t.Fatalf("plugins: %+v", ps)
	}
	cmd := waitFor(t, host.commands, "command registration", func(c CommandSpec) bool { return c.Name == "hello" })
	if cmd.Plugin != "TestPlugin" || len(cmd.Aliases) != 1 || cmd.Aliases[0] != "hi" {
		t.Fatalf("command: %+v", cmd)
	}
	for _, ev := range []string{EventJoin, EventBlockBreak, EventChat} {
		if !b.HasListeners(ev) {
			t.Fatalf("no listeners reported for %s", ev)
		}
	}
	if b.HasListeners(EventMove) {
		t.Fatal("unexpected listener for PlayerMove")
	}

	steve, _ := ParseUUID("069a79f4-44e9-4726-a5be-fca90e38aaf5")
	host.mu.Lock()
	host.players[steve] = PlayerInfo{UUID: steve, Name: "Steve", Location: Location{World: "world", Y: 64}, GameMode: "SURVIVAL", Address: "127.0.0.1:50000"}
	host.mu.Unlock()

	cancelled, mutated := b.FireEvent(ctx, Event{Name: EventJoin, Fields: map[string]any{"player": steve, "name": "Steve", "joinMessage": `{"text":"Steve joined"}`}})
	if cancelled {
		t.Fatal("join cancelled")
	}
	msg := waitFor(t, host.messages, "welcome message", func(m [2]string) bool { return strings.Contains(m[1], "Welcome Steve") })
	if msg[0] != steve.String() {
		t.Fatalf("welcome sent to %s", msg[0])
	}
	if jm, _ := mutated["joinMessage"].(string); !strings.Contains(jm, "Steve arrived") {
		t.Fatalf("join mutation: %v", mutated)
	}

	cancelled, _ = b.FireEvent(ctx, Event{Name: EventBlockBreak, Fields: map[string]any{"player": steve, "world": "world", "x": 1, "y": 2, "z": 3, "block": "minecraft:bedrock"}})
	if !cancelled {
		t.Fatal("bedrock break not cancelled")
	}
	cancelled, _ = b.FireEvent(ctx, Event{Name: EventBlockBreak, Fields: map[string]any{"player": steve, "world": "world", "x": 1, "y": 2, "z": 3, "block": "minecraft:stone"}})
	if cancelled {
		t.Fatal("stone break cancelled")
	}

	_, mutated = b.FireEvent(ctx, Event{Name: EventChat, Async: true, Fields: map[string]any{"player": steve, "message": "bad words"}})
	if m, _ := mutated["message"].(string); !strings.Contains(m, "[filtered]") {
		t.Fatalf("chat mutation: %v", mutated)
	}

	ok, err := b.Command(ctx, steve, "hi a b")
	if err != nil || !ok {
		t.Fatalf("command: %v %v", ok, err)
	}
	waitFor(t, host.messages, "command reply", func(m [2]string) bool { return strings.Contains(m[1], "hello a,b") })

	comps, err := b.TabComplete(ctx, UUID{}, "hel")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(comps, "hello") {
		t.Fatalf("tab completions: %v", comps)
	}

	done := make(chan struct{})
	go func() {
		for i := int64(1); ; i++ {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
				b.Tick(i)
			}
		}
	}()
	defer close(done)
	waitFor(t, host.bcast, "scheduled broadcast", func(s string) bool { return strings.Contains(s, "tick task") })
	waitFor(t, host.bcast, "folia broadcast", func(s string) bool { return strings.Contains(s, "folia task") })

	b.mu.Lock()
	sup := b.sup
	b.mu.Unlock()
	sup.mu.Lock()
	old := sup.cmd
	sup.mu.Unlock()
	old.Process.Kill()
	deadline := time.Now().Add(30 * time.Second)
	for {
		sup.mu.Lock()
		cur := sup.cmd
		sup.mu.Unlock()
		if cur != old && b.Running() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("jvm not restarted after crash")
		}
		time.Sleep(50 * time.Millisecond)
	}
	b.FireEvent(ctx, Event{Name: EventJoin, Fields: map[string]any{"player": steve, "name": "Steve"}})
	waitFor(t, host.messages, "welcome after restart", func(m [2]string) bool { return strings.Contains(m[1], "Welcome Steve") })

	cancelled, _ = b.FireEvent(ctx, Event{Name: EventQuit, Fields: map[string]any{"player": steve, "name": "Steve", "quitMessage": `{"text":"bye"}`, "reason": "DISCONNECTED"}})
	if cancelled {
		t.Fatal("quit cancelled")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
