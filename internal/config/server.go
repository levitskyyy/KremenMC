package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ServerProperties holds the server.properties keys the engine acts on. Other
// keys survive in Raw and are written back untouched.
type ServerProperties struct {
	ServerIP                    string
	ServerPort                  int
	OnlineMode                  bool
	PreventProxyConnections     bool
	EnforceSecureProfile        bool
	MaxPlayers                  int
	ViewDistance                int
	SimulationDistance          int
	MOTD                        string
	LevelName                   string
	LevelSeed                   string
	LevelType                   string
	GeneratorSettings           string
	GenerateStructures          bool
	Gamemode                    string
	ForceGamemode               bool
	Difficulty                  string
	Hardcore                    bool
	NetworkCompressionThreshold int
	SpawnProtection             int
	MaxWorldSize                int
	MaxTickTime                 int
	MaxChainedNeighborUpdates   int
	OpPermissionLevel           int
	FunctionPermissionLevel     int
	PlayerIdleTimeout           int
	RateLimit                   int
	EntityBroadcastRangePercent int
	PauseWhenEmptySeconds       int
	AllowFlight                 bool
	WhiteList                   bool
	EnforceWhitelist            bool
	EnableStatus                bool
	HideOnlinePlayers           bool
	EnableQuery                 bool
	QueryPort                   int
	EnableRCON                  bool
	RCONPort                    int
	RCONPassword                string
	BroadcastConsoleToOps       bool
	BroadcastRCONToOps          bool
	AcceptsTransfers            bool
	LogIPs                      bool
	SyncChunkWrites             bool
	RegionFileCompression       string
	ResourcePack                string
	ResourcePackID              string
	ResourcePackSHA1            string
	ResourcePackPrompt          string
	RequireResourcePack         bool
	InitialEnabledPacks         string
	InitialDisabledPacks        string
	BugReportLink               string
	StatusHeartbeatInterval     int
	EnableCodeOfConduct         bool

	Raw *Properties
}

type propField struct {
	key string
	def string
	get func(*ServerProperties) any
}

// Vanilla defaults in the order a fresh vanilla server writes them.
var serverFields = []propField{
	{"accepts-transfers", "false", func(s *ServerProperties) any { return &s.AcceptsTransfers }},
	{"allow-flight", "false", func(s *ServerProperties) any { return &s.AllowFlight }},
	{"broadcast-console-to-ops", "true", func(s *ServerProperties) any { return &s.BroadcastConsoleToOps }},
	{"broadcast-rcon-to-ops", "true", func(s *ServerProperties) any { return &s.BroadcastRCONToOps }},
	{"bug-report-link", "", func(s *ServerProperties) any { return &s.BugReportLink }},
	{"difficulty", "easy", func(s *ServerProperties) any { return &s.Difficulty }},
	{"enable-code-of-conduct", "false", func(s *ServerProperties) any { return &s.EnableCodeOfConduct }},
	{"enable-query", "false", func(s *ServerProperties) any { return &s.EnableQuery }},
	{"enable-rcon", "false", func(s *ServerProperties) any { return &s.EnableRCON }},
	{"enable-status", "true", func(s *ServerProperties) any { return &s.EnableStatus }},
	{"enforce-secure-profile", "true", func(s *ServerProperties) any { return &s.EnforceSecureProfile }},
	{"enforce-whitelist", "false", func(s *ServerProperties) any { return &s.EnforceWhitelist }},
	{"entity-broadcast-range-percentage", "100", func(s *ServerProperties) any { return &s.EntityBroadcastRangePercent }},
	{"force-gamemode", "false", func(s *ServerProperties) any { return &s.ForceGamemode }},
	{"function-permission-level", "2", func(s *ServerProperties) any { return &s.FunctionPermissionLevel }},
	{"gamemode", "survival", func(s *ServerProperties) any { return &s.Gamemode }},
	{"generate-structures", "true", func(s *ServerProperties) any { return &s.GenerateStructures }},
	{"generator-settings", "{}", func(s *ServerProperties) any { return &s.GeneratorSettings }},
	{"hardcore", "false", func(s *ServerProperties) any { return &s.Hardcore }},
	{"hide-online-players", "false", func(s *ServerProperties) any { return &s.HideOnlinePlayers }},
	{"initial-disabled-packs", "", func(s *ServerProperties) any { return &s.InitialDisabledPacks }},
	{"initial-enabled-packs", "vanilla", func(s *ServerProperties) any { return &s.InitialEnabledPacks }},
	{"level-name", "world", func(s *ServerProperties) any { return &s.LevelName }},
	{"level-seed", "", func(s *ServerProperties) any { return &s.LevelSeed }},
	{"level-type", "minecraft:normal", func(s *ServerProperties) any { return &s.LevelType }},
	{"log-ips", "true", func(s *ServerProperties) any { return &s.LogIPs }},
	{"max-chained-neighbor-updates", "1000000", func(s *ServerProperties) any { return &s.MaxChainedNeighborUpdates }},
	{"max-players", "20", func(s *ServerProperties) any { return &s.MaxPlayers }},
	{"max-tick-time", "60000", func(s *ServerProperties) any { return &s.MaxTickTime }},
	{"max-world-size", "29999984", func(s *ServerProperties) any { return &s.MaxWorldSize }},
	{"motd", "A Minecraft Server", func(s *ServerProperties) any { return &s.MOTD }},
	{"network-compression-threshold", "256", func(s *ServerProperties) any { return &s.NetworkCompressionThreshold }},
	{"online-mode", "true", func(s *ServerProperties) any { return &s.OnlineMode }},
	{"op-permission-level", "4", func(s *ServerProperties) any { return &s.OpPermissionLevel }},
	{"pause-when-empty-seconds", "60", func(s *ServerProperties) any { return &s.PauseWhenEmptySeconds }},
	{"player-idle-timeout", "0", func(s *ServerProperties) any { return &s.PlayerIdleTimeout }},
	{"prevent-proxy-connections", "false", func(s *ServerProperties) any { return &s.PreventProxyConnections }},
	{"query.port", "25565", func(s *ServerProperties) any { return &s.QueryPort }},
	{"rate-limit", "0", func(s *ServerProperties) any { return &s.RateLimit }},
	{"rcon.password", "", func(s *ServerProperties) any { return &s.RCONPassword }},
	{"rcon.port", "25575", func(s *ServerProperties) any { return &s.RCONPort }},
	{"region-file-compression", "deflate", func(s *ServerProperties) any { return &s.RegionFileCompression }},
	{"require-resource-pack", "false", func(s *ServerProperties) any { return &s.RequireResourcePack }},
	{"resource-pack", "", func(s *ServerProperties) any { return &s.ResourcePack }},
	{"resource-pack-id", "", func(s *ServerProperties) any { return &s.ResourcePackID }},
	{"resource-pack-prompt", "", func(s *ServerProperties) any { return &s.ResourcePackPrompt }},
	{"resource-pack-sha1", "", func(s *ServerProperties) any { return &s.ResourcePackSHA1 }},
	{"server-ip", "", func(s *ServerProperties) any { return &s.ServerIP }},
	{"server-port", "25565", func(s *ServerProperties) any { return &s.ServerPort }},
	{"simulation-distance", "10", func(s *ServerProperties) any { return &s.SimulationDistance }},
	{"spawn-protection", "16", func(s *ServerProperties) any { return &s.SpawnProtection }},
	{"status-heartbeat-interval", "0", func(s *ServerProperties) any { return &s.StatusHeartbeatInterval }},
	{"sync-chunk-writes", "true", func(s *ServerProperties) any { return &s.SyncChunkWrites }},
	{"view-distance", "10", func(s *ServerProperties) any { return &s.ViewDistance }},
	{"white-list", "false", func(s *ServerProperties) any { return &s.WhiteList }},
}

