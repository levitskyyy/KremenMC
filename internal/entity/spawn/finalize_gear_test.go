package spawn

import (
	"math"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

type gearLevel struct {
	fixedLevel
	biome string
}

func (g *gearLevel) Biome(int, int, int) string              { return g.biome }
func (g *gearLevel) Structures(int, int, int) []StructureHit { return nil }
func (g *gearLevel) MoonBrightness() float32                 { return 1 }

// TestFinalizeGearRates compares outcome rates of the approximate
// finalizeSpawn hooks with 200 reference finalizeSpawn calls per type
// (kremenmc.probe.mob.Mobs 200 hard 2000000 3600000 1.0 in the desert
// test world: hard instance, special multiplier 1, normal level).
func TestFinalizeGearRates(t *testing.T) {
	e := NewEngine(loadTestData(t))
	l := &gearLevel{fixedLevel{diff: Normal}, "minecraft:desert"}
	d := DifficultyInstance{Base: Hard, Effective: 6.75, Special: 1}
	r := rng.NewLegacy(1)
	const n = 4000
	cases := []struct {
		typ  string
		stat func(*Mob) bool
		ref  int // of 200
	}{
		{"zombie", func(m *Mob) bool { return m.Attrs["equip.head"] != nil }, 33},
		{"zombie", func(m *Mob) bool { return m.Attrs["baby"] == true }, 15},
		{"skeleton", func(m *Mob) bool { return m.Attrs["equip.head"] != nil }, 38},
		{"skeleton", func(m *Mob) bool { return m.Attrs["ench.mainhand"] == true }, 56},
		{"skeleton", func(m *Mob) bool { return m.Attrs["equip.feet"] != nil }, 79 * 200 / 205 * 38 / 200},
		{"drowned", func(m *Mob) bool { return m.Attrs["equip.mainhand"] == "trident" }, 12},
		{"drowned", func(m *Mob) bool { return m.Attrs["equip.offhand"] == "nautilus_shell" }, 9},
		{"piglin", func(m *Mob) bool { return m.Attrs["baby"] == true }, 48},
		{"piglin", func(m *Mob) bool { return m.Attrs["equip.mainhand"] == "crossbow" }, 69},
		{"zombified_piglin", func(m *Mob) bool { return m.Attrs["equip.mainhand"] == "golden_sword" }, 191},
		{"vindicator", func(m *Mob) bool { return m.Attrs["ench.mainhand"] == true }, 53},
		{"rabbit", func(m *Mob) bool { return m.Attrs["type"] == int32(rabbitGold) }, 200},
		{"sheep", func(m *Mob) bool { return m.Attrs["color"] == "brown" }, 169},
		{"cat", func(m *Mob) bool { return m.Attrs["variant"] == "minecraft:all_black" }, 24},
		{"pig", func(m *Mob) bool { return m.Attrs["variant"] == "minecraft:warm" }, 200},
		{"frog", func(m *Mob) bool { return m.Attrs["variant"] == "minecraft:warm" }, 200},
		{"wolf", func(m *Mob) bool { return m.Attrs["variant"] == "minecraft:pale" }, 200},
	}
	for _, c := range cases {
		typ := mustEntity(t, c.typ)
		hit := 0
		for range n {
			m := &Mob{Type: typ}
			e.FinalizeSpawn(l, r, m, d, ReasonNatural, nil)
			if c.stat(m) {
				hit++
			}
		}
		got, want := float64(hit)/n, float64(c.ref)/200
		// 3 sigma of the 200-sample reference plus 1 point.
		tol := 3*math.Sqrt(want*(1-want)/200) + 0.01
		if math.Abs(got-want) > tol {
			t.Errorf("%s: rate %.3f, reference %.3f (tol %.3f)", c.typ, got, want, tol)
		}
	}
}
