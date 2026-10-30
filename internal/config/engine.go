package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Duration is a time.Duration written as a Go duration string ("50ms").
type Duration time.Duration

func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

// Engine is kremenmc.toml. Zero worker counts mean "derive from GOMAXPROCS".
type Engine struct {
	Workers     Workers     `toml:"workers"`
	World       World       `toml:"world"`
	Plugins     Plugins     `toml:"plugins"`
	Translation Translation `toml:"translation"`
	Java        JavaBridge  `toml:"java"`
	Metrics     Metrics     `toml:"metrics"`
}

type Workers struct {
	Tick    int `toml:"tick" comment:"Region tick workers; 0 = GOMAXPROCS"`
	ChunkIO int `toml:"chunk-io" comment:"Chunk load/generate/save workers; 0 = GOMAXPROCS-1 (min 1)"`
	Network int `toml:"network" comment:"Packet encode/compress workers; 0 = GOMAXPROCS/2"`
	Async   int `toml:"async" comment:"Plugin async scheduler pool size"`
}

// ChunkIOWorkers is the effective chunk worker count. The default leaves
// one core free so generation cannot starve the tick and network goroutines.
func (w Workers) ChunkIOWorkers() int {
	if w.ChunkIO > 0 {
		return w.ChunkIO
	}
	return max(1, runtime.GOMAXPROCS(0)-1)
}

type World struct {
	RegionSize       int      `toml:"region-size" comment:"Chunks per region side (power of two, 2-64)"`
	TickThreads      int      `toml:"tick-threads" comment:"Worlds ticked in parallel; 0 = all"`
	AutosaveInterval Duration `toml:"autosave-interval"`
	MaxTickLag       Duration `toml:"max-tick-lag" comment:"Skip catch-up ticks after falling this far behind"`
}

type Plugins struct {
	Enabled       bool     `toml:"enabled"`
	Dir           string   `toml:"dir"`
	CacheDir      string   `toml:"cache-dir" comment:"Compiled WASM cache; empty disables it"`
	MaxMemoryMB   int      `toml:"max-memory-mb" comment:"Per-plugin linear memory limit"`
	CallTimeout   Duration `toml:"call-timeout" comment:"Deadline for one event/command/task call into a plugin"`
	EnableTimeout Duration `toml:"enable-timeout"`
	KVMaxBytes    int      `toml:"kv-max-bytes"`
	AutoFetch     bool     `toml:"auto-fetch" comment:"Download plugins listed in plugins.toml on startup"`
	UpdateOnStart bool     `toml:"update-on-start" comment:"Re-resolve unpinned plugins instead of using plugins.lock"`
}

type Translation struct {
	Enabled     bool  `toml:"enabled" comment:"Accept clients on other protocol versions"`
	MinProtocol int32 `toml:"min-protocol"`
	MaxProtocol int32 `toml:"max-protocol"`
}

type JavaBridge struct {
	Enabled       bool     `toml:"enabled" comment:"Run Paper/Spigot/Folia plugin jars through the Java bridge"`
	AutoProvision bool     `toml:"auto-provision" comment:"Download a JRE if none is found"`
	JavaHome      string   `toml:"java-home"`
	MinVersion    int      `toml:"min-version"`
	MaxHeapMB     int      `toml:"max-heap-mb"`
	ExtraArgs     []string `toml:"extra-args"`
}

type Metrics struct {
	Enabled bool   `toml:"enabled"`
	Listen  string `toml:"listen"`
	Path    string `toml:"path"`
}

func DefaultEngine() *Engine {
	return &Engine{
		Workers: Workers{Async: 8},
		World: World{
			RegionSize:       8,
			AutosaveInterval: Duration(5 * time.Minute),
			MaxTickLag:       Duration(2 * time.Second),
		},
		Plugins: Plugins{
			Enabled:       true,
			Dir:           "plugins",
			CacheDir:      "cache/wasm",
			MaxMemoryMB:   128,
			CallTimeout:   Duration(50 * time.Millisecond),
			EnableTimeout: Duration(10 * time.Second),
			KVMaxBytes:    16 << 20,
			AutoFetch:     true,
		},
		Translation: Translation{Enabled: true, MinProtocol: 47, MaxProtocol: 777},
		Java:        JavaBridge{Enabled: false, AutoProvision: true, MinVersion: 21, MaxHeapMB: 1024},
		Metrics:     Metrics{Listen: "127.0.0.1:9225", Path: "/metrics"},
	}
}

