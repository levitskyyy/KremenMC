package biome

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
)

// TestScanSensitive lists chunks whose biomes depend on the quart order
// (set KREMENMC_SCAN=seed:dim:radius).
func TestScanSensitive(t *testing.T) {
	spec := os.Getenv("KREMENMC_SCAN")
	if spec == "" {
		t.Skip()
	}
	f := strings.Split(spec, ":")
	seed, _ := strconv.ParseInt(f[0], 10, 64)
	r, _ := strconv.Atoi(f[2])
	src, err := ForDimension(f[1], seed)
	if err != nil {
		t.Fatal(err)
	}
	minSec, secs := int32(-4), int32(24)
	if f[1] != "overworld" {
		minSec, secs = 0, 16
	}
	var out []string
	for cx := int32(-r); cx < int32(r); cx++ {
		for cz := int32(-r); cz < int32(r); cz++ {
			var c1, c2, c3 climate.Cursor
			a := fillOrder(src, cx, cz, minSec, secs, [3]int{0, 1, 2}, &c1, false)
			b := fillOrder(src, cx, cz, minSec, secs, [3]int{2, 1, 0}, &c2, false)
			c := fillOrder(src, cx, cz, minSec, secs, [3]int{1, 2, 0}, &c3, false)
			for j := range a {
				if a[j] != b[j] || a[j] != c[j] {
					out = append(out, fmt.Sprintf("chunk:%d,%d", cx, cz))
					break
				}
			}
		}
	}
	fmt.Println(strings.Join(out, "+"))
}
