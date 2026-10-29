package structure_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

var hutSamples = [][2]int32{{22, -348}, {-336, -171}, {-122, -181}, {-114, 19}}

func TestSwampHutRefDump(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	reg := data.Canonical()
	for _, c := range hutSamples {
		st := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]
		if st == nil {
			t.Logf("no ref start at %v", c)
			continue
		}
		p := st["Children"].(nbt.List).Items[0].(nbt.Compound)
		t.Logf("start %v: %v", c, p)
		bb := p["BB"].([]int32)
		ch, _ := refChunk(t, "0-overworld", c[0], c[1])
		for y := bb[1] - 4; y <= bb[4]; y++ {
			line := ""
			for z := bb[2]; z <= bb[5]; z++ {
				for x := bb[0]; x <= bb[3]; x++ {
					line += fmt.Sprintf("%-14.14s", trimMC(reg.StateString(ch.Block(int(x&15), int(y), int(z&15)))))
				}
				line += "|"
			}
			t.Logf("y=%d %s", y, line)
		}
		dir := "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/entities"
		if r, err := anvil.OpenRegionReadOnly(dir, c[0]>>5, c[1]>>5); err == nil {
			if b, err := r.Read(int(c[0]&31), int(c[1]&31)); err == nil {
				_, root, _, _ := nbt.Unmarshal(b, false)
				if l, ok := root["Entities"].(nbt.List); ok {
					for _, e := range l.Items {
						ec := e.(nbt.Compound)
						t.Logf("entity %v %v persist=%v variant=%v collar=%v rot=%v", ec["id"], ec["Pos"], ec["PersistenceRequired"], ec["variant"], ec["CollarColor"], ec["Rotation"])
					}
				}
			}
			r.Close()
		}
	}
}

func trimMC(s string) string {
	if len(s) > 10 && s[:10] == "minecraft:" {
		return s[10:]
	}
	return s
}

// hutWorld maps a swamp hut local position to world x,z (inferred from the
// witch positions of 4 reference huts, one per orientation).
func hutWorld(o int32, bb []int32, x, z int32) (int32, int32) {
	switch o {
	case 2: // north
		return bb[0] + x, bb[5] - z
	case 0: // south
		return bb[0] + x, bb[2] + z
	case 3: // east
		return bb[0] + z, bb[2] + x
	default: // west
		return bb[3] - z, bb[2] + x
	}
}

func TestSwampHutLayoutDump(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	reg := data.Canonical()
	lay := make([]map[[3]int32]string, 0)
	for _, c := range hutSamples {
		p := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]["Children"].(nbt.List).Items[0].(nbt.Compound)
		bb := p["BB"].([]int32)
		o := p["O"].(int32)
		ch, _ := refChunk(t, "0-overworld", c[0], c[1])
		m := map[[3]int32]string{}
		for y := int32(-4); y <= 6; y++ {
			for z := int32(0); z < 9; z++ {
				for x := int32(0); x < 7; x++ {
					wx, wz := hutWorld(o, bb, x, z)
					m[[3]int32{x, y, z}] = trimMC(reg.StateString(ch.Block(int(wx&15), int(bb[1]+y), int(wz&15))))
				}
			}
		}
		lay = append(lay, m)
	}
	for y := int32(-4); y <= 6; y++ {
		for z := int32(0); z < 9; z++ {
			line := ""
			for x := int32(0); x < 7; x++ {
				k := [3]int32{x, y, z}
				v := lay[0][k]
				same := true
				for _, l := range lay[1:] {
					if l[k] != v {
						same = false
					}
				}
				if same {
					line += fmt.Sprintf(" %-32.32s", v)
				} else {
					line += fmt.Sprintf(" *%s/%s/%s/%s", lay[0][k], lay[1][k], lay[2][k], lay[3][k])
				}
			}
			fmt.Printf("y=%d z=%d%s\n", y, z, line)
		}
	}
}

