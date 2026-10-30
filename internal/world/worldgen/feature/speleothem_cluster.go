package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// speleothemCluster is the 26.x speleothem_cluster feature (dripstone
// clusters). Fitted to kremenmc.probe.feat_ow.Place traces and to
// kremenmc.probe.feat_drip.Fn tables of the single helpers (column scan,
// edge chance, column height); see docs/vanilla-parity.md.
type speleothemCluster struct {
	base, pointed     data.StateID
	replace           blockSet
	searchRange       int
	height, radius    IntProvider
	maxDiff, heightDv int
	layer             IntProvider
	density, wetness  FloatProvider
	chanceAtMax       float32
	edgeDist, biasMax int
}

type clampedNormalFloat struct{ mean, dev, min, max float32 }

func (c clampedNormalFloat) Sample(r *Random) float32 {
	return min(max(c.mean+float32(r.NextGaussian())*c.dev, c.min), c.max)
}

// parseFloatMore adds clamped_normal to parseFloat.
func parseFloatMore(b json.RawMessage) (FloatProvider, error) {
	var p struct {
		Type string  `json:"type"`
		Mean float32 `json:"mean"`
		Dev  float32 `json:"deviation"`
		Min  float32 `json:"min"`
		Max  float32 `json:"max"`
	}
	if !isNumber(b) && json.Unmarshal(b, &p) == nil && strings.TrimPrefix(p.Type, "minecraft:") == "clamped_normal" {
		return clampedNormalFloat{p.Mean, p.Dev, p.Min, p.Max}, nil
	}
	return parseFloat(b)
}

