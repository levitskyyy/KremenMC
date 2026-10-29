package structure

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

type mapLevel struct {
	blocks map[[3]int32]data.StateID
	be     map[[3]int32]nbt.Compound
	air    data.StateID
}

func (l *mapLevel) MinY() int32 { return -64 }
func (l *mapLevel) MaxY() int32 { return 320 }
func (l *mapLevel) Block(x, y, z int32) data.StateID {
	if s, ok := l.blocks[[3]int32{x, y, z}]; ok {
		return s
	}
	return l.air
}
func (l *mapLevel) SetBlock(x, y, z int32, s data.StateID) bool {
	l.blocks[[3]int32{x, y, z}] = s
	return true
}
func (l *mapLevel) Height(string, int32, int32) int32      { return 0 }
func (l *mapLevel) Biome(int32, int32, int32) data.BiomeID { return 0 }
func (l *mapLevel) SetBlockEntity(x, y, z int32, id string, c nbt.Compound) {
	l.be[[3]int32{x, y, z}] = c
}
func (l *mapLevel) AddEntity(nbt.Compound)              {}
func (l *mapLevel) MarkPostProcess(int32, int32, int32) {}

var colRe = regexp.MustCompile(`col(-?\d),(-?\d):(\S*)`)

// TestBuriedProbe replays the black-box buried treasure cases
// (testdata/buried_probe.txt from probe structlegacy.Buried): same block
// writes, chest loot seed, box and random state after placement.
func TestBuriedProbe(t *testing.T) {
	f, err := os.Open("testdata/buried_probe.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reg := data.Canonical()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	n, bad := 0, 0
	for sc.Scan() {
		line := sc.Text()
		setup, res, _ := strings.Cut(line, " =>")
		var seed int64
		fmt.Sscanf(setup, "case %d", &seed)
		c := int32(seed - 1000)
		if strings.Contains(line, "box=BoundingBox{minX=9,") {
			c = 0
		}
		air, _ := reg.DefaultState("air")
		lv := &mapLevel{blocks: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}, air: air}
		m := regexp.MustCompile(`minX=(-?\d+)`).FindStringSubmatch(res)
		x, _ := strconv.Atoi(m[1])
		z := int32(9)
		for _, cm := range colRe.FindAllStringSubmatch(setup, -1) {
			dx, _ := strconv.Atoi(cm[1])
			dz, _ := strconv.Atoi(cm[2])
			segs := strings.Split(strings.TrimSuffix(cm[3], ";"), ";")
			for i, sg := range segs {
				ys, st, _ := strings.Cut(sg, "=")
				y0, _ := strconv.Atoi(ys)
				y1 := 120
				if i+1 < len(segs) {
					y1, _ = strconv.Atoi(strings.SplitN(segs[i+1], "=", 2)[0])
				}
				s, ok := reg.ParseState("minecraft:" + st)
				if !ok {
					t.Fatalf("state %s", st)
				}
				for y := y0; y < y1; y++ {
					lv.blocks[[3]int32{int32(x + dx), int32(y), z + int32(dz)}] = s
				}
			}
		}
		before := map[[3]int32]data.StateID{}
		for k, v := range lv.blocks {
			before[k] = v
		}
		_ = c
		p := newBuriedPiece(int32(x), 90, z)
		r := rng.NewWorldgenRandom(rng.NewLegacy(seed))
		cx := int32(x) >> 4
		clip := Box{cx << 4, -64, 0, cx<<4 + 15, 319, 15}
		if err := p.Place(lv, r, clip, cx, 0); err != nil {
			t.Fatal(err)
		}
		var got []string
		keys := make([][3]int32, 0)
		for k := range lv.blocks {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if a[0] != b[0] {
				return a[0] < b[0]
			}
			if a[2] != b[2] {
				return a[2] < b[2]
			}
			return a[1] < b[1]
		})
		for _, k := range keys {
			if before[k] != lv.blocks[k] {
				s := fmt.Sprintf("%d,%d,%d=%s", k[0]-int32(x), k[1], k[2]-z, strings.TrimPrefix(reg.StateString(lv.blocks[k]), "minecraft:"))
				if be, ok := lv.be[k]; ok {
					s += fmt.Sprintf(" loot@%d=%s/%d", k[1], be["LootTable"], be["LootTableSeed"])
				}
				got = append(got, s)
			}
		}
		b := p.Box()
		got = append(got, fmt.Sprintf("box=BoundingBox{minX=%d, minY=%d, minZ=%d, maxX=%d, maxY=%d, maxZ=%d} next=%d", b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ, r.NextLong()))
		wantF := strings.Fields(res)
		gotF := strings.Fields(strings.Join(got, " "))
		sort.Strings(wantF)
		sort.Strings(gotF)
		n++
		if strings.Join(wantF, " ") != strings.Join(gotF, " ") {
			bad++
			if bad <= 8 {
				t.Errorf("case %d:\n want %v\n got  %v", seed, wantF, gotF)
			}
		}
	}
	t.Logf("%d/%d cases match", n-bad, n)
}
