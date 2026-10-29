package loot

import (
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// TestParseAll compiles every block and entity loot table.
func TestParseAll(t *testing.T) {
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, id := range pk.Keys("loot_table") {
		if !strings.HasPrefix(id, "minecraft:blocks/") && !strings.HasPrefix(id, "minecraft:entities/") {
			continue
		}
		raw, _ := pk.JSON("loot_table", id)
		if _, err := Parse(id, raw); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		n++
	}
	if n < 1300 {
		t.Fatalf("only %d tables", n)
	}
}
