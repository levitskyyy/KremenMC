package feature

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func TestGeodeFitNoise(t *testing.T) {
	f, err := os.Open(os.Getenv("NINT"))
	if err != nil {
		t.Skip()
	}
	defer f.Close()
	type row struct {
		x, y, z int
		a, b    float64
	}
	var rows []row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r row
		fmt.Sscan(sc.Text(), &r.x, &r.y, &r.z, &r.a, &r.b)
		rows = append(rows, r)
	}
	cands := map[string]noise.Noise{}
	for _, fo := range []int{-6, -5, -4, -3, -2} {
		nn, _ := noise.NewNormalNoiseParity(fo, []float64{1})
		cands[fmt.Sprintf("modern%d", fo)] = nn.Create(rng.NewWorldgenRandom(rng.NewLegacy(0)))
		cands[fmt.Sprintf("modernX%d", fo)] = nn.Create(rng.NewXoroshiro(0))
		st, _ := nn.CreateForLegacyNetherBiome(rng.NewWorldgenRandom(rng.NewLegacy(0)))
		cands[fmt.Sprintf("legacy%d", fo)] = st
	}
	for name, n := range cands {
		for _, sh := range []int{0} {
			bad := 0
			for _, r := range rows {
				v := float64(n.Get(float64(r.x+sh), float64(r.y), float64(r.z)))
				if v < r.a-1e-4 || v >= r.b+1e-4 {
					bad++
				}
			}
			t.Logf("%s: %d/%d bad", name, bad, len(rows))
		}
	}
}
