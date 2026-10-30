package server

import (
	"math"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

// Placement states (vanilla getStateForPlacement / canBeReplaced) per block
// family, from the minecraft.wiki block pages and reference observations.

type placeCtx struct {
	r       blockReader
	item    string // block item name without namespace
	clicked api.BlockPos
	face    int
	cursor  api.Vec3 // hit point relative to clicked
	yaw     float32
	pitch   float32
	sneak   bool

	replaceClicked bool
	pos            api.BlockPos
}

// relHit is the hit point relative to the target position.
func (c *placeCtx) relHit() api.Vec3 {
	return api.Vec3{X: float64(c.clicked.X-c.pos.X) + c.cursor.X, Y: float64(c.clicked.Y-c.pos.Y) + c.cursor.Y, Z: float64(c.clicked.Z-c.pos.Z) + c.cursor.Z}
}

// horizontal is the player's horizontal facing (Direction.fromYRot).
func (c *placeCtx) horizontal() int {
	return dirByName(horizontal[int(math.Floor(float64(c.yaw)/90+0.5))&3])
}

// looking is getNearestLookingDirections: directions ordered by the look
// vector, with the face opposite the clicked one first unless the clicked
// block itself is being replaced.
func (c *placeCtx) looking() []int {
	yr, pr := float64(c.yaw)*math.Pi/180, float64(c.pitch)*math.Pi/180
	vx, vy, vz := -math.Sin(yr)*math.Cos(pr), -math.Sin(pr), math.Cos(yr)*math.Cos(pr)
	ax := [3]struct {
		m float64
		d int
	}{{math.Abs(vx), pick(vx > 0, dEast, dWest)}, {math.Abs(vy), pick(vy > 0, dUp, dDown)}, {math.Abs(vz), pick(vz > 0, dSouth, dNorth)}}
	// stable order on ties: x, y, z
	for i := 1; i < 3; i++ {
		for j := i; j > 0 && ax[j].m > ax[j-1].m; j-- {
			ax[j], ax[j-1] = ax[j-1], ax[j]
		}
	}
	out := []int{ax[0].d, ax[1].d, ax[2].d, dirOpp[ax[2].d], dirOpp[ax[1].d], dirOpp[ax[0].d]}
	if c.replaceClicked {
		return out
	}
	first := dirOpp[c.face]
	res := []int{first}
	for _, d := range out {
		if d != first {
			res = append(res, d)
		}
	}
	return res
}

func pick(c bool, a, b int) int {
	if c {
		return a
	}
	return b
}

// itemBlocks maps block items whose block has another name.
var itemBlocks = map[string]string{
	"redstone": "redstone_wire", "string": "tripwire", "wheat_seeds": "wheat", "carrot": "carrots", "potato": "potatoes",
	"beetroot_seeds": "beetroots", "melon_seeds": "melon_stem", "pumpkin_seeds": "pumpkin_stem", "sweet_berries": "sweet_berry_bush",
	"cocoa_beans": "cocoa", "glow_berries": "cave_vines", "torchflower_seeds": "torchflower_crop", "pitcher_pod": "pitcher_crop",
}

func itemBlock(it string) string {
	it = strings.TrimPrefix(it, "minecraft:")
	if b, ok := itemBlocks[it]; ok {
		return b
	}
	return it
}

// wallVariant is the wall-mounted block of a standing-and-wall item.
func wallVariant(name string) (string, bool) {
	var w string
	switch {
	case strings.HasSuffix(name, "_hanging_sign"):
		return "", false
	case name == "torch":
		w = "wall_torch"
	case strings.HasSuffix(name, "_torch"), strings.HasSuffix(name, "_sign"), strings.HasSuffix(name, "_banner"),
		strings.HasSuffix(name, "_skull"), strings.HasSuffix(name, "_head"), strings.HasSuffix(name, "_fan"):
		i := strings.LastIndexByte(name, '_')
		w = name[:i] + "_wall" + name[i:]
	default:
		return "", false
	}
	_, ok := data.DefaultState(w)
	return w, ok
}

// canBeReplaced is BlockState.canBeReplaced(BlockPlaceContext) for st,
// with c.pos set to st's position.
func canBeReplaced(st data.StateID, c *placeCtx) bool {
	b := data.BlockOf(st)
	if b == nil {
		return false
	}
	same := b.Name == itemBlock(c.item)
	switch {
	case same && strings.HasSuffix(b.Name, "_slab"):
		t := prop(st, "type")
		if t == "double" {
			return false
		}
		if !c.replaceClicked {
			return true
		}
		upper := c.relHit().Y > 0.5
		hz := c.face >= dNorth
		if t == "bottom" {
			return c.face == dUp || upper && hz
		}
		return c.face == dDown || !upper && hz
	case same && b.Name == "snow":
		if n, _ := strconv.Atoi(prop(st, "layers")); n < 8 {
			return !c.replaceClicked || c.face == dUp
		}
		return false
	case same && (infoOf(st).kind == kCandle || b.Name == "sea_pickle" || b.Name == "turtle_egg"):
		k := map[string]string{"sea_pickle": "pickles", "turtle_egg": "eggs"}[b.Name]
		if k == "" {
			k = "candles"
		}
		n, _ := strconv.Atoi(prop(st, k))
		return !c.sneak && n < 4
	}
	if data.IsAir(st) {
		return true
	}
	return data.Flags(st)&data.FlagReplaceable != 0 && !same
}

func waterSource(st data.StateID) bool {
	b := data.BlockOf(st)
	return b != nil && b.Name == "water" && prop(st, "level") == "0"
}

// placement is getStateForPlacement for the block of c.item at c.pos
// replacing old; ok is false when nothing can be placed there.
func placement(c *placeCtx, base, old data.StateID) (data.StateID, bool) {
	b := data.BlockOf(base)
	name := b.Name
	bi := infoOf(base)
	x, y, z := c.pos.X, c.pos.Y, c.pos.Z
	st := with(base, "waterlogged", boolStr(waterSource(old)))
	if sameBlock(old, base) {
		switch {
		case strings.HasSuffix(name, "_slab"):
			return with(with(old, "type", "double"), "waterlogged", "false"), true
		case name == "snow":
			n, _ := strconv.Atoi(prop(old, "layers"))
			return with(old, "layers", strconv.Itoa(min(8, n+1))), true
		case bi.kind == kCandle || name == "sea_pickle" || name == "turtle_egg":
			for _, k := range [...]string{"candles", "pickles", "eggs"} {
				if v := prop(old, k); v != "" {
					n, _ := strconv.Atoi(v)
					return with(old, k, strconv.Itoa(n+1)), true
				}
			}
		}
	}
	h := c.horizontal()
	if wall, ok := wallVariant(name); ok {
		ws, _ := data.DefaultState(wall)
		for _, d := range c.looking() {
			var s data.StateID
			switch {
			case d == dUp:
				continue
			case d == dDown:
				s = rotated(st, c.yaw, name)
			default:
				s = with(with(ws, "facing", faceNames[dirOpp[d]]), "waterlogged", boolStr(waterSource(old)))
			}
			if survives(c.r, x, y, z, s) {
				return s, true
			}
		}
		return 0, false
	}
	switch bi.kind {
	case kAttached:
		for _, d := range c.looking() {
			s := st
			switch d {
			case dUp:
				s = with(with(s, "face", "ceiling"), "facing", faceNames[h])
			case dDown:
				s = with(with(s, "face", "floor"), "facing", faceNames[h])
			default:
				s = with(with(s, "face", "wall"), "facing", faceNames[dirOpp[d]])
			}
			if survives(c.r, x, y, z, s) {
				return s, true
			}
		}
		return 0, false
	case kLadder:
		if !c.replaceClicked {
			if o := at(c.r, x, y, z, dirOpp[c.face]); infoOf(o).kind == kLadder && prop(o, "facing") == faceNames[c.face] {
				return 0, false
			}
		}
		for _, d := range c.looking() {
			if d < dNorth {
				continue
			}
			if s := with(st, "facing", faceNames[dirOpp[d]]); survives(c.r, x, y, z, s) {
				return s, true
			}
		}
		return 0, false
	case kLantern:
		for _, d := range c.looking() {
			if d >= dNorth {
				continue
			}
			if s := with(st, "hanging", boolStr(d == dUp)); survives(c.r, x, y, z, s) {
				return s, true
			}
		}
		return 0, false
	case kChest:
		return chestPlacement(c, st), true
	case kWire:
		for _, d := range horiz {
			st = with(st, faceNames[d], "side")
		}
	case kDoor:
		st = with(with(st, "facing", faceNames[h]), "hinge", doorHinge(c, base, h))
	case kStairs:
		st = with(with(st, "facing", faceNames[h]), "half", pickStr(c.face != dUp && (c.face == dDown || c.relHit().Y > 0.5), "top", "bottom"))
	case kBed, kGate:
		st = with(st, "facing", faceNames[h])
	}
	if strings.HasSuffix(name, "_slab") {
		st = with(st, "type", pickStr(c.face != dDown && (c.face == dUp || c.relHit().Y <= 0.5), "bottom", "top"))
	}
	if strings.HasSuffix(name, "_trapdoor") {
		if !c.replaceClicked && c.face >= dNorth {
			st = with(with(st, "facing", faceNames[c.face]), "half", pickStr(c.relHit().Y > 0.5, "top", "bottom"))
		} else {
			st = with(with(st, "facing", faceNames[dirOpp[h]]), "half", pickStr(c.face == dUp, "bottom", "top"))
		}
	}
	if ax := prop(st, "axis"); ax != "" {
		st = with(st, "axis", [3]string{"x", "y", "z"}[dirAxis(c.face)])
	}
	switch bi.kind {
	case kDoor, kStairs, kBed, kGate:
	default:
		if !strings.HasSuffix(name, "_trapdoor") {
			st = genericFacing(c, st, name)
		}
	}
	if bi.kind == kStandSign || prop(st, "rotation") != "" {
		st = rotated(st, c.yaw, name)
	}
	if !placeSurvives(c.r, x, y, z, st) {
		return 0, false
	}
	switch bi.kind {
	case kDoor, kBed, kTallPlant:
		return st, true
	}
	return connect(c.r, x, y, z, st), true
}

func pickStr(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

// rotated sets a standing sign/banner/skull's 16-step rotation from yaw.
func rotated(st data.StateID, yaw float32, name string) data.StateID {
	if prop(st, "rotation") == "" {
		return st
	}
	y := float64(yaw) + 180
	if strings.HasSuffix(name, "_skull") || strings.HasSuffix(name, "_head") {
		y = float64(yaw)
	}
	return with(st, "rotation", strconv.Itoa(int(math.Floor(y*16/360+0.5))&15))
}

var (
	sameFacing = map[string]bool{"campfire": true, "soul_campfire": true, "decorated_pot": true}
	faceFacing = map[string]bool{"end_rod": true}
	lookFacing = map[string]bool{"observer": true}
)

// genericFacing orients a block with a plain facing property.
func genericFacing(c *placeCtx, st data.StateID, name string) data.StateID {
	b := data.BlockOf(st)
	pi := b.PropIndex("facing")
	if pi < 0 {
		return st
	}
	h := c.horizontal()
	vals := b.Props[pi].Values
	switch {
	case len(vals) == 4:
		switch {
		case sameFacing[name]:
			return with(st, "facing", faceNames[h])
		case strings.HasSuffix(name, "anvil"):
			return with(st, "facing", faceNames[dirCW[h]])
		}
		return with(st, "facing", faceNames[dirOpp[h]])
	case name == "hopper":
		d := dirOpp[c.face]
		if d == dUp {
			d = dDown
		}
		return with(st, "facing", faceNames[d])
	case faceFacing[name] || strings.HasSuffix(name, "shulker_box") || strings.HasSuffix(name, "lightning_rod") || strings.Contains(name, "amethyst_"):
		return with(st, "facing", faceNames[c.face])
	case lookFacing[name]:
		return with(st, "facing", faceNames[c.lookAll()[0]])
	}
	return with(st, "facing", faceNames[dirOpp[c.lookAll()[0]]])
}

// lookAll is Direction.orderedByNearest without the clicked-face bias.
func (c *placeCtx) lookAll() []int {
	rc := c.replaceClicked
	c.replaceClicked = true
	l := c.looking()
	c.replaceClicked = rc
	return l
}

// placeSurvives is canSurvive at placement time: two-part blocks check
// only the support of the half being placed.
func placeSurvives(r blockReader, x, y, z int, st data.StateID) bool {
	switch infoOf(st).kind {
	case kDoor:
		return sturdy(r.Block(x, y-1, z), dUp)
	case kTallPlant:
		return inSupport(r.Block(x, y-1, z), infoOf(st).support)
	case kBed:
		return true
	}
	return survives(r, x, y, z, st)
}

// doorHinge picks the hinge side from the neighbouring blocks and doors,
// then from where the click hit.
func doorHinge(c *placeCtx, door data.StateID, f int) string {
	x, y, z := c.pos.X, c.pos.Y, c.pos.Z
	l, r := faceOffsets[dirCCW[f]], faceOffsets[dirCW[f]]
	full := func(s data.StateID) int {
		if data.Flags(s)&data.FlagFullCube != 0 {
			return 1
		}
		return 0
	}
	ls, rs := c.r.Block(x+l[0], y, z+l[2]), c.r.Block(x+r[0], y, z+r[2])
	i := -full(ls) - full(c.r.Block(x+l[0], y+1, z+l[2])) + full(rs) + full(c.r.Block(x+r[0], y+1, z+r[2]))
	ld := sameBlock(ls, door) && prop(ls, "half") == "lower"
	rd := sameBlock(rs, door) && prop(rs, "half") == "lower"
	if ld && !rd || i > 0 {
		return "right"
	}
	if rd && !ld || i < 0 {
		return "left"
	}
	o := faceOffsets[f]
	hit := c.relHit()
	if o[0] < 0 && hit.Z < 0.5 || o[0] > 0 && hit.Z > 0.5 || o[2] < 0 && hit.X > 0.5 || o[2] > 0 && hit.X < 0.5 {
		return "right"
	}
	return "left"
}

// chestPlacement faces the chest toward the player and pairs it with a
// single neighbour chest facing the same way (only the clicked one when
// sneaking).
func chestPlacement(c *placeCtx, st data.StateID) data.StateID {
	x, y, z := c.pos.X, c.pos.Y, c.pos.Z
	facing := dirOpp[c.horizontal()]
	typ := "single"
	partner := func(d int) int {
		n := at(c.r, x, y, z, d)
		if sameBlock(n, st) && prop(n, "type") == "single" {
			return dirByName(prop(n, "facing"))
		}
		return -1
	}
	if c.face >= dNorth && c.sneak {
		if d := partner(dirOpp[c.face]); d >= 0 && dirAxis(d) != dirAxis(c.face) {
			facing = d
			typ = pickStr(dirCCW[d] == dirOpp[c.face], "right", "left")
		}
	}
	if typ == "single" && !c.sneak {
		if facing == partner(dirCW[facing]) {
			typ = "left"
		} else if facing == partner(dirCCW[facing]) {
			typ = "right"
		}
	}
	return with(with(st, "facing", faceNames[facing]), "type", typ)
}

// companion is the second block a two-part placement adds.
func companion(pos api.BlockPos, st data.StateID) (api.BlockPos, data.StateID, bool) {
	switch infoOf(st).kind {
	case kBed:
		o := faceOffsets[dirByName(prop(st, "facing"))]
		return pos.Add(o[0], 0, o[2]), with(st, "part", "head"), true
	}
	if prop(st, "half") == "lower" {
		return pos.Add(0, 1, 0), with(st, "half", "upper"), true
	}
	return pos, 0, false
}

// placeState resolves where and as what the block of item lands for a
// click on face of clicked; upper is set for the second block of a
// two-part placement.
func (p *Player) placeState(w *World, it string, clicked api.BlockPos, face api.BlockFace, cursor api.Vec3) (target api.BlockPos, old, placed data.StateID, upperPos api.BlockPos, upper data.StateID, ok bool) {
	loc := p.Location()
	c := &placeCtx{r: w.w, item: it, clicked: clicked, face: int(face), cursor: cursor, yaw: loc.Yaw, pitch: loc.Pitch, sneak: p.Sneaking()}
	return resolvePlace(c, w.w.Dim().Contains)
}

func resolvePlace(c *placeCtx, inDim func(y int) bool) (target api.BlockPos, old, placed data.StateID, upperPos api.BlockPos, upper data.StateID, ok bool) {
	clicked := c.clicked
	base, found := data.DefaultState(itemBlock(c.item))
	target, c.pos = clicked, clicked
	if !found {
		return target, 0, 0, target, 0, false
	}
	c.replaceClicked = canBeReplaced(c.r.Block(clicked.X, clicked.Y, clicked.Z), c)
	if !c.replaceClicked {
		o := faceOffsets[c.face]
		target = clicked.Add(o[0], o[1], o[2])
		c.pos = target
	}
	old = c.r.Block(target.X, target.Y, target.Z)
	if !inDim(target.Y) || !c.replaceClicked && !canBeReplaced(old, c) {
		return target, old, 0, target, 0, false
	}
	placed, ok = placement(c, base, old)
	if !ok {
		return target, old, 0, target, 0, false
	}
	if q, u, two := companion(target, placed); two {
		c.pos = q
		a := c.r.Block(q.X, q.Y, q.Z)
		if !inDim(q.Y) || !canBeReplaced(a, c) {
			return target, old, 0, target, 0, false
		}
		u = with(u, "waterlogged", boolStr(waterSource(a)))
		return target, old, placed, q, u, true
	}
	return target, old, placed, target, 0, true
}