func (e *Engine) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	w := e.Workers
	check(w.Tick >= 0 && w.ChunkIO >= 0 && w.Network >= 0 && w.Async >= 0, "workers: counts must be >= 0")
	rs := e.World.RegionSize
	check(rs >= 2 && rs <= 64 && rs&(rs-1) == 0, "world.region-size %d must be a power of two in 2-64", rs)
	check(e.World.TickThreads >= 0, "world.tick-threads must be >= 0")
	check(e.World.AutosaveInterval >= 0, "world.autosave-interval must be >= 0")
	p := e.Plugins
	check(p.Dir != "", "plugins.dir must be set")
	check(p.MaxMemoryMB >= 1 && p.MaxMemoryMB <= 4096, "plugins.max-memory-mb %d out of range 1-4096", p.MaxMemoryMB)
	check(p.CallTimeout > 0, "plugins.call-timeout must be > 0")
	check(p.EnableTimeout > 0, "plugins.enable-timeout must be > 0")
	check(p.KVMaxBytes >= 0, "plugins.kv-max-bytes must be >= 0")
	t := e.Translation
	check(!t.Enabled || t.MinProtocol > 0 && t.MinProtocol <= t.MaxProtocol, "translation: need 0 < min-protocol <= max-protocol")
	check(e.Java.MinVersion >= 8, "java.min-version must be >= 8")
	check(e.Java.MaxHeapMB >= 64, "java.max-heap-mb must be >= 64")
	if e.Metrics.Enabled {
		_, _, err := net.SplitHostPort(e.Metrics.Listen)
		check(err == nil, "metrics.listen %q: want host:port", e.Metrics.Listen)
		check(len(e.Metrics.Path) > 0 && e.Metrics.Path[0] == '/', "metrics.path must start with /")
	}
	return errors.Join(errs...)
}

// ParseEngine overlays data on defaults; unknown keys are errors so typos
// do not pass silently.
func ParseEngine(data []byte) (*Engine, error) {
	e := DefaultEngine()
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(e); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return nil, fmt.Errorf("kremenmc.toml: unknown keys:\n%s", sm.String())
		}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("kremenmc.toml:%d:%d: %s: %w", row, col, strings.Join(de.Key(), "."), err)
		}
		return nil, fmt.Errorf("kremenmc.toml: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("kremenmc.toml: %w", err)
	}
	return e, nil
}

func (e *Engine) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# kremenmc engine settings. server.properties holds vanilla-compatible settings.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(false)
	if err := enc.Encode(e); err != nil {
		return nil, fmt.Errorf("kremenmc.toml: %w", err)
	}
	return buf.Bytes(), nil
}

func (e *Engine) Save(path string) error {
	b, err := e.Bytes()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// LoadEngine reads path, writing defaults first if it does not exist.
func LoadEngine(path string) (e *Engine, created bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		e = DefaultEngine()
		if err := e.Save(path); err != nil {
			return nil, false, err
		}
		return e, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("kremenmc.toml: %w", err)
	}
	e, err = ParseEngine(data)
	return e, false, err
}

// Config bundles both files.
type Config struct {
	Server *ServerProperties
	Engine *Engine
}

// Load reads server.properties and kremenmc.toml from dir, writing defaults for
// missing files.
func Load(dir string) (*Config, error) {
	sp, _, err := LoadServerProperties(filepath.Join(dir, "server.properties"))
	if err != nil {
		return nil, err
	}
	en, _, err := LoadEngine(filepath.Join(dir, "kremenmc.toml"))
	if err != nil {
		return nil, err
	}
	return &Config{Server: sp, Engine: en}, nil
}
