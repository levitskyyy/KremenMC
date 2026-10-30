package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Modifier is a vanilla placement_modifier. Apply draws its random values
// for one input position and calls emit for every output position in order;
// placement is depth-first, as vanilla's lazily streamed positions are.
type Modifier interface {
	Apply(c *placeCtx, r *Random, p Pos, emit func(Pos))
}

type placeCtx struct {
	lv  *Level
	top *Placed // nil for features placed by other features
}

// biomeInfo is the reference Biome.BIOME_INFO_NOISE.
var biomeInfo noise.Noise = noise.NewSimplexOpt(rng.NewLegacy(2345), true)

type modCount struct{ n IntProvider }

func (m modCount) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	for range m.n.Sample(r) {
		emit(p)
	}
}

type modNoiseThresholdCount struct {
	level        float64
	below, above int
}

func (m modNoiseThresholdCount) Apply(_ *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	v := float64(biomeInfo.Get2(float64(p.X)/200, float64(p.Z)/200))
	n := m.above
	if v < m.level {
		n = m.below
	}
	for range n {
		emit(p)
	}
}

type modNoiseBasedCount struct {
	ratio          int
	factor, offset float64
}

func (m modNoiseBasedCount) Apply(_ *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	v := float64(biomeInfo.Get2(float64(p.X)/m.factor, float64(p.Z)/m.factor))
	n := int(math.Ceil((v + m.offset) * float64(m.ratio)))
	for range n {
		emit(p)
	}
}

type modInSquare struct{}

func (modInSquare) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	x := int(r.NextIntn(16))
	z := int(r.NextIntn(16))
	emit(Pos{p.X + x, p.Y, p.Z + z})
}

type modHeightmap struct{ h world.Heightmap }

func (m modHeightmap) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	y := c.lv.Height(m.h, p.X, p.Z)
	if y > c.lv.MinY() {
		emit(Pos{p.X, y, p.Z})
	}
}

type modBiome struct{}

func (modBiome) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	if c.top != nil && c.lv.st.biomeHas(c.lv.Biome(p), c.top) {
		emit(p)
	}
}

type modRarity struct{ chance int }

func (m modRarity) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	if r.NextFloat() < 1/float32(m.chance) {
		emit(p)
	}
}

type modChance struct{ chance float32 }

func (m modChance) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	if r.NextFloat() < m.chance {
		emit(p)
	}
}

type modOffset struct{ x, y, z IntProvider }

func (m modOffset) Apply(_ *placeCtx, r *Random, p Pos, emit func(Pos)) {
	dx := m.x.Sample(r)
	dy := m.y.Sample(r)
	dz := m.z.Sample(r)
	emit(Pos{p.X + dx, p.Y + dy, p.Z + dz})
}

type modFilter struct{ pred Predicate }

func (m modFilter) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	if m.pred.Test(c.lv, p) {
		emit(p)
	}
}

type modWaterDepth struct{ max int }

func (m modWaterDepth) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	floor := c.lv.Height(world.OceanFloor, p.X, p.Z)
	surf := c.lv.Height(world.WorldSurface, p.X, p.Z)
	if surf-floor <= m.max {
		emit(p)
	}
}

type modSurfaceRelative struct {
	h        world.Heightmap
	min, max int
}

func (m modSurfaceRelative) Apply(c *placeCtx, _ *Random, p Pos, emit func(Pos)) {
	y := c.lv.Height(m.h, p.X, p.Z)
	if lo, hi := int64(y)+int64(m.min), int64(y)+int64(m.max); int64(p.Y) >= lo && int64(p.Y) <= hi {
		emit(p)
	}
}

type modHeightRange struct{ h HeightProvider }

func (m modHeightRange) Apply(c *placeCtx, r *Random, p Pos, emit func(Pos)) {
	emit(Pos{p.X, m.h.Sample(r, c.lv.MinY(), c.lv.genHeight()), p.Z})
}

func parseHeightmap(s string) (world.Heightmap, error) {
	for h := world.Heightmap(0); h <= world.MotionBlockingNoLeaves; h++ {
		if h.String() == s {
			return h, nil
		}
	}
	return 0, fmt.Errorf("unknown heightmap %q", s)
}

func (rg *registry) parseModifier(b json.RawMessage) (Modifier, error) {
	var p struct {
		Type      string          `json:"type"`
		Count     json.RawMessage `json:"count"`
		Heightmap string          `json:"heightmap"`
		Chance    json.Number     `json:"chance"`
		X, Y, Z   json.RawMessage
		Predicate json.RawMessage `json:"predicate"`
		NoiseLvl  float64         `json:"noise_level"`
		Below     int             `json:"below_noise"`
		Above     int             `json:"above_noise"`
		Ratio     int             `json:"noise_to_count_ratio"`
		Factor    float64         `json:"noise_factor"`
		Offset    float64         `json:"noise_offset"`
		MaxDepth  int             `json:"max_water_depth"`
		MinIncl   *int            `json:"min_inclusive"`
		MaxIncl   *int            `json:"max_inclusive"`
		Height    json.RawMessage `json:"height"`
	}
	t := typeOf(b)
	if f, ok := modifierTypes[t]; ok {
		return f(rg, b)
	}
	switch t {
	case "count_on_every_layer", "environment_scan", "cuboid", "randomly_selected", "fixed_placement":
		return nil, unsupported("placement_modifier", t)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("placement modifier %s: %w", t, err)
	}
	switch t {
	case "count":
		n, err := parseInt(p.Count)
		return modCount{n}, err
	case "noise_threshold_count":
		return modNoiseThresholdCount{p.NoiseLvl, p.Below, p.Above}, nil
	case "noise_based_count":
		return modNoiseBasedCount{p.Ratio, p.Factor, p.Offset}, nil
	case "in_square":
		return modInSquare{}, nil
	case "heightmap":
		h, err := parseHeightmap(p.Heightmap)
		return modHeightmap{h}, err
	case "biome":
		return modBiome{}, nil
	case "rarity_filter":
		n, err := p.Chance.Int64()
		return modRarity{int(n)}, err
	case "random_chance":
		f, err := p.Chance.Float64()
		return modChance{float32(f)}, err
	case "offset":
		var m modOffset
		var err error
		for _, f := range []struct {
			dst *IntProvider
			src json.RawMessage
		}{{&m.x, p.X}, {&m.y, p.Y}, {&m.z, p.Z}} {
			if len(f.src) == 0 {
				*f.dst = constInt(0)
				continue
			}
			if *f.dst, err = parseInt(f.src); err != nil {
				return nil, err
			}
		}
		return m, nil
	case "block_predicate_filter":
		pr, err := rg.parsePredicate(p.Predicate)
		return modFilter{pr}, err
	case "surface_water_depth_filter":
		return modWaterDepth{p.MaxDepth}, nil
	case "surface_relative_threshold_filter":
		h, err := parseHeightmap(p.Heightmap)
		m := modSurfaceRelative{h: h, min: math.MinInt32, max: math.MaxInt32}
		if p.MinIncl != nil {
			m.min = *p.MinIncl
		}
		if p.MaxIncl != nil {
			m.max = *p.MaxIncl
		}
		return m, err
	case "height_range":
		h, err := parseHeight(p.Height)
		return modHeightRange{h}, err
	}
	return nil, unsupported("placement_modifier", p.Type)
}
