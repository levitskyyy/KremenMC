package world

import "testing"

func TestBlockEntityNetType(t *testing.T) {
	for _, id := range []string{"minecraft:chest", "minecraft:mob_spawner", "minecraft:barrel"} {
		if BlockEntityNetType(id) < 0 {
			t.Errorf("%s: no network id", id)
		}
	}
	if BlockEntityNetType("minecraft:nope") != -1 {
		t.Error("unknown id resolved")
	}
}
