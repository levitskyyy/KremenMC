package terrain

import (
	"bufio"
	"bytes"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// TestAquiferFunctionValues checks the six compiled aquifer functions bit for
// bit against the reference at single points.
func TestAquiferFunctionValues(t *testing.T) {
	cases := []struct{ file, settings, seed, points, pointSeed string }{
		{"aqvalues_overworld_0.txt", "minecraft:overworld", "0", "300", "1"},
		{"aqvalues_overworld_12345.txt", "minecraft:overworld", "12345", "300", "2"},
		{"aqvalues_overworld_8266.txt", "minecraft:overworld", "8266389061079214669", "300", "3"},
	}
	for _, vc := range cases {
		t.Run(vc.file, func(t *testing.T) {
			data := reftest.Golden(t, vc.file, "kremenmc.probe.aquifer.Values", vc.settings, vc.seed, vc.points, vc.pointSeed)
			seed, _ := strconv.ParseInt(vc.seed, 10, 64)
			r, err := density.CachedRouter(vc.settings, seed)
			if err != nil {
				t.Fatal(err)
			}
			var pts [][3]int32
			sc := bufio.NewScanner(bytes.NewReader(data))
			sc.Buffer(nil, 1<<22)
			total, bad := 0, 0
			for sc.Scan() {
				f := strings.Fields(sc.Text())
				switch f[0] {
				case "error":
					t.Error(sc.Text())
				case "points":
					for _, p := range f[1:] {
						var v [3]int32
						for i, s := range strings.Split(p, ",") {
							n, _ := strconv.Atoi(s)
							v[i] = int32(n)
						}
						pts = append(pts, v)
					}
				case "f":
					s := r.Aquifers[f[1]]
					if s == nil {
						t.Fatalf("no aquifer function %s", f[1])
					}
					ctx := &density.Ctx{}
					for i, p := range pts {
						got := strconv.FormatUint(uint64(math.Float32bits(s.Value(ctx, p[0], p[1], p[2]))), 16)
						total++
						if got != f[2+i] {
							bad++
							if bad <= 5 {
								t.Errorf("%s at %v: got %s want %s", f[1], p, got, f[2+i])
							}
						}
					}
				}
			}
			if bad > 0 || total == 0 {
				t.Errorf("%d/%d mismatches", bad, total)
			}
		})
	}
}
