package server

import (
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestExplosionRef replays testdata/explosion_scenes.txt against the golden
// reference output (regenerate with KREMENMC_REF=1): random call sequence,
// destroyed blocks, entity damage and knockback, primed TNT, drops, the
// explode packet, TNT motion, fire placement and igniter use.
func TestExplosionRef(t *testing.T) {
	scenes, err := filepath.Abs(filepath.Join("testdata", "explosion_scenes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(reftest.Golden(t, "explosion_ref.txt", "kremenmc.probe.explosion.Explode", scenes))
	var ops []exRefOp
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "worldseed "):
			exWorldSeed, _ = strconv.ParseInt(l[10:], 10, 64)
		case strings.HasPrefix(l, "op "):
			ops = append(ops, exRefOp{line: l[3:]})
		case strings.HasPrefix(l, "error"):
			t.Fatalf("reference: %s", l)
		case len(ops) > 0 && l != "" && !strings.HasPrefix(l, "warn"):
			ops[len(ops)-1].out = append(ops[len(ops)-1].out, l)
		}
	}
	sc := newExScene()
	checks := map[string]int{}
	for _, op := range ops {
		f := strings.Fields(op.line)
		ok := true
		switch f[0] {
		case "scene":
			sc = newExScene()
			sc.rules = exSceneRules(sc, ops, op)
			sc.name = f[1]
		case "cmd":
			if len(f) == 4 && f[1] == "gamerule" {
				exRules[f[2]] = f[3]
			}
		case "mob", "player":
			sc.addEnt(f)
		case "seen":
			sc.load(op.out)
			c := exVec(f[1:4])
			for _, l := range op.out {
				if g := strings.Fields(l); g[0] == "seen" {
					k, _ := strconv.Atoi(g[1])
					want := exPf32(g[2])
					if got := seenPercent(sc, c, sc.ents[k].exBox()); got != want {
						t.Errorf("%s: seen %d = %v, want %v", sc.name, k, got, want)
						ok = false
					}
				}
			}
			checks["seen"]++
		case "seenrand":
			sc.load(op.out)
			bad := 0
			for _, l := range op.out {
				g := strings.Fields(l)
				if g[0] != "sr" {
					continue
				}
				c := exVec(g[1:4])
				b := entity.AABB{MinX: exPf(g[4]), MinY: exPf(g[5]), MinZ: exPf(g[6]), MaxX: exPf(g[7]), MaxY: exPf(g[8]), MaxZ: exPf(g[9])}
				if got, want := seenPercent(sc, c, b), exPf32(g[10]); got != want {
					if bad < 5 {
						t.Errorf("seenrand: %s: got %v", l, got)
					}
					bad++
				}
				checks["seenrand"]++
			}
			ok = bad == 0
		case "firestate":
			sc.load(op.out)
			p := exBpos(f[1:4])
			for _, l := range op.out {
				g := strings.Fields(l)
				if g[0] != "fire" {
					continue
				}
				got := fmt.Sprintf("fire %d %d %d %s", p.X, p.Y, p.Z, data.StateString(fireStateAt(sc, p)))
				pl := "0"
				if canPlaceFire(sc, p) {
					pl = "1"
				}
				got += strings.Repeat(" "+pl, 4)
				if got != l {
					t.Errorf("firestate: got %s\nwant %s", got, l)
					ok = false
				}
			}
			checks["firestate"]++
		case "explode", "tntprime", "tnttick", "use":
			sc.load(op.out)
			got := sc.run(f)
			want := exFilterOut(op.out)
			if d := exDiffLines(got, want); d != "" {
				t.Errorf("%s: %s:\n%s", sc.name, op.line, d)
				ok = false
			}
			checks[f[0]]++
		case "tntmeta":
			fuse, _ := strconv.Atoi(f[1])
			got := "-"
			if m := tntMeta(int32(fuse)); len(m) > 1 {
				got = hex.EncodeToString(m)
			}
			if want := strings.Fields(op.out[0])[2]; got != want {
				t.Errorf("tntmeta %d: %s, want %s", fuse, got, want)
				ok = false
			}
			checks["tntmeta"]++
		}
		if !ok && testing.Verbose() {
			t.Logf("failed op %q in scene %s", op.line, sc.name)
		}
	}
	t.Logf("checked %v", checks)
}

