package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPropertiesParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		key  string
		want string
	}{
		{"equals", "a=b", "a", "b"},
		{"colon", "a:b", "a", "b"},
		{"space separator", "a b", "a", "b"},
		{"spaces around", "  a  =  b c ", "a", "b c "},
		{"escaped colon in value", `level-type=minecraft\:normal`, "level-type", "minecraft:normal"},
		{"escaped key chars", `my\ key\=x=v`, "my key=x", "v"},
		{"unicode escape", `motd=\u00a7aHi`, "motd", "§aHi"},
		{"utf8 literal", "motd=Привет", "motd", "Привет"},
		{"empty value", "level-seed=", "level-seed", ""},
		{"continuation", "a=one \\\n    two", "a", "one two"},
		{"last wins", "a=1\na=2", "a", "2"},
		{"bang comment ignored", "!a=1\nb=2", "a", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseProperties(strings.NewReader(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := p.Get(tt.key)
			if got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestPropertiesRoundTripPreservesLayout(t *testing.T) {
	in := `#Minecraft server properties
#Tue Oct 28 12:00:00 UTC 2026

# custom comment
motd=Hello\:World
custom-plugin-key = keep me
level-type=minecraft\:normal
view-distance=12
`
	p, err := ParseProperties(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.Bytes()); got != in {
		t.Fatalf("unmodified round trip changed file:\n%s", got)
	}
	p.Set("view-distance", "16")
	p.Set("new-key", "a=b")
	p.Delete("custom-plugin-key")
	want := `#Minecraft server properties
#Tue Oct 28 12:00:00 UTC 2026

# custom comment
motd=Hello\:World
level-type=minecraft\:normal
view-distance=16
new-key=a\=b
`
	if got := string(p.Bytes()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	re, _ := ParseProperties(strings.NewReader(want))
	if v, _ := re.Get("new-key"); v != "a=b" {
		t.Errorf("escaped value did not round trip: %q", v)
	}
}

func TestServerPropertiesDefaults(t *testing.T) {
	s := DefaultServerProperties()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"server-port", s.ServerPort, 25565},
		{"online-mode", s.OnlineMode, true},
		{"view-distance", s.ViewDistance, 10},
		{"simulation-distance", s.SimulationDistance, 10},
		{"max-players", s.MaxPlayers, 20},
		{"motd", s.MOTD, "A Minecraft Server"},
		{"level-name", s.LevelName, "world"},
		{"gamemode", s.Gamemode, "survival"},
		{"difficulty", s.Difficulty, "easy"},
		{"network-compression-threshold", s.NetworkCompressionThreshold, 256},
		{"spawn-protection", s.SpawnProtection, 16},
		{"level-type", s.LevelType, "minecraft:normal"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestServerPropertiesLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.properties")
	s, created, err := LoadServerProperties(path)
	if err != nil || !created {
		t.Fatalf("first load: created=%v err=%v", created, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "#Minecraft server properties\n") || !strings.Contains(string(data), "level-type=minecraft\\:normal\n") {
		t.Fatalf("default file:\n%s", data)
	}

	edited := strings.Replace(string(data), "max-players=20", "max-players = 50", 1) + "# trailing comment\nunknown.key=x\n"
	os.WriteFile(path, []byte(edited), 0o644)
	s, created, err = LoadServerProperties(path)
	if err != nil || created {
		t.Fatalf("reload: %v", err)
	}
	if s.MaxPlayers != 50 {
		t.Fatalf("max-players = %d", s.MaxPlayers)
	}
	s.MOTD = "§6kremenmc: fast"
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	for _, want := range []string{"max-players = 50\n", "# trailing comment\nunknown.key=x\n", `motd=§6kremenmc\: fast`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("saved file lacks %q:\n%s", want, out)
		}
	}
	again, _, err := LoadServerProperties(path)
	if err != nil || again.MOTD != "§6kremenmc: fast" {
		t.Fatalf("motd round trip = %q, %v", again.MOTD, err)
	}
}

func TestServerPropertiesErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bad int", "server-port=abc", "server-port"},
		{"bad bool", "online-mode=yes", "online-mode"},
		{"port range", "server-port=70000", "out of range"},
		{"view distance", "view-distance=1", "view-distance"},
		{"gamemode", "gamemode=god", "gamemode"},
		{"rcon without password", "enable-rcon=true", "rcon.password"},
		{"level name", "level-name=../etc", "level-name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := ParseServerProperties([]byte(tt.in))
			if err == nil {
				err = s.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestServerPropertiesHelpers(t *testing.T) {
	tests := []struct {
		seed     string
		want     int64
		wantOK   bool
		gamemode string
		gmName   string
	}{
		{"", 0, false, "1", "creative"},
		{"12345", 12345, true, "survival", "survival"},
		{"-9", -9, true, "3", "spectator"},
		{"kremenmc", -1406324530, true, "adventure", "adventure"},
	}
	for _, tt := range tests {
		s := DefaultServerProperties()
		s.LevelSeed, s.Gamemode = tt.seed, tt.gamemode
		got, ok := s.Seed()
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("Seed(%q) = %d,%v want %d,%v", tt.seed, got, ok, tt.want, tt.wantOK)
		}
		if s.GamemodeName() != tt.gmName {
			t.Errorf("GamemodeName(%q) = %q", tt.gamemode, s.GamemodeName())
		}
	}
}

func TestEngineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kremenmc.toml")
	e, created, err := LoadEngine(path)
	if err != nil || !created {
		t.Fatalf("first load: %v %v", created, err)
	}
	if e.World.RegionSize != 8 || e.Plugins.CallTimeout.D() != 50*time.Millisecond {
		t.Fatalf("defaults wrong: %+v", e)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `call-timeout = '50ms'`) || !strings.Contains(string(raw), "# Chunks per region side") {
		t.Errorf("written file:\n%s", raw)
	}
	e.World.RegionSize = 16
	e.Java.Enabled = true
	e.Java.ExtraArgs = []string{"-XX:+UseZGC"}
	e.Plugins.CallTimeout = Duration(100 * time.Millisecond)
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	back, created, err := LoadEngine(path)
	if err != nil || created {
		t.Fatal(err)
	}
	if back.World.RegionSize != 16 || !back.Java.Enabled || back.Java.ExtraArgs[0] != "-XX:+UseZGC" || back.Plugins.CallTimeout.D() != 100*time.Millisecond {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestEngineParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"partial overlays defaults", "[world]\nregion-size = 4\n", ""},
		{"unknown key", "[world]\nregion-sise = 4\n", "region-sise"},
		{"region not power of two", "[world]\nregion-size = 6\n", "power of two"},
		{"bad duration", "[plugins]\ncall-timeout = 'soon'\n", "call-timeout"},
		{"memory limit", "[plugins]\nmax-memory-mb = 0\n", "max-memory-mb"},
		{"protocol range", "[translation]\nmin-protocol = 800\n", "min-protocol"},
		{"metrics listen", "[metrics]\nenabled = true\nlisten = 'nope'\n", "metrics.listen"},
		{"vanilla worldgen backend removed", "[worldgen]\nbackend = 'vanilla'\naccept-eula = true\n", "worldgen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := ParseEngine([]byte(tt.in))
			if tt.want == "" {
				if err != nil || e.World.RegionSize != 4 || e.Plugins.MaxMemoryMB != 128 {
					t.Fatalf("e=%+v err=%v", e, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestLoadBoth(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil || c.Server.ServerPort != 25565 || !c.Engine.Plugins.Enabled {
		t.Fatalf("Load: %+v %v", c, err)
	}
	for _, f := range []string{"server.properties", "kremenmc.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written", f)
		}
	}
}
