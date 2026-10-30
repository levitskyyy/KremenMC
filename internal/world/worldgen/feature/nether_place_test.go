package feature

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

var (
	nFloor = [8]string{"netherrack", "crimson_nylium", "warped_nylium", "soul_sand", "soul_soil", "basalt", "blackstone", "crimson_nylium"}
	nCeil  = [4]string{"netherrack", "nether_wart_block", "warped_wart_block", "netherrack"}
	nPlant = [8]string{"fire", "soul_fire", "crimson_roots", "nether_sprouts", "warped_roots", "brown_mushroom", "air", "air"}
	nIsle  = [4]string{"basalt", "netherrack", "blackstone", "magma_block"}
	nFlat  = [4]string{"netherrack", "basalt", "blackstone", "magma_block"}
	nPits  = [8]string{"netherrack", "basalt", "blackstone", "magma_block", "soul_sand", "lava", "basalt", "netherrack"}
	nPitsT = [8]int{-2, -1, -1, -1, -1, 0, 0, 1}
)

// netherScene mirrors kremenmc.probe.feat_nether.Place.scene.
func netherScene(mode string, r int, seed int64, dx, dy, dz int) data.StateID {
	h := envHash(seed, dx, 0, dz)
	switch mode {
	case "air":
		return data.Air
	case "openc", "openw", "openfc", "openfw":
		if dx == 0 && dz == 0 {
			if dy == -1 {
				if strings.HasSuffix(mode, "c") {
					return envState("crimson_nylium")
				}
				return envState("warped_nylium")
			}
			if dy >= 0 {
				return data.Air
			}
		}
		fallthrough
	case "open", "bed":
		f := -1 - int(h>>4&1)
		switch {
		case dy == f+1 && strings.HasPrefix(mode, "openf"):
			return envState(nPlant[h>>20&7])
		case dy > f:
			return data.Air
		case dy == f && mode == "bed" && h>>13&7 == 0:
			return envState("bedrock")
		case dy == f:
			return envState(nFloor[h>>13&7])
		}
		return envState("netherrack")
	case "sea":
		if h>>4&3 == 0 {
			top := int(h >> 6 & 3)
			if top == 3 {
				top = 0
			}
			if dy <= top {
				return envState(nIsle[h>>13&3])
			}
			return data.Air
		}
		if dy <= -2 {
			if dy < -8 {
				return envState("netherrack")
			}
			return envState("lava")
		}
		return data.Air
	case "ceil":
		c := 1 + int(h>>4&1)
		switch {
		case dy == c:
			return envState(nCeil[h>>16&3])
		case dy > c:
			return envState("netherrack")
		case dy == -r:
			return envState("netherrack")
		}
		return data.Air
	case "grasso":
		if dy >= 2 && !(dx == 0 && dz == 0) && envHash(seed, dx, dy, dz)>>4&15 == 0 {
			return envState("stone")
		}
		fallthrough
	case "grass":
		switch {
		case dy >= 0:
			return data.Air
		case dy == -1:
			return envState("grass_block")
		}
		return envState("dirt")
	case "flat":
		switch {
		case dy >= 0:
			return data.Air
		case dy == -1:
			return envState(nFlat[h>>13&3])
		}
		return envState("netherrack")
	case "pits":
		t := nPitsT[h>>4&7]
		switch {
		case dy > t:
			return data.Air
		case dy == t:
			return envState(nPits[h>>13&7])
		case dy == t-1 && h>>20&15 == 0:
			return envState("bedrock")
		}
		return envState("netherrack")
	case "end":
		if dy <= -3+int(h>>4&3) {
			return envState("end_stone")
		}
		return data.Air
	}
	fA, cA := -10+int(h>>4&3), -3+int(h>>6&3)
	fB, cB := 1+int(h>>8&3), 9+int(h>>10&7)
	inA, inB := dy >= fA && dy < cA, dy >= fB && dy < cB
	if inA || inB {
		if inA && dy < fA+2 && h>>18&3 == 0 {
			return envState("lava")
		}
		if envHash(seed, dx, dy, dz)>>4&63 == 0 {
			return envState("netherrack")
		}
		return data.Air
	}
	switch dy {
	case fA - 1, fB - 1:
		return envState(nFloor[h>>13&7])
	case cA, cB:
		return envState(nCeil[h>>16&3])
	}
	return envState("netherrack")
}

type netherRegion struct {
	m, base    map[Pos]data.StateID
	be         map[Pos]nbt.Compound
	ticks      []string
	mode       string
	r          int
	seed       int64
	cx, y0, cz int
}

func (e *netherRegion) SeaLevel() int        { return -63 }
func (e *netherRegion) Dim() world.Dimension { return world.Overworld }
func (e *netherRegion) Block(x, y, z int) data.StateID {
	if s, ok := e.m[Pos{x, y, z}]; ok {
		return s
	}
	return e.baseAt(x, y, z)
}

