package structure

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world"
)

// TestRefOrder: the placement order of the two-start mineshaft chunks of
// the reference worlds (the order that matches their blocks).
func TestRefOrder(t *testing.T) {
	for _, c := range [][2]world.ChunkPos{
		{{X: -89, Z: -3}, {X: -87, Z: -1}},
		{{X: 113, Z: -294}, {X: 108, Z: -286}},
		{{X: 4, Z: -1}, {X: -4, Z: 1}},
		{{X: 1882, Z: 6242}, {X: 1874, Z: 6246}},
	} {
		for _, in := range [][]int64{{int64(c[0].Key()), int64(c[1].Key())}, {int64(c[1].Key()), int64(c[0].Key())}} {
			if got := refOrder(in); world.PosFromKey(uint64(got[0])) != c[0] {
				t.Errorf("%v: first %v", c, world.PosFromKey(uint64(got[0])))
			}
		}
	}
}
