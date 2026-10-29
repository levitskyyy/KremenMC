package structure

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// MsPlaceTraced places a mineshaft piece and reports every draw.
func MsPlaceTraced(p Piece, lv Level, r *rng.WorldgenRandom, clip Box, trace func(label, val string)) {
	m := newMsPlacer(p.(*msPiece), lv, r, clip)
	m.trace = trace
	m.persistSpider = true
	m.run()
}

// MsLayoutForTest is the layout at y 50 and the random after it.
func MsLayoutForTest(seed int64, cx, cz int32) ([]Box, *rng.WorldgenRandom) {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	r.NextDouble()
	ps := msLayout(r, cx, cz, true)
	var out []Box
	for _, p := range ps {
		out = append(out, p.box)
	}
	return out, r
}

// StagesProbeForTest exposes the terrain probe of s.
func StagesProbeForTest(s *Stages) Probe { return s.probe }
