package feature

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

func TestDumpTreeJSON(t *testing.T) {
	keys := os.Getenv("DUMPKEYS")
	if keys == "" {
		t.Skip()
	}
	p, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range strings.Split(keys, ",") {
		b, _ := p.JSON("worldgen/configured_feature", k)
		t.Logf("%s: %s", k, b)
	}
}

// TestConvertTreeRaw turns raw Draws output (TREERAW) into golden lines
// (TREEOUT).
func TestConvertTreeRaw(t *testing.T) {
	in, outp := os.Getenv("TREERAW"), os.Getenv("TREEOUT")
	if in == "" {
		t.Skip()
	}
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "minecraft:") {
			continue
		}
		head, rest, _ := strings.Cut(l, " => ")
		body, tail, _ := strings.Cut(rest, " | ")
		fs := strings.Fields(body)
		var got []string
		for _, f := range fs[1:] {
			pos, st, _ := strings.Cut(f, "=")
			got = append(got, pos+"="+normState(st))
		}
		sum := sha256.Sum256([]byte(strings.Join(got, " ")))
		lines = append(lines, fmt.Sprintf("%s => %s %d %x | %s", head, fs[0], len(got), sum[:8], tail))
	}
	if err := os.WriteFile(outp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDumpRegKeys(t *testing.T) {
	if os.Getenv("DUMPREG") == "" {
		t.Skip()
	}
	p, _ := wgdata.Load()
	for _, r := range p.Registries() {
		if strings.Contains(r, os.Getenv("DUMPREG")) {
			for _, k := range p.Keys(r) {
				b, _ := p.JSON(r, k)
				t.Logf("%s %s %s", r, k, b)
			}
		}
	}
}
