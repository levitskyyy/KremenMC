package density

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

type valueCase struct {
	file, settings, seed, points, registry, pointSeed string
}

var valueCases = []valueCase{
	{"values_overworld_0.txt", "minecraft:overworld", "0", "160", "1", "1"},
	{"values_nether_42.txt", "minecraft:nether", "42", "120", "1", "2"},
	{"values_overworld_1.txt", "minecraft:overworld", "1", "200", "0", "3"},
	{"values_overworld_m1.txt", "minecraft:overworld", "-1", "200", "0", "4"},
	{"values_overworld_12345.txt", "minecraft:overworld", "12345", "200", "0", "5"},
	{"values_overworld_8266.txt", "minecraft:overworld", "8266389061079214669", "200", "0", "6"},
	{"values_overworld_rand.txt", "minecraft:overworld", "-4172144997902289642", "200", "0", "7"},
	{"values_large_biomes_12345.txt", "minecraft:large_biomes", "12345", "200", "0", "8"},
	{"values_amplified_0.txt", "minecraft:amplified", "0", "200", "0", "9"},
	{"values_nether_8266.txt", "minecraft:nether", "8266389061079214669", "200", "0", "10"},
	{"values_end_12345.txt", "minecraft:end", "12345", "200", "0", "11"},
	{"values_caves_1.txt", "minecraft:caves", "1", "150", "0", "12"},
	{"values_floating_islands_m1.txt", "minecraft:floating_islands", "-1", "150", "0", "13"},
}

func TestValuesReference(t *testing.T) {
	reg, err := Vanilla()
	if err != nil {
		t.Fatal(err)
	}
	for _, vc := range valueCases {
		t.Run(vc.file, func(t *testing.T) {
			data := reftest.Golden(t, vc.file, "kremenmc.probe.density.Values", vc.settings, vc.seed, vc.points, vc.registry, vc.pointSeed)
			seed, _ := strconv.ParseInt(vc.seed, 10, 64)
			r, err := NewRouter(reg, vc.settings, seed)
			if err != nil {
				t.Fatal(err)
			}
			c := &compiler{s: r.State, refs: map[string]Sampler{}, nodes: map[*Node]Sampler{}}
			var pts [][3]int32
			sc := bufio.NewScanner(bytes.NewReader(data))
			sc.Buffer(nil, 1<<22)
			total, bad := 0, 0
			for sc.Scan() {
				f := strings.Fields(sc.Text())
				switch f[0] {
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
					var s Sampler
					if fs, ok := r.Fields[f[1]]; ok {
						s = fs
					} else {
						s = c.node(&Node{Kind: KRef, Ref: f[1]})
						if c.err != nil {
							t.Fatal(c.err)
						}
					}
					ctx := &Ctx{}
					nb := 0
					for i, p := range pts {
						got := strconv.FormatUint(uint64(f32bits(s.Value(ctx, p[0], p[1], p[2]))), 16)
						total++
						if got != f[4+i] {
							bad++
							nb++
							if nb <= 2 {
								t.Errorf("%s at %v: got %s want %s", f[1], p, got, f[4+i])
							}
						}
					}
				case "c":
					var q [9]int64
					for i := range q {
						q[i], _ = strconv.ParseInt(f[1+i], 10, 64)
					}
					tp := r.ClimateTarget(&Ctx{}, int32(q[0]), int32(q[1]), int32(q[2]))
					got := [6]int64{tp.Temperature, tp.Humidity, tp.Continentalness, tp.Erosion, tp.Depth, tp.Weirdness}
					total++
					if got != [6]int64(q[3:]) {
						bad++
						if bad <= 30 {
							t.Errorf("climate %v: got %v want %v", q[:3], got, q[3:])
						}
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d/%d mismatches", bad, total)
			}
			t.Logf("%d checks", total)
		})
	}
}
