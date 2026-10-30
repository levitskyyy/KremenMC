package feature

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// carpetScene mirrors the "carpet" mode of kremenmc.probe.feat_geode.Geode.
func carpetScene(seed int64, dx, dy, dz int) data.StateID {
	h := func(a, b, c int) uint32 {
		v := int32(a)*73856093 ^ int32(b)*19349663 ^ int32(c)*83492791 ^ int32(seed)
		v ^= int32(uint32(v) >> 13)
		v *= 0x5bd1e995
		v ^= int32(uint32(v) >> 15)
		return uint32(v)
	}
	name := "air"
	switch {
	case dy < -1:
		name = "stone"
	case dy == -1:
		if h(dx, 7, dz)&15 != 0 {
			name = "pale_moss_block"
		}
	case dx == 0 && dz == 0:
		if dy == 1 {
			name = [8]string{"stone", "short_grass", "pale_oak_leaves", "air", "air", "air", "air", "air"}[h(0, 9, 0)&7]
		}
	case dy <= int(h(dx, 1, dz)>>4&3):
		k := h(dx, 0, dz) >> 4 & 15
		switch {
		case k <= 1:
			name = "stone"
		case k == 2:
			name = "pale_oak_log"
		case k == 3:
			name = "pale_oak_leaves"
		case k == 4:
			name = "glass"
		case k == 5:
			name = "oak_fence"
		case k == 6 && dy == 0:
			name = "pale_moss_carpet"
		case k == 7 && dy == 0:
			name = "short_grass"
		}
	}
	s, _ := data.DefaultState(name)
	return s
}

type carpetRegion struct {
	geodeRegion
}

func (c *carpetRegion) Block(x, y, z int) data.StateID {
	if s, ok := c.m[Pos{x, y, z}]; ok {
		return s
	}
	return carpetScene(c.seed, x-c.ox, y-c.oy, z-c.oz)
}

// scriptedBools replays logged nextBoolean results.
type scriptedBools struct {
	*rng.Xoroshiro
	v    []bool
	over int
}

func (s *scriptedBools) NextBool() bool {
	if len(s.v) == 0 {
		s.over++
		return false
	}
	b := s.v[0]
	s.v = s.v[1:]
	return b
}

// TestMossyCarpet replays testdata/mossy_carpet.txt (kremenmc.probe.feat_geode.Geode
// carpet mode with FEAT_LOG: level-random draws are replayed).
func TestMossyCarpet(t *testing.T) {
	file := "testdata/mossy_carpet.txt"
	if f := os.Getenv("CARPET"); f != "" {
		file = f
	}
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	const box = 4
	var draws []bool
	pass, total := 0, 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "C ") {
			draws = nil
			continue
		}
		if strings.HasPrefix(line, "R ") && strings.Contains(line, "@MossyCarpetBlock") {
			draws = append(draws, strings.Fields(line)[4] == "true")
			continue
		}
		in, want, ok := strings.Cut(line, "\t")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.SplitN(in, " ", 6)
		y0, _ := strconv.Atoi(fs[1])
		seed, _ := strconv.ParseInt(fs[2], 10, 64)
		x, _ := strconv.Atoi(fs[3])
		z, _ := strconv.Atoi(fs[4])
		feat, err := rg.parseFeature(json.RawMessage(fs[5]))
		if err != nil {
			t.Fatal(err)
		}
		total++
		reg := &carpetRegion{geodeRegion{m: map[Pos]data.StateID{}, seed: seed, ox: x, oy: y0, oz: z}}
		lv := &Level{rg: reg, st: &Stage{}}
		lv.rr = &scriptedBools{Xoroshiro: rng.NewXoroshiro(0), v: append([]bool(nil), draws...)}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		placed := feat.Place(&placeCtx{lv: lv}, r, Pos{x, y0, z})
		var b strings.Builder
		fmt.Fprint(&b, placed)
		for dy := -box; dy <= box; dy++ {
			for dx := -box; dx <= box; dx++ {
				for dz := -box; dz <= box; dz++ {
					s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
					if set && s != carpetScene(seed, dx, dy, dz) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		sb := lv.rr.(*scriptedBools)
		if left := len(sb.v) - sb.over; b.String() == norm(want) && left == 0 {
			pass++
		} else {
			t.Errorf("%s\n got  %s (%d draws left)\n want %s", in[:20], b.String(), left, norm(want))
		}
	}
	t.Logf("%d/%d", pass, total)
}
