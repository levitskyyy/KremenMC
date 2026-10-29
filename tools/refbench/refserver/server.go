// Package refserver runs the official dedicated server as a headless,
// loopback-only reference oracle for the bench: instance setup
// (server.properties, eula.txt), process lifecycle, console commands with
// synchronous output capture, and level.dat reading.
package refserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultMainClass = "net.minecraft.server.Main"
	EULAURL          = "https://aka.ms/MinecraftEULA"

	// EULANote records why eula=true is written: the user accepted the EULA
	// on 2026-10-28 for running the official server locally as a test
	// oracle (localhost only, offline mode, no players).
	EULANote = "Accepted by the kremenmc developer on 2026-10-28 for the local reference bench (loopback only, offline, no players): " + EULAURL
)

type Config struct {
	Java       string // default "java" ($JAVA_HOME/bin/java when set)
	Classpath  []string
	MainClass  string   // default DefaultMainClass
	MainArgs   []string // arguments before the server's own (e.g. for a probe wrapper)
	Dir        string   // instance directory; the world lives in Dir/World
	World      string   // default "world"
	Seed       int64
	Structures bool
	Props      map[string]string // overrides on top of the bench defaults
	Memory     string            // -Xmx, default 4G
	Threads    int               // -Dmax.bg.threads (worldgen workers); 0 leaves the JVM default
	JVMArgs    []string
	Env        []string
	// StartTimeout bounds JVM start plus world creation; default 10m.
	StartTimeout time.Duration
	// Echo, when set, receives every console line.
	Echo io.Writer
}

type Server struct {
	cfg   Config
	cmd   *exec.Cmd
	stdin io.WriteCloser
	Port  int

	mu    sync.Mutex
	cond  *sync.Cond
	lines []string
	done  bool

	exited  chan struct{}
	exitErr error
	execMu  sync.Mutex
	seq     atomic.Int64
	logFile *os.File
}

func (c *Config) defaults() {
	if c.Java == "" {
		c.Java = "java"
		if h := os.Getenv("JAVA_HOME"); h != "" {
			c.Java = filepath.Join(h, "bin", "java")
		}
	}
	if c.MainClass == "" {
		c.MainClass = DefaultMainClass
	}
	if c.World == "" {
		c.World = "world"
	}
	if c.Memory == "" {
		c.Memory = "4G"
	}
	if c.StartTimeout <= 0 {
		c.StartTimeout = 10 * time.Minute
	}
}

// Properties returns the server.properties the instance runs with.
func (c *Config) Properties(port int) map[string]string {
	p := map[string]string{
		"level-name":                c.World,
		"level-seed":                strconv.FormatInt(c.Seed, 10),
		"level-type":                "minecraft:normal",
		"generate-structures":       strconv.FormatBool(c.Structures),
		"server-ip":                 "127.0.0.1",
		"server-port":               strconv.Itoa(port),
		"online-mode":               "false",
		"white-list":                "true",
		"enforce-whitelist":         "true",
		"max-players":               "1",
		"enable-status":             "false",
		"enable-query":              "false",
		"enable-rcon":               "false",
		"management-server-enabled": "false",
		"enable-jmx-monitoring":     "false",
		"pause-when-empty-seconds":  "0",
		"sync-chunk-writes":         "true",
		"view-distance":             "2",
		"simulation-distance":       "2",
		"spawn-protection":          "0",
		"log-ips":                   "false",
		"motd":                      "kremenmc reference bench",
	}
	maps.Copy(p, c.Props)
	p["server-ip"] = "127.0.0.1" // never listen beyond loopback
	return p
}

// Start writes the instance files, launches the server and waits until it
// finished loading ("Done (...)!").
func Start(ctx context.Context, cfg Config) (*Server, error) {
	cfg.defaults()
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "logs"), 0o755); err != nil {
		return nil, fmt.Errorf("refserver: %w", err)
	}
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("refserver: %w", err)
	}
	if err := writeInstance(&cfg, port); err != nil {
		return nil, err
	}
	args := []string{
		"-Xms256M", "-Xmx" + cfg.Memory,
		"-XX:+UseG1GC", "-XX:+ExitOnOutOfMemoryError", "-XX:+PerfDisableSharedMem",
		"-Dfile.encoding=UTF-8", "-Djava.awt.headless=true",
		"--enable-native-access=ALL-UNNAMED",
	}
	if cfg.Threads > 0 {
		args = append(args, "-Dmax.bg.threads="+strconv.Itoa(cfg.Threads))
	}
	args = append(args, cfg.JVMArgs...)
	args = append(args, "-cp", strings.Join(cfg.Classpath, string(os.PathListSeparator)), cfg.MainClass)
	args = append(args, cfg.MainArgs...)
	args = append(args, "--nogui", "--universe", ".", "--world", cfg.World, "--port", strconv.Itoa(port))

	logf, err := os.Create(filepath.Join(cfg.Dir, "logs", "console-"+time.Now().Format("20060102-150405")+".log"))
	if err != nil {
		return nil, fmt.Errorf("refserver: %w", err)
	}
	cmd := exec.Command(cfg.Java, args...)
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(), cfg.Env...)
	s := &Server{cfg: cfg, cmd: cmd, Port: port, exited: make(chan struct{}), logFile: logf}
	s.cond = sync.NewCond(&s.mu)
	fmt.Fprintf(logf, "# %s %s\n", cfg.Java, strings.Join(args, " "))
	if s.stdin, err = cmd.StdinPipe(); err != nil {
		logf.Close()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		logf.Close()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, fmt.Errorf("refserver: start %s: %w", cfg.Java, err)
	}
	go s.read(out)

	ctx, cancel := context.WithTimeout(ctx, cfg.StartTimeout)
	defer cancel()
	if _, _, err := s.WaitLine(ctx, 0, func(l string) bool { return strings.Contains(l, "]: Done (") }); err != nil {
		s.Kill()
		return nil, fmt.Errorf("refserver: startup: %w\n%s", err, s.Tail(30))
	}
	return s, nil
}

