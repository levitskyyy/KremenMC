package spawn

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

func TestLoadVanillaData(t *testing.T) {
	d := loadTestData(t)
	cases := []struct {
		biome string
		cat   Category
		total int32
		n     int
	}{
		{"minecraft:plains", Monster, 515, 9},
		{"minecraft:plains", Creature, 46, 6},
		{"minecraft:plains", Ambient, 10, 1},
		{"minecraft:soul_sand_valley", Monster, 0, -1},
		{"minecraft:the_void", Monster, 0, 0},
	}
	for _, c := range cases {
		l := d.Settings(c.biome).Mobs(c.cat)
		if c.n < 0 {
			if l.IsEmpty() {
				t.Errorf("%s %s: empty", c.biome, c.cat)
			}
			continue
		}
		var total int32
		n := 0
		if l != nil {
			total, n = l.Total, len(l.Entries)
		}
		if total != c.total || n != c.n {
			t.Errorf("%s %s: total %d entries %d, want %d %d", c.biome, c.cat, total, n, c.total, c.n)
		}
	}
	if b := d.Biomes["minecraft:snowy_plains"]; b == nil || b.WorldGenProbability != 0.07 {
		t.Errorf("snowy_plains creature probability %v", b)
	}
	if cost, ok := d.Settings("minecraft:soul_sand_valley").Cost(mustEntity(t, "skeleton")); !ok || cost.Charge != 0.7 || cost.EnergyBudget != 0.15 {
		t.Errorf("soul sand valley skeleton cost %+v %v", cost, ok)
	}
	f := d.Structures["minecraft:fortress"]
	if f == nil || f.Overrides[Monster] == nil || f.Overrides[Monster].Box != BoxPiece || f.Overrides[Monster].Spawns.Total != 28 {
		t.Errorf("fortress overrides %+v", f)
	}
	if m := d.Structures["minecraft:monument"]; m == nil || m.Overrides[Monster].Box != BoxFull || !m.Overrides[Axolotls].Spawns.IsEmpty() {
		t.Errorf("monument overrides %+v", m)
	}
	ow, ne := d.Dimensions["minecraft:overworld"], d.Dimensions["minecraft:the_nether"]
	if ow.MonsterLight != (IntProvider{"uniform", 0, 7}) || ow.MonsterBlockLightCap != 0 || ow.MinY != -64 {
		t.Errorf("overworld %+v", ow)
	}
	if ne.MonsterLight != (IntProvider{"constant", 7, 7}) || ne.MonsterBlockLightCap != 15 || !ne.HasCeiling || ne.AmbientLight != 0.1 {
		t.Errorf("nether %+v", ne)
	}
}

func mustEntity(t *testing.T, n string) *EntityType {
	t.Helper()
	e, ok := EntityByName(n)
	if !ok {
		t.Fatalf("entity %s", n)
	}
	return e
}

func TestIntProvider(t *testing.T) {
	cases := []struct {
		json string
		want IntProvider
		err  bool
	}{
		{`4`, IntProvider{"constant", 4, 4}, false},
		{`{"type":"minecraft:uniform","min_inclusive":2,"max_inclusive":6}`, IntProvider{"uniform", 2, 6}, false},
		{`{"type":"minecraft:constant","value":3}`, IntProvider{"constant", 3, 3}, false},
		{`{"type":"minecraft:weighted_list"}`, IntProvider{}, true},
	}
	for _, c := range cases {
		var p IntProvider
		err := json.Unmarshal([]byte(c.json), &p)
		if (err != nil) != c.err || (!c.err && p != c.want) {
			t.Errorf("%s: %+v %v", c.json, p, err)
		}
	}
	r := rng.NewLegacy(1)
	for range 100 {
		if v := (IntProvider{"uniform", 2, 6}).Sample(r); v < 2 || v > 6 {
			t.Fatalf("uniform sample %d", v)
		}
	}
}

func TestPotentialCalculator(t *testing.T) {
	var p PotentialCalculator
	p.AddCharge(0, 0, 0, 0.7)
	p.AddCharge(3, 4, 0, 0.7)
	c7, ten := 0.7, 10.0 // runtime values: constant folding would round differently
	cases := []struct {
		x, y, z int
		charge  float64
		want    float64
	}{
		{0, 0, 10, 0.7, (c7/ten + c7/distSqrt(125)) * c7},
		{1, 0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := p.EnergyChange(c.x, c.y, c.z, c.charge); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestCheckDespawnPeaceful(t *testing.T) {
	l := &fixedLevel{diff: Peaceful}
	zombie, cow := mustEntity(t, "zombie"), mustEntity(t, "cow")
	if !CheckDespawn(l, DespawnState{Type: zombie}, rng.NewLegacy(0)).Remove {
		t.Error("zombie survives peaceful")
	}
	if CheckDespawn(l, DespawnState{Type: cow}, rng.NewLegacy(0)).Remove {
		t.Error("cow removed in peaceful without players")
	}
}

type fixedLevel struct {
	Level
	diff Difficulty
}

func (f *fixedLevel) Difficulty() Difficulty { return f.diff }
func (f *fixedLevel) Players() []Player      { return nil }

func distSqrt(v float64) float64 { return math.Sqrt(v) }
