package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// geode is the geode feature (amethyst geodes), fitted to the
// kremenmc.probe.feat_geode.Geode probe (controlled configurations with
// draw logs, stone and hashed-hole scenes; see docs/vanilla-parity.md).
//
// Draws: distribution_points, nextDouble (crack size), nextFloat (crack
// chance), per point outer_wall_distance x, y, z and point_offset, then
// nextInt(4) for the crack corner when there is a crack. The box
// origin+[min_gen_offset, max_gen_offset]³ is walked x fastest, then y,
// then z. With D = points/max(outer_wall_distance) and
// s = Σ (1/sqrt(d²+point_offset) + noise·noise_multiplier) over the
// points (noise: parity NormalNoise, first octave -4, from Legacy(level seed)), a block with
// s >= 1/sqrt(outer+D) belongs to the geode: crack, then filling
// (s >= 1/sqrt(filling)), inner (1/sqrt(inner+D)), middle, outer. The crack
// is three points at y 1, 5, 7 on the vertical line through the corner
// chosen by the draw (x, z each 0 or 2·points+1); a block is cracked when
// Σ 1/sqrt(d²+crack_point_offset) >= 1/sqrt(size), size =
// base_crack_size + double/2 (+D with more than three points). Each inner
// block draws a float for the alternate layer; an alternate block draws
// another for use_potential_placements_chance. After the walk every
// potential position draws one inner placement and puts it on the first
// air or water neighbour (down, up, north, south, west, east), facing away.
type geode struct {
	filling, inner, alt, middle, outer StateProvider
	placements                         []data.StateID
	cannotReplace, invalid             blockSet

	lFill, lInner, lMiddle, lOuter float64

	crackChance, crackBase float64
	crackOff               int

	potential, altChance  float64
	owd, points, pointOff IntProvider
	owdMax                int
	minOff, maxOff        int
	noiseMul              float64
	invalidMax            int
}

var geodeDirs = [6]struct {
	name string
	d    Pos
}{{"down", Pos{0, -1, 0}}, {"up", Pos{0, 1, 0}}, {"north", Pos{0, 0, -1}}, {"south", Pos{0, 0, 1}}, {"west", Pos{-1, 0, 0}}, {"east", Pos{1, 0, 0}}}

// worldSeed is the level seed (WorldGenLevel.getSeed); regions without one
// (tests) report 0.
func (lv *Level) worldSeed() int64 {
	if s, ok := lv.rg.(interface{ Seed() int64 }); ok {
		return s.Seed()
	}
	return 0
}

// geodeNoise is the per-placement noise built from the level seed.
func geodeNoise(seed int64) noise.Noise {
	nn, err := noise.NewNormalNoiseParity(-4, []float64{1})
	if err != nil {
		panic(err)
	}
	return nn.Create(rng.NewWorldgenRandom(rng.NewLegacy(seed)))
}

func invSqrt(q float64) float64 { return 1 / math.Sqrt(q) }

