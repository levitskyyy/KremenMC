package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/carver"
)

// largeDripstone is the large_dripstone feature. Fitted to
// kremenmc.probe.feat_ow.Place traces and kremenmc.probe.feat_drip.Fn
// tables of the single helpers (shape curve, circle test, move-back,
// column writes, wind); see docs/vanilla-parity.md.
type largeDripstone struct {
	replace                    blockSet
	searchRange                int
	radiusMin, radiusMax       int
	heightScale                FloatProvider
	maxRatio                   float32
	stalactiteBl, stalagmiteBl FloatProvider
	windSpeed                  FloatProvider
	minRadiusWind              int
	minBluntWind               float32
	dripstone                  data.StateID
}

func parseLargeDripstone(rg *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		Replace     json.RawMessage `json:"replaceable_blocks"`
		Range       *int            `json:"floor_to_ceiling_search_range"`
		Radius      json.RawMessage `json:"column_radius"`
		HeightScale json.RawMessage `json:"height_scale"`
		Ratio       float32         `json:"max_column_radius_to_cave_height_ratio"`
		StalactiteB json.RawMessage `json:"stalactite_bluntness"`
		StalagmiteB json.RawMessage `json:"stalagmite_bluntness"`
		Wind        json.RawMessage `json:"wind_speed"`
		MinRadius   int             `json:"min_radius_for_wind"`
		MinBlunt    float32         `json:"min_bluntness_for_wind"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("large_dripstone: %w", err)
	}
	f := &largeDripstone{searchRange: 30, maxRatio: c.Ratio, minRadiusWind: c.MinRadius, minBluntWind: c.MinBlunt}
	if c.Range != nil {
		f.searchRange = *c.Range
	}
	var err error
	if f.replace, err = rg.blocks(oneOrMany(c.Replace)); err != nil {
		return nil, fmt.Errorf("large_dripstone: %w", err)
	}
	ip, err := parseInt(c.Radius)
	if err != nil {
		return nil, fmt.Errorf("large_dripstone: %w", err)
	}
	if f.radiusMin, f.radiusMax, err = intBounds(ip); err != nil {
		return nil, fmt.Errorf("large_dripstone: %w", err)
	}
	for _, fp := range []struct {
		dst *FloatProvider
		src json.RawMessage
	}{{&f.heightScale, c.HeightScale}, {&f.stalactiteBl, c.StalactiteB}, {&f.stalagmiteBl, c.StalagmiteB}, {&f.windSpeed, c.Wind}} {
		if *fp.dst, err = parseFloatMore(fp.src); err != nil {
			return nil, fmt.Errorf("large_dripstone: %w", err)
		}
	}
	f.dripstone, _ = data.DefaultState("dripstone_block")
	return f, nil
}

func oneOrMany(b json.RawMessage) []string {
	var one string
	if json.Unmarshal(b, &one) == nil {
		return []string{one}
	}
	var many []string
	_ = json.Unmarshal(b, &many)
	return many
}

// intBounds is IntProvider.getMinValue/getMaxValue for the providers
// large_dripstone uses.
func intBounds(p IntProvider) (int, int, error) {
	switch v := p.(type) {
	case constInt:
		return int(v), int(v), nil
	case uniformInt:
		return v.min, v.max, nil
	case clampedInt:
		lo, hi, err := intBounds(v.src)
		return max(lo, v.min), min(hi, v.max), err
	}
	return 0, 0, fmt.Errorf("int provider %T: no bounds", p)
}

func spEmptyOrWaterOrLava(st data.StateID) bool { return spEmptyOrWater(st) || spIsLava(st) }

// spShapeHeight is SpeleothemUtils.getSpeleothemHeight: with
// e = max(r, bluntness)/radius·0.384, radius/0.384·max(0,
// scale·(0.75e^(4/3) - e^(2/3) - ln(e)/3)). Derived by regression on Fn
// tables (exact to 1e-13).
func spShapeHeight(r, radius, scale, bluntness float64) float64 {
	if r < bluntness {
		r = bluntness
	}
	const k = 0.384
	e := r / radius * k
	v := 0.75*math.Pow(e, 4.0/3) - math.Pow(e, 2.0/3) - math.Log(e)/3
	v = max(v*scale, 0)
	return v / k * radius
}

type dripstoneSpike struct {
	root      Pos
	up        bool
	radius    int
	bluntness float64
	scale     float64
}

func (d *dripstoneSpike) heightAt(r float32) int {
	return int(spShapeHeight(float64(r), float64(d.radius), d.scale, d.bluntness))
}

// windOffsetter shifts positions by floor(speed·(originY - y)), clamped to
// ±maxOffset per axis; the direction is Mth.cos/sin of nextFloat·π.
type windOffsetter struct {
	on        bool
	originY   int
	sx, sz    float64
	maxOffset int
}

func newWind(originY int, r *Random, speed FloatProvider, maxOffset int) windOffsetter {
	s := speed.Sample(r)
	a := r.NextFloat() * math.Pi
	return windOffsetter{on: true, originY: originY, sx: float64(s * carver.Cos(float64(a))), sz: float64(s * carver.Sin(float64(a))), maxOffset: maxOffset}
}

func (w windOffsetter) offset(p Pos) Pos {
	if !w.on {
		return p
	}
	dy := float64(w.originY - p.Y)
	dx := min(max(int(math.Floor(w.sx*dy)), -w.maxOffset), w.maxOffset)
	dz := min(max(int(math.Floor(w.sz*dy)), -w.maxOffset), w.maxOffset)
	return Pos{p.X + dx, p.Y, p.Z + dz}
}

// spEmbedded is SpeleothemUtils.isCircleMostlyEmbeddedInStone: the centre
// and points (int)(r·cos a), (int)(r·sin a) for a = 0, 6/r, 12/r, ... < 2π
// must all be solid (not empty, water or lava).
func spEmbedded(lv *Level, p Pos, radius int) bool {
	if spEmptyOrWaterOrLava(lv.Block(p)) {
		return false
	}
	step := 6 / float32(radius)
	for a := float32(0); a < 2*math.Pi; a += step {
		dx := int(float32(radius) * carver.Cos(float64(a)))
		dz := int(float32(radius) * carver.Sin(float64(a)))
		if spEmptyOrWaterOrLava(lv.Block(p.Add(Pos{dx, 0, dz}))) {
			return false
		}
	}
	return true
}

// moveBack: per radius (halved while > 1), up to min(10, height) steps
// away from the tip until the circle at the (wind-shifted) position is
// embedded; lava at a step position fails at once.
func (d *dripstoneSpike) moveBack(lv *Level, w windOffsetter) bool {
	dir := 1
	if d.up {
		dir = -1
	}
	for d.radius > 1 {
		q := d.root
		n := min(10, d.heightAt(0))
		for range n {
			if spIsLava(lv.Block(q)) {
				return false
			}
			if spEmbedded(lv, w.offset(q), d.radius) {
				d.root = q
				return true
			}
			q.Y += dir
		}
		d.radius /= 2
	}
	return false
}

func (d *dripstoneSpike) place(lv *Level, r *Random, w windOffsetter, dripstone data.StateID, replace blockSet) {
	dir := -1
	if d.up {
		dir = 1
	}
	for dx := -d.radius; dx <= d.radius; dx++ {
		for dz := -d.radius; dz <= d.radius; dz++ {
			dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
			h := d.heightAt(dist)
			if h <= 0 {
				continue
			}
			if r.NextFloat() < 0.2 {
				h = int(float32(h) * (0.8 + r.NextFloat()*0.2))
			}
			base := d.root.Add(Pos{dx, 0, dz})
			if d.up {
				h = min(h, lv.Height(world.WorldSurfaceWG, base.X, base.Z)-base.Y)
			}
			placed := false
			for i := range h {
				q := w.offset(base.Add(Pos{0, dir * i, 0}))
				if st := lv.Block(q); spEmptyOrWaterOrLava(st) {
					placed = true
					lv.SetBlock(q, dripstone)
				} else if placed && replace.has(st) {
					break
				}
			}
		}
	}
}

func (f *largeDripstone) makeSpike(p Pos, up bool, r *Random, radius int, bl FloatProvider) *dripstoneSpike {
	b := bl.Sample(r)
	s := f.heightScale.Sample(r)
	return &dripstoneSpike{root: p, up: up, radius: radius, bluntness: float64(b), scale: float64(s)}
}

// windy is isSuitableForWind (Fn: radius >= min, bluntness >= min as float).
func (f *largeDripstone) windy(d *dripstoneSpike) bool {
	return d.radius >= f.minRadiusWind && d.bluntness >= float64(f.minBluntWind)
}

func (f *largeDripstone) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if !spEmptyOrWater(lv.Block(p)) {
		return false
	}
	edge := func(st data.StateID) bool { return st == f.dripstone || f.replace.has(st) || spIsLava(st) }
	ceil, floor, hasCeil, hasFloor, ok := spColumnScan(lv, p, f.searchRange, spEmptyOrWater, edge)
	if !ok || !hasCeil || !hasFloor {
		return false
	}
	height := ceil - floor - 1
	hi := min(max(int(float32(height)*f.maxRatio), f.radiusMin), f.radiusMax)
	radius := int(r.NextIntBetweenInclusive(int32(f.radiusMin), int32(hi)))
	top := f.makeSpike(Pos{p.X, ceil - 1, p.Z}, false, r, radius, f.stalactiteBl)
	bottom := f.makeSpike(Pos{p.X, floor + 1, p.Z}, true, r, radius, f.stalagmiteBl)
	var w windOffsetter
	if f.windy(top) && f.windy(bottom) {
		w = newWind(p.Y, r, f.windSpeed, radius)
	}
	topOK := top.moveBack(lv, w)
	bottomOK := bottom.moveBack(lv, w)
	if topOK {
		top.place(lv, r, w, f.dripstone, f.replace)
	}
	if bottomOK {
		bottom.place(lv, r, w, f.dripstone, f.replace)
	}
	return true
}

func init() {
	featureTypes["large_dripstone"] = parseLargeDripstone
}
