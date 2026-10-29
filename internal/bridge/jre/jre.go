// Package jre locates or provisions a Java runtime for the plugin bridge
// sidecar, downloading Eclipse Temurin from the Adoptium API when needed.
package jre

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMinVersion = 25
	DefaultAPIBase    = "https://api.adoptium.net"

	SourceSystem      = "system"
	SourceProvisioned = "provisioned"

	probeTimeout  = 30 * time.Second
	lockPoll      = 500 * time.Millisecond
	lockHeartbeat = 10 * time.Second
	lockStale     = time.Minute
)

type Options struct {
	RuntimeDir string
	MinVersion int
	APIBase    string
	HTTPClient *http.Client
	Logger     *slog.Logger
	GOOS       string
	GOARCH     string
	SkipSystem bool
	LookPath   func(string) (string, error)
	Getenv     func(string) string
}

type Runtime struct {
	JavaPath    string
	Version     int
	FullVersion string
	Source      string
}

func (o *Options) setDefaults() error {
	if o.RuntimeDir == "" {
		o.RuntimeDir = "runtime"
	}
	abs, err := filepath.Abs(o.RuntimeDir)
	if err != nil {
		return fmt.Errorf("resolve runtime dir: %w", err)
	}
	o.RuntimeDir = abs
	if o.MinVersion <= 0 {
		o.MinVersion = DefaultMinVersion
	}
	if o.APIBase == "" {
		o.APIBase = DefaultAPIBase
	}
	o.APIBase = strings.TrimRight(o.APIBase, "/")
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 15 * time.Minute}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	return nil
}

func (o *Options) exe() string {
	if o.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// Ensure returns a Java runtime of at least o.MinVersion, preferring a
// suitable system Java, then a previous install under RuntimeDir, and
// finally downloading Temurin into <RuntimeDir>/jre-<MinVersion>.
func Ensure(ctx context.Context, o Options) (*Runtime, error) {
	if err := o.setDefaults(); err != nil {
		return nil, err
	}
	if !o.SkipSystem {
		if rt := o.findSystem(ctx); rt != nil {
			return rt, nil
		}
	}
	target := filepath.Join(o.RuntimeDir, fmt.Sprintf("jre-%d", o.MinVersion))
	if rt := o.findInstalled(ctx, target); rt != nil {
		return rt, nil
	}
	if err := os.MkdirAll(o.RuntimeDir, 0o755); err != nil {
		return nil, fmt.Errorf("create runtime dir: %w", err)
	}
	unlock, err := acquireLock(ctx, target+".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if rt := o.findInstalled(ctx, target); rt != nil {
		return rt, nil
	}
	return o.provision(ctx, target)
}

func (o *Options) findSystem(ctx context.Context) *Runtime {
	var cands []string
	if home := o.Getenv("JAVA_HOME"); home != "" {
		cands = append(cands, filepath.Join(home, "bin", o.exe()))
	}
	if p, err := o.LookPath("java"); err == nil {
		cands = append(cands, p)
	}
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			continue
		}
		seen[c] = true
		rt, err := probe(ctx, c, SourceSystem)
		if err != nil {
			o.Logger.Debug("system java unusable", "path", c, "err", err)
			continue
		}
		if rt.Version >= o.MinVersion {
			o.Logger.Info("using system java", "path", c, "version", rt.FullVersion)
			return rt
		}
		o.Logger.Info("system java too old", "path", c, "version", rt.FullVersion, "need", o.MinVersion)
	}
	return nil
}

func (o *Options) findInstalled(ctx context.Context, dir string) *Runtime {
	for _, rel := range []string{
		filepath.Join("bin", o.exe()),
		filepath.Join("Contents", "Home", "bin", o.exe()),
	} {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		rt, err := probe(ctx, p, SourceProvisioned)
		if err != nil {
			o.Logger.Warn("provisioned java unusable", "path", p, "err", err)
			continue
		}
		if rt.Version >= o.MinVersion {
			return rt
		}
	}
	return nil
}

func (o *Options) provision(ctx context.Context, target string) (*Runtime, error) {
	a, err := o.fetchAsset(ctx)
	if err != nil {
		return nil, err
	}
	pkg := a.Binary.Package
	o.Logger.Info("downloading java runtime", "release", a.ReleaseName, "package", pkg.Name, "size", pkg.Size)
	archive, err := o.download(ctx, pkg)
	if err != nil {
		return nil, err
	}
	defer os.Remove(archive)

	tmp, err := os.MkdirTemp(o.RuntimeDir, ".jre-extract-")
	if err != nil {
		return nil, fmt.Errorf("create extract dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := extract(archive, tmp); err != nil {
		return nil, fmt.Errorf("extract %s: %w", pkg.Name, err)
	}
	root, err := installRoot(tmp)
	if err != nil {
		return nil, err
	}
	if err := verifySymlinks(root); err != nil {
		return nil, fmt.Errorf("extract %s: %w", pkg.Name, err)
	}

	if _, err := os.Lstat(target); err == nil {
		old := fmt.Sprintf("%s.old-%d", target, time.Now().UnixNano())
		if err := os.Rename(target, old); err != nil {
			return nil, fmt.Errorf("move aside broken runtime: %w", err)
		}
		defer os.RemoveAll(old)
	}
	if err := os.Rename(root, target); err != nil {
		if rt := o.findInstalled(ctx, target); rt != nil {
			return rt, nil
		}
		return nil, fmt.Errorf("install runtime: %w", err)
	}

	javaPath := filepath.Join(target, "bin", o.exe())
	rt, err := probe(ctx, javaPath, SourceProvisioned)
	if err != nil {
		return nil, fmt.Errorf("verify installed runtime: %w", err)
	}
	if rt.Version < o.MinVersion {
		_ = os.RemoveAll(target)
		return nil, fmt.Errorf("installed runtime is java %d, need %d", rt.Version, o.MinVersion)
	}
	o.Logger.Info("java runtime installed", "path", javaPath, "version", rt.FullVersion)
	return rt, nil
}

func probe(ctx context.Context, javaPath, source string) (*Runtime, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, javaPath, "-version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run %s -version: %w", javaPath, err)
	}
	feature, full, err := ParseVersion(string(out))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", javaPath, err)
	}
	return &Runtime{JavaPath: javaPath, Version: feature, FullVersion: full, Source: source}, nil
}

// acquireLock serialises installs across processes. The holder refreshes the
// lock's mtime so a lock left by a crashed process goes stale quickly.
func acquireLock(ctx context.Context, path string) (func(), error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			done := make(chan struct{})
			var wg sync.WaitGroup
			wg.Go(func() {
				t := time.NewTicker(lockHeartbeat)
				defer t.Stop()
				for {
					select {
					case <-done:
						return
					case now := <-t.C:
						_ = os.Chtimes(path, now, now)
					}
				}
			})
			return func() {
				close(done)
				wg.Wait()
				_ = os.Remove(path)
			}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create install lock: %w", err)
		}
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for install lock %s: %w", path, ctx.Err())
		case <-time.After(lockPoll):
		}
	}
}
