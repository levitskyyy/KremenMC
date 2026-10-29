package spawn

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var (
	testDataOnce sync.Once
	testData     *Data
	testDataErr  error
)

func loadTestData(t testing.TB) *Data {
	t.Helper()
	testDataOnce.Do(func() {
		p, err := wgdata.Load()
		if err != nil {
			testDataErr = err
			return
		}
		testData, testDataErr = FromPack(p)
	})
	if testDataErr != nil {
		t.Fatal(testDataErr)
	}
	return testData
}

// script replays a reference random/marker log. Sources created from it
// return the recorded results while the Go calls match the recorded calls;
// the first mismatch is remembered and later calls return zero values.
type script struct {
	java    []string
	pos     int
	got     []string
	diverge int // index into got of the first mismatch, -1 if none
}

func newScript(lines []string) *script { return &script{java: lines, diverge: -1} }

func (s *script) fail(line string) {
	if s.diverge < 0 {
		s.diverge = len(s.got)
	}
	s.got = append(s.got, line+"   <-- MISMATCH")
}

// call matches "r <tag> <call>" against the next recorded line and returns
// the recorded result text.
func (s *script) call(tag, call string) string {
	line := "r " + tag + " " + call
	if s.diverge >= 0 {
		s.got = append(s.got, line)
		if len(s.got) > s.diverge+8 {
			panic(errDiverged)
		}
		return ""
	}
	if s.pos < len(s.java) {
		j := s.java[s.pos]
		if j == line {
			s.pos++
			s.got = append(s.got, j)
			return ""
		}
		if pre, res, ok := strings.Cut(j, " = "); ok && pre == line {
			s.pos++
			s.got = append(s.got, j)
			return res
		}
	}
	s.fail(line)
	return ""
}

func (s *script) mark(line string) {
	if s.diverge >= 0 {
		s.got = append(s.got, line)
		return
	}
	if s.pos < len(s.java) && s.java[s.pos] == line {
		s.pos++
		s.got = append(s.got, line)
		return
	}
	s.fail(line)
}

// skipTo consumes recorded lines up to (not including) the next line with
// the prefix; used to step over unverified finalizeSpawn draws.
func (s *script) skipTo(prefix string) int {
	if s.diverge >= 0 {
		return 0
	}
	n := 0
	for s.pos < len(s.java) && !strings.HasPrefix(s.java[s.pos], prefix) {
		s.got = append(s.got, s.java[s.pos]+"   (skipped)")
		s.pos++
		n++
	}
	return n
}

func (s *script) check(t *testing.T, name string) {
	t.Helper()
	if s.diverge < 0 && s.pos == len(s.java) {
		return
	}
	var b strings.Builder
	at := s.diverge
	if at < 0 {
		at = len(s.got)
	}
	lo := max(0, at-12)
	fmt.Fprintf(&b, "%s: Go diverges from reference at Go line %d (reference line %d of %d)\n", name, at, s.pos, len(s.java))
	for i := lo; i < len(s.got) && i < at+3; i++ {
		fmt.Fprintf(&b, "  go:  %s\n", s.got[i])
	}
	for i := max(0, s.pos-3); i < len(s.java) && i < s.pos+4; i++ {
		fmt.Fprintf(&b, "  ref: %s\n", s.java[i])
	}
	t.Error(b.String())
}

// errDiverged aborts a replay a few calls after the first mismatch so
// draw-driven loops cannot spin on zero results.
var errDiverged = fmt.Errorf("replay diverged")

type scriptSource struct {
	s   *script
	tag string
}

func (r *scriptSource) atoi(res string) int32 {
	v, _ := strconv.ParseInt(res, 10, 64)
	return int32(v)
}

func fhex(v float32) string { return "f" + strconv.FormatUint(uint64(math.Float32bits(v)), 16) }
func dhex(v float64) string { return "d" + strconv.FormatUint(math.Float64bits(v), 16) }

func parseF(res string) float32 { f, _ := hexFloat(res); return f }
func parseD(res string) float64 { d, _ := hexDouble(res); return d }