func (e *netherRegion) baseAt(x, y, z int) data.StateID {
	if s, ok := e.base[Pos{x, y, z}]; ok {
		return s
	}
	dx, dy, dz := x-e.cx, y-e.y0, z-e.cz
	if max(dx, -dx, dy, -dy, dz, -dz) > e.r {
		// The GameTest world around the probe: bedrock, sandstone to y 3.
		switch {
		case y == -64:
			return envState("bedrock")
		case y <= 3:
			return envState("sandstone")
		}
		return data.Air
	}
	return netherScene(e.mode, e.r, e.seed, dx, dy, dz)
}
func (e *netherRegion) SetBlock(x, y, z int, s data.StateID) bool { e.m[Pos{x, y, z}] = s; return true }
func (e *netherRegion) ScheduleTick(x, y, z int, typ string, delay, _ int32, fluid bool) bool {
	k := "block:"
	if fluid {
		k = "fluid:"
	}
	e.ticks = append(e.ticks, fmt.Sprintf("%d,%d,%d=%s%s/%d", x-e.cx, y-e.y0, z-e.cz, k, typ, delay))
	return true
}
func (e *netherRegion) SetBlockEntity(x, y, z int, _ string, d nbt.Compound) bool {
	if e.be == nil {
		e.be = map[Pos]nbt.Compound{}
	}
	e.be[Pos{x, y, z}] = d
	return true
}
func (e *netherRegion) Height(h world.Heightmap, x, z int) int {
	for y := e.y0 + e.r; y >= -64; y-- {
		f := data.Flags(e.Block(x, y, z))
		switch h {
		case world.WorldSurface, world.WorldSurfaceWG:
			if f&data.FlagAir == 0 {
				return y + 1
			}
		default:
			if f&(data.FlagSolid|data.FlagFluid) != 0 {
				return y + 1
			}
		}
	}
	return -64
}
func (e *netherRegion) Biome(int, int, int) (data.BiomeID, bool) {
	b, _ := data.Canonical().BiomeByName("plains")
	return b, true
}

// TestNetherPlace replays testdata/nether_place.txt (tools/refprobe
// kremenmc.probe.feat_nether.Place): blocks written, result and the
// random's next long, per feature.
func TestNetherPlace(t *testing.T) { replayNether(t, "testdata/nether_place.txt") }

func replayNether(t *testing.T, file string) {
	t.Helper()
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
	pass, total, shown := map[string]int{}, map[string]int{}, 0
	for sc.Scan() {
		in, want, ok := strings.Cut(sc.Text(), "\t")
		if !ok || !strings.Contains(in, os.Getenv("KREMENMC_NP_ONLY")) {
			continue
		}
		fs := strings.SplitN(in, " ", 6)
		mode, rad := fs[0], 16
		if m, v, ok := strings.Cut(mode, ":"); ok {
			mode = m
			rad, _ = strconv.Atoi(v)
		}
		y0, _ := strconv.Atoi(fs[1])
		seed, _ := strconv.ParseInt(fs[2], 10, 64)
		x, _ := strconv.Atoi(fs[3])
		z, _ := strconv.Atoi(fs[4])
		js := json.RawMessage(fs[5])
		key := fs[5]
		if len(key) > 60 {
			key = key[:60]
		}
		reg := &netherRegion{m: map[Pos]data.StateID{}, base: map[Pos]data.StateID{}, mode: mode, r: rad, seed: seed, cx: x, y0: y0, cz: z}
		want, ov, _ := strings.Cut(want, "\t")
		for _, tok := range strings.Fields(ov) {
			c, v, _ := strings.Cut(tok, "=")
			var dx, dy, dz int
			fmt.Sscanf(c, "%d,%d,%d", &dx, &dy, &dz)
			st, ok := data.Canonical().ParseState(v)
			if !ok {
				t.Fatalf("override %s", tok)
			}
			reg.base[Pos{x + dx, y0 + dy, z + dz}] = st
		}
		lv := &Level{rg: reg, st: &Stage{}}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		var placed bool
		var probe struct {
			Feature json.RawMessage `json:"feature"`
		}
		if json.Unmarshal(js, &probe) == nil && probe.Feature != nil {
			pf, err := rg.parseInlinePlaced("", js)
			if err != nil {
				t.Errorf("%s: %v", key, err)
				continue
			}
			placed = pf.place(lv, r, Pos{x, y0, z}, nil)
		} else {
			feat, err := rg.parseFeature(js)
			if err != nil {
				t.Errorf("%s: %v", key, err)
				continue
			}
			placed = feat.Place(&placeCtx{lv: lv}, r, Pos{x, y0, z})
		}
		total[key]++
		var b strings.Builder
		fmt.Fprint(&b, placed)
		for dy := -rad; dy <= rad; dy++ {
			for dx := -rad; dx <= rad; dx++ {
				for dz := -rad; dz <= rad; dz++ {
					s, set := reg.m[Pos{x + dx, y0 + dy, z + dz}]
					if set && s != reg.baseAt(x+dx, y0+dy, z+dz) {
						fmt.Fprintf(&b, " %d,%d,%d=%s", dx, dy, dz, normState(data.StateString(s)))
					}
				}
			}
		}
		fmt.Fprintf(&b, " | %d", r.NextLong())
		if len(reg.ticks) > 0 && strings.Contains(want, " ticks ") {
			sort.Strings(reg.ticks)
			b.WriteString(" ticks " + strings.Join(reg.ticks, " "))
		}
		for dy := -rad; dy <= rad; dy++ {
			for dx := -rad; dx <= rad; dx++ {
				for dz := -rad; dz <= rad; dz++ {
					if d, ok := reg.be[Pos{x + dx, y0 + dy, z + dz}]; ok {
						fmt.Fprintf(&b, " be%d,%d,%d=%s", dx, dy, dz, snbt(d))
					}
				}
			}
		}
		if pre, tk, ok := strings.Cut(want, " ticks "); ok {
			ts := strings.Fields(tk)
			var rest []string
			for len(ts) > 0 && strings.HasPrefix(ts[len(ts)-1], "be") {
				rest = append([]string{ts[len(ts)-1]}, rest...)
				ts = ts[:len(ts)-1]
			}
			sort.Strings(ts)
			want = strings.Join(append([]string{pre, "ticks"}, append(ts, rest...)...), " ")
		}
		var wb strings.Builder
		for i, tok := range strings.Fields(want) {
			if i > 0 {
				wb.WriteByte(' ')
			}
			if c, v, ok := strings.Cut(tok, "="); ok && !strings.HasPrefix(tok, "be") {
				tok = c + "=" + normState(v)
			}
			wb.WriteString(tok)
		}
		if b.String() == wb.String() || (strings.HasPrefix(mode, "openf") && plantArtifacts(b.String(), wb.String())) {
			pass[key]++
		} else if dump := os.Getenv("KREMENMC_NP_DUMP"); dump != "" {
			if df, err := os.OpenFile(dump, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintf(df, "%s\nGOT %s\nWANT %s\n", in, b.String(), wb.String())
				df.Close()
			}
		} else if shown < 3 {
			shown++
			t.Errorf("%s\n got  %.1500s\n want %.1500s", in, b.String(), wb.String())
		}
	}
	for k, n := range total {
		t.Logf("%s: %d/%d", k, pass[k], n)
		if pass[k] != n {
			t.Errorf("%s: %d/%d match", k, pass[k], n)
		}
	}
}

