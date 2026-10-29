package bridge

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	backoffMin    = time.Second
	backoffMax    = time.Minute
	stableUptime  = time.Minute
	helloTimeout  = 30 * time.Second
	shutdownGrace = 15 * time.Second
)

type supervisor struct {
	b          *Bridge
	ln         net.Listener
	addr       string
	unlisten   func()
	token      string
	java       string
	classpath  []string
	jars       []string
	pluginsDir string

	conns     chan *Conn
	ready     chan error
	readyOnce sync.Once
	stop      chan struct{}
	stopOnce  sync.Once
	exited    chan struct{}

	mu  sync.Mutex
	cmd *exec.Cmd
}

// listen opens the IPC endpoint: a unix socket next to the runtime dir, or
// TCP loopback on Windows (authenticated by the token either way). The
// returned cleanup removes the socket file.
func listen(runtimeDir string) (net.Listener, string, func(), error) {
	if runtime.GOOS == "windows" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, "", nil, err
		}
		return ln, "tcp:" + ln.Addr().String(), func() {}, nil
	}
	path := filepath.Join(absPath(runtimeDir), fmt.Sprintf("bridge-%d.sock", os.Getpid()))
	cleanup := func() { _ = os.Remove(path) }
	if len(path) > 100 {
		dir, err := os.MkdirTemp("", "kremenmc")
		if err != nil {
			return nil, "", nil, err
		}
		path = filepath.Join(dir, "bridge.sock")
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, "unix:" + path, cleanup, nil
}

func (s *supervisor) log() *slog.Logger { return s.b.log }

func (s *supervisor) signalReady(err error) {
	s.readyOnce.Do(func() { s.ready <- err })
}

func (s *supervisor) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *supervisor) run() {
	defer close(s.exited)
	defer s.cleanup()
	go s.acceptLoop()
	backoff := backoffMin
	started := false
	for !s.stopping() {
		start := time.Now()
		ok, err := s.runOnce()
		started = started || ok
		if !started {
			s.signalReady(err)
			s.stopOnce.Do(func() { close(s.stop) })
			return
		}
		if s.stopping() {
			return
		}
		if time.Since(start) > stableUptime {
			backoff = backoffMin
		}
		s.log().Error("java bridge exited; restarting", "err", err, "in", backoff)
		select {
		case <-s.stop:
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, backoffMax)
	}
}

func (s *supervisor) cleanup() {
	_ = s.ln.Close()
	s.unlisten()
}

func (s *supervisor) jvmArgs() []string {
	cfg := s.b.cfg
	args := []string{"-Xms" + cfg.Xms, "-Xmx" + cfg.Xmx}
	if strings.EqualFold(cfg.GC, "g1") {
		args = append(args, "-XX:+UseG1GC", "-XX:MaxGCPauseMillis=50", "-XX:+ParallelRefProcEnabled")
	} else {
		args = append(args, "-XX:+UseZGC")
	}
	args = append(args,
		"-XX:+UseCompactObjectHeaders",
		"-XX:+ExitOnOutOfMemoryError",
		"-XX:+PerfDisableSharedMem",
		"-Dfile.encoding=UTF-8",
		"-Djava.awt.headless=true",
		"--enable-native-access=ALL-UNNAMED",
	)
	args = append(args, cfg.JVMArgs...)
	return append(args, "-cp", strings.Join(s.classpath, string(os.PathListSeparator)), "kremenmc.bridge.Main")
}

func (s *supervisor) runOnce() (connected bool, err error) {
	cmd := exec.Command(s.java, s.jvmArgs()...)
	cmd.Env = append(os.Environ(), "KREMENMC_BRIDGE_ADDR="+s.addr, "KREMENMC_BRIDGE_TOKEN="+s.token)
	cmd.Dir = filepath.Dir(s.pluginsDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start jvm: %w", err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	var pipes sync.WaitGroup
	pipes.Add(2)
	go func() { defer pipes.Done(); s.pump(stdout, false) }()
	go func() { defer pipes.Done(); s.pump(stderr, true) }()
	waitErr := make(chan error, 1)
	go func() {
		pipes.Wait()
		waitErr <- cmd.Wait()
	}()

	conn, err := s.accept(waitErr)
	if err != nil {
		_ = cmd.Process.Kill()
		<-waitErr
		return false, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), s.b.cfg.StartTimeout)
	res, err := conn.Call(ctx, OpLoadPlugins, toAnySlice(s.jars), s.pluginsDir)
	cancel()
	if err != nil {
		s.log().Error("loading java plugins failed", "err", err)
		_ = cmd.Process.Kill()
		<-waitErr
		return false, err
	}
	for _, v := range ArgList(res, 0) {
		if m, ok := v.(map[string]any); ok {
			s.b.setState(yStr(m["name"]), yStr(m["state"]))
		}
	}
	s.b.conn.Store(conn)
	s.signalReady(nil)
	defer s.b.conn.CompareAndSwap(conn, nil)

	select {
	case err := <-waitErr:
		return true, fmt.Errorf("jvm exited: %w", errOrExit(err))
	case <-conn.Done():
		select {
		case err := <-waitErr:
			return true, fmt.Errorf("jvm exited: %w", errOrExit(err))
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-waitErr
			return true, errors.New("ipc connection lost; jvm killed")
		}
	}
}