func writeInstance(cfg *Config, port int) error {
	props := cfg.Properties(port)
	var b strings.Builder
	b.WriteString("#Written by kremenmc tools/refbench; regenerated on every start\n")
	for _, k := range slices.Sorted(maps.Keys(props)) {
		b.WriteString(k + "=" + escapeProp(props[k]) + "\n")
	}
	if err := os.WriteFile(filepath.Join(cfg.Dir, "server.properties"), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("refserver: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Dir, "eula.txt"), []byte("#"+EULANote+"\neula=true\n"), 0o644); err != nil {
		return fmt.Errorf("refserver: %w", err)
	}
	return nil
}

func escapeProp(s string) string {
	return strings.NewReplacer(`\`, `\\`, ":", `\:`, "=", `\=`).Replace(s)
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (s *Server) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		fmt.Fprintln(s.logFile, line)
		if s.cfg.Echo != nil {
			fmt.Fprintln(s.cfg.Echo, line)
		}
		s.mu.Lock()
		s.lines = append(s.lines, line)
		s.cond.Broadcast()
		s.mu.Unlock()
	}
	s.exitErr = s.cmd.Wait()
	s.logFile.Close()
	s.mu.Lock()
	s.done = true
	s.cond.Broadcast()
	s.mu.Unlock()
	close(s.exited)
}

// Lines returns the number of console lines received so far.
func (s *Server) Lines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}

// Tail returns the last n console lines.
func (s *Server) Tail(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines[max(0, len(s.lines)-n):], "\n")
}

var ErrExited = errors.New("refserver: server exited")

// WaitLine waits for a console line at index >= from matching pred and
// returns its index and text.
func (s *Server) WaitLine(ctx context.Context, from int, pred func(string) bool) (int, string, error) {
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := from; ; {
		for ; i < len(s.lines); i++ {
			if pred(s.lines[i]) {
				return i, s.lines[i], nil
			}
		}
		if s.done {
			return 0, "", fmt.Errorf("%w: %v", ErrExited, s.exitErr)
		}
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		s.cond.Wait()
	}
}

// Message strips the "[time] [thread/LEVEL]: " prefix and the "System chat: "
// marker the 26.3 console uses for command feedback.
func Message(line string) string {
	if i := strings.Index(line, "]: "); i >= 0 && strings.HasPrefix(line, "[") {
		line = line[i+3:]
	}
	return strings.TrimPrefix(line, "System chat: ")
}

// Exec runs console commands and returns the server-thread output they
// produced, in order. A trailing marker command delimits the output.
func (s *Server) Exec(ctx context.Context, cmds ...string) ([]string, error) {
	s.execMu.Lock()
	defer s.execMu.Unlock()
	marker := fmt.Sprintf("kremenmc-sync-%d", s.seq.Add(1))
	from := s.Lines()
	var b strings.Builder
	for _, c := range cmds {
		if strings.ContainsAny(c, "\r\n") {
			return nil, fmt.Errorf("refserver: command %q contains a newline", c)
		}
		b.WriteString(c + "\n")
	}
	b.WriteString("say " + marker + "\n")
	if _, err := io.WriteString(s.stdin, b.String()); err != nil {
		return nil, fmt.Errorf("refserver: console: %w", err)
	}
	end, _, err := s.WaitLine(ctx, from, func(l string) bool {
		return strings.HasSuffix(l, " "+marker) || strings.HasSuffix(l, "]"+marker) || strings.HasSuffix(l, ": "+marker)
	})
	if err != nil {
		return nil, fmt.Errorf("refserver: %v: %w", cmds, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range s.lines[from:end] {
		if strings.Contains(l, "[Server thread/") && !strings.Contains(l, "Can't keep up!") {
			out = append(out, Message(l))
		}
	}
	return out, nil
}

// Stop saves and stops the server, killing it after timeout.
func (s *Server) Stop(timeout time.Duration) error {
	_, _ = io.WriteString(s.stdin, "stop\n")
	_ = s.stdin.Close()
	select {
	case <-s.exited:
		return s.exitErr
	case <-time.After(timeout):
		s.Kill()
		return errors.New("refserver: server did not stop in time; killed")
	}
}

// Kill terminates the process and waits for it.
func (s *Server) Kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.exited
}

// Exited is closed when the process has exited.
func (s *Server) Exited() <-chan struct{} { return s.exited }

// WorldDir is the world save directory.
func (s *Server) WorldDir() string { return filepath.Join(s.cfg.Dir, s.cfg.World) }

// DimensionDir is a dimension's directory inside a 26.3 world
// (world/dimensions/<namespace>/<path>).
func DimensionDir(world, dim string) string {
	ns, p, ok := strings.Cut(dim, ":")
	if !ok {
		ns, p = "minecraft", dim
	}
	return filepath.Join(world, "dimensions", ns, filepath.FromSlash(p))
}
