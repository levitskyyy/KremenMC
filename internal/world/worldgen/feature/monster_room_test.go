package feature

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

type beEnv struct {
	*envRegion
	bes map[Pos]nbt.Compound
}

func (e *beEnv) SetBlockEntity(x, y, z int, id string, d nbt.Compound) bool {
	d["id"] = id
	e.bes[Pos{x, y, z}] = d
	return true
}

// TestMonsterRoomBlockEntities checks the chests and spawner of the golden
// tunnel scene 199000597 get block entities.
func TestMonsterRoomBlockEntities(t *testing.T) {
	const seed, x, y, z = 199000597, 7504, -30, -6504
	reg := &beEnv{&envRegion{m: map[Pos]data.StateID{}, mode: "tunnel", seed: seed, cx: x, y0: y, cz: z}, map[Pos]nbt.Compound{}}
	lv := &Level{rg: reg, st: &Stage{}}
	if !(monsterRoom{}).Place(&placeCtx{lv: lv}, rng.NewWorldgenRandom(rng.NewXoroshiro(seed)), Pos{x, y, z}) {
		t.Fatal("not placed")
	}
	for _, c := range []struct {
		p  Pos
		id string
	}{{Pos{x - 3, y, z + 1}, "minecraft:chest"}, {Pos{x, y, z + 2}, "minecraft:chest"}, {Pos{x, y, z}, "minecraft:mob_spawner"}} {
		be := reg.bes[c.p]
		if be["id"] != c.id {
			t.Errorf("%v: block entity %v, want %s", c.p, be, c.id)
		}
		if c.id == "minecraft:chest" && be["LootTable"] != "minecraft:chests/simple_dungeon" {
			t.Errorf("%v: loot table %v", c.p, be["LootTable"])
		}
	}
	if len(reg.bes) != 3 {
		t.Errorf("%d block entities, want 3", len(reg.bes))
	}
}
