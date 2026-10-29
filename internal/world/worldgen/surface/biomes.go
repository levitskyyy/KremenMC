package surface

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// ObfuscateSeed is the reference BiomeManager.obfuscateSeed: the first 8
// bytes (little endian) of SHA-256 over the little-endian seed.
func ObfuscateSeed(seed int64) int64 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h := sha256.Sum256(b[:])
	return int64(binary.LittleEndian.Uint64(h[:8]))
}

func lcgNext(a, b int64) int64 { return a*(a*6364136223846793005+1442695040888963407) + b }

func fiddle(s int64) float64 {
	return (float64(((s>>24)%1024+1024)%1024)/1024 - 0.5) * 0.9
}

func fiddledDistance(seed int64, x, y, z int32, xf, yf, zf float64) float64 {
	m := lcgNext(seed, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	m = lcgNext(m, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	d := fiddle(m)
	m = lcgNext(m, seed)
	e := fiddle(m)
	m = lcgNext(m, seed)
	f := fiddle(m)
	zz, yy, xx := zf+f, yf+e, xf+d
	return zz*zz + yy*yy + xx*xx
}

// zoomQuart is the quart cell the reference BiomeManager.getBiome reads for
// a block position.
func zoomQuart(seed int64, x, y, z int32) (int32, int32, int32) {
	i, j, k := x-2, y-2, z-2
	l, m, n := i>>2, j>>2, k>>2
	d, e, f := float64(i&3)/4, float64(j&3)/4, float64(k&3)/4
	best, bestD := 0, 0.0
	for o := range 8 {
		p, q, r := l, m, n
		g, h, s := d, e, f
		if o&4 != 0 {
			p, g = l+1, d-1
		}
		if o&2 != 0 {
			q, h = m+1, e-1
		}
		if o&1 != 0 {
			r, s = n+1, f-1
		}
		dd := fiddledDistance(seed, p, q, r, g, h, s)
		if o == 0 || bestD > dd {
			best, bestD = o, dd
		}
	}
	qx, qy, qz := l, m, n
	if best&4 != 0 {
		qx++
	}
	if best&2 != 0 {
		qy++
	}
	if best&1 != 0 {
		qz++
	}
	return qx, qy, qz
}

// climate is the part of a biome's climate settings the temperature
// condition reads.
type climate struct {
	temperature float32
	frozen      bool
}

func loadClimate(reg Registry, key string) (climate, error) {
	b, ok := reg.JSON("worldgen/biome", key)
	if !ok {
		return climate{}, fmt.Errorf("surface: unknown biome %s", key)
	}
	var raw struct {
		Temperature float32 `json:"temperature"`
		Modifier    string  `json:"temperature_modifier"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return climate{}, fmt.Errorf("surface: biome %s: %w", key, err)
	}
	return climate{temperature: raw.Temperature, frozen: raw.Modifier == "frozen"}, nil
}

// tempNoises are the reference Biome static noises (seed independent).
type tempNoises struct {
	temp, frozen, info noise.Noise
}

var biomeNoises = func() tempNoises {
	r := rng.NewLegacy(3456)
	amps := []float32{1.0 / 7, 2.0 / 7, 4.0 / 7}
	freqs := []float64{1, 0.5, 0.25}
	var ls []noise.Layer
	for i := range 3 {
		ls = append(ls, noise.Layer{Noise: noise.NewSimplexOpt(r, true), Frequency: freqs[i], Amplitude: amps[i]})
	}
	return tempNoises{
		temp:   noise.NewSimplexOpt(rng.NewLegacy(1234), true),
		frozen: noise.NewStack(ls),
		info:   noise.NewSimplexOpt(rng.NewLegacy(2345), true),
	}
}()

// frozenModifier is the reference TemperatureModifier.FROZEN.
func frozenModifier(x, z int32, t float32) float32 {
	n := &biomeNoises
	f := n.frozen.Get2(float64(x)*0.05, float64(z)*0.05)*7 + n.info.Get2(float64(x)*0.2, float64(z)*0.2)
	if f < 0.3 && n.info.Get2(float64(x)*0.09, float64(z)*0.09) < 0.8 {
		return 0.2
	}
	return t
}

// heightAdjustedTemperature is the reference Biome.getHeightAdjustedTemperature.
func (c climate) heightAdjustedTemperature(x, y, z, seaLevel int32) float32 {
	t := c.temperature
	if c.frozen {
		t = frozenModifier(x, z, t)
	}
	if y > seaLevel+17 {
		n := biomeNoises.temp.Get2(float64(float32(x)/8), float64(float32(z)/8)) * 8
		return t - (n+float32(y)-float32(seaLevel+17))*0.05/40
	}
	return t
}

func (c climate) coldEnoughToSnow(x, y, z, seaLevel int32) bool {
	return c.heightAdjustedTemperature(x, y, z, seaLevel) < 0.15
}
