package server

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestChestFillReference replays the chest probe: a real reference chest
// with LootTable chests/simple_dungeon and a LootTableSeed, unpacked with no
// player. The roll and the fill both run on LegacyRandom(seed). Enchanted
// books compare by item and count only.
func TestChestFillReference(t *testing.T) {
	const table = "minecraft:chests/simple_dungeon"
	seeds := []string{"-3247778988095883450", "-8313445843520837373", "-4279480113536734294", "1", "2", "3", "0", "-1",
		"7736917862143332659", "1316188383464067185", "6817917661961110170", "-5469157321183157634", "123456789", "-987654321"}
	out := reftest.Golden(t, "chest_fill.txt", "kremenmc.probe.chest.Fill", append([]string{table}, seeds...)...)
	tb := loot.Get(table)
	if tb == nil {
		t.Fatal("no loot table")
	}
	reg := data.Canonical()
	name := func(s item.Stack) string {
		return fmt.Sprintf("%s*%d", strings.TrimPrefix(reg.ItemName(data.ItemID(s.Item)), "minecraft:"), s.Count)
	}
	n, bad := 0, 0
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "real" {
			continue
		}
		seed, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if seed == 0 {
			continue // LootTableSeed 0 means unseeded: the reference rolls on a random seed
		}
		src := rng.NewLegacy(seed)
		ctx := &loot.Context{Rand: src, Origin: api.Vec3{X: .5, Y: 100.5, Z: .5}, HasOrigin: true}
		var items [chestSlots]item.Stack
		fillContainer(&items, tb.Roll(ctx), src)
		var got []string
		for i, s := range items {
			if !s.IsEmpty() {
				got = append(got, fmt.Sprintf("%d:%s", i, name(s)))
			}
		}
		want := f[2:]
		for i, w := range want {
			if j := strings.IndexByte(w, '+'); j >= 0 {
				want[i] = w[:j]
			}
		}
		n++
		if strings.Join(got, " ") != strings.Join(want, " ") {
			bad++
			t.Errorf("seed %d:\n got  %s\n want %s", seed, strings.Join(got, " "), strings.Join(want, " "))
		}
	}
	if n == 0 {
		t.Fatal("no probe lines")
	}
	t.Logf("%d/%d chests identical", n-bad, n)
}