func (r *scriptSource) Fork() rng.Source               { r.s.call(r.tag, "fork"); return r }
func (r *scriptSource) ForkPositional() rng.Positional { r.s.call(r.tag, "forkPositional"); return nil }
func (r *scriptSource) SetSeed(seed int64)             { r.s.call(r.tag, fmt.Sprintf("setSeed %d", seed)) }
func (r *scriptSource) NextInt() int32                 { return r.atoi(r.s.call(r.tag, "nextInt")) }
func (r *scriptSource) NextIntn(b int32) int32 {
	return r.atoi(r.s.call(r.tag, fmt.Sprintf("nextIntn %d", b)))
}
func (r *scriptSource) NextIntRange(lo, hi int32) int32 {
	return r.atoi(r.s.call(r.tag, fmt.Sprintf("nextIntRange %d %d", lo, hi)))
}
func (r *scriptSource) NextIntBetweenInclusive(lo, hi int32) int32 {
	return r.atoi(r.s.call(r.tag, fmt.Sprintf("nextIntBetweenInclusive %d %d", lo, hi)))
}
func (r *scriptSource) NextLong() int64 {
	v, _ := strconv.ParseInt(r.s.call(r.tag, "nextLong"), 10, 64)
	return v
}
func (r *scriptSource) NextBool() bool        { return r.s.call(r.tag, "nextBool") == "true" }
func (r *scriptSource) NextFloat() float32    { return parseF(r.s.call(r.tag, "nextFloat")) }
func (r *scriptSource) NextDouble() float64   { return parseD(r.s.call(r.tag, "nextDouble")) }
func (r *scriptSource) NextGaussian() float64 { return parseD(r.s.call(r.tag, "nextGaussian")) }
func (r *scriptSource) Triangle(m, d float64) float64 {
	return parseD(r.s.call(r.tag, "triangle "+dhex(m)+" "+dhex(d)))
}
func (r *scriptSource) TriangleF(m, d float32) float32 {
	return parseF(r.s.call(r.tag, "triangleF "+fhex(m)+" "+fhex(d)))
}
func (r *scriptSource) Consume(n int) { r.s.call(r.tag, fmt.Sprintf("consume %d", n)) }

// sceneOut is a parsed Scene probe output.
type sceneOut struct {
	x0, y0, z0, x1, y1, z1 int
	blocks                 []data.StateID
	sky, blockLight, local []uint8
	biomes                 []string
	heights                [4][]int
	ticking                []bool
	env                    map[string]string
	players                []Player
	entities               []AABB
	existing               []Entity
	ops                    []sceneOp
	warn                   []string
}

type sceneOp struct {
	args  []string
	lines []string
}

func rleInts(fields []string, n int) ([]int, error) {
	out := make([]int, 0, n)
	for _, f := range fields {
		v, c, ok := strings.Cut(f, "*")
		if !ok {
			return nil, fmt.Errorf("rle %q", f)
		}
		vi, _ := strconv.Atoi(v)
		ci, _ := strconv.Atoi(c)
		for range ci {
			out = append(out, vi)
		}
	}
	if len(out) != n {
		return nil, fmt.Errorf("rle length %d want %d", len(out), n)
	}
	return out, nil
}

func parseScene(b []byte) (*sceneOut, error) {
	sc := &sceneOut{env: map[string]string{}}
	var palette []data.StateID
	var bpal []string
	var cur *sceneOp
	n := 0
	reg := data.Canonical()
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		if cur != nil {
			if line == "endop" {
				sc.ops = append(sc.ops, *cur)
				cur = nil
			} else {
				cur.lines = append(cur.lines, line)
			}
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "warn":
			sc.warn = append(sc.warn, line)
		case "dump":
			v := make([]int, 6)
			for i := range v {
				v[i], _ = strconv.Atoi(f[i+1])
			}
			sc.x0, sc.y0, sc.z0, sc.x1, sc.y1, sc.z1 = v[0], v[1], v[2], v[3], v[4], v[5]
			n = (sc.x1 - sc.x0 + 1) * (sc.y1 - sc.y0 + 1) * (sc.z1 - sc.z0 + 1)
		case "palette":
			s, ok := reg.ParseState(f[2])
			if !ok {
				return nil, fmt.Errorf("state %q", f[2])
			}
			if name, _, _ := strings.Cut(f[2], "["); BlockName(s) != name {
				return nil, fmt.Errorf("state %q maps to %s", f[2], BlockName(s))
			}
			palette = append(palette, s)
		case "biomepalette":
			bpal = append(bpal, f[2])
		case "blocks", "sky", "blocklight", "maxlocal", "biomes":
			v, err := rleInts(f[1:], n)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f[0], err)
			}
			switch f[0] {
			case "blocks":
				sc.blocks = make([]data.StateID, n)
				for i, x := range v {
					sc.blocks[i] = palette[x]
				}
			case "biomes":
				sc.biomes = make([]string, n)
				for i, x := range v {
					sc.biomes[i] = bpal[x]
				}
			default:
				u := make([]uint8, n)
				for i, x := range v {
					u[i] = uint8(x)
				}
				switch f[0] {
				case "sky":
					sc.sky = u
				case "blocklight":
					sc.blockLight = u
				default:
					sc.local = u
				}
			}
		case "heightmap":
			nx, nz := sc.x1-sc.x0+1, sc.z1-sc.z0+1
			v, err := rleInts(f[2:], nx*nz)
			if err != nil {
				return nil, err
			}
			for i, nm := range heightmapNames {
				if nm == f[1] {
					sc.heights[i] = v
				}
			}
		case "ticking":
			for _, x := range f[1:] {
				sc.ticking = append(sc.ticking, x == "1")
			}
		case "env":
			for k, v := range kv(f[1:]) {
				sc.env[k] = v
			}
		case "player":
			x, _ := hexDouble(f[1])
			y, _ := hexDouble(f[2])
			z, _ := hexDouble(f[3])
			sc.players = append(sc.players, Player{X: x, Y: y, Z: z})
		case "entity":
			et, _ := EntityByName(f[1])
			x, _ := hexDouble(f[2])
			y, _ := hexDouble(f[3])
			z, _ := hexDouble(f[4])
			m := kv(f[5:])
			sc.existing = append(sc.existing, Entity{Type: et, X: x, Y: y, Z: z, Persistent: m["persistent"] == "true"})
			if m["blocksBuilding"] == "true" {
				sc.entities = append(sc.entities, et.SpawnAABB(x, y, z))
			}
		case "enddump":
		case "op":
			cur = &sceneOp{args: f[1:]}
		default:
			return nil, fmt.Errorf("unknown line %q", line)
		}
	}
	return sc, nil
}