func (f geode) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	n := f.points.Sample(r)
	nz := geodeNoise(lv.worldSeed())
	crackD := r.NextDouble()
	crack := float64(r.NextFloat()) < f.crackChance
	type point struct {
		p   Pos
		off int
	}
	pts := make([]point, 0, n)
	invalid := 0
	for range n {
		x := f.owd.Sample(r)
		y := f.owd.Sample(r)
		z := f.owd.Sample(r)
		q := p.Add(Pos{x, y, z})
		b := lv.Block(q)
		if data.IsAir(b) || f.invalid.has(b) {
			invalid++
			if invalid > f.invalidMax {
				return false
			}
		}
		pts = append(pts, point{q, f.pointOff.Sample(r)})
	}
	d := float64(n) / float64(f.owdMax)
	tFill := invSqrt(f.lFill)
	tInner := invSqrt(f.lInner + d)
	tMiddle := invSqrt(f.lMiddle + d)
	tOuter := invSqrt(f.lOuter + d)
	size := f.crackBase + crackD/2
	if n > 3 {
		size += d
	}
	tCrack := invSqrt(size)
	var cracks []Pos
	if crack {
		far := 2*n + 1
		var cx, cz int
		switch r.NextIntn(4) {
		case 0:
			cx = far
		case 1:
			cz = far
		case 2:
			cx, cz = far, far
		}
		for _, y := range [3]int{1, 5, 7} {
			cracks = append(cracks, p.Add(Pos{cx, y, cz}))
		}
	}
	var potential []Pos
	for z := f.minOff; z <= f.maxOff; z++ {
		for y := f.minOff; y <= f.maxOff; y++ {
			for x := f.minOff; x <= f.maxOff; x++ {
				q := p.Add(Pos{x, y, z})
				nv := float64(nz.Get(float64(q.X), float64(q.Y), float64(q.Z))) * f.noiseMul
				s := 0.0
				for _, pt := range pts {
					s += invSqrt(float64(dist2(q, pt.p)+pt.off)) + nv
				}
				if s < tOuter {
					continue
				}
				if crack {
					sc := 0.0
					for _, cp := range cracks {
						sc += invSqrt(float64(dist2(q, cp)+f.crackOff)) + nv
					}
					if sc >= tCrack {
						f.set(lv, q, data.Air)
						continue
					}
				}
				switch {
				case s >= tFill:
					f.set(lv, q, f.filling.State(lv, r, q))
				case s >= tInner:
					if float64(r.NextFloat()) < f.altChance {
						f.set(lv, q, f.alt.State(lv, r, q))
						if float64(r.NextFloat()) < f.potential {
							potential = append(potential, q)
						}
					} else {
						f.set(lv, q, f.inner.State(lv, r, q))
					}
				case s >= tMiddle:
					f.set(lv, q, f.middle.State(lv, r, q))
				default:
					f.set(lv, q, f.outer.State(lv, r, q))
				}
			}
		}
	}
	reg := data.Canonical()
	for _, q := range potential {
		st := f.placements[r.NextIntn(int32(len(f.placements)))]
		for _, dir := range geodeDirs {
			t := q.Add(dir.d)
			b := lv.Block(t)
			water := fluidOf(b) == "water" && reg.BlockOf(b).Name == "minecraft:water"
			if !data.IsAir(b) && !water {
				continue
			}
			s, _ := reg.With(st, "facing", dir.name)
			if water {
				s, _ = reg.With(s, "waterlogged", "true")
			}
			f.set(lv, t, s)
			break
		}
	}
	return true
}

func dist2(a, b Pos) int {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}

func (f geode) set(lv *Level, q Pos, s data.StateID) {
	if f.cannotReplace.has(lv.Block(q)) {
		return
	}
	lv.SetBlock(q, s)
}

// intMax is IntProvider.getMaxValue for the provider types geodes use.
func intMax(p IntProvider) (int, error) {
	switch v := p.(type) {
	case constInt:
		return int(v), nil
	case uniformInt:
		return v.max, nil
	case biasedInt:
		return v.max, nil
	case clampedInt:
		return v.max, nil
	}
	return 0, fmt.Errorf("int provider %T: no max", p)
}

