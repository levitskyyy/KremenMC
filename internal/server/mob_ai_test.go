package server

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// aiScene is a flat test world ticked by hand (no server loop): entity
// physics, the AI snapshot, the region phases and the queued actions.
type aiScene struct {
	t     testing.TB
	w     *World
	y     float64
	dummy *Mob
	fake  []aiEnt
}

func newAIScene(t testing.TB) *aiScene {
	t.Helper()
	dir := t.TempDir()
	props := config.DefaultServerProperties()
	props.OnlineMode = false
	props.LevelType = "minecraft:flat"
	s, err := New(Config{Dir: dir, Props: props, Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorld(s, WorldConfig{Name: "world", Dir: dir + "/world", Seed: 1, Flat: true})
	if err != nil {
		t.Fatal(err)
	}
	s.AddWorld(w)
	t.Cleanup(func() { w.close() })
	w.SetTime(18000)
	w.SetDifficulty(api.DifficultyNormal)
	w.SetGameRule("doDaylightCycle", "false")
	sc := &aiScene{t: t, w: w, y: w.Spawn().Pos.Y}
	for range 200 {
		w.w.DoTick()
		if w.w.Loaded() >= 25 {
			break
		}
	}
	sc.y = float64(w.HighestBlockY(0, 0)) + 1
	aiExtraPlayers = func(*World) []aiEnt { return sc.fake }
	t.Cleanup(func() { aiExtraPlayers = nil })
	return sc
}

func (sc *aiScene) spawn(typ string, x, z float64) *Mob {
	e, err := sc.w.SpawnEntity(api.Key("minecraft:"+typ), api.Location{World: "world", Pos: api.Vec3{X: x, Y: sc.y, Z: z}}, nil)
	if err != nil {
		sc.t.Fatal(err)
	}
	return e.(*Mob)
}

// target places an AI-less villager that the brains see as a survival
// player at (x, z).
func (sc *aiScene) target(x, z float64, survival bool) {
	d := sc.spawn("villager", x, z)
	d.onTick = func(*Mob) {}
	sc.dummy = d
	sc.fake = []aiEnt{{m: d, id: 1<<30 + d.e.ID, uuid: d.e.UUID, pos: d.e.Pos, w: 0.6, h: 1.8, eye: 1.62, typ: "player", alive: true, targetOK: survival}}
}

func (sc *aiScene) tick() {
	w := sc.w
	w.s.tick.Add(1)
	w.tickEntities()
	if sc.dummy != nil {
		sc.fake[0].pos = sc.dummy.feet()
	}
	w.aiPrepare(nil)
	w.w.DoTick()
	w.aiApply()
}

func TestAIZombieChase(t *testing.T) {
	sc := newAIScene(t)
	sc.target(0.5, 0.5, true)
	z := sc.spawn("zombie", 14.5, 0.5)
	var hits []int
	var steady float64
	start := -1
	last := z.feet()
	for i := 1; i <= 240; i++ {
		sc.tick()
		p := z.feet()
		d := math.Hypot(p.X-last.X, p.Z-last.Z)
		last = p
		if start < 0 && d > 0.02 {
			start = i
		}
		if i == start+30 {
			steady = d
		}
		if sc.dummy.Health() < 20 {
			hits = append(hits, i)
			sc.dummy.SetHealth(20)
		}
	}
	t.Logf("zombie: starts moving at tick %d, steady %.4f b/t, hits at %v", start, steady, hits)
	if math.Abs(steady-0.1165) > 0.002 {
		t.Errorf("chase speed %.4f b/t, reference 0.1165", steady)
	}
	if len(hits) < 2 {
		t.Fatalf("hits %v", hits)
	}
	// the reference shows 20 while the target stays in reach and 15–28
	// when the attack goal restarts (target knocked out of reach)
	for i := 1; i < len(hits); i++ {
		if hits[i]-hits[i-1] < 15 {
			t.Errorf("attack interval %d < 15", hits[i]-hits[i-1])
		}
	}
}

func TestAISkeletonShoots(t *testing.T) {
	sc := newAIScene(t)
	sc.target(0.5, 0.5, true)
	s := sc.spawn("skeleton", 12.5, 0.5)
	id, _ := item.IDOf("minecraft:bow")
	s.liv.equip[slotMain] = item.Stack{Item: id, Count: 1}
	var shots []int
	minD, maxD := math.MaxFloat64, 0.0
	for i := 1; i <= 400; i++ {
		sc.tick()
		for _, m := range sc.w.mobList() {
			if m.typ.Name == "arrow" && !m.removed.Load() {
				shots = append(shots, i)
				m.Remove()
			}
		}
		sc.dummy.SetHealth(20)
		if i > 100 {
			d := math.Sqrt(s.feet().DistanceSq(sc.dummy.feet()))
			minD, maxD = min(minD, d), max(maxD, d)
		}
	}
	t.Logf("skeleton: shots at %v, distance after tick 100 in [%.1f, %.1f]", shots, minD, maxD)
	if len(shots) < 4 {
		t.Fatalf("shots %v", shots)
	}
	for i := 1; i < len(shots); i++ {
		if d := shots[i] - shots[i-1]; d != 60 {
			t.Errorf("shot interval %d, reference 60", d)
		}
	}
}

func TestAICreeperFuse(t *testing.T) {
	sc := newAIScene(t)
	sc.w.SetGameRule("mob_griefing", "false")
	sc.target(0.5, 0.5, true)
	c := sc.spawn("creeper", 8.5, 0.5)
	swellAt, boomAt := -1, -1
	var dist float64
	for i := 1; i <= 200 && boomAt < 0; i++ {
		sc.tick()
		if swellAt < 0 && c.ai != nil && c.ai.swell > 0 {
			swellAt = i
			dist = math.Sqrt(c.feet().DistanceSq(sc.dummy.feet()))
		}
		if c.removed.Load() {
			boomAt = i
		}
	}
	t.Logf("creeper: swell starts tick %d at distance %.2f, explodes tick %d (fuse %d)", swellAt, dist, boomAt, boomAt-swellAt)
	if swellAt < 0 || boomAt < 0 {
		t.Fatal("no explosion")
	}
	if dist >= 3 {
		t.Errorf("swell started at %.2f, reference < 3", dist)
	}
	if f := boomAt - swellAt; f != 29 && f != 30 {
		t.Errorf("fuse %d ticks, reference 30", f)
	}
}

func TestAIPassiveWander(t *testing.T) {
	if testing.Short() {
		t.Skip("2400 ticks")
	}
	sc := newAIScene(t)
	sc.target(0.5, 19.5, false)
	types := []string{"cow", "cow", "pig", "sheep", "chicken", "zombie", "skeleton", "creeper"}
	var ms []*Mob
	for i, ty := range types {
		z := -8.0
		if i >= 4 {
			z = 8
		}
		ms = append(ms, sc.spawn(ty, float64(-12+(i%4)*8)+0.5, z+0.5))
	}
	last := make([]api.Vec3, len(ms))
	moving := make([]bool, len(ms))
	walks := 0
	for i := 1; i <= 2400; i++ {
		sc.tick()
		for j, m := range ms {
			p := m.feet()
			mv := i > 1 && math.Hypot(p.X-last[j].X, p.Z-last[j].Z) > 0.003
			if mv && !moving[j] {
				walks++
			}
			moving[j], last[j] = mv, p
		}
	}
	t.Logf("wander: %d walks of %d mobs in 2400 ticks (reference 27)", walks, len(ms))
	// mobs drifting beyond 32 blocks of the player stop strolling, so this
	// scene gives fewer walks than the reference (docs/vanilla-parity.md)
	if walks < 4 || walks > 60 {
		t.Errorf("walks %d, reference 27", walks)
	}
}

// BenchmarkAITick is one world tick (physics, snapshot, AI phases, actions)
// with 100 zombies chasing and 100 cows idling near a fake player.
func BenchmarkAITick(b *testing.B) {
	sc := newAIScene(b)
	sc.target(0.5, 0.5, true)
	for i := range 100 {
		sc.spawn("zombie", float64(i%10*3)-14.5, float64(i/10*3)-14.5)
		sc.spawn("cow", float64(i%10*3)-13.5, float64(i/10*3)-13.5)
	}
	for range 40 {
		sc.tick()
	}
	b.ResetTimer()
	for b.Loop() {
		sc.dummy.SetHealth(20)
		sc.tick()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/200, "ns/mob")
}

func TestAITemptAndPanic(t *testing.T) {
	sc := newAIScene(t)
	sc.target(0.5, 0.5, false)
	sc.fake[0].held[0] = "minecraft:wheat"
	c := sc.spawn("cow", 7.5, 0.5)
	for range 200 {
		sc.tick()
	}
	if d := math.Sqrt(c.feet().DistanceSq(sc.dummy.feet())); d > 3.5 {
		t.Errorf("tempted cow at %.1f blocks, want within 2.5–3.5", d)
	}
	sc.fake[0].held[0] = ""
	c.Damage(1, api.DamageSource{Type: "minecraft:generic"})
	p0 := c.feet()
	moved := 0.0
	for range 40 {
		sc.tick()
	}
	moved = math.Sqrt(c.feet().DistanceSq(p0))
	if moved < 2 {
		t.Errorf("hurt cow moved %.1f blocks, want a panic run", moved)
	}
}
