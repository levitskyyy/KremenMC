package structure_test

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestLegacyOpsGen (KREMENMC_OPSGEN=log@altlog@minX,minY,maxZ@out) turns a
// full-clip kremenmc.probe.temple.TScene log of a north-facing (O=2)
// legacy piece into its op list in piece-local coordinates (x = wx-minX,
// y = wy-minY, z = maxZ-wz): every write in order, classified by the
// reference call path, plus the potential suspicious sand list. altlog is
// the same run with the other outcome of the region random's boolean, to
// find the writes that depend on it.
func TestLegacyOpsGen(t *testing.T) {
	v := os.Getenv("KREMENMC_OPSGEN")
	if v == "" {
		t.Skip("KREMENMC_OPSGEN not set")
	}
	p := strings.Split(v, "@")
	var x0, y0, z1 int32
	fmt.Sscanf(p[2], "%d,%d,%d", &x0, &y0, &z1)
	ops := legacyOps(t, p[0], x0, y0, z1)
	alt := legacyOps(t, p[1], x0, y0, z1)
	if len(ops) != len(alt) {
		t.Fatalf("op counts differ: %d vs %d", len(ops), len(alt))
	}
	f, err := os.Create(p[3])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	defer zw.Close()
	kinds := map[string]int{}
	for i, o := range ops {
		a := alt[i]
		if o.kind != a.kind || o.pos != a.pos {
			t.Fatalf("op %d differs in kind/position: %v vs %v", i, o, a)
		}
		line := fmt.Sprintf("%s %d %d %d %s", o.kind, o.pos[0], o.pos[1], o.pos[2], o.state)
		switch {
		case o.kind == "R" || o.kind == "P" || o.kind == "BOOL" || o.kind == "M":
			line = fmt.Sprintf("%s %d %d %d", o.kind, o.pos[0], o.pos[1], o.pos[2])
		case o.state != a.state:
			if !o.afterBool {
				t.Fatalf("op %d differs outside the boolean part: %v vs %v", i, o, a)
			}
			// o: boolean false, a: boolean true
			if o.boolVal {
				o, a = a, o
			}
			line = fmt.Sprintf("B %d %d %d %s %s", o.pos[0], o.pos[1], o.pos[2], o.state, a.state)
		}
		kinds[strings.Fields(line)[0]]++
		fmt.Fprintln(zw, line)
	}
	t.Logf("%d ops %v", len(ops), kinds)
}

type legacyOp struct {
	kind      string
	pos       [3]int32
	state     string
	afterBool bool
	boolVal   bool
}

func legacyOps(t *testing.T, path string, x0, y0, z1 int32) []legacyOp {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	loc := func(s string) [3]int32 {
		var x, y, z int32
		fmt.Sscanf(s, "%d,%d,%d", &x, &y, &z)
		return [3]int32{x - x0, y - y0, z1 - z}
	}
	var ops []legacyOp
	var lastF [3]int32
	haveF := false
	afterBool, boolVal := false, false
	moss := false
	for sc.Scan() {
		l := sc.Text()
		at := strings.LastIndex(l, " @")
		path := ""
		if at >= 0 {
			path = l[at+2:]
		}
		fs := strings.Fields(l)
		switch {
		case strings.HasPrefix(l, "STATE 0 getPotentialSuspiciousSandWorldPositions"):
			for _, s := range fs[3:] {
				ops = append(ops, legacyOp{kind: "P", pos: loc(s)})
			}
		case strings.HasPrefix(l, "R0 ") && strings.Contains(l, "MossStoneSelector"):
			moss = true
		case strings.HasPrefix(l, "Q ") && strings.Contains(l, "nextBoolean"):
			afterBool, boolVal = true, strings.Contains(l, "= true")
			ops = append(ops, legacyOp{kind: "BOOL"})
		case strings.HasPrefix(l, "W getBlockState") && strings.HasPrefix(path, "StructurePiece.fillColumnDown"):
			p := loc(fs[2])
			if haveF && p[0] == lastF[0] && p[2] == lastF[2] && p[1] == lastF[1]-1 {
				lastF = p
				continue
			}
			ops = append(ops, legacyOp{kind: "F", pos: p, state: "minecraft:sandstone"})
			lastF, haveF = p, true
		case strings.HasPrefix(l, "W setBlock"):
			p := loc(fs[2])
			st := "minecraft:" + fs[3]
			kind := ""
			switch {
			case strings.Contains(path, "afterPlace"):
				continue
			case strings.HasPrefix(path, "StructurePiece.fillColumnDown"):
				if st != "minecraft:sandstone" {
					t.Fatalf("fillColumnDown wrote %s", st)
				}
				continue
			case strings.HasPrefix(path, "StructurePiece.placeBlock<StructurePiece.generateBox<DesertPyramidPiece.addCellarRoom"):
				kind = "C"
			case strings.HasPrefix(path, "StructurePiece.placeBlock<DesertPyramidPiece.placeCollapsedRoofPiece"):
				kind = "R"
			case strings.HasPrefix(path, "StructurePiece.createChest"):
				kind = "K"
			case strings.Contains(path, "StructurePiece.createDispenser"):
				kind = "D"
			case moss:
				kind = "M"
			case strings.HasPrefix(path, "StructurePiece.placeBlock"):
				kind = "S"
			default:
				t.Fatalf("unknown write path %s", path)
			}
			if moss != (kind == "M") {
				t.Fatalf("moss draw not followed by its write: %s", l)
			}
			moss = false
			o := legacyOp{kind: kind, pos: p, state: st}
			if kind == "M" {
				o.state = ""
			}
			if afterBool && strings.Contains(path, "addCellarStairs") {
				o.afterBool, o.boolVal = true, boolVal
			}
			ops = append(ops, o)
		}
	}
	return ops
}