func init() {
	featureTypes["geode"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Blocks struct {
				Filling       json.RawMessage   `json:"filling_provider"`
				Inner         json.RawMessage   `json:"inner_layer_provider"`
				Alt           json.RawMessage   `json:"alternate_inner_layer_provider"`
				Middle        json.RawMessage   `json:"middle_layer_provider"`
				Outer         json.RawMessage   `json:"outer_layer_provider"`
				Placements    []json.RawMessage `json:"inner_placements"`
				CannotReplace string            `json:"cannot_replace"`
				Invalid       string            `json:"invalid_blocks"`
			} `json:"blocks"`
			Layers struct {
				Filling *float64 `json:"filling"`
				Inner   *float64 `json:"inner_layer"`
				Middle  *float64 `json:"middle_layer"`
				Outer   *float64 `json:"outer_layer"`
			} `json:"layers"`
			Crack struct {
				Chance *float64 `json:"generate_crack_chance"`
				Base   *float64 `json:"base_crack_size"`
				Offset *int     `json:"crack_point_offset"`
			} `json:"crack"`
			Potential   *float64        `json:"use_potential_placements_chance"`
			AltChance   *float64        `json:"use_alternate_layer0_chance"`
			RequireAlt  *bool           `json:"placements_require_layer0_alternate"`
			OWD         json.RawMessage `json:"outer_wall_distance"`
			Points      json.RawMessage `json:"distribution_points"`
			PointOffset json.RawMessage `json:"point_offset"`
			MinOff      *int            `json:"min_gen_offset"`
			MaxOff      *int            `json:"max_gen_offset"`
			NoiseMul    *float64        `json:"noise_multiplier"`
			InvalidMax  *int            `json:"invalid_blocks_threshold"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("geode: %w", err)
		}
		if raw.RequireAlt != nil && !*raw.RequireAlt {
			return nil, unsupported("feature", "geode (placements_require_layer0_alternate=false)")
		}
		f := geode{
			lFill: orF(raw.Layers.Filling, 1.7), lInner: orF(raw.Layers.Inner, 2.2),
			lMiddle: orF(raw.Layers.Middle, 3.2), lOuter: orF(raw.Layers.Outer, 4.2),
			crackChance: orF(raw.Crack.Chance, 1), crackBase: orF(raw.Crack.Base, 2), crackOff: orI(raw.Crack.Offset, 2),
			potential: orF(raw.Potential, 0.35), altChance: orF(raw.AltChance, 0),
			minOff: orI(raw.MinOff, -16), maxOff: orI(raw.MaxOff, 16),
			noiseMul: orF(raw.NoiseMul, 0.05), invalidMax: orI(raw.InvalidMax, 16),
		}
		var err error
		for _, e := range []struct {
			dst *StateProvider
			src json.RawMessage
		}{{&f.filling, raw.Blocks.Filling}, {&f.inner, raw.Blocks.Inner}, {&f.alt, raw.Blocks.Alt}, {&f.middle, raw.Blocks.Middle}, {&f.outer, raw.Blocks.Outer}} {
			if *e.dst, err = rg.parseStateProvider(e.src); err != nil {
				return nil, fmt.Errorf("geode: %w", err)
			}
		}
		for _, pl := range raw.Blocks.Placements {
			st, err := parseBlockState(pl)
			if err != nil {
				return nil, fmt.Errorf("geode inner_placements: %w", err)
			}
			f.placements = append(f.placements, st)
		}
		if len(f.placements) == 0 {
			return nil, unsupported("feature", "geode (no inner_placements)")
		}
		if f.cannotReplace, err = rg.tag(raw.Blocks.CannotReplace); err != nil {
			return nil, fmt.Errorf("geode cannot_replace: %w", err)
		}
		if f.invalid, err = rg.tag(raw.Blocks.Invalid); err != nil {
			return nil, fmt.Errorf("geode invalid_blocks: %w", err)
		}
		ip := func(b json.RawMessage, a, z int) (IntProvider, error) {
			if len(b) == 0 {
				return uniformInt{a, z}, nil
			}
			return parseInt(b)
		}
		if f.owd, err = ip(raw.OWD, 4, 5); err != nil {
			return nil, fmt.Errorf("geode outer_wall_distance: %w", err)
		}
		if f.points, err = ip(raw.Points, 3, 4); err != nil {
			return nil, fmt.Errorf("geode distribution_points: %w", err)
		}
		if f.pointOff, err = ip(raw.PointOffset, 1, 2); err != nil {
			return nil, fmt.Errorf("geode point_offset: %w", err)
		}
		if f.owdMax, err = intMax(f.owd); err != nil {
			return nil, fmt.Errorf("geode outer_wall_distance: %w", err)
		}
		return f, nil
	}
}

func orF(p *float64, d float64) float64 {
	if p == nil {
		return d
	}
	return *p
}

func orI(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}