func errOrExit(err error) error {
	if err == nil {
		return errors.New("exit status 0")
	}
	return err
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (s *supervisor) acceptLoop() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			if s.stopping() || errors.Is(err, net.ErrClosed) {
				return
			}
			s.log().Warn("bridge accept failed", "err", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go func() {
			c, err := s.handshake(nc)
			if err != nil {
				s.log().Warn("rejected bridge connection", "err", err)
				return
			}
			select {
			case s.conns <- c:
			case <-time.After(helloTimeout):
				c.Close()
			}
		}()
	}
}

// accept waits for the JVM to connect and authenticate with OpHello.
func (s *supervisor) accept(waitErr chan error) (*Conn, error) {
	select {
	case c := <-s.conns:
		return c, nil
	case err := <-waitErr:
		waitErr <- err
		return nil, fmt.Errorf("jvm exited before connecting: %w", errOrExit(err))
	case <-time.After(helloTimeout):
		return nil, errors.New("jvm did not connect in time")
	case <-s.stop:
		return nil, errors.New("stopping")
	}
}

func (s *supervisor) handshake(nc net.Conn) (*Conn, error) {
	hello := make(chan error, 1)
	report := func(err error) {
		select {
		case hello <- err:
		default:
		}
	}
	var authed atomic.Bool
	c := NewConn(nc, Handlers{
		Call: func(ctx context.Context, op uint16, args []any) ([]any, error) {
			if op == OpHello {
				tok := ArgString(args, 0)
				if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
					report(errors.New("bad token"))
					return nil, errors.New("bad token")
				}
				if v := ArgInt(args, 1); v != ProtocolVersion {
					report(fmt.Errorf("protocol version %d, want %d", v, ProtocolVersion))
					return nil, fmt.Errorf("unsupported protocol version %d", v)
				}
				authed.Store(true)
				s.log().Info("java bridge connected", "java", ArgString(args, 2))
				report(nil)
				return []any{s.b.serverInfo()}, nil
			}
			if !authed.Load() {
				return nil, errors.New("not authenticated")
			}
			return s.b.onCall(ctx, op, args)
		},
		Notify: func(op uint16, args []any) {
			if authed.Load() {
				s.b.onNotify(op, args)
			}
		},
	})
	go func() {
		_ = c.Serve()
		report(errors.New("connection closed before hello"))
	}()
	select {
	case err := <-hello:
		if err != nil {
			time.AfterFunc(time.Second, func() { c.Close() })
			return nil, err
		}
		return c, nil
	case <-time.After(helloTimeout):
		c.Close()
		return nil, errors.New("hello timeout")
	}
}

// pump forwards JVM output lines to slog. Lines written by the bridge's
// logging handler look like "LEVEL|logger|message".
func (s *supervisor) pump(r io.Reader, stderr bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	log := s.log().With("src", "java")
	for sc.Scan() {
		line := sc.Text()
		level := slog.LevelInfo
		if stderr {
			level = slog.LevelWarn
		}
		logger := ""
		if parts := strings.SplitN(line, "|", 3); len(parts) == 3 && isLevel(parts[0]) {
			level = javaLevel(parts[0])
			logger, line = parts[1], parts[2]
		}
		if logger != "" {
			log.Log(context.Background(), level, line, "logger", logger)
		} else {
			log.Log(context.Background(), level, line)
		}
	}
}

func isLevel(s string) bool {
	switch s {
	case "SEVERE", "WARNING", "INFO", "CONFIG", "FINE", "FINER", "FINEST":
		return true
	}
	return false
}

func javaLevel(s string) slog.Level {
	switch s {
	case "SEVERE":
		return slog.LevelError
	case "WARNING":
		return slog.LevelWarn
	case "INFO", "CONFIG":
		return slog.LevelInfo
	}
	return slog.LevelDebug
}

func (s *supervisor) shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	if c := s.b.conn.Load(); c != nil {
		cctx, cancel := context.WithTimeout(ctx, shutdownGrace)
		_, err := c.Call(cctx, OpShutdown)
		cancel()
		if err != nil && !errors.Is(err, ErrClosed) {
			s.log().Warn("java bridge shutdown call failed", "err", err)
		}
	}
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	select {
	case <-s.exited:
		return nil
	case <-ctx.Done():
	case <-time.After(shutdownGrace):
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-s.exited
	return ctx.Err()
}
