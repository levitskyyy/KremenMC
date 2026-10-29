package spawn

import (
	"math"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// TestChunkGenSeedFingerprint checks which random the reference uses for
// spawning during chunk generation. A real dedicated server generates a
// seeded noise world; every creature saved in a generated chunk must have a
// yaw equal to nextFloat()*360 drawn from ChunkGenSeed(seed, cx, cz) within
// the first draws, and an x/z that is an integer block coordinate or the
// chunk-edge clamp by the entity width (no +0.5 centering).
func TestChunkGenSeedFingerprint(t *testing.T) {
	b := reftest.Golden(t, "genseed.golden", "kremenmc.probe.spawn.GenSeed", "12345", "20000")
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 8 || f[0] != "entity" {
			continue
		}
		et, ok := EntityByName(f[1])
		if !ok || et.Category != Creature {
			continue
		}
		n++
		x, _ := hexDouble(f[4])
		z, _ := hexDouble(f[6])
		cx, cz := int(math.Floor(x))>>4, int(math.Floor(z))>>4
		found := -1
		for k := 0; k < 1000 && found < 0; k++ {
			s := ChunkGenSeed(12345, cx, cz)
			for range k {
				s.NextInt()
			}
			if fhex(s.NextFloat()*360) == f[7] {
				found = k
			}
		}
		if found < 0 {
			t.Errorf("%s: yaw %s not found in the decoration-seeded stream of chunk %d,%d", line, f[7], cx, cz)
		}
		w := float64(et.Width)
		for _, v := range []float64{x - float64(cx*16), z - float64(cz*16)} {
			if v != math.Floor(v) && v != w && v != 16-w {
				t.Errorf("%s: coordinate offset %v is neither integral nor the width clamp %v", line, v, w)
			}
		}
	}
	if n < 3 {
		t.Fatalf("only %d creatures in the reference world", n)
	}
	t.Logf("%d creatures fingerprinted", n)
}
