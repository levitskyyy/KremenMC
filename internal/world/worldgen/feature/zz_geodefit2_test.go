package feature

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func TestGeodeFitNoise2(t *testing.T) {
	f, err := os.Open(os.Getenv("NINT"))
	if err != nil {
		t.Skip()
	}
	defer f.Close()
	nn, _ := noise.NewNormalNoiseParity(-4, []float64{1})
	n := nn.Create(rng.NewWorldgenRandom(rng.NewLegacy(0)))
	o, _ := os.Create(os.Getenv("NOUT"))
	defer o.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var x, y, z int
		fmt.Sscan(sc.Text(), &x, &y, &z)
		fmt.Fprintf(o, "%d %d %d %.9f\n", x, y, z, n.Get(float64(x), float64(y), float64(z)))
	}
}
