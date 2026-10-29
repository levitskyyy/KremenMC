package noise

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func TestAddToVolumeReference(t *testing.T) {
	data := reftest.Golden(t, "volume.txt", "kremenmc.probe.noise.VolumeProbe", "3", "60")
	var p NormalParams
	if err := json.Unmarshal([]byte(`{"amplitude_modifiers":[1.5,0.0,1.0,0.0,0.0,0.0],"base_amplitude":1.2453007926713473,"base_octave":-10,"octave_count":6}`), &p); err != nil {
		t.Fatal(err)
	}
	nn, err := NewNormalNoise(p)
	if err != nil {
		t.Fatal(err)
	}
	noises := map[string]Noise{
		"perlin":  NewPerlin(rng.NewXoroshiro(1)),
		"smeared": NewSmearedPerlin(rng.NewXoroshiro(2), 0.7),
		"simplex": NewSimplex(rng.NewXoroshiro(5)),
		"normal":  nn.Create(rng.NewXoroshiro(3)),
		"fbm":     CreateFbm(rng.NewXoroshiro(4), -7, 4.27, 12.75),
	}
	c := &checker{t: t}
	var n Noise
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		lhs, rhs, _ := strings.Cut(sc.Text(), " = ")
		f := strings.Fields(lhs)
		if f[0] == "noise" {
			n = noises[f[1]]
			continue
		}
		var v [9]int
		for i := range v {
			v[i], _ = strconv.Atoi(f[1+i])
		}
		xz, ys, amp, fill := hexF64(t, f[10]), hexF64(t, f[11]), hexF32(t, f[12]), hexF32(t, f[13])
		var got []string
		for ix := range v[0] {
			for iy := range v[1] {
				for iz := range v[2] {
					bx, by, bz := v[3]+ix*v[6], v[4]+iy*v[7], v[5]+iz*v[8]
					got = append(got, h32(n.AddTo(fill, float64(bx), float64(by), float64(bz), xz, ys, amp)))
				}
			}
		}
		c.eq(lhs, strings.Join(got, " "), rhs)
	}
	c.done()
}
