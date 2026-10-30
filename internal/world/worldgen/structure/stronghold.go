package structure

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Stronghold layout, inferred from the reference start NBT of 24 ring
// chunks (3539 pieces, testdata/stronghold/tstarts_*) and the call-path draw
// logs of 3 starts (starts_log_*), both from the black-box probes
// kremenmc.probe.temple.TStarts and kremenmc.probe.legacy.Starts. See
// docs/vanilla-parity.md "Stronghold".
//
// Geometry: a child is created at a door point (the block just outside the
// parent's exit) facing a direction; its box is the door offset by the
// type's (ox, oy, oz) and size (w lateral, h, d forward) in that direction.
// Forward exits sit at lateral min + lx, y min + ly, one past the far end;
// side exits at lateral min - 1 (left) or max + 1 (right), y min + ly and
// forward-axis min + c, whatever the orientation (the five-way crossing's
// low/high side doors swap heights facing north and west).

type shKind uint8

const (
	shStart shKind = iota
	shStraight
	shPrison
	shLeft
	shRight
	shRoom
	shStraightStairs
	shStairs
	shFive
	shChest
	shLibrary
	shPortal
	shFiller
	shKinds
)

var shIDs = [shKinds]string{"minecraft:shstart", "minecraft:shs", "minecraft:shph", "minecraft:shlt", "minecraft:shrt",
	"minecraft:shrc", "minecraft:shssd", "minecraft:shsd", "minecraft:sh5c", "minecraft:shcc", "minecraft:shli",
	"minecraft:shpr", "minecraft:shfc"}

// shSize: offsets from the door and size (lateral, up, forward).
var shSize = [shKinds][6]int32{
	shStart:          {-1, -7, 0, 5, 11, 5},
	shStraight:       {-1, -1, 0, 5, 5, 7},
	shPrison:         {-1, -1, 0, 9, 5, 11},
	shLeft:           {-1, -1, 0, 5, 5, 5},
	shRight:          {-1, -1, 0, 5, 5, 5},
	shRoom:           {-4, -1, 0, 11, 7, 11},
	shStraightStairs: {-1, -7, 0, 5, 11, 8},
	shStairs:         {-1, -7, 0, 5, 11, 5},
	shFive:           {-4, -3, 0, 10, 9, 11},
	shChest:          {-1, -1, 0, 5, 5, 7},
	shLibrary:        {-4, -1, 0, 14, 11, 15},
	shPortal:         {-4, -1, 0, 11, 8, 16},
	shFiller:         {-1, -1, 0, 5, 5, 0},
}

// shDoor is a piece's entry door (EntryDoor).
type shDoor uint8

const (
	shOpening shDoor = iota
	shWoodDoor
	shGrates
	shIronDoor
)

var shDoorNames = [4]string{"OPENING", "WOOD_DOOR", "GRATES", "IRON_DOOR"}

type shPiece struct {
	kind  shKind
	box   Box
	dir   msDir
	depth int32
	door  shDoor

	left, right                            bool // straight
	leftLow, leftHigh, rightLow, rightHigh bool // five-way crossing
	roomType                               int32
	steps                                  int32 // filler
	tall                                   bool  // library
	source                                 bool  // stairs down (start)
	chest, mob                             bool  // placement state
}

func (p *shPiece) Type() string { return shIDs[p.kind] }
func (p *shPiece) Box() Box     { return p.box }

func b2i8(v bool) int8 {
	if v {
		return 1
	}
	return 0
}

func (p *shPiece) Save() nbt.Compound {
	c := nbt.Compound{"id": shIDs[p.kind], "GD": p.depth, "O": msDir2D[p.dir], "BB": p.box.IntArray(), "EntryDoor": shDoorNames[p.door]}
	switch p.kind {
	case shStart:
		c["Source"] = int8(1)
	case shStairs:
		c["Source"] = b2i8(p.source)
	case shStraight:
		c["Left"], c["Right"] = b2i8(p.left), b2i8(p.right)
	case shFive:
		c["leftLow"], c["leftHigh"], c["rightLow"], c["rightHigh"] = b2i8(p.leftLow), b2i8(p.leftHigh), b2i8(p.rightLow), b2i8(p.rightHigh)
	case shRoom:
		c["Type"] = p.roomType
	case shFiller:
		c["Steps"] = p.steps
	case shLibrary:
		c["Tall"] = b2i8(p.tall)
	case shChest:
		c["Chest"] = b2i8(p.chest)
	case shPortal:
		c["Mob"] = b2i8(p.mob)
	}
	return c
}

