package oracle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func TestWriteDatapack(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	for _, tt := range []struct {
		variant  string
		inPlace  bool
		settings string
		files    int
	}{
		{"vanilla", false, "minecraft:overworld", 2},
		{"surface", false, "minecraft:overworld", 2 + 67},
		{"noise", false, "kremenmc:stage_noise", 3 + 67},
		{"noise", true, "minecraft:overworld", 3 + 67},
	} {
		dir := t.TempDir()
		if err := WriteDatapack(ref.Jar, dir, "minecraft:overworld", tt.variant, tt.inPlace); err != nil {
			t.Fatal(err)
		}
		n := 0
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if !d.IsDir() {
				n++
			}
			return nil
		})
		var stem struct {
			Generator struct{ Settings string } `json:"generator"`
		}
		b, _ := os.ReadFile(filepath.Join(dir, "data/kremenmc/dimension/stage.json"))
		json.Unmarshal(b, &stem)
		if n != tt.files || stem.Generator.Settings != tt.settings {
			t.Errorf("%s inPlace=%v: %d files, settings %q", tt.variant, tt.inPlace, n, stem.Generator.Settings)
		}
		if tt.variant != "vanilla" {
			b, _ := os.ReadFile(filepath.Join(dir, "data/minecraft/worldgen/biome/plains.json"))
			if !strings.Contains(string(b), `"carvers":[]`) {
				t.Errorf("%s: carvers not removed", tt.variant)
			}
		}
	}
	if c, ok := Canonical("carvers"); !ok || c != "terrain" {
		t.Fatal("carvers alias")
	}
}