func parseSpeleothemCluster(rg *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		speleothemConfig
		Range     int             `json:"floor_to_ceiling_search_range"`
		Height    json.RawMessage `json:"height"`
		Radius    json.RawMessage `json:"radius"`
		MaxDiff   int             `json:"max_stalagmite_stalactite_height_diff"`
		HeightDev int             `json:"height_deviation"`
		Layer     json.RawMessage `json:"speleothem_block_layer_thickness"`
		Density   json.RawMessage `json:"density"`
		Wetness   json.RawMessage `json:"wetness"`
		ChanceMax float32         `json:"chance_of_speleothem_at_max_distance_from_center"`
		EdgeDist  int             `json:"max_distance_from_edge_affecting_chance_of_speleothem"`
		BiasMax   int             `json:"max_distance_from_center_affecting_height_bias"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("speleothem_cluster: %w", err)
	}
	f := &speleothemCluster{searchRange: c.Range, maxDiff: c.MaxDiff, heightDv: c.HeightDev, chanceAtMax: c.ChanceMax, edgeDist: c.EdgeDist, biasMax: c.BiasMax}
	var err error
	if f.base, f.pointed, f.replace, err = rg.parseSpeleothemBlocks(c.speleothemConfig); err != nil {
		return nil, err
	}
	for _, ip := range []struct {
		dst *IntProvider
		src json.RawMessage
	}{{&f.height, c.Height}, {&f.radius, c.Radius}, {&f.layer, c.Layer}} {
		if *ip.dst, err = parseInt(ip.src); err != nil {
			return nil, fmt.Errorf("speleothem_cluster: %w", err)
		}
	}
	if f.density, err = parseFloatMore(c.Density); err != nil {
		return nil, fmt.Errorf("speleothem_cluster: %w", err)
	}
	if f.wetness, err = parseFloatMore(c.Wetness); err != nil {
		return nil, fmt.Errorf("speleothem_cluster: %w", err)
	}
	return f, nil
}

// spColumnScan is Column.scan: from p while inside holds, up to rng-1 steps
// each way; the edge is the first block that fails inside and passes edge.
func spColumnScan(lv *Level, p Pos, rng int, inside, edge func(data.StateID) bool) (ceil, floor int, hasCeil, hasFloor, ok bool) {
	if !inside(lv.Block(p)) {
		return 0, 0, false, false, false
	}
	dir := func(d int) (int, bool) {
		q := p
		for i := 1; i < rng && inside(lv.Block(q)); i++ {
			q.Y += d
		}
		return q.Y, edge(lv.Block(q))
	}
	ceil, hasCeil = dir(1)
	floor, hasFloor = dir(-1)
	return ceil, floor, hasCeil, hasFloor, true
}

func spIsLava(st data.StateID) bool {
	f := fluidOf(st)
	return f == "lava" || f == "flowing_lava"
}

func (f *speleothemCluster) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if !spEmptyOrWater(lv.Block(p)) {
		return false
	}
	h := f.height.Sample(r)
	wet := f.wetness.Sample(r)
	dens := f.density.Sample(r)
	rx := f.radius.Sample(r)
	rz := f.radius.Sample(r)
	for dx := -rx; dx <= rx; dx++ {
		for dz := -rz; dz <= rz; dz++ {
			f.column(lv, r, p.Add(Pos{dx, 0, dz}), dx, dz, wet, f.chance(rx, rz, dx, dz), h, dens)
		}
	}
	return true
}

// chance is lerp(clamp(min(rx-|dx|, rz-|dz|) / edgeDist), chanceAtMax, 1),
// in float like the reference (Fn chance tables).
func (f *speleothemCluster) chance(rx, rz, dx, dz int) float64 {
	d := min(rx-abs(dx), rz-abs(dz))
	t := min(max(float32(d)/float32(f.edgeDist), 0), 1)
	return float64(f.chanceAtMax + t*(1-f.chanceAtMax))
}

// columnHeight: 0 when nextFloat > density, else
// floor(clamp(h/2·(1-clamp((|dx|+|dz|)/biasMax)) + dev·gaussian, 0, h)).
func (f *speleothemCluster) columnHeight(r *Random, dx, dz int, dens float32, h int) int {
	if h == 0 || r.NextFloat() > dens {
		return 0
	}
	t := min(max(float32(abs(dx)+abs(dz))/float32(f.biasMax), 0), 1)
	mean := float32(h) / 2 * (1 - t)
	v := min(max(mean+float32(r.NextGaussian())*float32(f.heightDv), 0), float32(h))
	return int(v)
}

// column: scan, optional pool (the floor moves one down), stalactite
// (height capped by ceil-floor), stalagmite (stalactite ± maxDiff, or its own
// height without a ceiling), then the excess over the gap is taken from both
// by one draw, and nextBoolean merges tips that meet.
func (f *speleothemCluster) column(lv *Level, r *Random, p Pos, dx, dz int, wet float32, chance float64, h int, dens float32) {
	solid := func(st data.StateID) bool { return !spEmptyOrWater(st) }
	ceil, floor, hasCeil, hasFloor, ok := spColumnScan(lv, p, f.searchRange, spEmptyOrWater, solid)
	if !ok || !hasCeil && !hasFloor {
		return
	}
	if r.NextFloat() < wet && hasFloor && f.canPlacePool(lv, Pos{p.X, floor, p.Z}) {
		lv.SetBlock(Pos{p.X, floor, p.Z}, waterSource())
		floor--
	}
	st, sm := 0, 0
	if r.NextDouble() < chance && hasCeil && !spIsLava(lv.Block(Pos{p.X, ceil, p.Z})) {
		f.baseLayers(lv, Pos{p.X, ceil, p.Z}, f.layer.Sample(r), 1)
		hh := h
		if hasFloor {
			hh = min(h, ceil-floor)
		}
		st = f.columnHeight(r, dx, dz, dens, hh)
	}
	if r.NextDouble() < chance && hasFloor && !spIsLava(lv.Block(Pos{p.X, floor, p.Z})) {
		f.baseLayers(lv, Pos{p.X, floor, p.Z}, f.layer.Sample(r), -1)
		if hasCeil {
			sm = max(int(r.NextIntBetweenInclusive(int32(st-f.maxDiff), int32(st+f.maxDiff))), 0)
		} else {
			sm = f.columnHeight(r, dx, dz, dens, h)
		}
	}
	gap := 0
	if hasCeil && hasFloor {
		gap = ceil - floor - 1
		if ex := st + sm - gap; ex > 0 {
			k := int(r.NextIntBetweenInclusive(int32(max(0, ex-sm)), int32(min(ex, st))))
			st -= k
			sm -= ex - k
		}
	}
	merge := r.NextBool() && st > 0 && sm > 0 && st+sm == gap
	if hasCeil {
		f.grow(lv, Pos{p.X, ceil, p.Z}, -1, st, merge)
	}
	if hasFloor {
		f.grow(lv, Pos{p.X, floor, p.Z}, 1, sm, merge)
	}
}

func waterSource() data.StateID {
	s, _ := data.DefaultState("water")
	return s
}

// canPlacePool: the floor block is not water, the base block or pointed
// dripstone, the block above holds no water, and the four sides (N, E, S,
// W) and the block below are replaceable or water.
func (f *speleothemCluster) canPlacePool(lv *Level, p Pos) bool {
	st := lv.Block(p)
	if fluidOf(st) == "water" || st == f.base || data.Canonical().BlockID(st) == data.Canonical().BlockID(f.pointed) {
		return false
	}
	if spWater(lv.Block(p.Add(Pos{0, 1, 0}))) {
		return false
	}
	for _, d := range [5]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}, {0, -1, 0}} {
		q := lv.Block(p.Add(d))
		if !f.replace.has(q) && !spWater(q) {
			return false
		}
	}
	return true
}

func spWater(st data.StateID) bool {
	f := fluidOf(st)
	return f == "water" || f == "flowing_water"
}

// baseLayers turns n blocks from p along dir into the base block, stopping
// at the first one that is not replaceable.
func (f *speleothemCluster) baseLayers(lv *Level, p Pos, n, dir int) {
	for i := range n {
		q := p.Add(Pos{0, dir * i, 0})
		if !f.replace.has(lv.Block(q)) {
			return
		}
		lv.SetBlock(q, f.base)
	}
}

// grow builds a pointed column of n blocks from the base at p along dir.
func (f *speleothemCluster) grow(lv *Level, p Pos, dir, n int, merge bool) {
	if st := lv.Block(p); st != f.base && !f.replace.has(st) {
		return
	}
	if n <= 0 {
		return
	}
	spColumnMerge(lv, p.Add(Pos{0, dir, 0}), dir, n, f.pointed, merge)
}

// spColumnMerge is spColumn with an optional merged tip, written once.
func spColumnMerge(lv *Level, p Pos, dir, h int, pointed data.StateID, merge bool) {
	if !merge {
		spColumn(lv, p, dir, h, pointed)
		return
	}
	reg := data.Canonical()
	vd := "up"
	if dir < 0 {
		vd = "down"
	}
	for i := range h {
		q := p.Add(Pos{0, dir * i, 0})
		th := "middle"
		switch {
		case i == h-1:
			th = "tip_merge"
		case i == h-2:
			th = "frustum"
		case i == 0:
			th = "base"
		}
		st, _ := reg.With(pointed, "thickness", th)
		st, _ = reg.With(st, "vertical_direction", vd)
		wl := "false"
		if f := fluidOf(lv.Block(q)); f == "water" || f == "flowing_water" {
			wl = "true"
		}
		st, _ = reg.With(st, "waterlogged", wl)
		lv.SetBlock(q, st)
	}
}

func init() {
	featureTypes["speleothem_cluster"] = parseSpeleothemCluster
}