type exRefOp struct {
	line string
	out  []string
}

var exRules = map[string]string{}

func exSceneRules(*exScene, []exRefOp, exRefOp) map[string]string { return exRules }

func exFilterOut(l []string) []string {
	var out []string
	for _, s := range l {
		if strings.HasPrefix(s, "pre ") || strings.HasPrefix(s, "gm ") || strings.HasPrefix(s, "upk") {
			continue
		}
		// Neighbour shape updates (fences reconnecting after a neighbour is
		// destroyed) are block update behaviour, not simulated here.
		if strings.HasPrefix(s, "block ") && (strings.Contains(s, "_fence[") || strings.Contains(s, "_stairs[") || strings.Contains(s, "_pane[")) {
			continue
		}
		if strings.HasPrefix(s, "used Success") {
			s = "used * held" + s[strings.Index(s, "] held")+6:]
		}
		out = append(out, s)
	}
	return out
}

func exDiffLines(got, want []string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < max(len(got), len(want)); i++ {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if !exSameLine(g, w, strings.HasPrefix(w, "new tnt")) {
			fmt.Fprintf(&b, "  got  %s\n  want %s\n", exTrunc(g), exTrunc(w))
			if n++; n >= 6 {
				break
			}
		}
	}
	return b.String()
}

func exTrunc(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// exSameLine compares field by field, numbers by value. With ulp, floats
// may differ in the last bit: the reference JVM's Math.sin intrinsic is
// not fdlibm (StrictMath), which tnt_trig.go reproduces exactly.
func exSameLine(a, b string, ulp bool) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	if len(fa) != len(fb) {
		return false
	}
	for i := range fa {
		if fa[i] == fb[i] || fa[i] == "*" || fb[i] == "*" {
			continue
		}
		x, e1 := strconv.ParseFloat(fa[i], 64)
		y, e2 := strconv.ParseFloat(fb[i], 64)
		if e1 != nil || e2 != nil {
			return false
		}
		if x != y && !(ulp && (math.Nextafter(x, y) == y || math.Nextafter(math.Nextafter(x, y), y) == y)) {
			return false
		}
	}
	return true
}

func exPf(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return v
}

func exPf32(s string) float32 {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		panic(err)
	}
	return float32(v)
}

func exVec(f []string) api.Vec3 { return api.Vec3{X: exPf(f[0]), Y: exPf(f[1]), Z: exPf(f[2])} }

func exBpos(f []string) api.BlockPos {
	x, _ := strconv.Atoi(f[0])
	y, _ := strconv.Atoi(f[1])
	z, _ := strconv.Atoi(f[2])
	return api.BlockPos{X: x, Y: y, Z: z}
}