// DefaultServerProperties returns vanilla defaults.
func DefaultServerProperties() *ServerProperties {
	s := &ServerProperties{Raw: NewProperties()}
	for _, f := range serverFields {
		if err := setField(f.get(s), f.def); err != nil {
			panic("config: bad default for " + f.key)
		}
		s.Raw.Set(f.key, f.def)
	}
	return s
}

func setField(ptr any, v string) error {
	switch p := ptr.(type) {
	case *string:
		*p = v
	case *bool:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("want true or false, got %q", v)
		}
		*p = b
	case *int:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("want an integer, got %q", v)
		}
		*p = n
	}
	return nil
}

func formatField(ptr any) string {
	switch p := ptr.(type) {
	case *string:
		return *p
	case *bool:
		return strconv.FormatBool(*p)
	case *int:
		return strconv.Itoa(*p)
	}
	return ""
}

// ParseServerProperties overlays file contents on vanilla defaults. Missing
// keys keep their defaults; malformed values are errors.
func ParseServerProperties(data []byte) (*ServerProperties, error) {
	raw, err := ParseProperties(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	s := DefaultServerProperties()
	s.Raw = raw
	var errs []error
	for _, f := range serverFields {
		v, ok := raw.Get(f.key)
		if !ok {
			continue
		}
		if err := setField(f.get(s), v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.key, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("server.properties: %w", err)
	}
	return s, nil
}

var (
	gamemodes    = []string{"survival", "creative", "adventure", "spectator"}
	difficulties = []string{"peaceful", "easy", "normal", "hard"}
)

func (s *ServerProperties) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	port := func(name string, p int) { check(p >= 0 && p <= 65535, "%s %d out of range 0-65535", name, p) }
	port("server-port", s.ServerPort)
	port("query.port", s.QueryPort)
	port("rcon.port", s.RCONPort)
	if s.ServerIP != "" {
		check(net.ParseIP(s.ServerIP) != nil, "server-ip %q is not an IP address", s.ServerIP)
	}
	check(s.MaxPlayers >= 0, "max-players must be >= 0")
	check(s.ViewDistance >= 2 && s.ViewDistance <= 32, "view-distance %d out of range 2-32", s.ViewDistance)
	check(s.SimulationDistance >= 2 && s.SimulationDistance <= 32, "simulation-distance %d out of range 2-32", s.SimulationDistance)
	check(s.NetworkCompressionThreshold >= -1, "network-compression-threshold must be >= -1")
	check(s.SpawnProtection >= 0, "spawn-protection must be >= 0")
	check(s.MaxWorldSize >= 1 && s.MaxWorldSize <= 29999984, "max-world-size %d out of range 1-29999984", s.MaxWorldSize)
	check(s.OpPermissionLevel >= 0 && s.OpPermissionLevel <= 4, "op-permission-level must be 0-4")
	check(s.FunctionPermissionLevel >= 1 && s.FunctionPermissionLevel <= 4, "function-permission-level must be 1-4")
	check(s.EntityBroadcastRangePercent >= 10 && s.EntityBroadcastRangePercent <= 1000, "entity-broadcast-range-percentage must be 10-1000")
	check(s.PlayerIdleTimeout >= 0 && s.RateLimit >= 0 && s.PauseWhenEmptySeconds >= 0, "timeouts and rate-limit must be >= 0")
	check(contains(gamemodes, s.Gamemode) || isDigit(s.Gamemode, 3), "gamemode %q unknown", s.Gamemode)
	check(contains(difficulties, s.Difficulty) || isDigit(s.Difficulty, 3), "difficulty %q unknown", s.Difficulty)
	check(contains([]string{"deflate", "lz4", "none"}, s.RegionFileCompression), "region-file-compression %q unknown", s.RegionFileCompression)
	check(s.LevelName != "" && !strings.ContainsAny(s.LevelName, `/\`) && s.LevelName != ".." && s.LevelName != ".", "level-name %q is not a valid directory name", s.LevelName)
	check(!s.EnableRCON || s.RCONPassword != "", "enable-rcon requires rcon.password")
	return errors.Join(errs...)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// isDigit accepts legacy numeric gamemode/difficulty values 0..max.
func isDigit(s string, max int) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= max
}

// GamemodeName normalizes legacy numeric values.
func (s *ServerProperties) GamemodeName() string {
	if n, err := strconv.Atoi(s.Gamemode); err == nil && n >= 0 && n < len(gamemodes) {
		return gamemodes[n]
	}
	return s.Gamemode
}

func (s *ServerProperties) DifficultyName() string {
	if n, err := strconv.Atoi(s.Difficulty); err == nil && n >= 0 && n < len(difficulties) {
		return difficulties[n]
	}
	return s.Difficulty
}

// Seed returns the numeric world seed: an integer as-is, any other text hashed
// like Java's String.hashCode, and ok=false for an empty seed (pick randomly).
func (s *ServerProperties) Seed() (int64, bool) {
	if s.LevelSeed == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s.LevelSeed, 10, 64); err == nil {
		return n, true
	}
	var h int32
	for _, c := range utf16Units(s.LevelSeed) {
		h = 31*h + int32(c)
	}
	return int64(h), true
}

func utf16Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// Bytes renders the file, writing typed fields back into Raw so comments and
// unknown keys are preserved.
func (s *ServerProperties) Bytes() []byte {
	if s.Raw == nil {
		s.Raw = NewProperties()
	}
	for _, f := range serverFields {
		v := formatField(f.get(s))
		if cur, ok := s.Raw.Get(f.key); ok && (cur == v || sameValue(f.get(s), cur, v)) {
			continue
		}
		s.Raw.Set(f.key, v)
	}
	return s.Raw.Bytes()
}

// sameValue avoids rewriting lines whose text differs only in formatting
// (" 10" vs "10", "TRUE" vs "true").
func sameValue(ptr any, cur, v string) bool {
	switch ptr.(type) {
	case *bool:
		b, err := strconv.ParseBool(strings.TrimSpace(cur))
		return err == nil && strconv.FormatBool(b) == v
	case *int:
		n, err := strconv.Atoi(strings.TrimSpace(cur))
		return err == nil && strconv.Itoa(n) == v
	}
	return false
}

func (s *ServerProperties) Save(path string) error { return writeFileAtomic(path, s.Bytes()) }

// LoadServerProperties reads path, creating it with vanilla defaults if it
// does not exist. created reports whether the file was written.
func LoadServerProperties(path string) (props *ServerProperties, created bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s := DefaultServerProperties()
		raw := NewProperties()
		raw.lines = append(raw.lines,
			propLine{raw: "#Minecraft server properties"},
			propLine{raw: "#" + time.Now().Format("Mon Jan 02 15:04:05 MST 2006")})
		for _, f := range serverFields {
			raw.Set(f.key, f.def)
		}
		s.Raw = raw
		if err := s.Save(path); err != nil {
			return nil, false, err
		}
		return s, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("server.properties: %w", err)
	}
	s, err := ParseServerProperties(data)
	if err != nil {
		return nil, false, err
	}
	if err := s.Validate(); err != nil {
		return nil, false, fmt.Errorf("server.properties: %w", err)
	}
	return s, false, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
