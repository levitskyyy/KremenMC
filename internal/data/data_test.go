package data

import "testing"

func TestCanonicalLookups(t *testing.T) {
	r := Canonical()
	if r.Protocol != 777 || r.Version != "26.3" {
		t.Fatalf("canonical = %s/%d", r.Version, r.Protocol)
	}
	tests := []struct {
		in   string
		want StateID
	}{
		{"air", 0},
		{"minecraft:stone", 1},
		{"grass_block", 9},
		{"grass_block[snowy=true]", 8},
		{"oak_stairs", 5474},
		{"oak_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]", 5534},
		{"minecraft:water[level=0]", 89},
		{"poplar_log[axis=x]", 166},
		{"white_wool_stairs", 2436},
	}
	for _, tt := range tests {
		got, ok := r.ParseState(tt.in)
		if !ok || got != tt.want {
			t.Errorf("ParseState(%q) = %d,%v want %d", tt.in, got, ok, tt.want)
		}
	}
	if _, ok := r.ParseState("nope"); ok {
		t.Error("unknown block resolved")
	}
}

func TestWith(t *testing.T) {
	r := Canonical()
	s := MustState("oak_stairs")
	tests := []struct {
		prop, val string
		want      string
	}{
		{"facing", "east", "minecraft:oak_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]"},
		{"half", "top", "minecraft:oak_stairs[facing=north,half=top,shape=straight,waterlogged=false]"},
		{"waterlogged", "true", "minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=true]"},
		{"shape", "outer_right", "minecraft:oak_stairs[facing=north,half=bottom,shape=outer_right,waterlogged=false]"},
	}
	for _, tt := range tests {
		got, ok := r.With(s, tt.prop, tt.val)
		if !ok || r.StateString(got) != tt.want {
			t.Errorf("With(%s=%s) = %s", tt.prop, tt.val, r.StateString(got))
		}
	}
	if _, ok := r.With(s, "axis", "x"); ok {
		t.Error("With accepted foreign property")
	}
	if _, ok := r.With(s, "facing", "up"); ok {
		t.Error("With accepted invalid value")
	}
}

func TestAllVersionsRoundTrip(t *testing.T) {
	vers := Versions()
	if len(vers) == 0 {
		t.Fatal("no embedded versions")
	}
	for _, v := range vers {
		r, err := Load(v)
		if err != nil {
			t.Fatal(err)
		}
		for s := StateID(0); int(s) < r.NumStates(); s++ {
			str := r.StateString(s)
			back, ok := r.ParseState(str)
			if !ok || back != s {
				t.Fatalf("%s: %d -> %s -> %d", v, s, str, back)
			}
			b := r.BlockOf(s)
			for p := range b.Props {
				for vi := range b.Props[p].Values {
					n := b.WithIndex(s, p, vi)
					if r.BlockOf(n) != b || b.ValueIndex(n, p) != vi {
						t.Fatalf("%s: WithIndex broke %s", v, str)
					}
				}
			}
		}
	}
}

func TestFlagsAndLight(t *testing.T) {
	tests := []struct {
		state      string
		air, solid bool
		fluid      bool
		emit, filt uint8
		collisionN int
		leaves     bool
	}{
		{"air", true, false, false, 0, 0, 0, false},
		{"cave_air", true, false, false, 0, 0, 0, false},
		{"stone", false, true, false, 0, 15, 1, false},
		{"water", false, false, true, 0, 1, 0, false},
		{"oak_leaves", false, true, false, 0, 1, 1, true},
		{"glowstone", false, true, false, 15, 15, 1, false},
		{"redstone_lamp[lit=true]", false, true, false, 15, 15, 1, false},
		{"redstone_lamp[lit=false]", false, true, false, 0, 15, 1, false},
		{"oak_slab[waterlogged=true]", false, true, true, 0, 0, 1, false},
		{"candle[candles=3,lit=true]", false, false, false, 9, 0, 1, false},
		{"torch", false, false, false, 14, 0, 0, false},
		{"cobweb", false, false, false, 0, 1, 0, false},
	}
	r := Canonical()
	for _, tt := range tests {
		s, ok := r.ParseState(tt.state)
		if !ok {
			t.Fatalf("unknown %s", tt.state)
		}
		if IsAir(s) != tt.air || r.IsSolid(s) != tt.solid || r.HasFluid(s) != tt.fluid || r.IsLeaves(s) != tt.leaves {
			t.Errorf("%s flags = %08b", tt.state, Flags(s))
		}
		if LightEmission(s) != tt.emit || LightFilter(s) != tt.filt {
			t.Errorf("%s light = %d/%d", tt.state, LightEmission(s), LightFilter(s))
		}
		if len(Collision(s)) != tt.collisionN {
			t.Errorf("%s collision = %v", tt.state, Collision(s))
		}
	}
	if !IsAir(Air) || IsAir(StateID(1<<30)) {
		t.Error("IsAir bounds")
	}
	if LightFilter(StateID(1<<30)) != 15 {
		t.Error("invalid state must be opaque")
	}
}

func TestRegistries(t *testing.T) {
	r := Canonical()
	if id, ok := r.ItemByName("minecraft:diamond"); !ok || id != 1012 || r.ItemName(id) != "diamond" {
		t.Errorf("diamond item = %d", id)
	}
	if id, ok := r.ItemByName("poplar_planks"); !ok || id != 72 {
		t.Errorf("poplar_planks = %d", id)
	}
	e, ok := r.EntityByName("player")
	if !ok || e.ID != 159 || e.Width != 0.6 || e.Height != 1.8 {
		t.Errorf("player = %+v", e)
	}
	if b, ok := r.BiomeByName("minecraft:plains"); !ok || r.BiomeName(b) != "plains" {
		t.Error("plains biome")
	}
	if _, ok := r.BiomeByName("dappled_forest"); !ok {
		t.Error("26.3 biome missing")
	}
	for i := 1; i < len(r.Biomes); i++ {
		if r.Biomes[i-1].Name >= r.Biomes[i].Name {
			t.Fatal("biomes not sorted")
		}
	}
}

func BenchmarkWith(b *testing.B) {
	r := Canonical()
	s := MustState("oak_stairs")
	for b.Loop() {
		s, _ = r.With(s, "facing", "east")
	}
}

func BenchmarkFlags(b *testing.B) {
	n := StateID(NumStates())
	var acc int
	for i := 0; b.Loop(); i++ {
		if IsAir(StateID(i) % n) {
			acc++
		}
	}
	_ = acc
}

func TestRemap(t *testing.T) {
	old, err := Load("26.1")
	if err != nil {
		t.Fatal(err)
	}
	cur := Canonical()
	down := NewRemap(cur, old)
	up := NewRemap(old, cur)
	if up.MissingStates != 0 || up.MissingItems != 0 || up.MissingEntities != 0 || up.MissingBiomes != 0 {
		t.Errorf("26.1 -> 26.3 lost entries: %+v", [4]int{up.MissingStates, up.MissingItems, up.MissingEntities, up.MissingBiomes})
	}
	for s := StateID(0); int(s) < old.NumStates(); s++ {
		if down.State(up.State(s)) != s {
			t.Fatalf("roundtrip %s", old.StateString(s))
		}
		if old.StateString(s) != cur.StateString(up.State(s)) {
			t.Fatalf("%s -> %s", old.StateString(s), cur.StateString(up.State(s)))
		}
	}
	poplar := MustState("poplar_log")
	if down.State(poplar) != Air || down.MissingStates == 0 {
		t.Error("26.3-only block should map to air in 26.1")
	}
	if down.State(StateID(1<<30)) != Air {
		t.Error("out of range")
	}
}
