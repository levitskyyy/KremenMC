package spawn

import (
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// The refdata files are probe outputs; with KREMENMC_REF=1 these tests
// regenerate them from the reference before checking.

func TestRefdataStatic(t *testing.T) {
	b := reftest.Golden(t, "../refdata/static.txt", "kremenmc.probe.spawn.Static")
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "category" {
			continue
		}
		n++
		m := kv(f[3:])
		c, ok := CategoryByName(f[2])
		if !ok {
			t.Fatalf("category %s unknown", f[2])
		}
		ord, _ := strconv.Atoi(f[1])
		info := c.Info()
		max, _ := strconv.Atoi(m["max"])
		des, _ := strconv.Atoi(m["despawn"])
		nod, _ := strconv.Atoi(m["nodespawn"])
		if int(c) != ord || info.Max != max || info.Despawn != des || info.NoDespawn != nod ||
			strconv.FormatBool(info.Friendly) != m["friendly"] || strconv.FormatBool(info.Persistent) != m["persistent"] {
			t.Errorf("category %s: have %+v (ordinal %d), reference %s", f[2], info, c, line)
		}
	}
	if n != int(NumCategories) {
		t.Errorf("reference has %d categories, Go %d", n, NumCategories)
	}
	consts := map[string]int{"MIN_SPAWN_DISTANCE": MinSpawnDistance, "SPAWN_DISTANCE_CHUNK": SpawnDistanceChunk,
		"SPAWN_DISTANCE_BLOCK": SpawnDistanceBlock, "INSCRIBED_SQUARE_SPAWN_DISTANCE_CHUNK": InscribedSquareSpawnDistanceChunk, "MAGIC_NUMBER": MagicNumber}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "const" {
			if v, _ := strconv.Atoi(f[2]); consts[f[1]] != v {
				t.Errorf("const %s = %d, reference %s", f[1], consts[f[1]], f[2])
			}
		}
		if len(f) > 1 && f[0] == "spawning_categories" {
			var have []string
			for _, c := range SpawningCategories {
				have = append(have, c.String())
			}
			if strings.Join(have, " ") != strings.Join(f[1:], " ") {
				t.Errorf("spawning categories %v, reference %v", have, f[1:])
			}
		}
	}
}

func TestRefdataEntitiesAndBlocks(t *testing.T) {
	reftest.Golden(t, "../refdata/entities.txt", "kremenmc.probe.spawn.Entities")
	reftest.Golden(t, "../refdata/blocks.txt", "kremenmc.probe.spawn.BlockTable")
	reg := data.Canonical()
	if reg.NumStates() != len(blockTable) {
		t.Fatalf("data registry has %d states, reference %d", reg.NumStates(), len(blockTable))
	}
	for s := range len(blockTable) {
		name := BlockName(data.StateID(s))
		if got := reg.BlockOf(data.StateID(s)); got == nil || qualify(got.Name) != name {
			t.Fatalf("state %d: data registry block %v, reference %s", s, got, name)
		}
	}
	for _, et := range EntityTypes() {
		if d, ok := reg.EntityByName(strings.TrimPrefix(et.Name, "minecraft:")); !ok || int32(d.ID) != et.Kind {
			t.Errorf("entity %s: reference id %d, data registry %v", et.Name, et.Kind, d)
		}
	}
}

func TestPlacementTable(t *testing.T) {
	cases := []struct {
		name      string
		placement Placement
		heightmap Heightmap
		category  Category
	}{
		{"zombie", PlacementOnGround, HeightmapMotionBlockingNoLeaves, Monster},
		{"cod", PlacementInWater, HeightmapMotionBlockingNoLeaves, WaterAmbient},
		{"strider", PlacementInLava, HeightmapMotionBlockingNoLeaves, Creature},
		{"ocelot", PlacementOnGround, HeightmapMotionBlocking, Creature},
		{"axolotl", PlacementInWater, HeightmapMotionBlockingNoLeaves, Axolotls},
		{"boat", PlacementNone, HeightmapMotionBlockingNoLeaves, Misc},
	}
	for _, c := range cases {
		et, ok := EntityByName(c.name)
		if !ok {
			if c.name == "boat" {
				continue
			}
			t.Fatalf("%s missing", c.name)
		}
		if et.Placement != c.placement || et.Heightmap != c.heightmap || et.Category != c.category {
			t.Errorf("%s: placement %d heightmap %v category %v", c.name, et.Placement, et.Heightmap, et.Category)
		}
	}
}

func TestDifficultyInstance(t *testing.T) {
	n := 0
	for _, line := range strings.Split(refStatic, "\n") {
		f := strings.Fields(line)
		if len(f) != 7 || f[0] != "difficulty" {
			continue
		}
		n++
		d, _ := strconv.Atoi(f[1])
		dt, _ := strconv.ParseInt(f[2], 10, 64)
		ih, _ := strconv.ParseInt(f[3], 10, 64)
		got := NewDifficultyInstance(Difficulty(d), dt, ih, parseF(f[4]))
		if fhex(got.Effective) != f[5] || fhex(got.Special) != f[6] {
			t.Errorf("%s: got eff=%s special=%s", line, fhex(got.Effective), fhex(got.Special))
		}
	}
	if n == 0 {
		t.Fatal("no difficulty vectors")
	}
}

func TestSpawnerDefaults(t *testing.T) {
	s := NewSpawner(0, 0, 0, nil)
	want := map[string]int{"DEFAULT_SPAWN_DELAY": s.Delay, "DEFAULT_MIN_SPAWN_DELAY": s.MinDelay, "DEFAULT_MAX_SPAWN_DELAY": s.MaxDelay,
		"DEFAULT_SPAWN_COUNT": s.Count, "DEFAULT_MAX_NEARBY_ENTITIES": s.MaxNearby, "DEFAULT_REQUIRED_PLAYER_RANGE": s.PlayerRange, "DEFAULT_SPAWN_RANGE": s.Range}
	n := 0
	for _, line := range strings.Split(refStatic, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "spawner" {
			n++
			if v, _ := strconv.Atoi(f[2]); want[f[1]] != v {
				t.Errorf("%s = %d, reference %s", f[1], want[f[1]], f[2])
			}
		}
	}
	if n != len(want) {
		t.Errorf("reference listed %d spawner constants", n)
	}
}
