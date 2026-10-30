package feature

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// sculkPatch is the sculk_patch feature: charge cursors spread sculk and
// sculk veins from the origin. Everything here was fitted to
// kremenmc.probe.feat_ow.Place traces (block reads and writes with their call
// paths, draws, and the cursor list after every update); see
// docs/vanilla-parity.md.
type sculkPatch struct {
	amount, charges, growth, attempts, rounds int
}

// sculkTrace, when set by a test, receives the cursor list after every
// update and every write and draw in the probe's line format.
var sculkTrace io.Writer

// sculkNoCheck skips the origin check, for probe runs that drive the
// spreader directly.
var sculkNoCheck bool

var sculkMoveLimit = 145

type sculkCursor struct {
	pos     Pos
	charge  int
	decay   int
	delay   int
	facings uint8
}

type sculkSpreader struct {
	lv          *Level
	origin      Pos
	replaceable blockSet
	cursors     []*sculkCursor
	buf6        [6]int
	buf18       [18]int
}

var sculkStates struct {
	once                                        sync.Once
	vein                                        [2][64]data.StateID
	veinBlock, sculkBlock, sensor, shrieker     data.BlockID
	sculk, sensorState, shriekerState, catalyst data.StateID
	air, water                                  data.StateID
	replaceable                                 blockSet
	err                                         error
}

func sculkInit(rg *registry) error {
	s := &sculkStates
	s.once.Do(func() {
		reg := data.Canonical()
		base, ok := data.DefaultState("sculk_vein")
		if !ok {
			s.err = fmt.Errorf("sculk_patch: no sculk_vein")
			return
		}
		for w := range 2 {
			for m := range 64 {
				st := base
				for d := range 6 {
					v := "false"
					if m&(1<<d) != 0 {
						v = "true"
					}
					st, _ = reg.With(st, dirOrder[d], v)
				}
				wl := "false"
				if w == 1 {
					wl = "true"
				}
				st, _ = reg.With(st, "waterlogged", wl)
				s.vein[w][m] = st
			}
		}
		s.veinBlock = reg.BlockID(base)
		s.sculk, _ = data.DefaultState("sculk")
		s.sculkBlock = reg.BlockID(s.sculk)
		s.sensorState, _ = data.DefaultState("sculk_sensor")
		s.sensor = reg.BlockID(s.sensorState)
		s.shriekerState, _ = data.DefaultState("sculk_shrieker")
		s.shrieker = reg.BlockID(s.shriekerState)
		s.shriekerState, _ = reg.With(s.shriekerState, "can_summon", "true")
		s.catalyst, _ = data.DefaultState("sculk_catalyst")
		s.air, _ = data.DefaultState("air")
		s.water, _ = data.DefaultState("water")
		s.replaceable, s.err = rg.tag("minecraft:sculk_replaceable_world_gen")
	})
	return s.err
}

