package feature

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

//go:embed testdata/block_pile_ref.txt
var blockPileRef string

// pileRegion is the probe's world: stone below y 0, air above, plus a
// layer of one block.
type pileRegion struct{ m map[Pos]data.StateID }

func (m *pileRegion) Dim() world.Dimension { return world.Overworld }
func (m *pileRegion) Block(x, y, z int) data.StateID {
	if s, ok := m.m[Pos{x, y, z}]; ok {
		return s
	}
	if y < 0 && y >= -4 {
		s, _ := data.DefaultState("stone")
		return s
	}
	return data.Air
}
func (m *pileRegion) SetBlock(x, y, z int, s data.StateID) bool { m.m[Pos{x, y, z}] = s; return true }
func (m *pileRegion) Height(world.Heightmap, int, int) int      { return 0 }
func (m *pileRegion) Biome(int, int, int) (data.BiomeID, bool)  { return 0, true }

// TestBlockPileReference replays reference BlockPileFeature runs
// (kremenmc.probe.jigsaw.PilePlace): placed blocks and the number of draws.
func TestBlockPileReference(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pf, err := rg.placedByKey("minecraft:pile_hay")
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	n := 0
	for _, line := range strings.Split(blockPileRef, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		seed, _ := strconv.ParseInt(f[0], 10, 64)
		draws, _ := strconv.Atoi(f[2])
		m := &pileRegion{m: map[Pos]data.StateID{}}
		if f[1] != "-" {
			b, y, _ := strings.Cut(f[1], "@y")
			st, ok := reg.ParseState(b)
			if !ok {
				t.Fatalf("state %s", b)
			}
			yy, _ := strconv.Atoi(y)
			for x := -4; x <= 4; x++ {
				for z := -4; z <= 4; z++ {
					m.m[Pos{x, yy, z}] = st
				}
			}
		}
		before := map[Pos]data.StateID{}
		for k, v := range m.m {
			before[k] = v
		}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		lv := &Level{rg: m}
		pf.Feature.Place(&placeCtx{lv: lv}, r, Pos{0, 0, 0})
		var got []string
		for p, s := range m.m {
			if before[p] != s {
				got = append(got, fmt.Sprintf("%d,%d,%d=%s", p.X, p.Y, p.Z, strings.TrimPrefix(reg.StateString(s), "minecraft:")))
			}
		}
		want := append([]string(nil), f[3:]...)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("seed %s %s: placed\n got  %v\n want %v", f[0], f[1], got, want)
		}
		next := r.NextInt()
		ref := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		k := 0
		for ; k < 1000 && ref.NextInt() != next; k++ {
		}
		if k != draws {
			t.Errorf("seed %s %s: %d draws, reference %d", f[0], f[1], k, draws)
		}
		n++
	}
	t.Logf("%d reference runs", n)
}

// TestBlockPileSeedSearch looks for the random a reference pile was placed
// with (KREMENMC_PILESEARCH=world@x,y,z@worldseed): decoration seeds of the
// pile's chunk for every structure index and step, each advanced by up to
// 3000 draws.
func TestBlockPileSeedSearch(t *testing.T) {
	v := os.Getenv("KREMENMC_PILESEARCH")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var px, py, pz int
	var seed int64
	fmt.Sscanf(parts[1], "%d,%d,%d", &px, &py, &pz)
	fmt.Sscan(parts[2], &seed)
	dir := "../../../../.refcache/26.3/worlds/" + parts[0] + "/world/dimensions/minecraft/overworld/region"
	chunks := map[[2]int]*world.Chunk{}
	get := func(x, y, z int) data.StateID {
		k := [2]int{x >> 4, z >> 4}
		c, ok := chunks[k]
		if !ok {
			r, err := anvil.OpenRegionReadOnly(dir, int32(k[0]>>5), int32(k[1]>>5))
			if err == nil {
				b, err := r.Read(k[0]&31, k[1]&31)
				r.Close()
				if err == nil {
					_, root, _, _ := nbt.Unmarshal(b, false)
					c, _, _ = anvil.DecodeChunk(root, world.Overworld)
				}
			}
			chunks[k] = c
		}
		if c == nil {
			return data.Air
		}
		return c.Block(x&15, y, z&15)
	}
	reg := data.Canonical()
	hay, _ := reg.BlockByName("hay_block")
	want := map[Pos]data.StateID{}
	base := map[Pos]data.StateID{}
	for x := px - 4; x <= px+4; x++ {
		for z := pz - 4; z <= pz+4; z++ {
			for y := py - 1; y <= py+2; y++ {
				s := get(x, y, z)
				if reg.BlockOf(s) == hay {
					want[Pos{x, y, z}] = s
					s = data.Air
				}
				base[Pos{x, y, z}] = s
			}
		}
	}
	rg, _ := theRegistry()
	pf, _ := rg.placedByKey("minecraft:pile_hay")
	cx, cz := px>>4, pz>>4
	for idx := 0; idx < 60; idx++ {
		for step := 0; step < 11; step++ {
			r0 := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
			deco := r0.SetDecorationSeed(seed, int32(cx<<4), int32(cz<<4))
			for k := 0; k < 3000; k++ {
				r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
				r.SetFeatureSeed(deco, int32(idx), int32(step))
				for range k {
					r.NextInt()
				}
				m := &mapLevel{m: map[Pos]data.StateID{}, base: base}
				pf.Feature.Place(&placeCtx{lv: &Level{rg: m}}, r, Pos{px, py, pz})
				ok := len(m.m) == len(want)
				for p, s := range m.m {
					if want[p] != s {
						ok = false
						break
					}
				}
				if ok {
					t.Logf("match: index %d step %d skip %d", idx, step, k)
				}
			}
		}
	}
}

type mapLevel struct {
	m    map[Pos]data.StateID
	base map[Pos]data.StateID
}

func (m *mapLevel) Dim() world.Dimension { return world.Overworld }
func (m *mapLevel) Block(x, y, z int) data.StateID {
	if s, ok := m.m[Pos{x, y, z}]; ok {
		return s
	}
	return m.base[Pos{x, y, z}]
}
func (m *mapLevel) SetBlock(x, y, z int, s data.StateID) bool { m.m[Pos{x, y, z}] = s; return true }
func (m *mapLevel) Height(world.Heightmap, int, int) int      { return 0 }
func (m *mapLevel) Biome(int, int, int) (data.BiomeID, bool)  { return 0, true }
