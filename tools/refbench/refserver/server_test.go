package refserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPropertiesLoopbackOnly(t *testing.T) {
	c := Config{Seed: -5, Props: map[string]string{"server-ip": "0.0.0.0", "view-distance": "4"}}
	c.defaults()
	p := c.Properties(1234)
	if p["server-ip"] != "127.0.0.1" || p["online-mode"] != "false" || p["level-seed"] != "-5" || p["view-distance"] != "4" || p["server-port"] != "1234" {
		t.Fatalf("%v", p)
	}
	dir := t.TempDir()
	c.Dir = dir
	c.Props["level-type"] = "minecraft:normal"
	if err := writeInstance(&c, 1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(b), "level-type=minecraft\\:normal\n") || !strings.Contains(string(b), "server-ip=127.0.0.1\n") {
		t.Fatalf("%s", b)
	}
	e, _ := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if !strings.HasSuffix(string(e), "eula=true\n") {
		t.Fatalf("%s", e)
	}
}

func TestMessage(t *testing.T) {
	tests := map[string]string{
		"[15:28:13] [Server thread/INFO]: System chat: Test passed": "Test passed",
		"[15:28:13] [Server thread/INFO]: Done (1.6s)!":             "Done (1.6s)!",
		"plain": "plain",
	}
	for in, want := range tests {
		if got := Message(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	if DimensionDir("w", "minecraft:the_nether") != filepath.Join("w", "dimensions", "minecraft", "the_nether") {
		t.Fatal(DimensionDir("w", "minecraft:the_nether"))
	}
}
