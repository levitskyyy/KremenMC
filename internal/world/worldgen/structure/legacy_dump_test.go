package structure_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// TestDumpLegacyStarts logs the reference piece lists of legacy structures
// (KREMENMC_DUMP=1).
func TestDumpLegacyStarts(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	for _, w := range strings.Split(os.Getenv("KREMENMC_DUMP"), ",") {
		dir := "../../../../.refcache/26.3/worlds/" + w + "/world/dimensions/minecraft/overworld/region"
		ref := refStarts(t, dir)
		keys := make([]string, 0, len(ref))
		for k := range ref {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := ref[k]
			ch := c["Children"].(nbt.List)
			fmt.Printf("%s %s: %d pieces\n", w, k, len(ch.Items))
			if strings.Contains(k, "shipwreck") {
				continue
			}
			for _, it := range ch.Items {
				pc := it.(nbt.Compound)
				ks := make([]string, 0, len(pc))
				for kk := range pc {
					ks = append(ks, kk)
				}
				sort.Strings(ks)
				var sb strings.Builder
				for _, kk := range ks {
					fmt.Fprintf(&sb, " %s=%v", kk, pc[kk])
				}
				fmt.Println("   ", sb.String())
			}
		}
	}
}
