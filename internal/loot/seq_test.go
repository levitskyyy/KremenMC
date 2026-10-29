package loot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func TestSequenceSourceMatchesReference(t *testing.T) {
	cases := []struct {
		seed int64
		id   string
	}{{0, "minecraft:blocks/stone"}, {8266389061079214669, "minecraft:blocks/dirt"}, {-5, "minecraft:entities/zombie"}, {12345, "minecraft:blocks/oak_leaves"}}
	var in, want strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&in, "seq|%d|%s|4\n", c.seed, c.id)
		r := SequenceSource(c.seed, c.id)
		fmt.Fprintf(&want, "seq %d %s %d %d %d %d\n", c.seed, c.id, r.NextLong(), r.NextLong(), r.NextLong(), r.NextLong())
	}
	inPath := filepath.Join(t.TempDir(), "seq_in.txt")
	if os.Getenv("KREMENMC_REF") == "1" {
		os.WriteFile(inPath, []byte(in.String()), 0o644)
	}
	got := string(reftest.Golden(t, "loot_seq_out.txt", "kremenmc.probe.dig.Loot", inPath))
	if got != want.String() {
		t.Fatalf("reference:\n%s\ngo:\n%s", got, want.String())
	}
}