// TestSwampHutStarts compares the swamp hut start generator (unregistered:
// placement is missing) with the 4 reference starts of seed 0, one per
// orientation: xz box and saved orientation.
func TestSwampHutStarts(t *testing.T) {
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	match, total := 0, 0
	for _, c := range hutSamples {
		st := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]
		if st == nil {
			continue
		}
		total++
		w := st["Children"].(nbt.List).Items[0].(nbt.Compound)
		r := rng.NewWorldgenRandom(rng.NewLegacy(0))
		r.SetLargeFeatureSeed(0, c[0], c[1])
		ps, err := structure.SwampHutTypeForTest().Generate(&structure.GenContext{Seed: 0, ChunkX: c[0], ChunkZ: c[1], Random: r,
			Height: func(x, z int32, hm string) int32 { return 64 }})
		if err != nil || len(ps) != 1 {
			t.Fatalf("%v: %v %v", c, ps, err)
		}
		g := ps[0].Save()
		wb, gb := w["BB"].([]int32), g["BB"].([]int32)
		if g["O"] == w["O"] && g["id"] == w["id"] && wb[0] == gb[0] && wb[2] == gb[2] && wb[3] == gb[3] && wb[5] == gb[5] {
			match++
		} else {
			t.Errorf("%v: kremenmc %v, reference %v", c, g, w)
		}
	}
	if total == 0 {
		t.Skip("no reference swamp huts")
	}
	t.Logf("swamp hut starts (xz box, orientation) matching the reference: %d/%d", match, total)
}