func parseSculkPatch(rg *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		Amount   int `json:"amount_per_charge"`
		Charges  int `json:"charge_count"`
		Growth   int `json:"growth_rounds"`
		Attempts int `json:"spread_attempts"`
		Rounds   int `json:"spread_rounds"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("sculk_patch: %w", err)
	}
	if err := sculkInit(rg); err != nil {
		return nil, err
	}
	return sculkPatch{c.Amount, c.Charges, c.Growth, c.Attempts, c.Rounds}, nil
}

func isSculkVein(st data.StateID) bool {
	return data.Canonical().BlockID(st) == sculkStates.veinBlock
}

func isSculkBlock(st data.StateID) bool {
	return data.Canonical().BlockID(st) == sculkStates.sculkBlock
}

// veinMask returns the faces of a sculk vein (bit d for dirOrder[d]).
func veinMask(st data.StateID) uint8 {
	if !isSculkVein(st) {
		return 0
	}
	reg := data.Canonical()
	var m uint8
	for d := range 6 {
		if v, _ := reg.Value(st, dirOrder[d]); v == "true" {
			m |= 1 << d
		}
	}
	return m
}

func veinWater(st data.StateID) int {
	if isSculkVein(st) {
		if v, _ := data.Canonical().Value(st, "waterlogged"); v == "true" {
			return 1
		}
		return 0
	}
	if mfWaterSource(st) {
		return 1
	}
	return 0
}

func (sp *sculkSpreader) trace(format string, a ...any) {
	if sculkTrace != nil {
		fmt.Fprintf(sculkTrace, format, a...)
	}
}

// traceRead logs a cursor-update read like the probe's G lines.
func (sp *sculkSpreader) traceRead(p Pos) {
	if sculkTrace != nil {
		sp.trace("G %d,%d,%d @SculkSpreader$ChargeCursor.update\n", p.X-sp.origin.X, p.Y-sp.origin.Y, p.Z-sp.origin.Z)
	}
}

func (sp *sculkSpreader) set(p Pos, st data.StateID) {
	sp.lv.SetBlock(p, st)
	if sculkTrace == nil {
		return
	}
	sp.trace("W %d,%d,%d %s\n", p.X-sp.origin.X, p.Y-sp.origin.Y, p.Z-sp.origin.Z, data.StateString(st))
}

func (sp *sculkSpreader) nextInt(r *Random, n int) int {
	v := int(r.NextIntn(int32(n)))
	if sculkTrace != nil {
		sp.trace("R nextInt(%d) = %d\n", n, v)
	}
	return v
}

// shuffle returns a shuffled 0..n-1 in a buffer reused by the next call
// with the same n.
func (sp *sculkSpreader) shuffle(r *Random, n int) []int {
	l := sp.buf6[:]
	if n > 6 {
		l = sp.buf18[:]
	}
	l = l[:n]
	for i := range l {
		l[i] = i
	}
	for i := n; i > 1; i-- {
		j := sp.nextInt(r, i)
		l[i-1], l[j] = l[j], l[i-1]
	}
	return l
}

func (f sculkPatch) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if !sculkNoCheck && !sculkCanSpreadFrom(lv, p) {
		return false
	}
	sp := &sculkSpreader{lv: lv, origin: p, replaceable: sculkStates.replaceable}
	for round := range f.rounds + f.growth {
		for range f.charges {
			sp.cursors = append(sp.cursors, &sculkCursor{pos: p, charge: f.amount})
		}
		conv := round < f.rounds
		for j := range f.attempts {
			sp.updateCursors(r, conv)
			if sculkTrace != nil {
				fmt.Fprintf(sculkTrace, "K %d:", j)
				for _, k := range sp.cursors {
					fmt.Fprintf(sculkTrace, " %d,%d,%d/%d/%d/%d", k.pos.X-p.X, k.pos.Y-p.Y, k.pos.Z-p.Z, k.charge, k.decay, k.facings)
				}
				fmt.Fprintln(sculkTrace)
			}
		}
		sp.cursors = sp.cursors[:0]
	}
	return true
}

// sculkCanSpreadFrom: the origin must be air or water with a full block
// beside it.
func sculkCanSpreadFrom(lv *Level, p Pos) bool {
	st := lv.Block(p)
	if isSculkVein(st) || isSculkBlock(st) {
		return true
	}
	if !data.IsAir(st) && !mfWaterBlock(st) {
		return false
	}
	for d := range 6 {
		if data.Flags(lv.Block(p.Add(mfDirOff[d])))&data.FlagFullCube != 0 {
			return true
		}
	}
	return false
}

func (sp *sculkSpreader) updateCursors(r *Random, conv bool) {
	for _, c := range sp.cursors {
		sp.update(c, r, conv)
	}
	out := sp.cursors[:0]
	for _, c := range sp.cursors {
		if c.charge > 0 {
			out = append(out, c)
		}
	}
	for i := len(out); i < len(sp.cursors); i++ {
		sp.cursors[i] = nil
	}
	sp.cursors = out
}

type sculkKind int

const (
	sculkDefault sculkKind = iota
	sculkKindVein
	sculkKindBlock
)

func sculkKindOf(st data.StateID) sculkKind {
	switch {
	case isSculkVein(st):
		return sculkKindVein
	case isSculkBlock(st):
		return sculkKindBlock
	}
	return sculkDefault
}

func (sp *sculkSpreader) update(c *sculkCursor, r *Random, conv bool) {
	if c.charge <= 0 {
		return
	}
	if c.delay > 0 {
		c.delay--
		return
	}
	lv := sp.lv
	st := lv.Block(c.pos)
	sp.traceRead(c.pos)
	k := sculkKindOf(st)
	if conv && sp.attemptSpreadVein(k, c.pos, st, c.facings) {
		st = lv.Block(c.pos)
		sp.traceRead(c.pos)
		k = sculkKindOf(st)
	}
	c.charge = sp.attemptUseCharge(k, c, r, conv)
	if c.charge <= 0 {
		sp.onDischarged(k, st, c.pos)
		return
	}
	next, ok := sp.validMovementPos(c.pos, r)
	if !ok {
		c.charge = 0
		return
	}
	sp.onDischarged(k, st, c.pos)
	c.pos = next
	c.facings = veinMask(lv.Block(next))
	sp.traceRead(next)
	if k == sculkDefault {
		c.decay = max(c.decay-1, 0)
	} else {
		c.decay = 1
	}
	c.delay = 1
}

func (sp *sculkSpreader) attemptSpreadVein(k sculkKind, p Pos, st data.StateID, facings uint8) bool {
	if k == sculkDefault {
		if facings != 0 {
			return sp.regrow(p, st, facings)
		}
		if !veinCanReplace(st) {
			return false
		}
	}
	return sp.spreadAll(st, p) > 0
}

// regrow puts a vein with the given faces at p where they can attach.
func (sp *sculkSpreader) regrow(p Pos, st data.StateID, facings uint8) bool {
	if !veinCanReplace(st) && !isSculkVein(st) {
		return false
	}
	m := veinMask(st)
	n := m
	for d := range 6 {
		if facings&(1<<d) != 0 && sculkCanAttach(sp.lv.Block(p.Add(mfDirOff[d])), d) {
			n |= 1 << d
		}
	}
	if n == m {
		return false
	}
	sp.set(p, sculkStates.vein[veinWater(st)][n])
	return true
}

func sculkCanAttach(st data.StateID, d int) bool {
	return data.Flags(st)&data.FlagFullCube != 0 || faceSturdy(st, mfOpposite(d))
}

func (sp *sculkSpreader) attemptUseCharge(k sculkKind, c *sculkCursor, r *Random, conv bool) int {
	switch k {
	case sculkKindVein:
		if conv && sp.attemptPlaceSculk(c.pos, r) {
			return c.charge - 1
		}
		if sp.nextInt(r, 5) == 0 {
			return c.charge / 2
		}
		return c.charge
	case sculkKindBlock:
		return sp.sculkUseCharge(c, r)
	}
	return c.charge
}

func (sp *sculkSpreader) sculkUseCharge(c *sculkCursor, r *Random) int {
	if sp.nextInt(r, 5) != 0 {
		return c.charge
	}
	d := c.pos.Add(Pos{-sp.origin.X, -sp.origin.Y, -sp.origin.Z})
	up := c.pos.Add(Pos{0, 1, 0})
	a := sp.lv.Block(up)
	// A waterlogged vein above blocks growth (cavemix probes): the water
	// test is on the block.
	if d != (Pos{}) && (data.IsAir(a) || mfWaterBlock(a)) && !sp.growthNearby(c.pos) {
		if sp.nextInt(r, 50) < c.charge {
			st := sculkStates.sensorState
			if sp.nextInt(r, 11) == 0 {
				st = sculkStates.shriekerState
			}
			if mfWaterBlock(a) {
				st, _ = data.Canonical().With(st, "waterlogged", "true")
			}
			sp.set(up, st)
		}
		return max(0, c.charge-50)
	}
	if sp.nextInt(r, 10) == 0 {
		return c.charge - 1
	}
	return c.charge
}

// growthNearby reports three or more sensors or shriekers in x and z
// within 4 of p and y from p to p+2 (probe-fitted).
func (sp *sculkSpreader) growthNearby(p Pos) bool {
	reg := data.Canonical()
	n := 0
	for dy := 0; dy <= 2; dy++ {
		for dz := -4; dz <= 4; dz++ {
			for dx := -4; dx <= 4; dx++ {
				id := reg.BlockID(sp.lv.Block(Pos{p.X + dx, p.Y + dy, p.Z + dz}))
				if id == sculkStates.sensor || id == sculkStates.shrieker {
					n++
					if n >= 3 {
						return true
					}
				}
			}
		}
	}
	return false
}
func (sp *sculkSpreader) attemptPlaceSculk(p Pos, r *Random) bool {
	st := sp.lv.Block(p)
	m := veinMask(st)
	for _, d := range sp.shuffle(r, 6) {
		if m&(1<<d) == 0 {
			continue
		}
		q := p.Add(mfDirOff[d])
		if !sp.replaceable.has(sp.lv.Block(q)) {
			continue
		}
		sp.set(q, sculkStates.sculk)
		sp.spreadAll(sculkStates.sculk, q)
		for e := range 6 {
			if e == mfOpposite(d) {
				continue
			}
			n := q.Add(mfDirOff[e])
			if s := sp.lv.Block(n); isSculkVein(s) {
				sp.onDischarged(sculkKindVein, s, n)
			}
		}
		return true
	}
	return false
}

// onDischarged drops the faces of a vein that rest on sculk.
func (sp *sculkSpreader) onDischarged(k sculkKind, st data.StateID, p Pos) {
	if k != sculkKindVein {
		return
	}
	// The faces come from st, the state read at the start of the update:
	// faces spread onto the block since then are dropped too.
	cur := st
	if !isSculkVein(cur) {
		return
	}
	m := veinMask(cur)
	n := m
	for d := range 6 {
		if m&(1<<d) != 0 && isSculkBlock(sp.lv.Block(p.Add(mfDirOff[d]))) {
			n &^= 1 << d
		}
	}
	w := veinWater(cur)
	if n == 0 {
		if w == 1 {
			sp.set(p, sculkStates.water)
		} else {
			sp.set(p, sculkStates.air)
		}
		return
	}
	sp.set(p, sculkStates.vein[w][n])
}

// sculkMoveOffsets is the 3x3x3 neighbourhood without centre and corners,
// z outermost, then y, then x.
var sculkMoveOffsets = func() []Pos {
	var l []Pos
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if n := abs(x) + abs(y) + abs(z); n == 0 || n == 3 {
					continue
				}
				l = append(l, Pos{x, y, z})
			}
		}
	}
	return l
}()

func (sp *sculkSpreader) validMovementPos(p Pos, r *Random) (Pos, bool) {
	var fb Pos
	have := false
	for _, i := range sp.shuffle(r, len(sculkMoveOffsets)) {
		o := sculkMoveOffsets[i]
		q := p.Add(o)
		if hx, hz := q.X-sp.origin.X, q.Z-sp.origin.Z; hx*hx+hz*hz >= sculkMoveLimit {
			continue
		}
		st := sp.lv.Block(q)
		k := sculkKindOf(st)
		if k == sculkDefault {
			continue
		}
		if abs(o.X)+abs(o.Y)+abs(o.Z) == 2 && !sp.unobstructed(p, o) {
			continue
		}
		// The fallback is the last sculk or vein candidate, with or
		// without substrate access (cavezoo probes: a last vein facing
		// air or stairs is taken).
		if k == sculkKindVein && sp.hasSubstrateAccess(st, q) {
			return q, true
		}
		fb, have = q, true
	}
	return fb, have
}

// unobstructed: an edge move needs one intermediate block (x, y or z step)
// whose face toward p is not sturdy (cavemix probes: stairs backs and
// bottom slabs from above obstruct).
func (sp *sculkSpreader) unobstructed(p Pos, o Pos) bool {
	for _, c := range [3]Pos{{o.X, 0, 0}, {0, o.Y, 0}, {0, 0, o.Z}} {
		if c == (Pos{}) {
			continue
		}
		d := 0
		for i, v := range mfDirOff {
			if v == c {
				d = i
			}
		}
		if !sculkBlocksCorner(sp.lv.Block(p.Add(c)), mfOpposite(d)) {
			return true
		}
	}
	return false
}

func (sp *sculkSpreader) hasSubstrateAccess(st data.StateID, p Pos) bool {
	m := veinMask(st)
	for d := range 6 {
		if m&(1<<d) != 0 && sp.replaceable.has(sp.lv.Block(p.Add(mfDirOff[d]))) {
			return true
		}
	}
	return false
}

// spreadAll spreads src's faces at p one step in every direction (same
// block, same plane, around the edge) and returns how many faces it placed.
// A vein spreads from its own faces, any other block from all six; air only
// into its own block.
func (sp *sculkSpreader) spreadAll(src data.StateID, p Pos) int {
	srcMask := veinMask(src)
	isVein := isSculkVein(src)
	airLike := !isVein && (data.IsAir(src) || mfWaterSource(src))
	n := 0
	for from := range 6 {
		if isVein && srcMask&(1<<from) == 0 {
			continue
		}
		hasFrom := !airLike && (!isVein || srcMask&(1<<from) != 0)
		for to := range 6 {
			if to>>1 == from>>1 || srcMask&(1<<to) != 0 {
				continue
			}
			if sp.spreadFromFaceToward(p, from, to, hasFrom) {
				n++
			}
		}
	}
	return n
}

func (sp *sculkSpreader) spreadFromFaceToward(p Pos, from, to int, hasFrom bool) bool {
	type cand struct {
		pos    Pos
		face   int
		corner bool
	}
	cs := [3]cand{
		{p, to, false},
		{p.Add(mfDirOff[to]), from, false},
		{p.Add(mfDirOff[to]).Add(mfDirOff[from]), mfOpposite(to), true},
	}
	nc := 3
	if !hasFrom {
		nc = 1
	}
	for _, c := range cs[:nc] {
		st := sp.lv.Block(c.pos)
		if !sp.canSpreadInto(st, c.pos, c.face) {
			continue
		}
		if c.corner && sculkBlocksCorner(sp.lv.Block(p.Add(mfDirOff[to])), mfOpposite(to)) {
			continue
		}
		sp.set(c.pos, sculkStates.vein[veinWater(st)][veinMask(st)|1<<c.face])
		return true
	}
	return false
}

// sculkBlocksCorner: a wrap-around spread or an edge move is blocked by an
// intermediate block whose face toward the source is sturdy (stone, bottom
// slabs from above, the back of stairs; not leaves; cavemix/cavezoo probes).
func sculkBlocksCorner(st data.StateID, face int) bool {
	return faceSturdy(st, face)
}

func (sp *sculkSpreader) canSpreadInto(st data.StateID, p Pos, face int) bool {
	if isSculkVein(st) {
		if veinMask(st)&(1<<face) != 0 {
			return false
		}
	} else if !veinCanReplace(st) {
		return false
	}
	a := sp.lv.Block(p.Add(mfDirOff[face]))
	if isSculkBlock(a) {
		return false
	}
	return sculkCanAttach(a, face)
}

func init() {
	featureTypes["sculk_patch"] = parseSculkPatch
}
