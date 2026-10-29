package selfcheck

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// TestPipelineSynthetic runs the full selfcheck on internal/world/gen with
// its synthetic stage set (order-dependent features across chunk borders).
// Set KREMENMC_SELFCHECK_LOG=1 to print the result table.
func TestPipelineSynthetic(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	ps := append(positions(-2, -2, 1, 1), positions(30, -34, 33, -31)...)
	ps = append(ps, positions(-1875002, 6249998, -1874999, 6250001)...)
	b := SyntheticBackend(42, "overworld")
	b.CacheSize = 512
	res, err := Run(context.Background(), Options{
		Backend: b, Chunks: ps, Workers: []int{1, 4}, SpeedSet: positions(0, 0, 5, 5), TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res.Print(&out)
	if os.Getenv("KREMENMC_SELFCHECK_LOG") == "1" {
		t.Log("\n" + out.String())
	}
	if !res.OK() {
		t.Fatalf("pipeline output differs between runs:\n%s", out.String())
	}
}

// TestPipelineAnvilRestart runs the save/restart checks with vanilla region
// storage and a small cache (evict/reload through Anvil).
func TestPipelineAnvilRestart(t *testing.T) {
	b, err := NewPipelineBackend("synthetic", 7, "overworld")
	if err != nil {
		t.Fatal(err)
	}
	b.Storage, b.CacheSize = "anvil", 128
	res, err := Run(context.Background(), Options{
		Backend: b, Chunks: append(positions(-2, -2, 1, 1), positions(30, -34, 33, -31)...),
		Workers: []int{4}, Orders: []string{"spiral"}, SkipSpeed: true, TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		var out bytes.Buffer
		res.Print(&out)
		t.Fatalf("anvil restart differs:\n%s", out.String())
	}
}
