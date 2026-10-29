package item

import (
	"bytes"
	"testing"
)

func TestPatchNormalisation(t *testing.T) {
	sword, _ := IDOf("diamond_sword")
	stone, _ := IDOf("stone")
	tests := []struct {
		name    string
		s       Stack
		wantLen int
	}{
		{"default damage dropped", Stack{Item: sword, Count: 1}.WithDamage(0), 0},
		{"damage kept", Stack{Item: sword, Count: 1}.WithDamage(3), 1},
		{"default stack size dropped", Stack{Item: stone, Count: 1}.Set(MaxStackSize, EncodeVarInt(64)), 0},
		{"stack size kept", Stack{Item: stone, Count: 1}.Set(MaxStackSize, EncodeVarInt(16)), 1},
		{"removing absent default is a no-op", Stack{Item: stone, Count: 1}.Remove(Unbreakable), 0},
		{"removing a default is recorded", Stack{Item: stone, Count: 1}.Remove(MaxStackSize), 1},
	}
	for _, tc := range tests {
		if got := tc.s.Patch.Len(); got != tc.wantLen {
			t.Errorf("%s: patch len %d, want %d", tc.name, got, tc.wantLen)
		}
	}
	if (Stack{Item: stone, Count: 1}.Remove(MaxStackSize)).MaxStackSize() != 1 {
		t.Error("removed max_stack_size must default to 1")
	}
}

func TestSameItemSameComponents(t *testing.T) {
	stone, _ := IDOf("stone")
	a := Stack{Item: stone, Count: 3}.Set(Damage, EncodeVarInt(1)).Set(RepairCost, EncodeVarInt(2))
	b := Stack{Item: stone, Count: 9}.Set(RepairCost, EncodeVarInt(2)).Set(Damage, EncodeVarInt(1))
	if !SameItemSameComponents(a, b) {
		t.Error("patch order must not matter")
	}
	if SameItemSameComponents(a, b.Set(Damage, EncodeVarInt(2))) {
		t.Error("different values must not merge")
	}
	if Matches(a, b) {
		t.Error("counts differ")
	}
	if !bytes.Equal(AppendStack(nil, Stack{}), []byte{0}) {
		t.Error("empty stack encodes as 0")
	}
}
