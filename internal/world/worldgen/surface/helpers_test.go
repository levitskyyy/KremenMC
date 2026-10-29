package surface

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

func mustPack(t testing.TB) *wgdata.Pack {
	t.Helper()
	p, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