func TestSwampHutHeightDump(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	tr, _ := vanilla.NewTerrain("overworld", 0)
	stages, _ := tr.Stages("overworld", 0)
	p, _ := gen.New(gen.Config{Seed: 0, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	reg := data.Canonical()
	for _, c := range hutSamples {
		w := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]["Children"].(nbt.List).Items[0].(nbt.Compound)
		bb := w["BB"].([]int32)
		pc, err := p.Get(context.Background(), world.ChunkPos{X: c[0], Z: c[1]}, gen.StatusTerrain)
		if err != nil {
			t.Fatal(err)
		}
		var sumAir, sumSolid, n int
		row := ""
		for z := bb[2]; z <= bb[5]; z++ {
			for x := bb[0]; x <= bb[3]; x++ {
				y := 300
				for ; y > -64 && reg.IsAir(pc.Chunk.Block(int(x&15), y, int(z&15))); y-- {
				}
				ys := y
				for ; ys > -64; ys-- {
					b := reg.BlockOf(pc.Chunk.Block(int(x&15), ys, int(z&15)))
					if b.Name != "water" && !reg.IsAir(pc.Chunk.Block(int(x&15), ys, int(z&15))) {
						break
					}
				}
				sumAir += y + 1
				sumSolid += ys + 1
				n++
				row += fmt.Sprintf(" %d/%d", y+1, ys+1)
			}
		}
		t.Logf("%v HPos=%v n=%d avgSurface=%.3f avgFloor=%.3f %s", c, w["HPos"], n, float64(sumAir)/float64(n), float64(sumSolid)/float64(n), row)
	}
}

// hutOrient is the mirror and rotation that turn the north hut's world
// states into each orientation's (inferred from the 4 reference huts).
var hutOrient = map[int32]struct {
	m Mirror_
	r structure.Rotation
}{2: {structure.MirrorNone, structure.RotNone}, 0: {structure.MirrorLeftRight, structure.RotNone},
	3: {structure.MirrorNone, structure.Rot90}, 1: {structure.MirrorLeftRight, structure.Rot90}}

type Mirror_ = structure.Mirror

// TestSwampHutConsensus builds the hut layout from the north reference
// hut and checks it against the other three orientations.
func TestSwampHutConsensus(t *testing.T) {
	if os.Getenv("KREMENMC_DUMP") == "" {
		t.Skip("KREMENMC_DUMP not set")
	}
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	reg := data.Canonical()
	hutBlocks := map[string]bool{"spruce_planks": true, "oak_log": true, "spruce_stairs": true, "oak_fence": true, "potted_red_mushroom": true,
		"crafting_table": true, "cauldron": true, "flower_pot": true}
	type smp struct {
		o  int32
		bb []int32
		ch interface {
			Block(x, y, z int) data.StateID
		}
	}
	var ss []smp
	for _, c := range hutSamples {
		w := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]["Children"].(nbt.List).Items[0].(nbt.Compound)
		ch, _ := refChunk(t, "0-overworld", c[0], c[1])
		ss = append(ss, smp{w["O"].(int32), w["BB"].([]int32), ch})
	}
	at := func(s smp, x, y, z int32) data.StateID {
		wx, wz := hutWorld(s.o, s.bb, x, z)
		return s.ch.Block(int(wx&15), int(s.bb[1]+y), int(wz&15))
	}
	var out []string
	bad := 0
	for y := int32(0); y <= 6; y++ {
		for z := int32(0); z < 9; z++ {
			for x := int32(0); x < 7; x++ {
				n := at(ss[0], x, y, z)
				nb := reg.BlockOf(n)
				allAir, agree := true, true
				for _, s := range ss {
					g := at(s, x, y, z)
					if !reg.IsAir(g) {
						allAir = false
					}
					tr := hutOrient[s.o]
					if structure.TransformState(n, tr.m, tr.r) != g {
						agree = false
					}
				}
				switch {
				case hutBlocks[nb.Name] && agree:
					out = append(out, fmt.Sprintf("%d %d %d %s", x, y, z, reg.StateString(n)))
				case hutBlocks[nb.Name]:
					bad++
					var vs []string
					for _, s := range ss {
						vs = append(vs, reg.StateString(at(s, x, y, z)))
					}
					t.Logf("disagree %d,%d,%d: %v", x, y, z, vs)
				case allAir && y >= 1:
					out = append(out, fmt.Sprintf("%d %d %d minecraft:air", x, y, z))
				}
			}
		}
	}
	t.Logf("layout entries %d, disagreements %d", len(out), bad)
	if f := os.Getenv("KREMENMC_HUT_OUT"); f != "" {
		os.WriteFile(f, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	}
}

// TestSwampHutReference runs the pipeline to full on the 4 reference hut
// chunks of seed 0 and compares every block of the hut's box (and the leg
// columns below it) with the reference, plus the witch and cat.
func TestSwampHutReference(t *testing.T) {
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/0-overworld/world/dimensions/minecraft/overworld/region")
	tr, err := vanilla.NewTerrain("overworld", 0)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, 0)
	if err != nil {
		t.Fatal(err)
	}
	stages, _ := tr.Stages("overworld", 0)
	p, _ := gen.New(gen.Config{Seed: 0, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	reg := data.Canonical()
	hut := map[string]bool{"spruce_planks": true, "oak_log": true, "spruce_stairs": true, "oak_fence": true, "potted_red_mushroom": true,
		"crafting_table": true, "cauldron": true}
	for _, c := range hutSamples {
		rs := ref[fmt.Sprintf("minecraft:swamp_hut@%d,%d", c[0], c[1])]
		if rs == nil {
			continue
		}
		w := rs["Children"].(nbt.List).Items[0].(nbt.Compound)
		st, err := ss.StartAt(c[0], c[1])
		if err != nil || st == nil || st.ID != "minecraft:swamp_hut" {
			t.Errorf("%v: kremenmc start %v %v", c, st, err)
			continue
		}
		g := st.Pieces[0].Save()
		for _, k := range []string{"id", "O", "BB", "HPos", "Width", "Height", "Depth"} {
			if fmt.Sprint(g[k]) != fmt.Sprint(w[k]) {
				t.Errorf("%v %s: kremenmc %v, reference %v", c, k, g[k], w[k])
			}
		}
		pc, err := p.Get(context.Background(), world.ChunkPos{X: c[0], Z: c[1]}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		rc, _ := refChunk(t, "0-overworld", c[0], c[1])
		bb := w["BB"].([]int32)
		same, total, hutSame, hutTotal := 0, 0, 0, 0
		for y := bb[1] - 6; y <= bb[4]; y++ {
			for z := bb[2]; z <= bb[5]; z++ {
				for x := bb[0]; x <= bb[3]; x++ {
					ws, gs := rc.Block(int(x&15), int(y), int(z&15)), pc.Chunk.Block(int(x&15), int(y), int(z&15))
					total++
					if ws == gs {
						same++
					} else if os.Getenv("KREMENMC_DUMP") != "" {
						t.Logf("%v other %d,%d,%d: kremenmc %s, reference %s", c, x, y, z, reg.StateString(gs), reg.StateString(ws))
					}
					if hut[reg.BlockOf(ws).Name] || hut[reg.BlockOf(gs).Name] {
						hutTotal++
						if ws == gs {
							hutSame++
						} else {
							t.Logf("%v %d,%d,%d: kremenmc %s, reference %s", c, x, y, z, reg.StateString(gs), reg.StateString(ws))
						}
					}
				}
			}
		}
		t.Logf("%v: hut blocks %d/%d, all blocks of the box and 6 below %d/%d", c, hutSame, hutTotal, same, total)
		if hutSame != hutTotal {
			t.Errorf("%v: hut blocks differ", c)
		}
		ents := map[string]string{}
		for _, e := range pc.Entities {
			ents[fmt.Sprint(e["id"])] = fmt.Sprint(e["Pos"])
		}
		t.Logf("%v: kremenmc entities %v", c, ents)
		if len(ents) != 2 {
			t.Errorf("%v: entities %v", c, ents)
		}
	}
}