func loadShPiece(c nbt.Compound) (Piece, error) {
	id, _ := c["id"].(string)
	p := &shPiece{kind: shKinds}
	for k, s := range shIDs {
		if s == qualify(id) {
			p.kind = shKind(k)
		}
	}
	if p.kind == shKinds {
		return nil, fmt.Errorf("stronghold: unknown piece %s", id)
	}
	bb := intList(c["BB"])
	if len(bb) != 6 {
		return nil, fmt.Errorf("stronghold: bad BB")
	}
	p.box = Box{bb[0], bb[1], bb[2], bb[3], bb[4], bb[5]}
	p.depth, _ = c["GD"].(int32)
	o, _ := c["O"].(int32)
	for i, d := range msDir2D {
		if d == o {
			p.dir = msDir(i)
		}
	}
	ed, _ := c["EntryDoor"].(string)
	for i, n := range shDoorNames {
		if n == ed {
			p.door = shDoor(i)
		}
	}
	flag := func(k string) bool { v, _ := c[k].(int8); return v != 0 }
	p.left, p.right = flag("Left"), flag("Right")
	p.leftLow, p.leftHigh, p.rightLow, p.rightHigh = flag("leftLow"), flag("leftHigh"), flag("rightLow"), flag("rightHigh")
	p.roomType, _ = c["Type"].(int32)
	p.steps, _ = c["Steps"].(int32)
	p.tall, p.source, p.chest, p.mob = flag("Tall"), flag("Source"), flag("Chest"), flag("Mob")
	return p, nil
}

// shBox is the box of a piece of size sz created at door x,y,z facing d.
func shBox(x, y, z int32, d msDir, sz [6]int32) Box {
	ox, oy, oz, w, h, dp := sz[0], sz[1], sz[2], sz[3], sz[4], sz[5]
	switch d {
	case msSouth:
		return Box{x + ox, y + oy, z + oz, x + ox + w - 1, y + oy + h - 1, z + oz + dp - 1}
	case msNorth:
		return Box{x + ox, y + oy, z - oz - dp + 1, x + ox + w - 1, y + oy + h - 1, z - oz}
	case msEast:
		return Box{x + oz, y + oy, z + ox, x + oz + dp - 1, y + oy + h - 1, z + ox + w - 1}
	default:
		return Box{x - oz - dp + 1, y + oy, z + ox, x - oz, y + oy + h - 1, z + ox + w - 1}
	}
}

// Layout bounds. The depth bound is checked on 3 probe starts of other
// seeds (TestStrongholdBoundSearch); the two y bounds are reached by no
// start of 20,000 seeds and are not verified.
var (
	shMaxDepth   int32 = 50
	shMinY       int32 = 10
	shFillerMinY int32 = 1
)

// shWeight is one entry of the weighted piece list.
type shWeight struct {
	kind   shKind
	weight int32
	limit  int32 // 0 = unlimited
	count  int32
}

type shBuilder struct {
	r       *rng.WorldgenRandom
	pieces  []*shPiece
	pending []*shPiece
	list    []*shWeight
	total   int32
	start   *shPiece
	imposed shKind // shKinds = none
	prev    *shWeight
	portal  bool
	trace   func(string)
}

func (b *shBuilder) intn(n int32, what string) int32 {
	v := b.r.NextIntn(n)
	if b.trace != nil {
		b.trace(fmt.Sprintf("nextInt(%d)=%d %s", n, v, what))
	}
	return v
}

func (b *shBuilder) boolean(what string) bool {
	v := b.r.NextBool()
	if b.trace != nil {
		b.trace(fmt.Sprintf("nextBoolean()=%v %s", v, what))
	}
	return v
}