func exJd(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// exScene is a block map and entity list implementing exLevel.
type exScene struct {
	name   string
	blocks map[api.BlockPos]data.StateID
	before map[api.BlockPos]data.StateID
	ents   []*exFakeEnt
	fresh  []*exFakeEnt
	rules  map[string]string
	ex     *explosion
	rnd    *exRecRandom
	sounds int
}

func newExScene() *exScene {
	return &exScene{blocks: map[api.BlockPos]data.StateID{}, rules: exRules}
}

func (s *exScene) rule(id string, def bool) bool {
	if v, ok := s.rules[id]; ok {
		return v != "false"
	}
	return def
}

// load replaces the blocks with the op's "pre" dump.
func (s *exScene) load(out []string) {
	s.blocks = map[api.BlockPos]data.StateID{}
	for _, l := range out {
		f := strings.Fields(l)
		if f[0] != "pre" {
			continue
		}
		p := exBpos(f[1:4])
		z1, _ := strconv.Atoi(f[4])
		st, ok := data.ParseState(f[5])
		if !ok {
			panic("bad state " + f[5])
		}
		for z := p.Z; z <= z1; z++ {
			s.blocks[api.BlockPos{X: p.X, Y: p.Y, Z: z}] = st
		}
	}
	s.before = exCopyBlocks(s.blocks)
}

func exCopyBlocks(m map[api.BlockPos]data.StateID) map[api.BlockPos]data.StateID {
	o := make(map[api.BlockPos]data.StateID, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}

func (s *exScene) Block(x, y, z int) data.StateID          { return s.blocks[api.BlockPos{X: x, Y: y, Z: z}] }
func (s *exScene) worldBounds() (int, int)                 { return -64, 320 }
func (s *exScene) setBlock(p api.BlockPos, v data.StateID) { s.blocks[p] = v }

func (s *exScene) targets(box entity.AABB) []exTarget {
	var out []exTarget
	for _, e := range s.ents {
		if !e.removed && e.exBox().Intersects(box) {
			out = append(out, e)
		}
	}
	return out
}

func (s *exScene) primeExploded(p api.BlockPos, ex *explosion) {
	if !s.rule("tnt_explodes", true) {
		return
	}
	pos, vel := primedSpawn(p, ex.rnd)
	s.fresh = append(s.fresh, &exFakeEnt{typ: "tnt", pos: pos, vel: vel, fuse: shortFuse(tntDefaultFuse, ex.rnd)})
}

// exWorldSeed and exSeqs are the reference world's seed and loot random
// sequences, which persist across scenes like in the probe's world.
var (
	exWorldSeed int64
	exSeqs      = map[string]rng.Source{}
)

func (s *exScene) blockDrops(p api.BlockPos, st data.StateID, ex *explosion) []item.Stack {
	t := loot.Get(loot.BlockTable(data.BlockOf(st)))
	if t == nil {
		return nil
	}
	ctx := &loot.Context{
		Origin: api.Vec3{X: float64(p.X) + 0.5, Y: float64(p.Y) + 0.5, Z: float64(p.Z) + 0.5}, HasOrigin: true,
		State: st, HasState: true, HasTool: true, World: s, Rand: ex.rnd,
	}
	if t.Sequence != "" {
		if exSeqs[t.Sequence] == nil {
			exSeqs[t.Sequence] = loot.SequenceSource(exWorldSeed, t.Sequence)
		}
		ctx.Rand = exSeqs[t.Sequence]
	}
	if ex.interaction == exDestroyWithDecay {
		ctx.ExplosionRadius, ctx.HasExplosion = ex.radius, true
	}
	return t.Roll(ctx)
}

func (s *exScene) popResource(p api.BlockPos, st item.Stack) {
	at := exPopOffset(p, s.ex.rnd)
	if st.IsEmpty() || !s.rule("block_drops", true) {
		return
	}
	s.fresh = append(s.fresh, &exFakeEnt{typ: "item", pos: at, item: st})
}

func (s *exScene) primeTNT(p api.BlockPos) bool {
	if !s.rule("tnt_explodes", true) {
		return false
	}
	pos, vel := primedSpawn(p, s.rnd)
	s.fresh = append(s.fresh, &exFakeEnt{typ: "tnt", pos: pos, vel: vel, fuse: tntDefaultFuse})
	return true
}

func (s *exScene) playSound(api.Vec3, string, float32) { s.sounds++ }

func (s *exScene) addEnt(f []string) {
	e := &exFakeEnt{}
	if f[0] == "player" {
		e.typ, e.player, e.gm = "player", true, "survival"
		if len(f) > 4 {
			e.gm = f[4]
		}
		e.pos = exVec(f[1:4])
	} else {
		e.typ = f[1]
		e.pos = exVec(f[2:5])
	}
	t, _ := data.Canonical().EntityByName(e.typ)
	e.w, e.h = float64(t.Width), float64(t.Height)
	if p, ok := entity.PhysicsOf(e.typ); ok {
		e.eye = float64(float32(p.Eye))
	}
	if e.player {
		e.eye = float64(float32(1.62))
	}
	e.living = entity.HasAttributes(e.typ)
	e.hp = 20
	if a := entity.NewAttrs(e.typ); a != nil {
		e.hp = float32(a.Value(entity.AttrMaxHealth))
	}
	if len(f) > 6 && f[5] == "hp" {
		e.hp = exPf32(f[6])
	}
	if e.typ == "tnt" {
		e.fuse = tntDefaultFuse
	}
	s.ents = append(s.ents, e)
}

func (s *exScene) run(f []string) []string {
	var out []string
	s.fresh = nil
	seed, _ := strconv.ParseInt(f[len(f)-1], 10, 64)
	switch f[0] {
	case "explode":
		seed, _ = strconv.ParseInt(f[7], 10, 64)
		s.rnd = newExRecRandom(seed)
		var direct api.Entity
		if len(f) > 8 && f[8] == "tnt" {
			direct = exAPIEntity{}
		}
		s.explode(exVec(f[1:4]), exPf32(f[4]), f[6] == "1", f[5], direct)
	case "tntprime":
		s.rnd = newExRecRandom(seed)
		out = append(out, fmt.Sprintf("primed %v", s.primeTNT(exBpos(f[1:4]))))
	case "tnttick":
		s.rnd = newExRecRandom(seed)
		fuse, _ := strconv.Atoi(f[1])
		n, _ := strconv.Atoi(f[8])
		tt, _ := data.Canonical().EntityByName("tnt")
		e := entity.New(tt)
		e.Pos, e.Prev = exVec(f[2:5]), exVec(f[2:5])
		e.Vel = exVec(f[5:8])
		for k := 0; k < n; k++ {
			e.Tick(s)
			fuse--
			out = append(out, fmt.Sprintf("t %d %s %s %s %s %s %s %v %d", k, exJd(e.Pos.X), exJd(e.Pos.Y), exJd(e.Pos.Z), exJd(e.Vel.X), exJd(e.Vel.Y), exJd(e.Vel.Z), e.OnGround, fuse))
			if fuse <= 0 {
				if s.rule("tnt_explodes", true) {
					s.explode(api.Vec3{X: e.Pos.X, Y: e.Pos.Y + tntFuseOffsetDY*e.Height, Z: e.Pos.Z}, tntPower, false, "TNT", exAPIEntity{})
				}
				break
			}
		}
	case "use":
		s.rnd = newExRecRandom(seed)
		kind, _ := igniterOf("minecraft:" + f[1])
		face := map[string]api.BlockFace{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}[f[5]]
		res := igniteOn(s, kind, exBpos(f[2:5]), face, f[6] == "1", s.rnd)
		count, damage := 2, 0
		switch res {
		case igniteLit, ignitePrimed:
			if kind == igniteFlint {
				damage = 1
			} else {
				count = 1
			}
		}
		verdict := "Fail[]"
		if res == igniteLit || res == ignitePrimed {
			verdict = "*"
		}
		out = append(out, fmt.Sprintf("used %s held %s %d damage %d", verdict, f[1], count, damage))
		// The fake player itself shows up as a new entity.
		pl := &exFakeEnt{typ: "player", hp: 20}
		pl.pos = api.Vec3{X: math.NaN()}
		s.fresh = append([]*exFakeEnt{pl}, s.fresh...)
	}
	out = append(out, "rand "+s.rnd.String())
	out = append(out, s.blockLines()...)
	for k, e := range s.ents {
		out = append(out, fmt.Sprintf("ent %d %s", k, e.line()))
	}
	for _, e := range s.fresh {
		out = append(out, "new "+e.line())
	}
	if s.ex != nil {
		k := -1
		for _, e := range s.ents {
			if !e.player {
				continue
			}
			k++
			if e.pos.DistanceSq(s.ex.center) >= 4096 {
				continue
			}
			if e.hurt && e.gm == "survival" {
				out = append(out, fmt.Sprintf("pkt %d ClientboundDamageEventPacket", k))
				if e.hp <= 0 {
					out = append(out, fmt.Sprintf("pkt %d ClientboundPlayerCombatKillPacket", k), fmt.Sprintf("pkt %d ClientboundEntityEventPacket", k))
				}
			}
			pk := &packets.ClientboundExplode{
				Center: packets.Vec3f64{X: s.ex.center.X, Y: s.ex.center.Y, Z: s.ex.center.Z},
				Radius: s.ex.radius, BlockCount: int32(s.ex.blocks), Raw: explodeTail(s.ex.small()),
			}
			if v, ok := s.ex.hitPlayers[e]; ok {
				pk.PlayerKnockback = proto.Opt[packets.Vec3f64]{Ok: true, V: packets.Vec3f64{X: v.X, Y: v.Y, Z: v.Z}}
			}
			var w proto.Writer
			pk.Encode(&w)
			out = append(out, fmt.Sprintf("pkt %d explode %s", k, hex.EncodeToString(w.B)))
		}
		s.ex = nil
	}
	return out
}

func (s *exScene) explode(c api.Vec3, power float32, fire bool, kind string, direct api.Entity) {
	k := map[string]explosionKind{"NONE": explodeNone, "BLOCK": explodeBlock, "MOB": explodeMob, "TNT": explodeTNT, "TRIGGER": explodeTrigger}[kind]
	inter := exKeep
	decay := func(r string, def bool) blockInteraction {
		if s.rule(r, def) {
			return exDestroyWithDecay
		}
		return exDestroy
	}
	switch k {
	case explodeBlock:
		inter = decay("block_explosion_drop_decay", true)
	case explodeMob:
		if s.rule("mob_griefing", true) {
			inter = decay("mob_explosion_drop_decay", true)
		}
	case explodeTNT:
		inter = decay("tnt_explosion_drop_decay", false)
	case explodeTrigger:
		inter = exTriggerBlock
	}
	s.ex = &explosion{center: c, radius: power, fire: fire, interaction: inter, direct: direct, rnd: s.rnd}
	s.ex.run(s)
}

func (s *exScene) blockLines() []string {
	var keys []api.BlockPos
	for p, v := range s.blocks {
		if s.before[p] != v {
			keys = append(keys, p)
		}
	}
	for p := range s.before {
		if _, ok := s.blocks[p]; !ok && s.before[p] != data.Air {
			keys = append(keys, p)
		}
	}
	slices.SortFunc(keys, func(a, b api.BlockPos) int {
		if a.X != b.X {
			return a.X - b.X
		}
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.Z - b.Z
	})
	keys = slices.Compact(keys)
	var out []string
	for _, p := range keys {
		out = append(out, fmt.Sprintf("block %d %d %d %s", p.X, p.Y, p.Z, data.StateString(s.blocks[p])))
	}
	return out
}

type exAPIEntity struct{ api.Entity }

type exFakeEnt struct {
	typ          string
	pos, vel     api.Vec3
	w, h, eye    float64
	hp           float32
	living       bool
	player       bool
	gm           string
	fuse         int32
	item         item.Stack
	removed      bool
	hurt         bool
	hpUnchecked  bool
	dropUnchecks bool
}

func (e *exFakeEnt) line() string {
	if math.IsNaN(e.pos.X) {
		return "player * * * * * * hp *"
	}
	s := fmt.Sprintf("%s %s %s %s ", e.typ, exJd(e.pos.X), exJd(e.pos.Y), exJd(e.pos.Z))
	if e.typ == "item" && e.vel == (api.Vec3{}) && !e.removed {
		s += "* 0.2 *"
	} else {
		s += fmt.Sprintf("%s %s %s", exJd(e.vel.X), exJd(e.vel.Y), exJd(e.vel.Z))
	}
	if e.living {
		if e.typ == "zombie" || e.typ == "armor_stand" {
			s += " hp *"
		} else {
			s += " hp " + strconv.FormatFloat(float64(e.hp), 'g', -1, 32)
		}
	}
	if e.typ == "item" {
		if e.removed {
			s += " item air 0"
		} else {
			s += fmt.Sprintf(" item %s %d", strings.TrimPrefix(item.NameOf(e.item.Item), "minecraft:"), e.item.Count)
		}
	}
	if e.typ == "tnt" {
		s += fmt.Sprintf(" fuse %d", e.fuse)
	}
	if e.removed {
		s += " removed"
	}
	return s
}

func (e *exFakeEnt) exPos() api.Vec3 { return e.pos }
func (e *exFakeEnt) exAim() api.Vec3 {
	if e.typ == "tnt" {
		return e.pos
	}
	return api.Vec3{X: e.pos.X, Y: e.pos.Y + e.eye, Z: e.pos.Z}
}
func (e *exFakeEnt) exBox() entity.AABB { return entity.BoxAt(e.pos.X, e.pos.Y, e.pos.Z, e.w, e.h) }
func (e *exFakeEnt) exImmune() bool     { return e.removed || e.gm == "spectator" }
func (e *exFakeEnt) exHurt(_ *explosion, amount float32) {
	switch {
	case e.typ == "armor_stand" || e.typ == "item":
		e.removed = true
	case e.living && e.gm != "creative":
		e.hp = max(0, e.hp-amount)
		e.hurt = true
	}
}
func (e *exFakeEnt) exPush(v api.Vec3) {
	e.vel = api.Vec3{X: e.vel.X + v.X, Y: e.vel.Y + v.Y, Z: e.vel.Z + v.Z}
}
func (e *exFakeEnt) exKnockbackResistance() (float64, bool) { return 0, e.living }
func (e *exFakeEnt) exRecordsKnockback() bool               { return e.player && e.gm != "spectator" }

// exRecRandom records calls in the probe's compressed token form.
type exRecRandom struct {
	*rng.Legacy
	toks []string
}

func newExRecRandom(seed int64) *exRecRandom { return &exRecRandom{Legacy: rng.NewLegacy(seed)} }

func (r *exRecRandom) NextFloat() float32 { r.toks = append(r.toks, "F"); return r.Legacy.NextFloat() }
func (r *exRecRandom) NextDouble() float64 {
	r.toks = append(r.toks, "D")
	return r.Legacy.NextDouble()
}
func (r *exRecRandom) NextIntn(n int32) int32 {
	v := r.Legacy.NextIntn(n)
	r.toks = append(r.toks, fmt.Sprintf("I%d=%d", n, v))
	return v
}
func (r *exRecRandom) NextInt() int32 {
	v := r.Legacy.NextInt()
	r.toks = append(r.toks, fmt.Sprintf("nextInt_=_%d", v))
	return v
}
func (r *exRecRandom) NextIntRange(a, b int32) int32 {
	v := r.Legacy.NextIntRange(a, b)
	r.toks = append(r.toks, fmt.Sprintf("nextIntRange_%d_%d_=_%d", a, b, v))
	return v
}
func (r *exRecRandom) NextIntBetweenInclusive(a, b int32) int32 {
	v := r.Legacy.NextIntBetweenInclusive(a, b)
	r.toks = append(r.toks, fmt.Sprintf("nextIntBetweenInclusive_%d_%d_=_%d", a, b, v))
	return v
}
func (r *exRecRandom) NextBool() bool {
	v := r.Legacy.NextBool()
	r.toks = append(r.toks, fmt.Sprintf("nextBool_=_%v", v))
	return v
}

func (r *exRecRandom) String() string {
	var b strings.Builder
	for i := 0; i < len(r.toks); {
		j := i
		for j < len(r.toks) && r.toks[j] == r.toks[i] {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(r.toks[i])
		if j-i > 1 {
			fmt.Fprintf(&b, "*%d", j-i)
		}
		i = j
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}
