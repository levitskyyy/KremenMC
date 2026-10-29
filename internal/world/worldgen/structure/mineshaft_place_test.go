package structure

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// fakeLevel is the MinePlace scene: x,z -24..24, y -59..-20, stone or air.
type fakeLevel struct {
	m     map[[3]int32]data.StateID
	scene data.StateID
	ents  []string
}

func (f *fakeLevel) MinY() int32 { return -64 }
func (f *fakeLevel) MaxY() int32 { return 320 }
func (f *fakeLevel) Block(x, y, z int32) data.StateID {
	if s, ok := f.m[[3]int32{x, y, z}]; ok {
		return s
	}
	if x >= -24 && x <= 24 && z >= -24 && z <= 24 && y >= -59 && y <= -20 {
		return f.scene
	}
	if y < -59 {
		s, _ := data.Canonical().ParseState("minecraft:dirt")
		return s
	}
	return data.Canonical().Blocks[0].DefaultState
}
func (f *fakeLevel) SetBlock(x, y, z int32, s data.StateID) bool {
	f.m[[3]int32{x, y, z}] = s
	return true
}
func (f *fakeLevel) Height(string, int32, int32) int32                        { return -20 }
func (f *fakeLevel) Biome(int32, int32, int32) data.BiomeID                   { return 0 }
func (f *fakeLevel) SetBlockEntity(int32, int32, int32, string, nbt.Compound) {}
func (f *fakeLevel) AddEntity(e nbt.Compound) {
	p := e["Pos"].(nbt.List).Items
	f.ents = append(f.ents, fmt.Sprint(p[0], " ", p[1], " ", p[2], " ", e["LootTableSeed"]))
}
func (f *fakeLevel) MarkPostProcess(int32, int32, int32) {}

var reInt = regexp.MustCompile(`(\w+):(-?\d+)b?[,}]`)
var reBB = regexp.MustCompile(`BB:\[I;([-0-9,]+)\]`)
var reID = regexp.MustCompile(`id:"([a-z:]+)"`)

// TestMineshaftPlaceProbe replays kremenmc.probe.legacy.MinePlace cases
// (testdata/mineshaft_place.txt): blocks and the random's next long.
func TestMineshaftPlaceProbe(t *testing.T) {
	f, err := os.Open("testdata/mineshaft_place.txt")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	reg := data.Canonical()
	type tcase struct {
		line string
		want map[[3]int32]string
		ents []string
		next string
	}
	var cases []*tcase
	var cur *tcase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "CASE "):
			cur = &tcase{line: l[5:], want: map[[3]int32]string{}}
			cases = append(cases, cur)
		case strings.HasPrefix(l, "B "):
			var x, y, z int32
			var s string
			fmt.Sscan(l[2:], &x, &y, &z, &s)
			cur.want[[3]int32{x, y, z}] = "minecraft:" + s
		case strings.HasPrefix(l, "E minecraft:chest_minecart"):
			f := strings.Fields(l)
			cur.ents = append(cur.ents, f[2]+" "+f[3]+" "+f[4]+" "+strings.TrimSuffix(f[6], "L"))
		case strings.HasPrefix(l, "NEXT "):
			cur.next = l[5:]
		}
	}
	only := os.Getenv("MSCASE")
	okCases := 0
	for ci, c := range cases {
		parts := strings.SplitN(c.line, " ", 3)
		seed, _ := strconv.ParseInt(parts[1], 10, 64)
		snbt := parts[2]
		id := reID.FindStringSubmatch(snbt)[1]
		var bb []int32
		for _, v := range strings.Split(reBB.FindStringSubmatch(snbt)[1], ",") {
			n, _ := strconv.Atoi(v)
			bb = append(bb, int32(n))
		}
		ints := map[string]int32{}
		for _, m := range reInt.FindAllStringSubmatch(snbt, -1) {
			n, _ := strconv.Atoi(m[2])
			ints[m[1]] = int32(n)
		}
		var ents []any
		for _, em := range regexp.MustCompile(`\[I;([-0-9,]+)\]`).FindAllStringSubmatch(snbt, -1)[1:] {
			var e []int32
			for _, v := range strings.Split(em[1], ",") {
				n, _ := strconv.Atoi(v)
				e = append(e, int32(n))
			}
			ents = append(ents, e)
		}
		c2 := nbt.Compound{"Entrances": nbt.List{Type: nbt.TagIntArray, Items: ents}, "id": id, "BB": bb, "O": ints["O"], "GD": ints["GD"], "MST": ints["MST"], "Num": ints["Num"], "D": ints["D"],
			"hr": int8(ints["hr"]), "sc": int8(ints["sc"]), "tf": int8(ints["tf"])}
		pc, err := loadMsPiece(c2)
		if err != nil {
			t.Fatal(err)
		}
		p := pc.(*msPiece)
		scene := reg.Blocks[0].DefaultState
		if parts[0] == "stone" {
			scene, _ = reg.ParseState("minecraft:stone")
		}
		lv := &fakeLevel{m: map[[3]int32]data.StateID{}, scene: scene}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		if parts[0] == "air" {
			// The air scene ran on a server level: cave_air written into an
			// all-air section reads back as air, and shape updates ran.
			// MineScene (raw writes) supersedes these cases.
			continue
		}
		m := newMsPlacer(p, lv, r, Box{-24, -59, -24, 24, -20, 24})
		m.persistSpider = true
		m.run()
		got := map[[3]int32]string{}
		for k, s := range lv.m {
			if s != scene {
				got[k] = reg.StateString(s)
			}
		}
		bad := 0
		norm := func(s string) string {
			if strings.HasPrefix(s, "minecraft:oak_fence") {
				return "minecraft:oak_fence"
			}
			return s
		}
		var diffs []string
		for k, w := range c.want {
			if norm(got[k]) != norm(w) {
				bad++
				diffs = append(diffs, fmt.Sprintf("%v kremenmc %q want %q", k, got[k], w))
			}
		}
		for k, g := range got {
			if _, ok := c.want[k]; !ok {
				bad++
				diffs = append(diffs, fmt.Sprintf("%v kremenmc %q want scene", k, g))
			}
		}
		next := strconv.FormatInt(r.NextLong(), 10)
		if fmt.Sprint(lv.ents) != fmt.Sprint(c.ents) {
			bad++
			diffs = append(diffs, fmt.Sprintf("entities kremenmc %v want %v", lv.ents, c.ents))
		}
		if bad == 0 && next == c.next {
			okCases++
		} else if only == "" || only == strconv.Itoa(ci) {
			if len(diffs) > 6 {
				diffs = diffs[:6]
			}
			t.Logf("case %d %s: %d block diffs, next %v (want %s) %v", ci, c.line[:min(len(c.line), 110)], bad, next == c.next, c.next, diffs)
		}
	}
	t.Logf("mineshaft placement cases identical: %d", okCases)
}
