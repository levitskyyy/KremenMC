package selfcheck

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
)

func positions(x0, z0, x1, z1 int32) []compare.Pos {
	var ps []compare.Pos
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			ps = append(ps, compare.Pos{X: x, Z: z})
		}
	}
	return ps
}

func TestTerrainDeterministic(t *testing.T) {
	f, err := gens.Lookup("terrain")
	if err != nil {
		t.Fatal(err)
	}
	ps := append(positions(-2, -2, 1, 1), positions(30, -34, 33, -31)...)
	res, err := Run(context.Background(), Options{
		Backend: &WorldBackend{Gen: "terrain", Factory: f, Seed: 42, Dim: "overworld"},
		Chunks:  ps, Workers: []int{1, 3}, SpeedSet: positions(0, 0, 3, 3), TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("terrain differs between runs: %+v", res.Checks)
	}
	if len(res.Checks) != 1+2+len(Orders)+2 || len(res.Speed) != 2 {
		t.Fatalf("%d checks, %d speed rows", len(res.Checks), len(res.Speed))
	}
}

// orderGen depends on generation order: a shared counter picks the block.
type orderGen struct{ n atomic.Int64 }

func (g *orderGen) Generate(c *world.Chunk) {
	st := data.MustState("stone")
	if g.n.Add(1)%3 == 0 {
		st = data.MustState("dirt")
	}
	for x := range 16 {
		for z := range 16 {
			c.SetBlock(x, 0, z, st)
		}
	}
	c.Finish()
}

func TestDetectsOrderDependence(t *testing.T) {
	b := &WorldBackend{Gen: "order", Factory: func(int64, string) world.Generator { return &orderGen{} }, Dim: "overworld"}
	res, err := Run(context.Background(), Options{
		Backend: b, Chunks: positions(-2, -2, 1, 1), Workers: []int{1, 2}, Orders: []string{"reverse-spiral"},
		SkipSpeed: true, TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("order-dependent generator passed")
	}
	var diff bool
	for _, c := range res.Checks {
		if c.Name == "order=reverse-spiral workers=2" && !c.Equal && c.Detail != nil && len(c.Detail.Confusion) > 0 {
			diff = true
		}
	}
	if !diff {
		t.Fatalf("reverse-spiral not reported as different: %+v", res.Checks)
	}
}

func TestOrders(t *testing.T) {
	ps := positions(-2, -2, 1, 1)
	for _, name := range Orders {
		got, err := Order(name, ps, 1)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[compare.Pos]bool{}
		for _, p := range got {
			seen[p] = true
		}
		if len(got) != len(ps) || len(seen) != len(ps) {
			t.Fatalf("%s is not a permutation: %v", name, got)
		}
	}
	a, _ := Order("random", ps, 7)
	b, _ := Order("random", ps, 7)
	if !equal(a, b) {
		t.Fatal("random order not reproducible")
	}
	if _, err := Order("nope", ps, 0); err == nil {
		t.Fatal("unknown order accepted")
	}
}

func equal(a, b []compare.Pos) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}
