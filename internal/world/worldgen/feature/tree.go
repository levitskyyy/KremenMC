package feature

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// tree is the vanilla "tree" feature for the trunk and foliage placers kremenmc
// implements; draw order verified against the reference with
// tools/refprobe kremenmc.probe.feature.Tree (testdata/trees.txt).
type tree struct {
	trunk, foliage StateProvider
	below          *ruleState // nil: keep the block below the trunk
	tp             trunkPlacer
	fp             foliagePlacer
	size           featureSize
	ignoreVines    bool
	decorators     []decorator
}

type attachment struct {
	pos       Pos
	radiusOff int
	double    bool
}

type trunkPlacer interface {
	height(r *Random) int
	place(t *treeRun, r *Random, height int, p Pos) []attachment
}

type foliagePlacer interface {
	foliageHeight(r *Random, height int) int
	foliageRadius(r *Random, trunkLen int) int
	place(t *treeRun, r *Random, height int, a attachment, fh, fr int)
}

type decorator interface {
	decorate(t *treeRun, r *Random)
}

// treeRun is one placement attempt: the positions written, for leaf
// distances and decorators.
type treeRun struct {
	cfg    *tree
	lv     *Level
	logs   []Pos
	leaves []Pos
	soil   []Pos
	// branchTops are the top logs of dark oak branch columns; the foliage
	// placer puts a small crown on each.
	branchTops []Pos
	// deco are the decorators' writes; with the rest they bound the leaf
	// distance update.
	deco []Pos
	box  box
}

// setDeco is a decorator's block write.
func (t *treeRun) setDeco(p Pos, st data.StateID) {
	t.lv.SetBlock(p, st)
	t.deco = append(t.deco, p)
}

var treeTags = sync.OnceValues(func() (blockSet, blockSet) {
	rg, err := theRegistry()
	if err != nil {
		return nil, nil
	}
	repl, _ := rg.tag("minecraft:replaceable_by_trees")
	logs, _ := rg.tag("minecraft:logs")
	return repl, logs
})

// validTreePos: air or a block trees replace (leaves, small plants).
func validTreePos(lv *Level, p Pos) bool {
	st := lv.Block(p)
	repl, _ := treeTags()
	return data.IsAir(st) || repl.has(st)
}

func isFree(lv *Level, p Pos) bool {
	_, logs := treeTags()
	return validTreePos(lv, p) || logs.has(lv.Block(p))
}

func (t *treeRun) placeLog(r *Random, p Pos) bool {
	if !validTreePos(t.lv, p) {
		return false
	}
	t.lv.SetBlock(p, t.cfg.trunk.State(t.lv, r, p))
	t.logs = append(t.logs, p)
	return true
}

func (t *treeRun) placeLeaf(r *Random, p Pos) bool {
	if !validTreePos(t.lv, p) {
		return false
	}
	st := t.cfg.foliage.State(t.lv, r, p)
	if f := fluidOf(t.lv.Block(p)); f == "water" {
		if w, ok := data.Canonical().With(st, "waterlogged", "true"); ok {
			st = w
		}
	}
	t.lv.SetBlock(p, st)
	t.leaves = append(t.leaves, p)
	return true
}

func (t *treeRun) setDirt(r *Random, p Pos) {
	if t.cfg.below == nil {
		return
	}
	// Only a changed block joins the trunk positions (a dark oak on a
	// slope over dirt: kremenmc.probe.feat_region.Region).
	if st, ok := t.cfg.below.pick(t.lv, r, p); ok && st != t.lv.Block(p) {
		t.lv.SetBlock(p, st)
		t.soil = append(t.soil, p)
	}
}

