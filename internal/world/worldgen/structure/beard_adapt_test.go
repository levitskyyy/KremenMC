package structure

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestBeardAdaptationsProbe compares the beard_box, bury and encapsulate
// densities (plus a junction) with the reference Beardifier sampled by
// kremenmc.probe.beard.BeardProbe:
//
//	A -20 -20 -20 30 30 30 R BURY 0 0 0 9 5 7 1 R ENCAPSULATE 4 2 3 15 12 9 0
//	R BEARD_BOX -6 -3 -4 2 6 2 2 J 5 3 5 2 RIGID Q -18 -18 -18 28 28 25
func TestBeardAdaptationsProbe(t *testing.T) {
	f, err := os.Open("testdata/beard/mix.txt.gz")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[3]int32]uint32{}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		var x, y, z int32
		var bits int64
		var v float64
		if n, _ := fmt.Sscan(sc.Text(), &x, &y, &z, &bits, &v); n == 5 {
			want[[3]int32{x, y, z}] = uint32(bits)
		}
	}
	bd := &Beard{rigid: []beardRigid{{Box{0, 0, 0, 9, 5, 7}, 1, "bury"}, {Box{4, 2, 3, 15, 12, 9}, 0, "encapsulate"}, {Box{-6, -3, -4, 2, 6, 2}, 2, "beard_box"}},
		junctions: []Junction{{5, 3, 5, 2, true}}, aff: Box{-20, -20, -20, 30, 30, 30}}
	ok, n := 0, 0
	for x := int32(-18); x <= 28; x++ {
		for y := int32(-18); y <= 28; y++ {
			for z := int32(-18); z <= 25; z++ {
				n++
				if g := math.Float32bits(bd.Value(x, y, z)); g == want[[3]int32{x, y, z}] {
					ok++
				} else if n-ok <= 5 {
					t.Errorf("%d,%d,%d: got %08x want %08x", x, y, z, g, want[[3]int32{x, y, z}])
				}
			}
		}
	}
	t.Logf("%d/%d samples bit-exact", ok, n)
}

type capLevel struct{}

func (capLevel) MinY() int32                                        { return -64 }
func (capLevel) MaxY() int32                                        { return 320 }
func (capLevel) Block(x, y, z int32) data.StateID                   { return data.Canonical().Blocks[0].DefaultState }
func (capLevel) SetBlock(x, y, z int32, s data.StateID) bool        { return true }
func (capLevel) Height(string, int32, int32) int32                  { return 0 }
func (capLevel) Biome(x, y, z int32) data.BiomeID                   { return 0 }
func (capLevel) SetBlockEntity(x, y, z int32, id string, c nbtComp) {}
func (capLevel) AddEntity(e nbtComp)                                {}
func (capLevel) MarkPostProcess(x, y, z int32)                      {}

// TestCappedProbe reproduces kremenmc.probe.jigsaw.CapProbe
// (trail_ruins_roads_archaeology, 40 blocks, every third gravel, world seed
// 0): the changed indices and their loot seeds.
func TestCappedProbe(t *testing.T) {
	pl, err := LoadProcessorList("minecraft:trail_ruins_roads_archaeology")
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	gravel, _ := reg.ParseState("minecraft:gravel")
	stone, _ := reg.ParseState("minecraft:stone")
	cases := []struct {
		o    [3]int32
		idx  [2]int
		seed [2]int64
	}{
		{[3]int32{0, 64, 0}, [2]int{15, 24}, [2]int64{-4909112306499428704, 3209681742443826372}},
		{[3]int32{1, 64, 0}, [2]int{24, 36}, [2]int64{1754470892126679674, 4198926514838755228}},
		{[3]int32{0, 65, 0}, [2]int{12, 18}, [2]int64{6715608276939552753, 182558979247316552}},
		{[3]int32{0, 64, 1}, [2]int{0, 27}, [2]int64{-1782122465151653938, 8896592342151834187}},
		{[3]int32{5, 70, 9}, [2]int{0, 33}, [2]int64{8545002481106398537, 4055908770502803100}},
	}
	for _, c := range cases {
		var es []cappedEntry
		for i := 0; i < 40; i++ {
			s := stone
			if i%3 == 0 {
				s = gravel
			}
			es = append(es, cappedEntry{c.o[0] + int32(i%16), c.o[1] + int32(i/256), c.o[2] + int32(i/16%16), s, nil})
		}
		pl.finalizeCapped(capLevel{}, reg, c.o[0], c.o[1], c.o[2], es)
		var got []string
		for i, e := range es {
			if e.be != nil {
				got = append(got, fmt.Sprintf("%d:%v", i, e.be["LootTableSeed"]))
			}
		}
		w := fmt.Sprintf("[%d:%d %d:%d]", c.idx[0], c.seed[0], c.idx[1], c.seed[1])
		if fmt.Sprint(got) != w {
			t.Errorf("origin %v: got %v want %s", c.o, got, w)
		}
	}
}

// TestAliasProbe reproduces kremenmc.probe.jigsaw.AliasProbe lookups of the
// trial chambers aliases: PoolAliasLookup.create(aliases, pos, seed).
func TestAliasProbe(t *testing.T) {
	f, err := os.Open("testdata/jigsaw/alias_lookup_trial_chambers.txt")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	pk := mustPack(t)
	b, _ := pk.JSON("worldgen/structure", "minecraft:trial_chambers")
	var j struct {
		Aliases jsonRaw `json:"pool_aliases"`
	}
	if err := jsonUnmarshal(b, &j); err != nil {
		t.Fatal(err)
	}
	al, err := parseAliases(j.Aliases)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"melee", "ranged", "slow_ranged", "small_melee"}
	sc := bufio.NewScanner(f)
	n, ok := 0, 0
	for sc.Scan() {
		var x, y, z int32
		var seed int64
		var w [4]string
		if c, _ := fmt.Sscanf(sc.Text(), "L %d %d %d %d %s %s %s %s", &x, &y, &z, &seed, &w[0], &w[1], &w[2], &w[3]); c != 8 {
			continue
		}
		m := resolveAliases(rng.NewLegacy(seed).ForkPositional().At(x, y, z), al)
		n++
		good := true
		for i, k := range keys {
			tgt := m["minecraft:trial_chambers/spawner/contents/"+k]
			if tgt[len(tgt)-len(w[i])-1:] != "/"+w[i] {
				good = false
			}
		}
		if good {
			ok++
		} else if n-ok <= 3 {
			t.Errorf("%d,%d,%d seed %d: got %v want %v", x, y, z, seed, m, w)
		}
	}
	t.Logf("%d/%d lookups identical", ok, n)
}