// sceneLevel implements Level over a scene dump.
type sceneLevel struct {
	sc       *sceneOut
	override map[[3]int]data.StateID
	onAdd    func(*Mob)
	addedMob []*Mob
	dim      *Dimension
	added    []AABB
	t        testing.TB
}

func (l *sceneLevel) idx(x, y, z int) int {
	sc := l.sc
	if x < sc.x0 || x > sc.x1 || y < sc.y0 || y > sc.y1 || z < sc.z0 || z > sc.z1 {
		return -1
	}
	nx, nz := sc.x1-sc.x0+1, sc.z1-sc.z0+1
	return (x - sc.x0) + nx*((z-sc.z0)+nz*(y-sc.y0))
}

func (l *sceneLevel) outside(what string, x, y, z int) {
	l.t.Errorf("scene query outside dump: %s %d %d %d", what, x, y, z)
}

func (l *sceneLevel) Dimension() *Dimension { return l.dim }
func (l *sceneLevel) SeaLevel() int         { v, _ := strconv.Atoi(l.sc.env["sea"]); return v }
func (l *sceneLevel) BlockState(x, y, z int) data.StateID {
	if s, ok := l.override[[3]int{x, y, z}]; ok {
		return s
	}
	i := l.idx(x, y, z)
	if i < 0 {
		if y <= l.sc.y1 && y >= l.sc.y0 {
			l.outside("block", x, y, z)
		}
		return 0
	}
	return l.sc.blocks[i]
}
func (l *sceneLevel) SkyLight(x, y, z int) int {
	i := l.idx(x, y, z)
	if i < 0 {
		if y <= l.sc.y1 {
			l.outside("sky", x, y, z)
		}
		return 15
	}
	return int(l.sc.sky[i])
}
func (l *sceneLevel) BlockLight(x, y, z int) int {
	i := l.idx(x, y, z)
	if i < 0 {
		if y <= l.sc.y1 {
			l.outside("blocklight", x, y, z)
		}
		return 0
	}
	return int(l.sc.blockLight[i])
}
func (l *sceneLevel) SkyDarken() int { v, _ := strconv.Atoi(l.sc.env["skyDarken"]); return v }
func (l *sceneLevel) Height(h Heightmap, x, z int) int {
	sc := l.sc
	if x < sc.x0 || x > sc.x1 || z < sc.z0 || z > sc.z1 {
		l.outside("height", x, 0, z)
		return 0
	}
	return sc.heights[h][(x-sc.x0)+(sc.x1-sc.x0+1)*(z-sc.z0)]
}
func (l *sceneLevel) Biome(x, y, z int) string {
	i := l.idx(x, y, z)
	if i < 0 {
		l.outside("biome", x, y, z)
		return ""
	}
	return l.sc.biomes[i]
}
func (l *sceneLevel) Players() []Player { return l.sc.players }
func (l *sceneLevel) SharedSpawn() (int, int, int) {
	p := strings.Split(l.sc.env["spawn"], ",")
	x, _ := strconv.Atoi(p[0])
	y, _ := strconv.Atoi(p[1])
	z, _ := strconv.Atoi(p[2])
	return x, y, z
}
func (l *sceneLevel) CanSpawnEntitiesInChunk(cx, cz int) bool {
	sc := l.sc
	cx0, cz0, cx1, cz1 := sc.x0>>4, sc.z0>>4, sc.x1>>4, sc.z1>>4
	if cx < cx0 || cx > cx1 || cz < cz0 || cz > cz1 {
		return false
	}
	return sc.ticking[(cx-cx0)+(cx1-cx0+1)*(cz-cz0)]
}
func (l *sceneLevel) NoCollision(b AABB) bool {
	reg := data.Canonical()
	for x := blockPos(b.MinX) - 1; x <= blockPos(b.MaxX)+1; x++ {
		for y := blockPos(b.MinY) - 1; y <= blockPos(b.MaxY)+1; y++ {
			for z := blockPos(b.MinZ) - 1; z <= blockPos(b.MaxZ)+1; z++ {
				for _, c := range reg.Collision(l.BlockState(x, y, z)) {
					o := AABB{float64(x) + c.MinX, float64(y) + c.MinY, float64(z) + c.MinZ, float64(x) + c.MaxX, float64(y) + c.MaxY, float64(z) + c.MaxZ}
					if intersects(b, o) {
						return false
					}
				}
			}
		}
	}
	return true
}
func intersects(a, b AABB) bool {
	return a.MinX < b.MaxX && a.MaxX > b.MinX && a.MinY < b.MaxY && a.MaxY > b.MinY && a.MinZ < b.MaxZ && a.MaxZ > b.MinZ
}
func (l *sceneLevel) IsUnobstructed(b AABB) bool {
	for _, e := range append(l.sc.entities, l.added...) {
		if intersects(b, e) {
			return false
		}
	}
	return true
}
func (l *sceneLevel) Structures(x, y, z int) []StructureHit { return nil }
func (l *sceneLevel) Difficulty() Difficulty {
	switch l.sc.env["difficulty"] {
	case "peaceful":
		return Peaceful
	case "easy":
		return Easy
	case "hard":
		return Hard
	}
	return Normal
}
func (l *sceneLevel) DifficultyAt(x, y, z int) DifficultyInstance {
	return DifficultyInstance{l.Difficulty(), parseF(l.sc.env["effDifficulty"]), parseF(l.sc.env["special"])}
}
func (l *sceneLevel) Thundering() bool { return l.sc.env["thundering"] == "true" }
func (l *sceneLevel) Seed() int64 {
	v, _ := strconv.ParseInt(l.sc.env["seed"], 10, 64)
	return v
}
func (l *sceneLevel) MoonBrightness() float32                { return parseF(l.sc.env["moonBrightness"]) }
func (l *sceneLevel) SurfaceSlimeChance(x, y, z int) float32 { return parseF(l.sc.env["slimeChance"]) }