func (cfg *tree) Place(c *placeCtx, r *Random, p Pos) bool {
	t := &treeRun{cfg: cfg, lv: c.lv}
	h := cfg.tp.height(r)
	fh := cfg.fp.foliageHeight(r, h)
	fr := cfg.fp.foliageRadius(r, h-fh)
	if p.Y < c.lv.MinY()+1 || p.Y+h+1 > c.lv.MaxY() {
		return false
	}
	free := cfg.maxFreeHeight(c.lv, h, p)
	if free < h && (cfg.size.minClipped < 0 || free < cfg.size.minClipped) {
		return false
	}
	h = free
	for _, a := range cfg.tp.place(t, r, h, p) {
		cfg.fp.place(t, r, h, a, fh, fr)
	}
	for _, d := range cfg.decorators {
		d.decorate(t, r)
	}
	for _, l := range [][]Pos{t.soil, t.logs, t.leaves, t.deco} {
		for _, p := range l {
			t.box.add(p)
		}
	}
	t.updateLeaves()
	t.updateNeighbours()
	t.scheduleLeafTicks()
	return true
}

func (cfg *tree) maxFreeHeight(lv *Level, h int, p Pos) int {
	for y := 0; y <= h+1; y++ {
		s := cfg.size.sizeAt(h, y)
		for dx := -s; dx <= s; dx++ {
			for dz := -s; dz <= s; dz++ {
				q := Pos{p.X + dx, p.Y + y, p.Z + dz}
				if !isFree(lv, q) || !cfg.ignoreVines && blockName(lv.Block(q)) == "vine" {
					return y - 2
				}
			}
		}
	}
	return h
}

type featureSize struct {
	limit, lower, upper int
	minClipped          int // -1 when absent
	// three_layers_feature_size: middle below upperLimit levels from the top.
	three              bool
	upperLimit, middle int
}

func (f featureSize) sizeAt(h, y int) int {
	if f.three {
		switch {
		case y < f.limit:
			return f.lower
		case y >= h-f.upperLimit:
			return f.upper
		}
		return f.middle
	}
	if y < f.limit {
		return f.lower
	}
	return f.upper
}

type straightTrunk struct{ base, a, b int }

func (s straightTrunk) height(r *Random) int {
	return s.base + int(r.NextIntn(int32(s.a+1))) + int(r.NextIntn(int32(s.b+1)))
}

func (s straightTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	t.setDirt(r, p.Add(Pos{0, -1, 0}))
	for i := range h {
		t.placeLog(r, p.Add(Pos{0, i, 0}))
	}
	return []attachment{{pos: p.Add(Pos{0, h, 0})}}
}

// blobFoliage is blob_foliage_placer (also the base of the round bush).
type blobFoliage struct {
	radius, offset IntProvider
	h              int
}

func (b blobFoliage) foliageHeight(*Random, int) int     { return b.h }
func (b blobFoliage) foliageRadius(r *Random, _ int) int { return b.radius.Sample(r) }
func (b blobFoliage) skip(r *Random, dx, y, dz, rng int) bool {
	return dx == rng && dz == rng && (r.NextIntn(2) == 0 || y == 0)
}

func (b blobFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	off := b.offset.Sample(r)
	for y := off; y >= off-fh; y-- {
		rad := max(fr+a.radiusOff-1-y/2, 0)
		leavesRow(t, r, a.pos, rad, y, a.double, b.skip)
	}
}

