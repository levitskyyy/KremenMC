package biome

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// fillOrder fills a chunk with a given axis nesting (outer to inner) and
// cursor mode, returning biome keys indexed [section][qy][qz][qx].
func fillOrder(src Source, cx, cz, minSec, secs int32, order [3]int, cur *climate.Cursor, fresh bool) []string {
	out := make([]string, secs*64)
	ctx := &density.Ctx{}
	for s := range secs {
		if fresh {
			cur = &climate.Cursor{}
		}
		var i [3]int32
		for i[0] = 0; i[0] < 4; i[0]++ {
			for i[1] = 0; i[1] < 4; i[1]++ {
				for i[2] = 0; i[2] < 4; i[2]++ {
					var v [3]int32
					for k := range 3 {
						v[order[k]] = i[k]
					}
					x, y, z := v[0], v[1], v[2]
					out[s*64+y*16+z*4+x] = src.Biome(ctx, cur, cx<<2|x, (minSec+s)<<2+y, cz<<2|z)
				}
			}
		}
	}
	return out
}

// TestCursorSensitivity quantifies how much the per-thread cursor history
// matters: fills chunks with x/y/z, z/y/x and y/z/x nesting and with no
// cursor (fresh-thread lookups), counting quarts that differ from the
// reference fill (one fresh Cursor per chunk, x-major).
func TestCursorSensitivity(t *testing.T) {
	src, err := ForDimension("overworld", 12345)
	if err != nil {
		t.Fatal(err)
	}
	type variant struct {
		name  string
		order [3]int
		mode  int // 0 per-chunk cursor, 1 nil cursor, 2 cursor shared across chunks
	}
	vs := []variant{{"zyx", [3]int{2, 1, 0}, 0}, {"yzx", [3]int{1, 2, 0}, 0}, {"nil", [3]int{0, 1, 2}, 1}, {"shared", [3]int{0, 1, 2}, 2}}
	diff := make([]int, len(vs))
	total := 0
	var shared climate.Cursor
	for cx := int32(-6); cx < 6; cx++ {
		for cz := int32(-6); cz < 6; cz++ {
			var base []string
			{
				var cur climate.Cursor
				base = fillOrder(src, cx, cz, -4, 24, [3]int{0, 1, 2}, &cur, false)
			}
			total += len(base)
			for i, v := range vs {
				var got []string
				switch v.mode {
				case 0:
					var cur climate.Cursor
					got = fillOrder(src, cx, cz, -4, 24, v.order, &cur, false)
				case 1:
					got = fillOrder(src, cx, cz, -4, 24, v.order, nil, false)
				default:
					got = fillOrder(src, cx, cz, -4, 24, v.order, &shared, false)
				}
				for j := range got {
					if got[j] != base[j] {
						diff[i]++
						t.Logf("%s chunk %d,%d sec %d q y%d z%d x%d: base %s got %s", v.name, cx, cz, j/64-4, j%64/16, j%16/4, j%4, base[j], got[j])
					}
				}
			}
		}
	}
	for i, v := range vs {
		t.Logf("%-6s differs in %d of %d quarts (%.4f%%)", v.name, diff[i], total, 100*float64(diff[i])/float64(total))
	}
}