func (b *shBuilder) collides(bx Box) bool {
	for _, p := range b.pieces {
		if p.box.Intersects(bx) {
			return true
		}
	}
	return false
}

func newShList() []*shWeight {
	return []*shWeight{
		{kind: shStraight, weight: 40},
		{kind: shPrison, weight: 5, limit: 5},
		{kind: shLeft, weight: 20},
		{kind: shRight, weight: 20},
		{kind: shRoom, weight: 10, limit: 6},
		{kind: shStraightStairs, weight: 5, limit: 5},
		{kind: shStairs, weight: 5, limit: 5},
		{kind: shFive, weight: 5, limit: 4},
		{kind: shChest, weight: 5, limit: 4},
		{kind: shLibrary, weight: 10, limit: 2},
		{kind: shPortal, weight: 20, limit: 1},
	}
}

// allowed is the per-depth rule of a list entry.
func (w *shWeight) allowed(depth int32) bool {
	if w.limit != 0 && w.count >= w.limit {
		return false
	}
	switch w.kind {
	case shLibrary:
		return depth > 4
	case shPortal:
		return depth > 5
	}
	return true
}

func (b *shBuilder) okBox(bx Box) bool {
	ok := bx.MinY > shMinY && !b.collides(bx)
	if !ok && b.trace != nil {
		for _, p := range b.pieces {
			if p.box.Intersects(bx) {
				b.trace(fmt.Sprintf("~collide %v with %s %v", bx, shIDs[p.kind], p.box))
				return false
			}
		}
		b.trace(fmt.Sprintf("~low %v", bx))
	}
	return ok
}

func (b *shBuilder) door() shDoor {
	switch b.intn(5, "door") {
	case 2:
		return shWoodDoor
	case 3:
		return shGrates
	case 4:
		return shIronDoor
	}
	return shOpening
}

// create makes a piece of kind k at the door, or nil when its box is not
// free.
func (b *shBuilder) create(k shKind, x, y, z int32, d msDir, depth int32) *shPiece {
	bx := shBox(x, y, z, d, shSize[k])
	p := &shPiece{kind: k, dir: d, depth: depth, box: bx}
	switch k {
	case shLibrary:
		p.tall = true
		if !b.okBox(bx) {
			sz := shSize[k]
			sz[4] = 6
			bx = shBox(x, y, z, d, sz)
			if !b.okBox(bx) {
				return nil
			}
			p.tall = false
			p.box = bx
		}
	case shPortal:
		if !b.okBox(bx) {
			return nil
		}
		return p
	default:
		if !b.okBox(bx) {
			return nil
		}
	}
	p.door = b.door()
	switch k {
	case shStraight:
		p.left = b.intn(2, "left") == 0
		p.right = b.intn(2, "right") == 0
	case shFive:
		p.leftLow = b.boolean("leftLow")
		p.leftHigh = b.boolean("leftHigh")
		p.rightLow = b.boolean("rightLow")
		p.rightHigh = b.intn(3, "rightHigh") > 0
	case shRoom:
		p.roomType = b.intn(5, "roomType")
	}
	return p
}

// fillerBox: a corridor of 1..3 blocks up to the next piece ahead, when
// one is there.
func (b *shBuilder) filler(x, y, z int32, d msDir, depth int32) *shPiece {
	sz := shSize[shFiller]
	sz[5] = 4
	probe := shBox(x, y, z, d, sz)
	var hit *shPiece
	for _, p := range b.pieces {
		if p.box.Intersects(probe) {
			hit = p
			break
		}
	}
	if hit == nil {
		return nil
	}
	if hit.box.MinY != probe.MinY {
		return nil
	}
	// The corridor reaches into the hit piece's first block, at most 3
	// long; nothing when the hit piece starts at the door.
	for n := int32(3); n >= 2; n-- {
		sz[5] = n - 1
		if hit.box.Intersects(shBox(x, y, z, d, sz)) {
			continue
		}
		sz[5] = n
		bx := shBox(x, y, z, d, sz)
		if bx.MinY <= shFillerMinY {
			return nil
		}
		return &shPiece{kind: shFiller, dir: d, depth: depth, box: bx, steps: n}
	}
	return nil
}