func leavesRow(t *treeRun, r *Random, c Pos, rng, y int, double bool, skip func(r *Random, dx, y, dz, rng int) bool) {
	extra := 0
	if double {
		extra = 1
	}
	for dx := -rng; dx <= rng+extra; dx++ {
		for dz := -rng; dz <= rng+extra; dz++ {
			ax, az := abs(dx), abs(dz)
			if double {
				if dx > 0 {
					ax = dx - 1
				}
				if dz > 0 {
					az = dz - 1
				}
			}
			if !skip(r, ax, y, az, rng) {
				t.placeLeaf(r, c.Add(Pos{dx, y, dz}))
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type beehive struct{ p float32 }

// beehiveSides is east, south, west: the order of a log's hive candidates.
var beehiveSides = [3]Pos{{1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

// decorate: with probability p, a south-facing bee_nest beside a log one
// block below the lowest leaf. Candidates are each such log (trunk
// decorator order) times east, south, west, shuffled; the first air one
// wins. Then the bee count (2 + nextInt(2)) and one nextInt(599) per bee.
// Fitted to kremenmc.probe.feat_tree.Draws (fancy_oak_bees, p = 1). The bees
// themselves are not stored in the block entity yet.
func (b beehive) decorate(t *treeRun, r *Random) {
	if r.NextFloat() >= b.p {
		return
	}
	if len(t.leaves) == 0 {
		return
	}
	y := t.leaves[0].Y
	for _, l := range t.leaves {
		y = min(y, l.Y)
	}
	y--
	var cand []Pos
	for _, l := range t.trunkPositions() {
		if l.Y != y || !slices.Contains(t.logs, l) {
			continue
		}
		for _, s := range beehiveSides {
			cand = append(cand, l.Add(s))
		}
	}
	for i := len(cand); i > 1; i-- {
		j := int(r.NextIntn(int32(i)))
		cand[i-1], cand[j] = cand[j], cand[i-1]
	}
	for _, c := range cand {
		if !data.IsAir(t.lv.Block(c)) {
			continue
		}
		st, _ := data.DefaultState("bee_nest")
		st, _ = data.Canonical().With(st, "facing", "south")
		t.setDeco(c, st)
		n := 2 + int(r.NextIntn(2))
		for range n {
			r.NextIntn(599)
		}
		return
	}
}

func (rg *registry) parseTree(b json.RawMessage) (Feature, error) {
	var p struct {
		Trunk, Foliage, Below json.RawMessage
		TP                    json.RawMessage `json:"trunk_placer"`
		FP                    json.RawMessage `json:"foliage_placer"`
		Size                  struct {
			Type       string `json:"type"`
			Limit      *int   `json:"limit"`
			UpperLimit *int   `json:"upper_limit"`
			Middle     *int   `json:"middle_size"`
			Lower      int    `json:"lower_size"`
			Upper      *int   `json:"upper_size"`
			MinClipped *int   `json:"min_clipped_height"`
		} `json:"minimum_size"`
		IgnoreVines bool              `json:"ignore_vines"`
		Decorators  []json.RawMessage `json:"decorators"`
		Root        json.RawMessage   `json:"root_placer"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if len(p.Root) > 0 {
		return nil, unsupported("root_placer", typeOf(p.Root))
	}
	t := &tree{ignoreVines: p.IgnoreVines}
	var err error
	if t.trunk, err = rg.parseStateProvider(raw["trunk_provider"]); err != nil {
		return nil, err
	}
	if t.foliage, err = rg.parseStateProvider(raw["foliage_provider"]); err != nil {
		return nil, err
	}
	if bt, ok := raw["below_trunk_provider"]; ok {
		sp, err := rg.parseStateProvider(bt)
		if err != nil {
			return nil, err
		}
		rs, ok := sp.(*ruleState)
		if !ok {
			return nil, unsupported("below_trunk_provider", "non rule_based")
		}
		t.below = rs
	}
	switch strings.TrimPrefix(p.Size.Type, "minecraft:") {
	case "two_layers_feature_size":
		t.size = featureSize{limit: 1, lower: p.Size.Lower, upper: 1, minClipped: -1}
		if p.Size.Limit != nil {
			t.size.limit = *p.Size.Limit
		}
		if p.Size.Upper != nil {
			t.size.upper = *p.Size.Upper
		}
		if p.Size.MinClipped != nil {
			t.size.minClipped = *p.Size.MinClipped
		}
	case "three_layers_feature_size":
		t.size = featureSize{three: true, limit: 1, upperLimit: 1, lower: p.Size.Lower, middle: 1, upper: 1, minClipped: -1}
		for _, f := range []struct {
			src *int
			dst *int
		}{{p.Size.Limit, &t.size.limit}, {p.Size.UpperLimit, &t.size.upperLimit}, {p.Size.Middle, &t.size.middle}, {p.Size.Upper, &t.size.upper}, {p.Size.MinClipped, &t.size.minClipped}} {
			if f.src != nil {
				*f.dst = *f.src
			}
		}
	default:
		return nil, unsupported("feature_size", p.Size.Type)
	}
	if t.tp, err = parseTrunk(p.TP); err != nil {
		return nil, err
	}
	if t.fp, err = parseFoliage(p.FP); err != nil {
		return nil, err
	}
	for _, d := range p.Decorators {
		dec, err := rg.parseDecorator(d)
		if err != nil {
			return nil, err
		}
		t.decorators = append(t.decorators, dec)
	}
	return t, nil
}

// parseDecorator parses one tree decorator; decorator types other than the
// built-in ones register in decoratorTypes.
func (rg *registry) parseDecorator(d json.RawMessage) (decorator, error) {
	var dd struct {
		Type   string          `json:"type"`
		P      float32         `json:"probability"`
		Tries  int             `json:"tries"`
		Radius *int            `json:"radius"`
		Height *int            `json:"height"`
		State  json.RawMessage `json:"block_state_provider"`
	}
	if err := json.Unmarshal(d, &dd); err != nil {
		return nil, err
	}
	typ := strings.TrimPrefix(dd.Type, "minecraft:")
	if f, ok := decoratorTypes[typ]; ok {
		return f(rg, d)
	}
	switch typ {
	case "beehive":
		return beehive{dd.P}, nil
	case "trunk_vine":
		return trunkVine{}, nil
	case "cocoa":
		return cocoa{dd.P}, nil
	case "leave_vine":
		return leaveVine{dd.P}, nil
	case "place_on_ground":
		d := placeOnGround{tries: dd.Tries, radius: 2, height: 1}
		if dd.Radius != nil {
			d.radius = *dd.Radius
		}
		if dd.Height != nil {
			d.height = *dd.Height
		}
		var err error
		if d.state, err = rg.parseStateProvider(dd.State); err != nil {
			return nil, err
		}
		return d, nil
	}
	// Without it the tree would not match vanilla: not placed.
	return nil, unsupported("tree_decorator", typ)
}

// decoratorTypes holds tree decorators registered from the tree_*.go files.
var decoratorTypes = map[string]func(rg *registry, b json.RawMessage) (decorator, error){}

// trunkTypes and foliageTypes hold the placers registered from the
// tree_*.go files.
var (
	trunkTypes   = map[string]func(b json.RawMessage) (trunkPlacer, error){}
	foliageTypes = map[string]func(b json.RawMessage) (foliagePlacer, error){}
)

// trunkHeight is the base_height/height_rand_a/height_rand_b triple every
// trunk placer has.
type trunkHeight struct {
	Base int `json:"base_height"`
	A    int `json:"height_rand_a"`
	B    int `json:"height_rand_b"`
}

func (s trunkHeight) height(r *Random) int {
	return s.Base + int(r.NextIntn(int32(s.A+1))) + int(r.NextIntn(int32(s.B+1)))
}

func parseTrunk(b json.RawMessage) (trunkPlacer, error) {
	if f, ok := trunkTypes[typeOf(b)]; ok {
		return f(b)
	}
	if t := typeOf(b); t != "straight_trunk_placer" {
		return nil, unsupported("trunk_placer", t)
	}
	var p struct {
		Type string `json:"type"`
		Base int    `json:"base_height"`
		A    int    `json:"height_rand_a"`
		B    int    `json:"height_rand_b"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	switch strings.TrimPrefix(p.Type, "minecraft:") {
	case "straight_trunk_placer":
		return straightTrunk{p.Base, p.A, p.B}, nil
	}
	return nil, unsupported("trunk_placer", p.Type)
}

func parseFoliage(b json.RawMessage) (foliagePlacer, error) {
	var p struct {
		Type        string          `json:"type"`
		Radius      json.RawMessage `json:"radius"`
		Offset      json.RawMessage `json:"offset"`
		Height      json.RawMessage `json:"height"`
		TrunkHeight json.RawMessage `json:"trunk_height"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	t := strings.TrimPrefix(p.Type, "minecraft:")
	if f, ok := foliageTypes[t]; ok {
		return f(b)
	}
	switch t {
	case "blob_foliage_placer", "spruce_foliage_placer", "pine_foliage_placer":
	default:
		return nil, unsupported("foliage_placer", t)
	}
	rad, err := parseInt(p.Radius)
	if err != nil {
		return nil, err
	}
	off, err := parseInt(p.Offset)
	if err != nil {
		return nil, err
	}
	switch t {
	case "spruce_foliage_placer":
		th, err := parseInt(p.TrunkHeight)
		return spruceFoliage{rad, off, th}, err
	case "pine_foliage_placer":
		h, err := parseInt(p.Height)
		return pineFoliage{rad, off, h}, err
	}
	var h int
	if err := json.Unmarshal(p.Height, &h); err != nil {
		return nil, err
	}
	return blobFoliage{rad, off, h}, nil
}

func skipCorners(_ *Random, dx, _, dz, rng int) bool { return dx == rng && dz == rng && rng > 0 }

// spruceFoliage: layer radii grow from a random 0 or 1 and fall back to a
// smaller radius whenever they reach a growing limit (observed in the
// reference: 1,0,1,2,1,2,3,1 for a start of 1).
type spruceFoliage struct{ radius, offset, trunkHeight IntProvider }

func (s spruceFoliage) foliageHeight(r *Random, h int) int {
	return max(4, h-s.trunkHeight.Sample(r))
}
func (s spruceFoliage) foliageRadius(r *Random, _ int) int { return s.radius.Sample(r) }

func (s spruceFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	off := s.offset.Sample(r)
	i := int(r.NextIntn(2))
	j, k := 1, 0
	for y := off; y >= -fh; y-- {
		leavesRow(t, r, a.pos, i, y, a.double, skipCorners)
		if i >= j {
			i, k, j = k, 1, min(j+1, fr+a.radiusOff)
		} else {
			i++
		}
	}
}

// pineFoliage: radii grow by one per layer from the top up to the radius;
// the last layer shrinks again.
type pineFoliage struct{ radius, offset, height IntProvider }

func (p pineFoliage) foliageHeight(r *Random, _ int) int { return p.height.Sample(r) }

// foliageRadius widens with the trunk: radius + nextInt(trunk + 1).
func (p pineFoliage) foliageRadius(r *Random, trunk int) int {
	return p.radius.Sample(r) + int(r.NextIntn(int32(max(trunk+1, 1))))
}

func (p pineFoliage) place(t *treeRun, r *Random, _ int, a attachment, fh, fr int) {
	off := p.offset.Sample(r)
	i := 0
	for y := off; y >= off-fh; y-- {
		leavesRow(t, r, a.pos, i, y, a.double, skipCorners)
		if i >= 1 && y == off-fh+1 {
			i--
		} else if i < fr+a.radiusOff {
			i++
		}
	}
}

type placeOnGround struct {
	tries, radius, height int
	state                 StateProvider
}

// decorate: tries random positions within radius/height of the block below
// the trunk base; a block goes on top of p when p is the top of the
// MOTION_BLOCKING_NO_LEAVES heightmap, p is not a fluid and the block above
// is air. Fitted to kremenmc.probe.feat_tree.Draws with primed heightmaps
// (oak_leaf_litter and fancy_oak_leaf_litter, ground of grass, stone,
// water, snow, short grass, overhangs and leaves).
func (d placeOnGround) decorate(t *treeRun, r *Random) {
	all := t.trunkPositions()
	if len(all) == 0 {
		return
	}
	// The tries cover the box of the lowest trunk positions (the soil
	// under a 2x2 trunk), grown by radius and height
	// (kremenmc.probe.feat_tree.Draws, dark_oak_leaf_litter).
	lo, hi := all[0], all[0]
	for _, p := range all {
		if p.Y != all[0].Y {
			continue
		}
		lo = Pos{min(lo.X, p.X), lo.Y, min(lo.Z, p.Z)}
		hi = Pos{max(hi.X, p.X), hi.Y, max(hi.Z, p.Z)}
	}
	for range d.tries {
		x := int(r.NextIntBetweenInclusive(int32(lo.X-d.radius), int32(hi.X+d.radius)))
		y := int(r.NextIntBetweenInclusive(int32(lo.Y-d.height), int32(lo.Y+d.height)))
		z := int(r.NextIntBetweenInclusive(int32(lo.Z-d.radius), int32(hi.Z+d.radius)))
		p := Pos{x, y, z}
		up := p.Add(Pos{0, 1, 0})
		if t.lv.Height(world.MotionBlockingNoLeaves, p.X, p.Z) != up.Y || fluidOf(t.lv.Block(p)) != "" || !data.IsAir(t.lv.Block(up)) {
			continue
		}
		t.setDeco(up, d.state.State(t.lv, r, up))
	}
}
