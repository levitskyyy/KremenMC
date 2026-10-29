// Command kremenmc runs the server.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/plugin"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/server"
	"github.com/levitskyyy/KremenMC/internal/translate"
)

func main() {
	dir := flag.String("dir", ".", "server directory (server.properties, kremenmc.toml, worlds, plugins)")
	pprof := flag.String("pprof", "", "serve net/http/pprof on this address, e.g. localhost:6060")
	level := flag.String("log-level", "info", "debug, info, warn or error")
	noConsole := flag.Bool("nogui", false, "accepted for compatibility; the console always reads stdin")
	flag.Parse()
	_ = noConsole

	var lv slog.Level
	if err := lv.UnmarshalText([]byte(*level)); err != nil {
		fmt.Fprintln(os.Stderr, "bad -log-level:", err)
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
	slog.SetDefault(log)
	if err := run(*dir, *pprof, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(dir, pprofAddr string, log *slog.Logger) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	props, eng := cfg.Server, cfg.Engine
	if pprofAddr != "" {
		go func() {
			log.Info("pprof listening", "addr", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				log.Error("pprof", "err", err)
			}
		}()
	}

	var reg translate.Registry
	if eng.Translation.Enabled {
		reg = protocolRange{translate.Default(), eng.Translation.MinProtocol, eng.Translation.MaxProtocol}
	}
	var secret []byte
	if b, err := os.ReadFile(filepath.Join(dir, "forwarding.secret")); err == nil {
		secret = []byte(strings.TrimSpace(string(b)))
		log.Info("velocity modern forwarding enabled")
	}

	srv, err := server.New(server.Config{Dir: dir, Props: props, Engine: eng, Translations: reg, VelocitySecret: secret, Logger: log})
	if err != nil {
		return err
	}
	seed, ok := props.Seed()
	if !ok {
		seed = rand.Int64()
	}
	start := time.Now()
	w, err := server.OpenWorld(srv, server.WorldConfig{
		Name:      props.LevelName,
		Dir:       filepath.Join(dir, props.LevelName),
		Seed:      seed,
		Flat:      strings.Contains(strings.ToLower(props.LevelType), "flat"),
		FlatSpec:  "",
		Workers:   eng.Workers.ChunkIO,
		TickProcs: eng.Workers.Tick,
	})
	if err != nil {
		return err
	}
	srv.AddWorld(w)
	if err := srv.OpenDimensions(w, eng.Workers.ChunkIO, eng.Workers.Tick); err != nil {
		return err
	}
	log.Info("world ready", "name", props.LevelName, "seed", w.Seed(), "spawn", w.Spawn().Pos, "took", time.Since(start).Round(time.Millisecond))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var mgr *plugin.Manager
	pluginsDir := filepath.Join(dir, eng.Plugins.Dir)
	if eng.Plugins.Enabled {
		if eng.Plugins.AutoFetch {
			fetchPlugins(ctx, dir, pluginsDir, eng, log)
		}
		cache := ""
		if eng.Plugins.CacheDir != "" {
			cache = filepath.Join(dir, eng.Plugins.CacheDir)
		}
		mgr, err = plugin.NewManager(srv, srv.Bus(), plugin.Options{
			Dir: pluginsDir, CacheDir: cache, Logger: log,
			Limits: plugin.Limits{
				MaxMemoryMB: eng.Plugins.MaxMemoryMB, CallTimeout: eng.Plugins.CallTimeout.D(),
				EnableTimeout: eng.Plugins.EnableTimeout.D(), KVMaxBytes: eng.Plugins.KVMaxBytes,
			},
		})
		if err != nil {
			return err
		}
		if err := mgr.LoadAll(ctx); err != nil {
			return err
		}
		srv.SetPlugins(mgr)
		if err := srv.Commands().Register("kremenmc", mgr.AdminCommand("kremenmc.command.plugins"), "pl"); err != nil {
			log.Warn("plugins command", "err", err)
		}
		mgr.EnableAll(ctx)
	}

	var jb *bridge.Bridge
	if eng.Java.Enabled && hasJars(pluginsDir) {
		bc := bridge.Config{PluginsDir: pluginsDir, RuntimeDir: filepath.Join(dir, "runtime"), JVMArgs: eng.Java.ExtraArgs, Logger: log}
		if eng.Java.JavaHome != "" {
			bc.JavaPath = filepath.Join(eng.Java.JavaHome, "bin", "java")
		}
		if eng.Java.MaxHeapMB > 0 {
			bc.Xmx = fmt.Sprintf("%dM", eng.Java.MaxHeapMB)
		}
		jb = bridge.New(bc, srv.BridgeHost())
		srv.SetBridge(jb)
		if err := jb.Start(ctx); err != nil {
			log.Error("java bridge disabled", "err", err)
			srv.SetBridge(nil)
			jb = nil
		}
	}

	if err := srv.Start(); err != nil {
		return err
	}
	log.Info("done", "startup", time.Since(start).Round(time.Millisecond), "version", packets.Version)
	go console(srv, log)

	select {
	case <-ctx.Done():
		log.Info("stopping")
	case <-srv.Done():
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	if mgr != nil {
		mgr.DisableAll(stopCtx)
	}
	if jb != nil {
		if err := jb.Close(stopCtx); err != nil {
			log.Warn("java bridge close", "err", err)
		}
	}
	err = srv.Stop(api.Text("Server closed"))
	if mgr != nil {
		mgr.Close(stopCtx)
	}
	return err
}

func console(srv *server.Server, log *slog.Logger) {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "/")
		if line != "" {
			srv.ConsoleCommand(line)
		}
	}
	if err := sc.Err(); err != nil {
		log.Debug("console closed", "err", err)
	}
}

func fetchPlugins(ctx context.Context, dir, pluginsDir string, eng *config.Engine, log *slog.Logger) {
	spec, err := plugin.LoadFetchSpec(filepath.Join(dir, "plugins.toml"))
	if err != nil {
		log.Error("plugins.toml", "err", err)
		return
	}
	if len(spec.Plugins) == 0 {
		return
	}
	f := &plugin.Fetcher{GameVersion: packets.Version, Dir: pluginsDir, LockPath: filepath.Join(dir, "plugins.lock"), Update: eng.Plugins.UpdateOnStart, Log: log}
	res, err := f.Sync(ctx, spec)
	if err != nil {
		log.Error("plugin fetch", "err", err)
	}
	if len(res.Fetched) > 0 || len(res.Removed) > 0 {
		log.Info("plugins synced", "fetched", res.Fetched, "removed", res.Removed)
	}
}

func hasJars(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
	return len(m) > 0
}

// protocolRange limits translation to the configured protocol window.
type protocolRange struct {
	translate.Registry
	min, max int32
}

func (r protocolRange) Lookup(p int32) (translate.Codec, bool) {
	if p < r.min || p > r.max {
		return nil, false
	}
	return r.Registry.Lookup(p)
}

func (r protocolRange) Supported() []int32 {
	var out []int32
	for _, p := range r.Registry.Supported() {
		if p >= r.min && p <= r.max {
			out = append(out, p)
		}
	}
	return out
}