// TestLegacySceneScramble (KREMENMC_SCRAMBLE=in@out@seed@y0,y1) rewrites
// a TScene input: inside the start's box columns (grown by 2), y0..y1,
// every block becomes a pseudo-random pick of air, fluids, plants, leaves
// and solids, to probe the pieces' replace conditions.
func TestLegacySceneScramble(t *testing.T) {
	v := os.Getenv("KREMENMC_SCRAMBLE")
	if v == "" {
		t.Skip("KREMENMC_SCRAMBLE not set")
	}
	p := strings.Split(v, "@")
	var seed int64
	var y0, y1 int32
	fmt.Sscan(p[2], &seed)
	fmt.Sscanf(p[3], "%d,%d", &y0, &y1)
	s := readLScene(t, p[0])
	reg := data.Canonical()
	picks := []string{"minecraft:air", "minecraft:air", "minecraft:air", "minecraft:water", "minecraft:water[level=3]", "minecraft:lava",
		"minecraft:stone", "minecraft:sand", "minecraft:sandstone", "minecraft:oak_leaves[distance=7,persistent=true,waterlogged=false]",
		"minecraft:short_grass", "minecraft:cobweb", "minecraft:glass", "minecraft:snow[layers=1]", "minecraft:cave_air",
		"minecraft:seagrass", "minecraft:oak_slab[type=bottom,waterlogged=false]", "minecraft:chest[facing=north,type=single,waterlogged=false]", "minecraft:bedrock"}
	if v := os.Getenv("KREMENMC_SCRAMBLE_PICKS"); v != "" {
		picks = strings.Split(v, ";")
	}
	var ids []data.StateID
	for _, n := range picks {
		st, ok := reg.ParseState(n)
		if !ok {
			t.Fatalf("state %s", n)
		}
		ids = append(ids, st)
	}
	r := rng.NewLegacy(seed)
	bb := s.start["Children"].(nbt.List).Items[0].(nbt.Compound)["BB"].([]int32)
	for y := y0; y <= y1; y++ {
		for z := bb[2] - 2; z <= bb[5]+2; z++ {
			for x := bb[0] - 2; x <= bb[3]+2; x++ {
				if s.rg.Contains(x, y, z) {
					s.blocks[s.idx(x, y, z)] = ids[r.NextIntn(int32(len(ids)))]
				}
			}
		}
	}
	// rewrite: copy every line but DATA
	src, err := os.ReadFile(p[0])
	if err != nil {
		t.Fatal(err)
	}
	pal := map[data.StateID]int{}
	var palList []data.StateID
	var rle strings.Builder
	last, run := -1, 0
	for _, b := range s.blocks {
		i, ok := pal[b]
		if !ok {
			i = len(palList)
			pal[b] = i
			palList = append(palList, b)
		}
		if i == last {
			run++
			continue
		}
		if run > 0 {
			fmt.Fprintf(&rle, " %d*%d", last, run)
		}
		last, run = i, 1
	}
	fmt.Fprintf(&rle, " %d*%d", last, run)
	var out strings.Builder
	lines := strings.Split(string(src), "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "PAL "):
			var n int
			fmt.Sscan(l[4:], &n)
			i += n
			fmt.Fprintf(&out, "PAL %d\n", len(palList))
			for _, st := range palList {
				fmt.Fprintln(&out, reg.StateString(st))
			}
		case strings.HasPrefix(l, "DATA"):
			fmt.Fprintf(&out, "DATA%s\n", rle.String())
		case l == "":
		default:
			fmt.Fprintln(&out, l)
		}
	}
	if err := os.WriteFile(p[1], []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
