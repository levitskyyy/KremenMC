package structure

import (
	"math"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// SetStartAt is the start the structure set setKey ("minecraft:shipwrecks")
// would place at chunk cx,cz on its own, ignoring the other sets; nil when
// the set is not enabled or does not start there.
func (s *Stages) SetStartAt(setKey string, cx, cz int32) (*Start, error) {
	for _, set := range s.sets {
		if qualify(set.Key) != qualify(setKey) || !set.Placement.IsSpreadChunk(s.seed, cx, cz) {
			continue
		}
		for _, ws := range set.Structures {
			st, err := s.tryStructure(ws, cx, cz)
			if err != nil || st != nil {
				return st, err
			}
		}
	}
	return nil, nil
}

// TerrainProbe exposes the start-time terrain probe (tests, tools).
func (s *Stages) TerrainProbe() Probe { return s.probe }

// RingPositions is concentric_rings placement: the start chunks of the set,
// inferred from the black-box probe structlegacy.Rings (testdata/rings).
//
// A legacy random seeded with the world seed draws the first ring's angle
// (nextDouble·2π). Rings hold spread, then count += 2·count/(ring+1)
// positions (the last ring the remainder). Per position: distance
// nextDouble d → distance·(4 + 6·ring) + (d − 0.5)·2.5·distance chunks,
// chunk = round(cos/sin(angle)·dist), then a forked random (nextLong) snaps
// it to a preferred biome; the angle advances by 2π/count, and each new
// ring adds nextDouble·2π... (see ringAngleStep).
func RingPositions(p *Placement, seed int64, biome func(x, y, z int32) data.BiomeID) []ChunkXZ {
	pk, _ := wgdata.Load()
	pref := map[data.BiomeID]bool{}
	reg := data.Canonical()
	if strings.HasPrefix(p.PreferredBiomes, "#") {
		l, _ := pk.Tag("worldgen/biome", p.PreferredBiomes[1:])
		for _, n := range l {
			if id, ok := reg.BiomeByName(trimNS(n)); ok {
				pref[id] = true
			}
		}
	}
	r := rng.NewLegacy(seed)
	angle := r.NextDouble() * math.Pi * 2
	out := make([]ChunkXZ, 0, p.Count)
	inRing, ring := int32(0), int32(0)
	// The last ring keeps its full angular step and holds the remainder.
	per := p.Spread
	for i := int32(0); i < p.Count; i++ {
		d := r.NextDouble()
		dist := float64(4*p.Distance+p.Distance*ring*6) + (d-0.5)*float64(p.Distance)*2.5
		cx := int32(math.Round(math.Cos(angle) * dist))
		cz := int32(math.Round(math.Sin(angle) * dist))
		fr := rng.NewLegacy(r.NextLong())
		if bx, bz, ok := findBiomeHorizontal(cx<<4+8, cz<<4+8, 112, fr, func(x, z int32) bool { return pref[biome(x, 0, z)] }); ok {
			cx, cz = bx>>4, bz>>4
		}
		out = append(out, ChunkXZ{cx, cz})
		angle += math.Pi * 2 / float64(per)
		inRing++
		if inRing == per {
			ring++
			inRing = 0
			per += 2 * per / (ring + 1)
			per = min(per, p.Count-i)
			angle += ringAngleStep(r)
		}
	}
	return out
}

func ringAngleStep(r *rng.Legacy) float64 { return r.NextDouble() * math.Pi * 2 }

// ChunkXZ is a chunk position.
type ChunkXZ struct{ X, Z int32 }

// findBiomeHorizontal searches the square of quarter positions within
// radius blocks of x,z (step one quart) for a matching biome; matches are
// reservoir-sampled with r (k-th match kept when nextInt(k) == 0).
func findBiomeHorizontal(x, z, radius int32, r *rng.Legacy, ok func(x, z int32) bool) (int32, int32, bool) {
	qx, qz, qr := x>>2, z>>2, radius>>2
	var bx, bz int32
	n := int32(0)
	for dz := -qr; dz <= qr; dz++ {
		for dx := -qr; dx <= qr; dx++ {
			px, pz := (qx+dx)<<2, (qz+dz)<<2
			if !ok(px, pz) {
				continue
			}
			if n == 0 || r.NextIntn(n+1) == 0 {
				bx, bz = px, pz
			}
			n++
		}
	}
	return bx, bz, n > 0
}

type ringCache struct {
	once sync.Once
	at   map[ChunkXZ]bool
}

// ringChunk reports whether chunk cx,cz is one of a concentric_rings set's
// start chunks. The positions are computed once per set, with the probe's
// shared-cursor biome search when it has one (vanilla's tie-breaking).
func (s *Stages) ringChunk(set *Set, cx, cz int32) bool {
	v, _ := s.rings.LoadOrStore(set.Key, &ringCache{})
	rc := v.(*ringCache)
	rc.once.Do(func() {
		biome := s.probe.Biome
		if sb, ok := s.probe.(interface {
			SearchBiome() func(x, y, z int32) data.BiomeID
		}); ok {
			biome = sb.SearchBiome()
		}
		rc.at = map[ChunkXZ]bool{}
		for _, p := range RingPositions(&set.Placement, s.seed, biome) {
			rc.at[p] = true
		}
	})
	return rc.at[ChunkXZ{X: cx, Z: cz}]
}