// snbt formats the NBT kinds generated block entities use, keys sorted,
// like vanilla's CompoundTag.toString.
func snbt(v any) string {
	switch x := v.(type) {
	case nbt.Compound:
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range ks {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(k + ":" + snbt(x[k]))
		}
		return b.String() + "}"
	case int8:
		return fmt.Sprintf("%db", x)
	case int64:
		return fmt.Sprintf("%dL", x)
	case int32:
		return fmt.Sprint(x)
	case []int32:
		var b strings.Builder
		b.WriteString("[I;")
		for i, n := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprint(&b, n)
		}
		return b.String() + "]"
	}
	return fmt.Sprintf("?%T", v)
}

// TestEndGateway replays testdata/end_gateway.txt (blocks and block
// entity NBT).
func TestEndGateway(t *testing.T) { replayNether(t, "testdata/end_gateway.txt") }

// TestNetherLayer replays testdata/nether_layer.txt: count_on_every_layer.
func TestNetherLayer(t *testing.T) { replayNether(t, "testdata/nether_layer.txt") }

// TestNetherSpread replays testdata/nether_spread.txt: glowstone_extra
// (random_neighbor_spread; three with the origin inside the ceiling) and
// weeping_vines (spread + block_column).
func TestNetherSpread(t *testing.T) { replayNether(t, "testdata/nether_spread.txt") }

// TestNetherBlobs replays testdata/nether_blobs.txt: basalt_blobs and
// blackstone_blobs (netherrack_replace_blobs).
func TestNetherBlobs(t *testing.T) { replayNether(t, "testdata/nether_blobs.txt") }

// plantArtifacts accepts a plant-floor scene result that differs from the
// probe's only where the reference ServerLevel's neighbour updates popped
// floor plants or turned fire into soul fire (a WorldGenRegion does
// neither): same result and final random, every block we wrote is in the
// reference diff, and the reference's extra cells are air or fire.
func plantArtifacts(got, want string) bool {
	g, w := strings.Fields(got), strings.Fields(want)
	if len(g) < 2 || len(w) < 2 || g[0] != w[0] || g[len(g)-1] != w[len(w)-1] {
		return false
	}
	ws := map[string]bool{}
	for _, t := range w {
		ws[t] = true
	}
	gs := map[string]bool{}
	for _, t := range g {
		if !ws[t] {
			return false
		}
		gs[t] = true
	}
	for _, t := range w {
		if gs[t] {
			continue
		}
		_, v, _ := strings.Cut(t, "=")
		if v != "minecraft:air" && v != "air" && !strings.Contains(v, "fire") {
			return false
		}
	}
	return true
}