func (l *sceneLevel) SpawnerBlocksWork() bool { return true }

func (l *sceneLevel) CountNearby(t *EntityType, box AABB) int {
	n := 0
	for _, m := range l.addedMob {
		if m.Type == t && intersects(box, m.BoundingBox()) {
			n++
		}
	}
	return n
}

func (l *sceneLevel) AddMob(m *Mob) {
	l.addedMob = append(l.addedMob, m)
	if l.onAdd != nil {
		l.onAdd(m)
	}
	l.added = append(l.added, m.BoundingBox())
	for _, p := range m.Passengers {
		l.added = append(l.added, p.BoundingBox())
	}
}

// loadScene returns the probe output for testdata/scenes/<name>.txt,
// regenerating it from the reference with KREMENMC_REF=1.
func loadScene(t *testing.T, name string) *sceneOut {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "scenes", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	b := reftest.Golden(t, "scenes/"+name+".golden", "kremenmc.probe.spawn.Scene", abs)
	sc, err := parseScene(b)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, w := range sc.warn {
		if !strings.Contains(w, "No blocks were filled") && !strings.Contains(w, "did not change") && !strings.Contains(w, "Could not set the block") {
			t.Errorf("%s: probe setup: %s", name, w)
		}
	}
	return sc
}

func formatSpawned(m *Mob, d DifficultyInstance) string {
	return fmt.Sprintf("m spawned %s %s %s %s %s eff=%s special=%s", m.Type.Name, dhex(m.X), dhex(m.Y), dhex(m.Z), fhex(m.Yaw), fhex(d.Effective), fhex(d.Special))
}

// stripRiders drops passenger/vehicle annotations, which depend on
// finalizeSpawn hooks that may be skipped.
func stripRiders(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.HasPrefix(l, "m spawned ") {
			f := strings.Fields(l)
			var keep []string
			for _, x := range f {
				if !strings.HasPrefix(x, "passenger=") && !strings.HasPrefix(x, "vehicle=") && !strings.HasPrefix(x, "group=") && !strings.HasPrefix(x, "cluster=") {
					keep = append(keep, x)
				}
			}
			l = strings.Join(keep, " ")
		}
		out[i] = l
	}
	return out
}

func atoiT(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
