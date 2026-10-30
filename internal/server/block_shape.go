package server

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Neighbour shape updates and survival checks (vanilla updateShape /
// canSurvive), derived from the minecraft.wiki block pages, the embedded
// block tags and reference observations (TestPlaceReference).

type blockReader interface {
	Block(x, y, z int) data.StateID
}

// Directions index faceOffsets/faceNames: down, up, north, south, west, east.
const (
	dDown = iota
	dUp
	dNorth
	dSouth
	dWest
	dEast
)

var (
	dirOpp = [6]int{dUp, dDown, dSouth, dNorth, dEast, dWest}
	dirCW  = [6]int{dDown, dUp, dEast, dWest, dNorth, dSouth}
	dirCCW = [6]int{dDown, dUp, dWest, dEast, dSouth, dNorth}
	horiz  = [4]int{dNorth, dEast, dSouth, dWest}
)

func dirAxis(d int) int { return [6]int{1, 1, 2, 2, 0, 0}[d] }

func dirByName(s string) int {
	for i, n := range faceNames {
		if n == s {
			return i
		}
	}
	return -1
}

type shapeKind uint8

const (
	kNone shapeKind = iota
	kFence
	kWall
	kPane
	kGate
	kStairs
	kDoor
	kBed
	kChest
	kTorch
	kWallTorch
	kStandSign
	kWallSign
	kAttached
	kLadder
	kSnow
	kPlant
	kTallPlant
	kCarpet
	kPlate
	kRail
	kDiode
	kWire
	kCandle
	kPickle
	kLantern
	kCactus
	kSugarCane
)

type blockInfo struct {
	kind         shapeKind
	wooden       bool // #wooden_fences
	wall         bool // #walls
	exception    bool // isExceptionForConnection
	postOverride bool // #wall_post_override
	signal       bool // redstone wire connects from any side
	support      string
}

var (
	infoOnce sync.Once
	infos    []blockInfo
)

func infoOf(s data.StateID) *blockInfo {
	infoOnce.Do(buildInfos)
	id := data.Canonical().BlockID(s)
	if int(id) >= len(infos) {
		return &infos[0]
	}
	return &infos[id]
}

var signalSources = map[string]bool{
	"lever": true, "redstone_torch": true, "redstone_wall_torch": true, "redstone_block": true, "daylight_detector": true,
	"target": true, "tripwire_hook": true, "trapped_chest": true, "detector_rail": true, "lectern": true, "comparator": true,
	"sculk_sensor": true, "calibrated_sculk_sensor": true, "jukebox": true, "redstone_wire": true,
}

var plantSupport = map[string]string{
	"short_grass": "supports_vegetation", "fern": "supports_vegetation", "sweet_berry_bush": "supports_vegetation",
	"pink_petals": "supports_vegetation", "wildflowers": "supports_vegetation", "bush": "supports_vegetation",
	"firefly_bush": "supports_vegetation", "wither_rose": "supports_wither_rose", "azalea": "supports_azalea",
	"flowering_azalea": "supports_azalea", "dead_bush": "supports_dry_vegetation", "short_dry_grass": "supports_dry_vegetation",
	"tall_dry_grass": "supports_dry_vegetation", "wheat": "supports_crops", "carrots": "supports_crops",
	"potatoes": "supports_crops", "beetroots": "supports_crops", "torchflower_crop": "supports_crops",
	"melon_stem": "supports_stem_crops", "pumpkin_stem": "supports_stem_crops", "attached_melon_stem": "supports_stem_crops",
	"attached_pumpkin_stem": "supports_stem_crops", "nether_wart": "supports_nether_wart",
	"crimson_fungus": "supports_crimson_fungus", "warped_fungus": "supports_warped_fungus",
	"crimson_roots": "supports_crimson_roots", "warped_roots": "supports_warped_roots", "nether_sprouts": "supports_nether_sprouts",
}

var tallPlants = map[string]bool{
	"sunflower": true, "lilac": true, "rose_bush": true, "peony": true, "tall_grass": true, "large_fern": true, "pitcher_plant": true,
}

func buildInfos() {
	reg := data.Canonical()
	infos = make([]blockInfo, len(reg.Blocks))
	tag := func(t, n string) bool { return inTag("block", "minecraft:"+t, "minecraft:"+n) }
	for i := range reg.Blocks {
		b := &reg.Blocks[i]
		n := b.Name
		bi := &infos[b.ID]
		bi.wooden = tag("wooden_fences", n)
		bi.wall = tag("walls", n)
		bi.postOverride = tag("wall_post_override", n)
		bi.exception = tag("leaves", n) || tag("shulker_boxes", n) ||
			n == "barrier" || n == "carved_pumpkin" || n == "jack_o_lantern" || n == "melon" || n == "pumpkin"
		bi.signal = signalSources[n] || strings.HasSuffix(n, "_button") || strings.HasSuffix(n, "_pressure_plate")
		has := func(p string) bool { return b.PropIndex(p) >= 0 }
		switch {
		case tag("fences", n):
			bi.kind = kFence
		case bi.wall:
			bi.kind = kWall
		case (strings.HasSuffix(n, "_pane") || strings.HasSuffix(n, "_bars")) && has("north"):
			bi.kind = kPane
		case tag("fence_gates", n):
			bi.kind = kGate
		case strings.HasSuffix(n, "_stairs"):
			bi.kind = kStairs
		case strings.HasSuffix(n, "_door"):
			bi.kind = kDoor
		case strings.HasSuffix(n, "_bed"):
			bi.kind = kBed
		case has("type") && has("facing") && (n == "chest" || n == "trapped_chest" || strings.HasSuffix(n, "copper_chest")):
			bi.kind = kChest
		case n == "torch" || n == "soul_torch" || n == "redstone_torch" || n == "copper_torch":
			bi.kind = kTorch
		case strings.HasSuffix(n, "wall_torch"):
			bi.kind = kWallTorch
		case strings.HasSuffix(n, "_wall_sign") && !strings.Contains(n, "hanging"), strings.HasSuffix(n, "_wall_banner"):
			bi.kind = kWallSign
		case strings.HasSuffix(n, "_sign") && !strings.Contains(n, "hanging"), strings.HasSuffix(n, "_banner"):
			bi.kind = kStandSign
		case strings.HasSuffix(n, "_button") || n == "lever":
			bi.kind = kAttached
		case n == "ladder":
			bi.kind = kLadder
		case n == "snow":
			bi.kind = kSnow
		case tallPlants[n]:
			bi.kind, bi.support = kTallPlant, "supports_vegetation"
		case plantSupport[n] != "":
			bi.kind, bi.support = kPlant, plantSupport[n]
		case tag("small_flowers", n) || tag("saplings", n) && n != "mangrove_propagule":
			bi.kind, bi.support = kPlant, "supports_vegetation"
		case strings.HasSuffix(n, "_carpet"):
			bi.kind = kCarpet
		case strings.HasSuffix(n, "_pressure_plate"):
			bi.kind = kPlate
		case n == "rail" || strings.HasSuffix(n, "_rail"):
			bi.kind = kRail
		case n == "repeater" || n == "comparator":
			bi.kind = kDiode
		case n == "redstone_wire":
			bi.kind = kWire
		case strings.HasSuffix(n, "candle") && has("candles"):
			bi.kind = kCandle
		case n == "sea_pickle":
			bi.kind = kPickle
		case strings.HasSuffix(n, "lantern") && has("hanging"):
			bi.kind = kLantern
		case n == "cactus":
			bi.kind, bi.support = kCactus, "supports_cactus"
		case n == "sugar_cane":
			bi.kind, bi.support = kSugarCane, "supports_sugar_cane"
		}
	}
}

// Face geometry from the collision shapes: bit d is a sturdy (fully
// covered) face d, bits 6/7 support the centre of the top/bottom face.
var (
	geoOnce sync.Once
	geo     []uint8
)

const (
	geoCenterUp   = 1 << 6
	geoCenterDown = 1 << 7
)

func geometry(s data.StateID) uint8 {
	geoOnce.Do(func() {
		n := data.NumStates()
		geo = make([]uint8, n)
		reg := data.Canonical()
		for i := range n {
			st := data.StateID(i)
			b := reg.BlockOf(st)
			if b != nil && (b.Name == "soul_sand" || b.Name == "mud") {
				geo[i] = 0x3f | geoCenterUp | geoCenterDown
				continue
			}
			var g uint8
			for d := range 6 {
				if faceCovers(st, d, 0, 0, 1, 1) {
					g |= 1 << d
				}
			}
			if faceCovers(st, dUp, 7.0/16, 7.0/16, 9.0/16, 9.0/16) {
				g |= geoCenterUp
			}
			if faceCovers(st, dDown, 7.0/16, 7.0/16, 9.0/16, 9.0/16) {
				g |= geoCenterDown
			}
			geo[i] = g
		}
	})
	if int(s) >= len(geo) {
		return 0
	}
	return geo[s]
}

func sturdy(s data.StateID, d int) bool { return geometry(s)&(1<<d) != 0 }

const eps = 1e-7

// faceCovers reports whether the collision boxes of s touching face d cover
// the rectangle [u0,u1]x[v0,v1] of that face (u,v = x,z for vertical faces,
// x,y for north/south and z,y for west/east).
func faceCovers(s data.StateID, d int, u0, v0, u1, v1 float64) bool {
	var rects [][4]float64
	for _, b := range data.Collision(s) {
		var touch bool
		var r [4]float64
		switch d {
		case dDown:
			touch, r = b.MinY <= eps, [4]float64{b.MinX, b.MinZ, b.MaxX, b.MaxZ}
		case dUp:
			touch, r = b.MaxY >= 1-eps, [4]float64{b.MinX, b.MinZ, b.MaxX, b.MaxZ}
		case dNorth:
			touch, r = b.MinZ <= eps, [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}
		case dSouth:
			touch, r = b.MaxZ >= 1-eps, [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}
		case dWest:
			touch, r = b.MinX <= eps, [4]float64{b.MinZ, b.MinY, b.MaxZ, b.MaxY}
		case dEast:
			touch, r = b.MaxX >= 1-eps, [4]float64{b.MinZ, b.MinY, b.MaxZ, b.MaxY}
		}
		if touch {
			rects = append(rects, r)
		}
	}
	if len(rects) == 0 {
		return false
	}
	us, vs := []float64{u0, u1}, []float64{v0, v1}
	for _, r := range rects {
		for _, u := range [2]float64{r[0], r[2]} {
			if u > u0 && u < u1 {
				us = append(us, u)
			}
		}
		for _, v := range [2]float64{r[1], r[3]} {
			if v > v0 && v < v1 {
				vs = append(vs, v)
			}
		}
	}
	for i := range us {
		for j := range vs {
			if us[i] >= u1 || vs[j] >= v1 {
				continue
			}
			nu, nv := u1, v1
			for _, u := range us {
				if u > us[i] && u < nu {
					nu = u
				}
			}
			for _, v := range vs {
				if v > vs[j] && v < nv {
					nv = v
				}
			}
			cu, cv := (us[i]+nu)/2, (vs[j]+nv)/2
			in := false
			for _, r := range rects {
				if cu > r[0] && cu < r[2] && cv > r[1] && cv < r[3] {
					in = true
					break
				}
			}
			if !in {
				return false
			}
		}
	}
	return true
}

// conductor approximates isRedstoneConductor: an opaque full cube.
func conductor(s data.StateID) bool {
	f := data.Flags(s)
	return f&data.FlagFullCube != 0 && f&data.FlagTransparent == 0
}

func solid(s data.StateID) bool { return data.Flags(s)&data.FlagSolid != 0 }

func prop(s data.StateID, k string) string {
	v, _ := data.Canonical().Value(s, k)
	return v
}

func with(s data.StateID, k, v string) data.StateID {
	if n, ok := data.With(s, k, v); ok {
		return n
	}
	return s
}

func sameBlock(a, b data.StateID) bool {
	reg := data.Canonical()
	return reg.BlockID(a) == reg.BlockID(b)
}

func inSupport(s data.StateID, tag string) bool {
	b := data.BlockOf(s)
	return b != nil && inTag("block", "minecraft:"+tag, "minecraft:"+b.Name)
}

func at(r blockReader, x, y, z, d int) data.StateID {
	o := faceOffsets[d]
	return r.Block(x+o[0], y+o[1], z+o[2])
}

func gateConnects(s data.StateID, d int) bool {
	f := dirByName(prop(s, "facing"))
	return f >= 0 && dirAxis(f) == dirAxis(dirCW[d])
}

// connects is the fence/wall/pane connectsTo of self (kind k) toward the
// neighbour n in direction d.
func connects(self *blockInfo, n data.StateID, d int) bool {
	ni := infoOf(n)
	plain := !ni.exception && sturdy(n, dirOpp[d])
	gate := ni.kind == kGate && gateConnects(n, d)
	switch self.kind {
	case kFence:
		return plain || ni.kind == kFence && ni.wooden == self.wooden || gate
	case kWall:
		return ni.wall || plain || ni.kind == kPane || gate
	case kPane:
		return plain || ni.kind == kPane || ni.wall
	}
	return false
}

var wallTests = map[int][4]float64{
	dNorth: {7.0 / 16, 0, 9.0 / 16, 9.0 / 16},
	dSouth: {7.0 / 16, 7.0 / 16, 9.0 / 16, 1},
	dWest:  {0, 7.0 / 16, 9.0 / 16, 9.0 / 16},
	dEast:  {7.0 / 16, 7.0 / 16, 1, 9.0 / 16},
}

func coversDown(s data.StateID, r [4]float64) bool {
	return faceCovers(s, dDown, r[0], r[1], r[2], r[3])
}

func wallShape(r blockReader, x, y, z int, st data.StateID, self *blockInfo) data.StateID {
	above := r.Block(x, y+1, z)
	var side [6]string
	for _, d := range horiz {
		v := "none"
		if connects(self, at(r, x, y, z, d), d) {
			v = "low"
			if coversDown(above, wallTests[d]) {
				v = "tall"
			}
		}
		side[d] = v
		st = with(st, faceNames[d], v)
	}
	return with(st, "up", boolStr(raisePost(above, side)))
}

func raisePost(above data.StateID, side [6]string) bool {
	ai := infoOf(above)
	if ai.postOverride || ai.kind == kWall && prop(above, "up") == "true" {
		return true
	}
	n, s, e, w := side[dNorth] != "none", side[dSouth] != "none", side[dEast] != "none", side[dWest] != "none"
	if !(n && s && !e && !w || e && w && !n && !s) {
		return true
	}
	if side[dNorth] == "tall" && side[dSouth] == "tall" || side[dEast] == "tall" && side[dWest] == "tall" {
		return false
	}
	return coversDown(above, [4]float64{7.0 / 16, 7.0 / 16, 9.0 / 16, 9.0 / 16})
}

func isStairs(s data.StateID) bool { return infoOf(s).kind == kStairs }

// stairShape is the stair shape from the stairs behind and in front.
func stairShape(r blockReader, x, y, z int, st data.StateID) string {
	f := dirByName(prop(st, "facing"))
	half := prop(st, "half")
	takes := func(d int) bool {
		o := at(r, x, y, z, d)
		return !isStairs(o) || prop(o, "facing") != prop(st, "facing") || prop(o, "half") != half
	}
	if b := at(r, x, y, z, f); isStairs(b) && prop(b, "half") == half {
		if d := dirByName(prop(b, "facing")); dirAxis(d) != dirAxis(f) && takes(dirOpp[d]) {
			if d == dirCCW[f] {
				return "outer_left"
			}
			return "outer_right"
		}
	}
	if b := at(r, x, y, z, dirOpp[f]); isStairs(b) && prop(b, "half") == half {
		if d := dirByName(prop(b, "facing")); dirAxis(d) != dirAxis(f) && takes(d) {
			if d == dirCCW[f] {
				return "inner_left"
			}
			return "inner_right"
		}
	}
	return "straight"
}

// chestConnected is the side a chest half's partner is on.
func chestConnected(s data.StateID) int {
	f := dirByName(prop(s, "facing"))
	switch prop(s, "type") {
	case "left":
		return dirCW[f]
	case "right":
		return dirCCW[f]
	}
	return -1
}

func chestOpposite(t string) string {
	switch t {
	case "left":
		return "right"
	case "right":
		return "left"
	}
	return t
}

func wireConnects(n data.StateID, d int) bool {
	ni := infoOf(n)
	switch {
	case ni.kind == kWire:
		return true
	case data.BlockOf(n).Name == "repeater":
		f := dirByName(prop(n, "facing"))
		return f == d || f == dirOpp[d]
	case data.BlockOf(n).Name == "observer":
		return dirByName(prop(n, "facing")) == d
	}
	return ni.signal
}

func wireShape(r blockReader, x, y, z int, st data.StateID) data.StateID {
	coverAbove := conductor(r.Block(x, y+1, z))
	var v [6]string
	for _, d := range horiz {
		o := faceOffsets[d]
		n := r.Block(x+o[0], y, z+o[2])
		switch {
		case !coverAbove && infoOf(r.Block(x+o[0], y+1, z+o[2])).kind == kWire && sturdy(n, dUp):
			v[d] = "up"
		case wireConnects(n, dirOpp[d]), !conductor(n) && infoOf(r.Block(x+o[0], y-1, z+o[2])).kind == kWire:
			v[d] = "side"
		default:
			v[d] = "none"
		}
	}
	on := func(d int) bool { return v[d] != "none" }
	if !on(dNorth) && !on(dSouth) && !on(dEast) && !on(dWest) {
		// an isolated wire is a cross unless it was toggled to a dot
		dot := true
		for _, d := range horiz {
			dot = dot && prop(st, faceNames[d]) == "none"
		}
		for _, d := range horiz {
			st = with(st, faceNames[d], pickStr(dot, "none", "side"))
		}
		return st
	}
	if !on(dNorth) && !on(dSouth) && (on(dEast) != on(dWest)) {
		if !on(dEast) {
			v[dEast] = "side"
		} else {
			v[dWest] = "side"
		}
	}
	if !on(dEast) && !on(dWest) && (on(dNorth) != on(dSouth)) {
		if !on(dNorth) {
			v[dNorth] = "side"
		} else {
			v[dSouth] = "side"
		}
	}
	for _, d := range horiz {
		st = with(st, faceNames[d], v[d])
	}
	return st
}

// attachedDir is where a face-attached block (button, lever) is held from.
func attachedDir(s data.StateID) int {
	switch prop(s, "face") {
	case "floor":
		return dDown
	case "ceiling":
		return dUp
	}
	return dirOpp[dirByName(prop(s, "facing"))]
}

// survives is canSurvive for the kinds that need support.
func survives(r blockReader, x, y, z int, st data.StateID) bool {
	bi := infoOf(st)
	below := func() data.StateID { return r.Block(x, y-1, z) }
	behind := func() data.StateID { return at(r, x, y, z, dirOpp[dirByName(prop(st, "facing"))]) }
	switch bi.kind {
	case kTorch, kCandle:
		return geometry(below())&geoCenterUp != 0
	case kWallTorch, kLadder:
		return sturdy(behind(), dirByName(prop(st, "facing")))
	case kStandSign:
		return solid(below())
	case kWallSign:
		return solid(behind())
	case kAttached:
		d := attachedDir(st)
		return sturdy(at(r, x, y, z, d), dirOpp[d])
	case kSnow:
		b := below()
		switch {
		case inSupport(b, "cannot_support_snow_layer"):
			return false
		case inSupport(b, "support_override_snow_layer"):
			return true
		}
		return sturdy(b, dUp) || infoOf(b).kind == kSnow && prop(b, "layers") == "8"
	case kPlant:
		return inSupport(below(), bi.support)
	case kTallPlant:
		if prop(st, "half") == "upper" {
			b := below()
			return sameBlock(b, st) && prop(b, "half") == "lower"
		}
		return inSupport(below(), bi.support)
	case kDoor:
		b := below()
		if prop(st, "half") == "upper" {
			return sameBlock(b, st) && prop(b, "half") == "lower"
		}
		return sturdy(b, dUp)
	case kCarpet:
		return !data.IsAir(below())
	case kPlate:
		return sturdy(below(), dUp) || geometry(below())&geoCenterUp != 0
	case kRail, kDiode:
		return sturdy(below(), dUp)
	case kWire:
		b := below()
		return sturdy(b, dUp) || data.BlockOf(b).Name == "hopper"
	case kPickle:
		b := below()
		return sturdy(b, dUp) || len(data.Collision(b)) > 0 && faceCoversAny(b)
	case kLantern:
		if prop(st, "hanging") == "true" {
			return geometry(r.Block(x, y+1, z))&geoCenterDown != 0
		}
		return geometry(below())&geoCenterUp != 0
	case kCactus:
		b := below()
		if !sameBlock(b, st) && !inSupport(b, bi.support) {
			return false
		}
		for _, d := range horiz {
			n := at(r, x, y, z, d)
			if solid(n) || data.BlockOf(n).Name == "lava" {
				return false
			}
		}
		return true
	case kSugarCane:
		b := below()
		if sameBlock(b, st) {
			return true
		}
		if !inSupport(b, bi.support) {
			return false
		}
		for _, d := range horiz {
			o := faceOffsets[d]
			n := r.Block(x+o[0], y-1, z+o[2])
			if nm := data.BlockOf(n).Name; nm == "water" || nm == "frosted_ice" || prop(n, "waterlogged") == "true" {
				return true
			}
		}
		return false
	}
	return true
}

func faceCoversAny(s data.StateID) bool {
	for _, b := range data.Collision(s) {
		if b.MaxY >= 1-eps {
			return true
		}
	}
	return false
}

// connect recomputes the neighbour-derived properties of st at x,y,z;
// two-part blocks whose other part is gone become air.
func connect(r blockReader, x, y, z int, st data.StateID) data.StateID {
	bi := infoOf(st)
	switch bi.kind {
	case kFence, kPane:
		for _, d := range horiz {
			st = with(st, faceNames[d], boolStr(connects(bi, at(r, x, y, z, d), d)))
		}
	case kWall:
		st = wallShape(r, x, y, z, st, bi)
	case kGate:
		f := dirByName(prop(st, "facing"))
		in := infoOf(at(r, x, y, z, dirCW[f])).wall || infoOf(at(r, x, y, z, dirCCW[f])).wall
		st = with(st, "in_wall", boolStr(in))
	case kStairs:
		st = with(st, "shape", stairShape(r, x, y, z, st))
	case kWire:
		st = wireShape(r, x, y, z, st)
	case kDoor:
		d, want := dUp, "upper"
		if prop(st, "half") == "upper" {
			d, want = dDown, "lower"
		}
		o := at(r, x, y, z, d)
		if !sameBlock(o, st) || prop(o, "half") != want {
			return data.Air
		}
		for _, k := range [...]string{"facing", "open", "hinge", "powered"} {
			st = with(st, k, prop(o, k))
		}
	case kTallPlant:
		if prop(st, "half") == "lower" {
			if o := r.Block(x, y+1, z); !sameBlock(o, st) || prop(o, "half") != "upper" {
				return data.Air
			}
		}
	case kBed:
		f := dirByName(prop(st, "facing"))
		d := f
		if prop(st, "part") == "head" {
			d = dirOpp[f]
		}
		if o := at(r, x, y, z, d); !sameBlock(o, st) || prop(o, "part") == prop(st, "part") {
			return data.Air
		}
	case kChest:
		cd := chestConnected(st)
		for _, d := range horiz {
			n := at(r, x, y, z, d)
			if sameBlock(n, st) {
				if prop(st, "type") == "single" && prop(n, "type") != "single" && prop(n, "facing") == prop(st, "facing") && chestConnected(n) == dirOpp[d] {
					st = with(st, "type", chestOpposite(prop(n, "type")))
					cd = chestConnected(st)
				}
			} else if d == cd {
				st, cd = with(st, "type", "single"), -1
			}
		}
	}
	return st
}

// shapeAt is updateShape over every side: the state st at x,y,z should
// take, air when it can no longer stay.
func shapeAt(r blockReader, x, y, z int, st data.StateID) data.StateID {
	if infoOf(st).kind == kNone {
		return st
	}
	if !survives(r, x, y, z, st) {
		return data.Air
	}
	return connect(r, x, y, z, st)
}

type quietKey struct {
	w   *World
	pos api.BlockPos
}

// quietBreaks marks counterpart halves a creative break removes without
// drops (doors, beds, double plants).
var quietBreaks sync.Map

type blockRW interface {
	blockReader
	SetBlock(x, y, z int, s data.StateID) bool
}

// updateNeighbours re-derives the shape of the neighbours of a block that
// changed from old to s; broke is called for each one that lost support
// and was set to air.
func updateNeighbours(t blockRW, x, y, z int, old, s data.StateID, broke func(api.BlockPos, data.StateID)) {
	for d := range 6 {
		o := faceOffsets[d]
		reshape(t, x+o[0], y+o[1], z+o[2], broke)
	}
	if infoOf(old).kind == kWire || infoOf(s).kind == kWire {
		for _, d := range horiz {
			o := faceOffsets[d]
			for _, dy := range [2]int{-1, 1} {
				if n := t.Block(x+o[0], y+dy, z+o[2]); infoOf(n).kind == kWire {
					reshape(t, x+o[0], y+dy, z+o[2], broke)
				}
			}
		}
	}
}

func reshape(t blockRW, x, y, z int, broke func(api.BlockPos, data.StateID)) {
	st := t.Block(x, y, z)
	ns := shapeAt(t, x, y, z, st)
	if ns == st {
		return
	}
	if ns != data.Air {
		t.SetBlock(x, y, z, ns)
		return
	}
	if t.SetBlock(x, y, z, data.Air) && broke != nil {
		broke(api.BlockPos{X: x, Y: y, Z: z}, st)
	}
}

// shapeUpdate is the region ShapeUpdate hook: neighbours of a changed block
// re-derive their shape; ones that lose support break with drops.
func (w *World) shapeUpdate(t *world.TickContext, x, y, z int, old, s data.StateID) {
	updateNeighbours(t, x, y, z, old, s, func(pos api.BlockPos, st data.StateID) {
		w.levelEvent(nil, levelBreak, pos, int32(st))
		if _, quiet := quietBreaks.LoadAndDelete(quietKey{w, pos}); !quiet {
			w.DropBlockLoot(pos, st, item.Stack{}, nil, 0)
		}
	})
}

// counterpart is the other half of a door, double plant or bed.
func counterpart(pos api.BlockPos, st data.StateID) (api.BlockPos, bool) {
	switch infoOf(st).kind {
	case kDoor, kTallPlant:
		if prop(st, "half") == "upper" {
			return pos.Add(0, -1, 0), true
		}
		return pos.Add(0, 1, 0), true
	case kBed:
		f := dirByName(prop(st, "facing"))
		if prop(st, "part") == "head" {
			f = dirOpp[f]
		}
		o := faceOffsets[f]
		return pos.Add(o[0], o[1], o[2]), true
	}
	return pos, false
}
