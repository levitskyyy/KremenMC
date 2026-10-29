package carver

import "math"

var sinTable = func() *[65536]float32 {
	var t [65536]float32
	for i := range t {
		t[i] = float32(math.Sin(float64(i) * math.Pi * 2 / 65536))
	}
	return &t
}()

// Sin and Cos are the reference Mth.sin and Mth.cos (table lookups).
func Sin(v float64) float32 { return sinTable[int(int64(v*sinScale))&0xffff] }
func Cos(v float64) float32 { return sinTable[int(int64(v*sinScale+16384))&0xffff] }

const sinScale = 10430.378350470453
