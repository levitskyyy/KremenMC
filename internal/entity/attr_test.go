package entity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// TestVanillaTable checks the embedded reference table; with KREMENMC_REF=1 it
// is regenerated from the reference first.
func TestVanillaTable(t *testing.T) {
	if os.Getenv("KREMENMC_REF") == "1" {
		ref, err := reftest.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := ref.Run(&buf, "kremenmc.probe.combat.Attrs"); err != nil {
			t.Fatal(err)
		}
		if buf.String() != vanillaTable {
			if err := os.WriteFile(filepath.Join("data", "vanilla-26.3.txt"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Fatal("data/vanilla-26.3.txt regenerated; rebuild and rerun")
		}
	}
	tests := []struct {
		typ  string
		attr Attr
		want float64
	}{
		{"player", AttrAttackDamage, 1},
		{"player", AttrMovementSpeed, float64(float32(0.1))},
		{"player", AttrMaxHealth, 20},
		{"zombie", AttrArmor, 2},
		{"zombie", AttrAttackDamage, 3},
		{"zombie_horse", AttrSafeFallDistance, 6},
		{"armor_stand", AttrStepHeight, 0},
		{"enderman", AttrMaxHealth, 40},
	}
	for _, tt := range tests {
		a := NewAttrs(tt.typ)
		if a == nil {
			t.Fatalf("%s has no attributes", tt.typ)
		}
		if got := a.Value(tt.attr); got != tt.want {
			t.Errorf("%s %s = %v, want %v", tt.typ, AttrInfoOf(tt.attr).Name, got, tt.want)
		}
	}
	if NewAttrs("arrow") != nil {
		t.Error("arrow has attributes")
	}
	if p, _ := PhysicsOf("arrow"); p.Gravity != 0.05 || p.Drag != 0.99 {
		t.Errorf("arrow physics %+v", p)
	}
}

func TestAttributeModifiers(t *testing.T) {
	a := NewAttrs("player")
	a.TakeDirty()
	a.SetModifier(AttrAttackDamage, Modifier{ID: "minecraft:base_attack_damage", Amount: 6})
	a.SetModifier(AttrAttackDamage, Modifier{ID: "minecraft:effect.strength", Amount: 3})
	a.SetModifier(AttrAttackDamage, Modifier{ID: "x:base", Amount: 0.5, Op: OpAddMultipliedBase})
	a.SetModifier(AttrAttackDamage, Modifier{ID: "x:total", Amount: 1, Op: OpAddMultipliedTotal})
	// (1+6+3) = 10; +10×0.5 = 15; ×2 = 30.
	if v := a.Value(AttrAttackDamage); v != 30 {
		t.Fatalf("value %v, want 30", v)
	}
	if d := a.TakeDirty(); len(d) != 0 {
		t.Fatalf("attack_damage is not client-synced, dirty %v", d)
	}
	a.SetModifier(AttrArmor, Modifier{ID: "a", Amount: 50})
	if v := a.Value(AttrArmor); v != 30 {
		t.Fatalf("armor %v, want clamp 30", v)
	}
	if d := a.TakeDirty(); len(d) != 1 || d[0] != AttrArmor {
		t.Fatalf("dirty %v", d)
	}
	a.RemoveModifier(AttrArmor, "a")
	if v := a.Value(AttrArmor); v != 0 {
		t.Fatalf("armor %v after removal", v)
	}
}