func (b *shBuilder) fromDoor(x, y, z int32, d msDir, depth int32) *shPiece {
	// Once every limited type is used up nothing more is added.
	limited := false
	for _, w := range b.list {
		limited = limited || w.limit != 0
	}
	if !limited {
		return nil
	}
	if b.imposed != shKinds {
		k := b.imposed
		b.imposed = shKinds
		if p := b.create(k, x, y, z, d, depth); p != nil {
			return p
		}
	}
	for try := 0; try < 5; try++ {
		j := b.intn(b.total, "pick")
		for _, w := range b.list {
			j -= w.weight
			if j >= 0 {
				continue
			}
			if !w.allowed(depth) || w == b.prev {
				if b.trace != nil {
					b.trace(fmt.Sprintf("~reject %s depth %d prev %v", shIDs[w.kind], depth, w == b.prev))
				}
				break
			}
			p := b.create(w.kind, x, y, z, d, depth)
			if p == nil {
				continue
			}
			b.count(w)
			b.prev = w
			if w.kind == shPortal {
				b.portal = true
			}
			return p
		}
	}
	return b.filler(x, y, z, d, depth)
}

func (b *shBuilder) count(w *shWeight) {
	w.count++
	if w.limit != 0 && w.count >= w.limit {
		for i, e := range b.list {
			if e == w {
				b.list = append(b.list[:i], b.list[i+1:]...)
				break
			}
		}
		b.total -= w.weight
	}
}

func (b *shBuilder) add(x, y, z int32, d msDir, depth int32) *shPiece {
	if depth > shMaxDepth {
		return nil
	}
	if abs32(x-b.start.box.MinX) > 112 || abs32(z-b.start.box.MinZ) > 112 {
		return nil
	}
	if b.trace != nil {
		b.trace(fmt.Sprintf("~door %d %d %d dir %d depth %d (start %d %d)", x, y, z, d, depth, b.start.box.MinX, b.start.box.MinZ))
	}
	p := b.fromDoor(x, y, z, d, depth+1)
	if p == nil {
		return nil
	}
	b.pieces = append(b.pieces, p)
	b.pending = append(b.pending, p)
	return p
}

func (b *shBuilder) forward(p *shPiece, lx, ly int32) *shPiece {
	bx := p.box
	y := bx.MinY + ly
	switch p.dir {
	case msNorth:
		return b.add(bx.MinX+lx, y, bx.MinZ-1, p.dir, p.depth)
	case msSouth:
		return b.add(bx.MinX+lx, y, bx.MaxZ+1, p.dir, p.depth)
	case msWest:
		return b.add(bx.MinX-1, y, bx.MinZ+lx, p.dir, p.depth)
	default:
		return b.add(bx.MaxX+1, y, bx.MinZ+lx, p.dir, p.depth)
	}
}

func (b *shBuilder) side(p *shPiece, right bool, ly, c int32) *shPiece {
	bx := p.box
	y := bx.MinY + ly
	if p.dir == msNorth || p.dir == msSouth {
		if right {
			return b.add(bx.MaxX+1, y, bx.MinZ+c, msEast, p.depth)
		}
		return b.add(bx.MinX-1, y, bx.MinZ+c, msWest, p.depth)
	}
	if right {
		return b.add(bx.MinX+c, y, bx.MaxZ+1, msSouth, p.depth)
	}
	return b.add(bx.MinX+c, y, bx.MinZ-1, msNorth, p.depth)
}

