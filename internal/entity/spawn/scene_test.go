package spawn

import (
	"fmt"
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runSceneOp replays one reference operation through the Go engine and
// compares the full random-call and marker log.
func runSceneOp(t *testing.T, e *Engine, sc *sceneOut, op sceneOp) (s *script) {
	t.Helper()
	s = newScript(stripRiders(op.lines))
	defer func() {
		if r := recover(); r != nil && r != errDiverged {
			panic(r)
		}
	}()
	l := &sceneLevel{sc: sc, dim: e.Data.Dimensions["minecraft:overworld"], t: t}
	lr := &scriptSource{s, "l"}
	a := op.args
	pred := func(et *EntityType, x, y, z int) bool {
		s.mark(fmt.Sprintf("m pred %s %d %d %d", et.Name, x, y, z))
		return true
	}
	after := func(m *Mob) {
		x, y, z := m.BlockPos()
		s.mark(formatSpawned(m, l.DifficultyAt(x, y, z)))
	}
	eng := *e
	eng.Finalize = map[string]FinalizeFunc{}
	for _, et := range EntityTypes() {
		if et.Mob && !VerifiedFinalize(et) {
			eng.Finalize[et.Name] = func(ctx *FinalizeContext, m *Mob, group any) any {
				s.skipTo("m spawned ")
				return group
			}
		}
	}
	switch a[0] {
	case "rules":
		et, ok := EntityByName(a[1])
		if !ok {
			t.Fatalf("entity %s", a[1])
		}
		x, y, z := atoiT(t, a[2]), atoiT(t, a[3]), atoiT(t, a[4])
		s.mark(fmt.Sprintf("m placement %v", eng.PlacementOK(l, et, x, y, z)))
		s.mark(fmt.Sprintf("m rules %v", eng.CheckSpawnRules(l, et, ReasonNatural, x, y, z, &scriptSource{s, "p"})))
	case "mobcheck":
		et, _ := EntityByName(a[1])
		x, y, z := atoiT(t, a[2]), atoiT(t, a[3]), atoiT(t, a[4])
		s.mark(fmt.Sprintf("m light magic=%s cost=%s maxlocal=%d effsky=%d raw0=%d", fhex(lightMagic(l, x, y, z)), fhex(pathfindingCostFromLight(l, x, y, z)),
			MaxLocalRawBrightness(l, x, y, z), max(0, l.SkyLight(x, y, z)-l.SkyDarken()), rawBrightness(l, x, y, z, 0)))
		if !et.Mob {
			break
		}
		if et.is(classPathfinder) {
			s.mark("m walk " + fhex(eng.walkTargetValue(l, et, x, y, z)))
		}
		m := &Mob{Type: et, X: float64(x) + 0.5, Y: float64(y), Z: float64(z) + 0.5}
		s.mark(fmt.Sprintf("m mobrules %v", eng.mobCheckSpawnRules(l, et, x, y, z)))
		s.mark(fmt.Sprintf("m obstruction %v", eng.checkSpawnObstruction(l, et, m.BoundingBox(), x, y, z)))
	case "natural":
		c, _ := CategoryByName(a[1])
		x, y, z := atoiT(t, a[2]), atoiT(t, a[3]), atoiT(t, a[4])
		eng.SpawnCategoryForPosition(c, l, lr, x>>4, z>>4, x, y, z, pred, after, l)
	case "chunk":
		c, _ := CategoryByName(a[1])
		eng.SpawnCategoryForChunk(c, l, lr, atoiT(t, a[2]), atoiT(t, a[3]), pred, after, l)
	case "potential":
		c, _ := CategoryByName(a[1])
		st := eng.NewSpawnState(l, 1, sc.existing, nil, func(int, int) []int { return nil })
		eng.SpawnCategoryForChunk(c, l, lr, atoiT(t, a[2]), atoiT(t, a[3]), func(et *EntityType, x, y, z int) bool {
			ok := st.CanSpawn(et, x, y, z)
			s.mark(fmt.Sprintf("m canspawn %s %d %d %d %v", et.Name, x, y, z, ok))
			return ok
		}, func(m *Mob) {
			st.AfterSpawn(m)
			after(m)
		}, l)
	case "gen":
		cx, cz := atoiT(t, a[1]), atoiT(t, a[2])
		l.onAdd = after
		eng.SpawnMobsForChunkGeneration(l, &scriptSource{s, "w"}, lr, cx*16+8, l.SeaLevel(), cz*16+8, cx, cz, l)
	case "tick":
		cx, cz := atoiT(t, a[1]), atoiT(t, a[2])
		st := eng.NewSpawnState(l, atoiT(t, a[3]), nil, nil, func(int, int) []int { return nil })
		cats := st.FilteredSpawningCategories(a[4] == "1", a[5] == "1", true)
		line := "m categories"
		for _, c := range cats {
			line += " " + c.String()
		}
		s.mark(line)
		l.onAdd = after
		eng.SpawnForChunk(l, lr, cx, cz, st, cats, l)
	case "despawn":
		et, _ := EntityByName(a[1])
		ds := DespawnState{Type: et, X: parseFloat(t, a[2]), Y: parseFloat(t, a[3]), Z: parseFloat(t, a[4]), NoActionTime: atoiT(t, a[5]),
			PersistenceRequired: strings.Contains(a[6], "persistent"), CustomPersistence: et.CustomPersist, FarAway: et.FarAway}
		if strings.Contains(a[6], "named") {
			ds.FarAway = ds.FarAway && !keepsNamed(et)
		}
		res := CheckDespawn(l, ds, &scriptSource{s, "e"})
		s.mark(fmt.Sprintf("m removed %v noaction %d", res.Remove, res.NoActionTime))
	case "spawner":
		et, _ := EntityByName(a[1])
		x, y, z := atoiT(t, a[2]), atoiT(t, a[3]), atoiT(t, a[4])
		sp := NewSpawner(x, y, z, et)
		sp.Delay, sp.MinDelay, sp.MaxDelay, sp.Count = atoiT(t, a[6]), atoiT(t, a[7]), atoiT(t, a[8]), atoiT(t, a[9])
		sp.MaxNearby, sp.Range, sp.PlayerRange = atoiT(t, a[10]), atoiT(t, a[11]), atoiT(t, a[12])
		first, _, _ := StateRange("minecraft:spawner")
		l.override = map[[3]int]data.StateID{{x, y, z}: first}
		l.onAdd = after
		for i := range atoiT(t, a[5]) {
			eng.TickSpawner(l, sp, lr)
			s.mark(fmt.Sprintf("m tick %d delay=%d", i, sp.Delay))
		}
	default:
		t.Fatalf("op %s not replayed", a[0])
	}
	return s
}

func sceneNames(t *testing.T) []string {
	ents, err := os.ReadDir(filepath.Join("testdata", "scenes"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		if n, ok := strings.CutSuffix(e.Name(), ".txt"); ok {
			names = append(names, n)
		}
	}
	return names
}

func TestScenes(t *testing.T) {
	d := loadTestData(t)
	e := NewEngine(d)
	for _, name := range sceneNames(t) {
		t.Run(name, func(t *testing.T) {
			sc := loadScene(t, name)
			for i, op := range sc.ops {
				t.Run(fmt.Sprintf("%02d_%s", i, op.args[0]), func(t *testing.T) {
					s := runSceneOp(t, e, sc, op)
					s.check(t, strings.Join(op.args, " "))
				})
			}
		})
	}
}

func parseFloat(t *testing.T, v string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