func (b *shBuilder) children(p *shPiece) {
	switch p.kind {
	case shStart:
		b.imposed = shFive
		b.forward(p, 1, 1)
	case shStairs, shStraightStairs, shPrison, shChest:
		b.forward(p, 1, 1)
	case shStraight:
		b.forward(p, 1, 1)
		if p.left {
			b.side(p, false, 1, 2)
		}
		if p.right {
			b.side(p, true, 1, 2)
		}
	case shLeft:
		b.side(p, p.dir == msSouth || p.dir == msWest, 1, 1)
	case shRight:
		b.side(p, p.dir == msNorth || p.dir == msEast, 1, 1)
	case shRoom:
		b.forward(p, 4, 1)
		b.side(p, false, 1, 4)
		b.side(p, true, 1, 4)
	case shFive:
		// "Low" doors are at forward-axis min + 1, "high" at min + 7;
		// facing north or west their heights swap.
		lo, hi := int32(3), int32(5)
		if p.dir == msNorth || p.dir == msWest {
			lo, hi = 5, 3
		}
		b.forward(p, 5, 1)
		if p.leftLow {
			b.side(p, false, lo, 1)
		}
		if p.leftHigh {
			b.side(p, false, hi, 7)
		}
		if p.rightLow {
			b.side(p, true, lo, 1)
		}
		if p.rightHigh {
			b.side(p, true, hi, 7)
		}
	}
}

// shLayout runs one generation attempt from the start chunk with a random
// already seeded; ok is false when no portal room was placed.
func shLayout(r *rng.WorldgenRandom, cx, cz int32, trace func(string)) ([]*shPiece, bool) {
	b := &shBuilder{r: r, list: newShList(), imposed: shKinds, trace: trace}
	for _, w := range b.list {
		b.total += w.weight
	}
	d := msDir(b.intn(4, "dir"))
	x, z := cx<<4+2, cz<<4+2
	start := &shPiece{kind: shStart, dir: d, source: true, box: Box{x, 64, z, x + 4, 74, z + 4}}
	b.start = start
	b.pieces = []*shPiece{start}
	b.children(start)
	for len(b.pending) > 0 {
		i := b.intn(int32(len(b.pending)), "pending")
		p := b.pending[i]
		b.pending = append(b.pending[:i], b.pending[i+1:]...)
		b.children(p)
	}
	return b.pieces, b.portal
}

// shGenerate generates the stronghold start at chunk cx,cz: attempts with the
// large-feature seed of seed+attempt until one has a portal room, then the
// vertical move below sea level.
func shGenerate(seed int64, cx, cz int32, trace func(string)) []*shPiece {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	for attempt := int64(0); ; attempt++ {
		r.SetLargeFeatureSeed(seed+attempt, cx, cz)
		if trace != nil {
			trace("attempt")
		}
		ps, ok := shLayout(r, cx, cz, trace)
		bb := ps[0].box
		for _, p := range ps[1:] {
			bb = bb.Encapsulate(p.box)
		}
		dy := -62 + r.NextIntn(116-(bb.MaxY-bb.MinY+1)) - bb.MinY
		if trace != nil {
			trace(fmt.Sprintf("move %d", dy))
		}
		if !ok {
			continue
		}
		for _, p := range ps {
			p.box.MinY += dy
			p.box.MaxY += dy
		}
		return ps
	}
}

// StrongholdStartForTest returns the stronghold pieces' NBT for a start at
// chunk cx,cz (no ring or biome test).
func StrongholdStartForTest(seed int64, cx, cz int32, trace func(string)) []nbt.Compound {
	ps := shGenerate(seed, cx, cz, trace)
	out := make([]nbt.Compound, len(ps))
	for i, p := range ps {
		out[i] = p.Save()
	}
	return out
}

type strongholdType struct{}

func (strongholdType) Step() int                 { return 4 }
func (strongholdType) terrainAdaptation() string { return "bury" }

// Generate: the layout, then the biome test at the start chunk's min
// corner, y 0 (the probe's stub position).
func (strongholdType) Generate(ctx *GenContext) ([]Piece, error) {
	if ctx.BiomeOK != nil && !ctx.BiomeOK(ctx.ChunkX<<4, 0, ctx.ChunkZ<<4) {
		return nil, nil
	}
	ps := shGenerate(ctx.Seed, ctx.ChunkX, ctx.ChunkZ, nil)
	out := make([]Piece, len(ps))
	for i, p := range ps {
		out[i] = p
	}
	return out, nil
}

func init() {
	for _, id := range shIDs {
		RegisterPiece(id, loadShPiece)
	}
	RegisterType("minecraft:stronghold", func(key string, _ json.RawMessage) (Type, error) {
		if !legacyEnabled[key] {
			return nil, nil
		}
		return strongholdType{}, nil
	})
}
