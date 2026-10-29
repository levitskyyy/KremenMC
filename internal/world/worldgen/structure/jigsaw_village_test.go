package structure_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

var villageKeys = []string{"minecraft:village_plains", "minecraft:village_desert", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga"}

// TestJigsawFindVillages lists kremenmc's village starts near the origin
// (KREMENMC_VFIND=1), to pick reference chunks.
func TestJigsawFindVillages(t *testing.T) {
	if os.Getenv("KREMENMC_VFIND") == "" {
		t.Skip("set KREMENMC_VFIND=1")
	}
	structure.EnableJigsaw(villageKeys...)
	seed := int64(refSeed)
	if v := os.Getenv("KREMENMC_VSEED"); v != "" {
		seed, _ = strconv.ParseInt(v, 10, 64)
	}
	only := os.Getenv("KREMENMC_VONLY")
	tr, _ := vanilla.NewTerrain("overworld", seed)
	ss, _ := vanilla.StructureStagesForTest(tr, seed)
	seen := map[string]int{}
	for cz := int32(-96); cz < 96; cz++ {
		for cx := int32(-96); cx < 96; cx++ {
			st, err := ss.StartAt(cx, cz)
			if err != nil {
				t.Fatal(err)
			}
			if st != nil && strings.Contains(st.ID, "village") && strings.Contains(st.ID, only) && seen[st.ID] < 3 {
				seen[st.ID]++
				t.Logf("%s@%d,%d %d pieces box %v", st.ID, cx, cz, len(st.Pieces), st.Box())
			}
		}
	}
}

// TestJigsawBoxes relates each reference terrain_matching piece's saved box
// to the terrain under it (KREMENMC_VBOX=id@cx,cz).
func TestJigsawBoxes(t *testing.T) {
	want := os.Getenv("KREMENMC_VBOX")
	if want == "" {
		t.Skip("set KREMENMC_VBOX")
	}
	ref := refStarts(t, refWorldRegion)
	c := ref[want]
	tr, _ := vanilla.NewTerrain("overworld", refSeed)
	ss, _ := vanilla.StructureStagesForTest(tr, refSeed)
	for i, it := range c["Children"].(nbt.List).Items {
		p := it.(nbt.Compound)
		pe, _ := p["pool_element"].(nbt.Compound)
		if pe["projection"] != "terrain_matching" {
			continue
		}
		bb := p["BB"].([]int32)
		sz := structure.TemplateSize(pe["location"].(string))
		lo, hi := int32(1000), int32(-1000)
		for x := bb[0]; x <= bb[3]; x++ {
			for z := bb[2]; z <= bb[5]; z++ {
				h := ss.ProbeHeight(x, z) - 1
				lo, hi = min(lo, h), max(hi, h)
			}
		}
		t.Logf("%3d %-45v size %v BB y %d..%d (+%d) pos %v terrain %d..%d (%d)", i, pe["location"], sz, bb[1], bb[4], bb[4]-bb[1]+1-sz[1], p["PosY"], lo, hi, hi-lo)
	}
}

func TestJigsawVillagePoolHeights(t *testing.T) {
	if os.Getenv("KREMENMC_VPH") == "" {
		t.Skip()
	}
	for _, v := range []string{"plains", "desert", "savanna", "snowy", "taiga"} {
		for _, n := range []string{"streets", "decor", "houses", "terminators", "town_centers", "villagers", "zombie/houses", "zombie/streets"} {
			h, err := structure.PoolMaxHeight("minecraft:village/" + v + "/" + n)
			t.Log(v, n, h, err)
		}
	}
}

func TestJigsawRefChunkMeta(t *testing.T) {
	if os.Getenv("KREMENMC_VMETA") == "" {
		t.Skip()
	}
	ref := refStarts(t, refWorldRegion)
	for k, c := range ref {
		ks := []string{}
		for kk, v := range c {
			if kk != "Children" {
				ks = append(ks, kk+"="+fmt.Sprint(v))
			}
		}
		t.Log(k, ks)
	}
}

func vRefChunk(t *testing.T, dir string, cx, cz int32) nbt.Compound {
	r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := r.Read(int(cx&31), int(cz&31))
	if err != nil {
		t.Fatal(err)
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestJigsawRefChunkStatus(t *testing.T) {
	v := os.Getenv("KREMENMC_VSTAT")
	if v == "" {
		t.Skip()
	}
	var cx, cz int32
	fmt.Sscanf(v, "%d,%d", &cx, &cz)
	c := vRefChunk(t, refWorldRegion, cx, cz)
	t.Log(c["Status"], c["DataVersion"], c["LastUpdate"], c["InhabitedTime"])
}

func TestJigsawRawJigsaws(t *testing.T) {
	loc := os.Getenv("KREMENMC_VRAW")
	if loc == "" {
		t.Skip()
	}
	for _, s := range structure.RawJigsaws(loc) {
		t.Log(s)
	}
}

// TestVillageBlocks generates chunks with kremenmc (villages enabled) and
// compares them block for block with a reference world
// (KREMENMC_VBLK=seed@x0,z0,x1,z1[@stage]; stage full or terrain).
func TestVillageBlocks(t *testing.T) {
	v := os.Getenv("KREMENMC_VBLK")
	if v == "" {
		t.Skip("set KREMENMC_VBLK")
	}
	parts := strings.Split(v, "@")
	var seed int64
	var x0, z0, x1, z1 int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	st := gen.StatusFull
	if len(parts) > 2 && parts[2] == "terrain" {
		st = gen.StatusTerrain
	}
	if os.Getenv("KREMENMC_VNOJIG") == "" {
		structure.EnableJigsaw(villageKeys...)
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	diff := map[string]int{}
	same, total := 0, 0
	show := 0
	// KREMENMC_VBOXONLY=1: only blocks inside the boxes of the jigsaw starts
	// referenced by the compared chunks.
	var boxes []structure.Box
	if os.Getenv("KREMENMC_VBOXONLY") != "" {
		ss, err := vanilla.StructureStagesForTest(tr, seed)
		if err != nil {
			t.Fatal(err)
		}
		for cz := z0 - 8; cz <= z1+8; cz++ {
			for cx := x0 - 8; cx <= x1+8; cx++ {
				st, err := ss.StartAt(cx, cz)
				if err != nil || st == nil || !strings.Contains(st.ID, "village") {
					continue
				}
				for _, p := range st.Pieces {
					boxes = append(boxes, p.Box())
				}
			}
		}
	}
	inBoxes := func(x, y, z int32) bool {
		if boxes == nil {
			return true
		}
		for _, b := range boxes {
			if b.Contains(x, y, z) {
				return true
			}
		}
		return false
	}
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, st)
			if err != nil {
				t.Fatal(err)
			}
			rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
			cs := 0
			for y := -64; y < 320; y++ {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						if !inBoxes(cx<<4+int32(x), int32(y), cz<<4+int32(z)) {
							continue
						}
						ws, gs := rc.Block(x, y, z), pc.Chunk.Block(x, y, z)
						total++
						if ws == gs {
							same++
							cs++
							continue
						}
						diff[reg.BlockOf(gs).Name+" -> "+reg.BlockOf(ws).Name]++
						if show < 40 && os.Getenv("KREMENMC_VSHOW") != "" {
							show++
							t.Logf("%d,%d,%d: kremenmc %s, ref %s", cx<<4+int32(x), y, cz<<4+int32(z), reg.StateString(gs), reg.StateString(ws))
						}
					}
				}
			}
			t.Logf("chunk %d,%d: %d/%d", cx, cz, cs, 16*16*384)
		}
	}
	type kv struct {
		k string
		n int
	}
	var l []kv
	for k, n := range diff {
		l = append(l, kv{k, n})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
	for i, e := range l {
		if i < 40 {
			t.Logf("%6d kremenmc %s", e.n, e.k)
		}
	}
	t.Logf("blocks %d/%d (%.4f%%)", same, total, 100*float64(same)/float64(total))
}

// TestVillageTerrain compares kremenmc's terrain status (villages enabled) with
// the stage oracle's terrain dump (refbench stages -structures=true)
// (KREMENMC_VTER=seed@x0,z0,x1,z1).
func TestVillageTerrain(t *testing.T) {
	v := os.Getenv("KREMENMC_VTER")
	if v == "" {
		t.Skip("set KREMENMC_VTER")
	}
	parts := strings.Split(v, "@")
	var seed int64
	var x0, z0, x1, z1 int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	stage := "terrain"
	if len(parts) > 2 {
		stage = parts[2]
	}

	dump, err := oracle.ReadDump(fmt.Sprintf("../../../../.refcache/26.3/stages/%d-overworld/%s.dump", seed, stage))
	if err != nil {
		t.Skip(err)
	}
	refs := map[[2]int32]*anvil.RawChunk{}
	for _, c := range dump {
		refs[[2]int32{c.X, c.Z}] = c
	}
	if os.Getenv("KREMENMC_VNOJIG") == "" {
		structure.EnableJigsaw(villageKeys...)
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := tr.Stages("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	diff := map[string]int{}
	same, total, show := 0, 0, 0
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			rc := refs[[2]int32{cx, cz}]
			if rc == nil {
				t.Logf("no dump chunk %d,%d", cx, cz)
				continue
			}
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusTerrain)
			if err != nil {
				t.Fatal(err)
			}
			cs := 0
			for y := -64; y < 320; y++ {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						ws := rc.Block(x, y, z)
						gs := reg.StateString(pc.Chunk.Block(x, y, z))
						total++
						if ws == gs {
							same++
							cs++
							continue
						}
						diff[gs+" -> "+ws]++
						if show < 60 && os.Getenv("KREMENMC_VSHOW") != "" {
							show++
							t.Logf("%d,%d,%d: kremenmc %s, ref %s", cx<<4+int32(x), y, cz<<4+int32(z), gs, ws)
						}
					}
				}
			}
			t.Logf("chunk %d,%d: %d/%d", cx, cz, cs, 16*16*384)
		}
	}
	type kv struct {
		k string
		n int
	}
	var l []kv
	for k, n := range diff {
		l = append(l, kv{k, n})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
	for i, e := range l {
		if i < 20 {
			t.Logf("%6d kremenmc %s", e.n, e.k)
		}
	}
	t.Logf("blocks %d/%d (%.4f%%)", same, total, 100*float64(same)/float64(total))
}

func TestJigsawRefElements(t *testing.T) {
	if os.Getenv("KREMENMC_VEL") == "" {
		t.Skip()
	}
	ref := refStarts(t, refWorldRegion)
	seen := map[string]bool{}
	for _, c := range ref {
		ch, ok := c["Children"].(nbt.List)
		if !ok {
			continue
		}
		for _, it := range ch.Items {
			p := it.(nbt.Compound)
			pe, ok := p["pool_element"].(nbt.Compound)
			if !ok {
				continue
			}
			k := fmt.Sprint(pe["element_type"])
			if seen[k] {
				continue
			}
			seen[k] = true
			t.Logf("%#v", pe)
			t.Logf("piece keys: %#v", p)
		}
	}
}

// TestVillagePieceDiff compares kremenmc and the reference world inside each
// piece box of a start (KREMENMC_VPIECE=seed@id@cx,cz@x0,z0,x1,z1: start, then
// the full reference chunks to look at).
func TestVillagePieceDiff(t *testing.T) {
	v := os.Getenv("KREMENMC_VPIECE")
	if v == "" {
		t.Skip("set KREMENMC_VPIECE")
	}
	parts := strings.Split(v, "@")
	var seed int64
	var scx, scz, x0, z0, x1, z1 int32
	fmt.Sscan(parts[0], &seed)
	id := parts[1]
	fmt.Sscanf(parts[2], "%d,%d", &scx, &scz)
	fmt.Sscanf(parts[3], "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	structure.EnableJigsaw(villageKeys...)
	tr, _ := vanilla.NewTerrain("overworld", seed)
	ss, _ := vanilla.StructureStagesForTest(tr, seed)
	st, err := ss.StartAt(scx, scz)
	if err != nil || st == nil || st.ID != id {
		t.Fatalf("start %v %v", st, err)
	}
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	reg := data.Canonical()
	type cc struct{ g, r *world.Chunk }
	chunks := map[[2]int32]cc{}
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
			chunks[[2]int32{cx, cz}] = cc{pc.Chunk, rc}
		}
	}
	only := os.Getenv("KREMENMC_VPIECEONLY")
	for i, pc := range st.Pieces {
		jp := pc.(*structure.JigsawPiece)
		if only != "" && !strings.Contains(jp.Location(), only) {
			continue
		}
		b := pc.Box()
		same, total, shown := 0, 0, 0
		diff := map[string]int{}
		for x := b.MinX; x <= b.MaxX; x++ {
			for z := b.MinZ; z <= b.MaxZ; z++ {
				c, ok := chunks[[2]int32{x >> 4, z >> 4}]
				if !ok {
					continue
				}
				for y := b.MinY; y <= b.MaxY; y++ {
					gs, ws := c.g.Block(int(x&15), int(y), int(z&15)), c.r.Block(int(x&15), int(y), int(z&15))
					total++
					if gs == ws {
						same++
						continue
					}
					diff[reg.BlockOf(gs).Name+">"+reg.BlockOf(ws).Name]++
					if only != "" && shown < 30 {
						shown++
						t.Logf("   %d,%d,%d (local y %d): kremenmc %s ref %s", x, y, z, y-jp.Y, reg.StateString(gs), reg.StateString(ws))
					}
				}
			}
		}
		if total == 0 || same == total {
			continue
		}
		t.Logf("%3d %-50s rigid=%v pos %d,%d,%d %d/%d %v", i, jp.Location(), jp.Rigid(), jp.X, jp.Y, jp.Z, same, total, diff)
	}
}

// TestVillageColumn prints one column in the stage oracle's terrain dump,
// the reference world and kremenmc (KREMENMC_VCOL=seed@x,z@y0,y1).
func TestVillageColumn(t *testing.T) {
	v := os.Getenv("KREMENMC_VCOL")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var x, z, y0, y1 int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d", &x, &z)
	fmt.Sscanf(parts[2], "%d,%d", &y0, &y1)
	dump, _ := oracle.ReadDump(fmt.Sprintf("../../../../.refcache/26.3/stages/%d-overworld/terrain.dump", seed))
	var rt *anvil.RawChunk
	for _, c := range dump {
		if c.X == x>>4 && c.Z == z>>4 {
			rt = c
		}
	}
	structure.EnableJigsaw(villageKeys...)
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	pt, _ := p.Get(context.Background(), world.ChunkPos{X: x >> 4, Z: z >> 4}, gen.StatusTerrain)
	ptb := make([]data.StateID, 0)
	for y := y0; y <= y1; y++ {
		ptb = append(ptb, pt.Chunk.Block(int(x&15), int(y), int(z&15)))
	}
	pf, _ := p.Get(context.Background(), world.ChunkPos{X: x >> 4, Z: z >> 4}, gen.StatusFull)
	rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), x>>4, z>>4)
	reg := data.Canonical()
	for y := y1; y >= y0; y-- {
		rts := "-"
		if rt != nil {
			rts = rt.Block(int(x&15), int(y), int(z&15))
		}
		t.Logf("y=%d terrain ref %s kremenmc %s | full ref %s kremenmc %s", y, rts, reg.StateString(ptb[y-y0]),
			reg.StateString(rc.Block(int(x&15), int(y), int(z&15))), reg.StateString(pf.Chunk.Block(int(x&15), int(y), int(z&15))))
	}
}

// TestVillageMap prints a y layer of one chunk: oracle terrain, reference
// full, kremenmc full (KREMENMC_VMAP=seed@cx,cz@y).
func TestVillageMap(t *testing.T) {
	v := os.Getenv("KREMENMC_VMAP")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var cx, cz, y int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d", &cx, &cz)
	fmt.Sscan(parts[2], &y)
	dump, _ := oracle.ReadDump(fmt.Sprintf("../../../../.refcache/26.3/stages/%d-overworld/terrain.dump", seed))
	var rt *anvil.RawChunk
	for _, c := range dump {
		if c.X == cx && c.Z == cz {
			rt = c
		}
	}
	structure.EnableJigsaw(villageKeys...)
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: t.Logf})
	pf, _ := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
	rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
	reg := data.Canonical()
	ch := func(n string) string {
		n = strings.TrimPrefix(n, "minecraft:")
		if i := strings.IndexByte(n, '['); i >= 0 {
			n = n[:i]
		}
		m := map[string]string{"air": ".", "water": "~", "grass_block": "g", "dirt": "d", "sand": "s", "dirt_path": "p", "oak_planks": "P", "stone": "#", "short_grass": ",", "gravel": "v", "hay_block": "H", "sandstone": "S", "smooth_sandstone": "m", "cut_sandstone": "c"}
		if c, ok := m[n]; ok {
			return c
		}
		return "?"
	}
	for z := 0; z < 16; z++ {
		a, b, c := "", "", ""
		for x := 0; x < 16; x++ {
			if rt != nil {
				a += ch(rt.Block(x, int(y), z))
			}
			b += ch(reg.StateString(rc.Block(x, int(y), z)))
			c += ch(reg.StateString(pf.Chunk.Block(x, int(y), z)))
		}
		t.Logf("z=%d  %s  %s  %s", cz<<4+int32(z), a, b, c)
	}
}

func TestJigsawPoolFeatureTypes(t *testing.T) {
	if os.Getenv("KREMENMC_VPF") == "" {
		t.Skip()
	}
	pk, _ := wgdata.Load()
	keys, _ := structure.PoolFeatures()
	for _, k := range keys {
		b, _ := pk.JSON("worldgen/placed_feature", k)
		var pf struct {
			Feature json.RawMessage `json:"feature"`
		}
		json.Unmarshal(b, &pf)
		var name string
		typ := ""
		if json.Unmarshal(pf.Feature, &name) == nil {
			cb, _ := pk.JSON("worldgen/configured_feature", name)
			var cf struct {
				Type string `json:"type"`
			}
			json.Unmarshal(cb, &cf)
			typ = cf.Type
		}
		t.Logf("%s %s", k, typ)
	}
}

func TestJigsawGrepPack(t *testing.T) {
	q := os.Getenv("KREMENMC_VGREP")
	if q == "" {
		t.Skip()
	}
	pk, _ := wgdata.Load()
	for _, reg := range []string{"worldgen/configured_feature", "worldgen/placed_feature"} {
		for _, k := range pk.Keys(reg) {
			b, _ := pk.JSON(reg, k)
			if strings.Contains(string(b), q) {
				t.Logf("%s %s: %.300s", reg, k, b)
			}
		}
	}
}

type testFeatures struct{ f *feature.StructureFeatures }

func (s testFeatures) PlaceFeature(sc *gen.StageContext, key string, r *rng.WorldgenRandom, x, y, z int32) bool {
	return s.f.Place(sc, key, r, int(x), int(y), int(z))
}

// villageStages mirrors vanilla.Terrain.Stages with access to the
// structure stages.
func villageStages(t *testing.T, seed int64, mod func(ss *structure.Stages)) []gen.Stage {
	structure.EnableJigsaw(villageKeys...)
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	if mod != nil {
		mod(ss)
	}
	fs, err := feature.NewStage("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	fs.StructureStep = ss.PlaceStep
	keys, _ := structure.PoolFeatures()
	sf, err := feature.NewStructureFeatures(fs, keys)
	if err != nil {
		t.Fatal(err)
	}
	ss.Features = testFeatures{sf}
	return []gen.Stage{ss.StartsStage(), ss.ReferencesStage(),
		gen.StageFunc{S: gen.StatusBiomes, F: func(ctx *gen.StageContext) error { return tr.GenerateStage(ctx.Chunk().Chunk, "biomes") }},
		gen.StageFunc{S: gen.StatusTerrain, F: func(ctx *gen.StageContext) error {
			pc := ctx.Chunk()
			var beard func(x, y, z int32) float32
			if b := ss.BeardFor(ctx); b != nil {
				beard = b.Value
			}
			if err := tr.GenerateStageBeard(pc.Chunk, "terrain", beard); err != nil {
				return err
			}
			if pc.Data == nil {
				pc.Data = nbt.Compound{}
			}
			pc.Data[gen.AnvilWGKey] = anvil.PackWG(pc.Chunk)
			return nil
		}},
		fs}
}

// TestVillageIndexSearch finds the decoration index under which a
// structure's chunk matches the reference best (KREMENMC_VIDX=seed@key@cx,cz).
func TestVillageIndexSearch(t *testing.T) {
	v := os.Getenv("KREMENMC_VIDX")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var cx, cz int32
	fmt.Sscan(parts[0], &seed)
	key := parts[1]
	fmt.Sscanf(parts[2], "%d,%d", &cx, &cz)
	rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
	for idx := -1; idx < 45; idx++ {
		cur := 0
		stages := villageStages(t, seed, func(ss *structure.Stages) {
			cur = ss.StructureIndex(key)
			if idx >= 0 {
				ss.SetStructureIndex(key, idx)
			}
		})
		p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages})
		pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		same := 0
		for y := 60; y < 120; y++ {
			for z := 0; z < 16; z++ {
				for x := 0; x < 16; x++ {
					if rc.Block(x, y, z) == pc.Chunk.Block(x, y, z) {
						same++
					}
				}
			}
		}
		t.Logf("index %d (default %d): %d", idx, cur, same)
		p.Close()
	}
}

// TestVillageSkipSearch advances the structure random by k draws before the
// feature piece at x,y,z (KREMENMC_VSKIP=seed@cx,cz@x,y,z@kind).
func TestVillageSkipSearch(t *testing.T) {
	v := os.Getenv("KREMENMC_VSKIP")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var cx, cz, fx, fy, fz int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d", &cx, &cz)
	fmt.Sscanf(parts[2], "%d,%d,%d", &fx, &fy, &fz)
	kind := parts[3]
	rc, _ := refChunk(t, fmt.Sprintf("%d-overworld", seed), cx, cz)
	for k := 0; k < 40; k++ {
		structure.SetFeatureHook(func(x, y, z int32, r *rng.WorldgenRandom, _ func(x, y, z int32) data.StateID) {
			if x != fx || y != fy || z != fz {
				return
			}
			for range k {
				switch kind {
				case "long":
					r.NextLong()
				case "int":
					r.NextInt()
				case "float":
					r.NextFloat()
				}
			}
		})
		p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: villageStages(t, seed, nil)})
		pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		same := 0
		for y := fy - 2; y < fy+3; y++ {
			for z := 0; z < 16; z++ {
				for x := 0; x < 16; x++ {
					if rc.Block(x, int(y), z) == pc.Chunk.Block(x, int(y), z) {
						same++
					}
				}
			}
		}
		t.Logf("skip %d %s: %d", k, kind, same)
		p.Close()
	}
	structure.SetFeatureHook(nil)
}

func TestJigsawTemplateBE(t *testing.T) {
	loc := os.Getenv("KREMENMC_VBE")
	if loc == "" {
		t.Skip()
	}
	for _, s := range structure.TemplateBlockEntities(loc) {
		t.Log(s)
	}
}

// TestVillageFeatureBase prints the blocks around a feature piece at
// placement time next to the reference world (KREMENMC_VFB=seed@cx,cz@x,y,z).
func TestVillageFeatureBase(t *testing.T) {
	v := os.Getenv("KREMENMC_VFB")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var cx, cz, fx, fy, fz int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d", &cx, &cz)
	fmt.Sscanf(parts[2], "%d,%d,%d", &fx, &fy, &fz)
	reg := data.Canonical()
	refs := map[[2]int32]*world.Chunk{}
	ref := func(x, y, z int32) data.StateID {
		k := [2]int32{x >> 4, z >> 4}
		if refs[k] == nil {
			refs[k], _ = refChunk(t, fmt.Sprintf("%d-overworld", seed), k[0], k[1])
		}
		return refs[k].Block(int(x&15), int(y), int(z&15))
	}
	structure.SetFeatureHook(func(x, y, z int32, r *rng.WorldgenRandom, block func(x, y, z int32) data.StateID) {
		if x != fx || y != fy || z != fz {
			return
		}
		for yy := y - 1; yy <= y+2; yy++ {
			for zz := z - 4; zz <= z+4; zz++ {
				for xx := x - 4; xx <= x+4; xx++ {
					g, w := block(xx, yy, zz), ref(xx, yy, zz)
					if g != w && reg.BlockOf(w).Name != "hay_block" {
						t.Logf("%d,%d,%d: kremenmc %s, ref final %s", xx, yy, zz, reg.StateString(g), reg.StateString(w))
					}
				}
			}
		}
	})
	defer structure.SetFeatureHook(nil)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: villageStages(t, seed, nil)})
	for _, c := range [][2]int32{{cx - 1, cz - 1}, {cx, cz - 1}, {cx + 1, cz - 1}, {cx - 1, cz}, {cx, cz}} {
		if _, err := p.Get(context.Background(), world.ChunkPos{X: c[0], Z: c[1]}, gen.StatusFull); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVillageFeaturePieces(t *testing.T) {
	v := os.Getenv("KREMENMC_VFP")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var cx, cz int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d", &cx, &cz)
	structure.EnableJigsaw(villageKeys...)
	tr, _ := vanilla.NewTerrain("overworld", seed)
	ss, _ := vanilla.StructureStagesForTest(tr, seed)
	st, _ := ss.StartAt(cx, cz)
	for i, p := range st.Pieces {
		jp := p.(*structure.JigsawPiece)
		if jp.Primary() == nil {
			t.Logf("%d %s %d,%d,%d chunk %d,%d", i, jp.Location(), jp.X, jp.Y, jp.Z, jp.X>>4, jp.Z>>4)
		}
	}
}

// TestRefVillagePresence checks, for every chunk the reference world holds
// (status >= structure_starts), that kremenmc has a village start exactly where
// the reference has one (KREMENMC_VPRES=seed).
func TestRefVillagePresence(t *testing.T) {
	v := os.Getenv("KREMENMC_VPRES")
	if v == "" {
		t.Skip()
	}
	var seed int64
	fmt.Sscan(v, &seed)
	dir := fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-overworld/world/dimensions/minecraft/overworld/region", seed)
	sets, _ := structure.LoadSets()
	var vs *structure.Set
	for _, s := range sets {
		if s.Key == "minecraft:villages" {
			vs = s
		}
	}
	structure.EnableJigsaw(villageKeys...)
	tr, _ := vanilla.NewTerrain("overworld", seed)
	ss, _ := vanilla.StructureStagesForTest(tr, seed)
	files, _ := filepath.Glob(filepath.Join(dir, "r.*.mca"))
	checked, bad := 0, 0
	for _, f := range files {
		var rx, rz int32
		fmt.Sscanf(filepath.Base(f), "r.%d.%d.mca", &rx, &rz)
		r, err := anvil.OpenRegionReadOnly(dir, rx, rz)
		if err != nil {
			continue
		}
		for lz := 0; lz < 32; lz++ {
			for lx := 0; lx < 32; lx++ {
				cx, cz := rx*32+int32(lx), rz*32+int32(lz)
				if !r.Has(lx, lz) || !vs.Placement.IsSpreadChunk(seed, cx, cz) {
					continue
				}
				b, err := r.Read(lx, lz)
				if err != nil {
					continue
				}
				_, root, _, _ := nbt.Unmarshal(b, false)
				refID := ""
				if sts, ok := root["structures"].(nbt.Compound); ok {
					if st, ok := sts["starts"].(nbt.Compound); ok {
						for id, e := range st {
							if c, _ := e.(nbt.Compound); c["id"] != "INVALID" && strings.Contains(id, "village") {
								refID = id
							}
						}
					}
				}
				st, err := ss.StartAt(cx, cz)
				gID := ""
				if err == nil && st != nil && strings.Contains(st.ID, "village") {
					gID = st.ID
				}
				checked++
				if gID != refID {
					bad++
					t.Errorf("chunk %d,%d (status %v): ref %q kremenmc %q", cx, cz, root["Status"], refID, gID)
				} else if refID != "" {
					t.Logf("chunk %d,%d: %s", cx, cz, refID)
				}
			}
		}
		r.Close()
	}
	t.Logf("village spread chunks checked %d, differing %d", checked, bad)
}

// TestVillageEntities compares structure entities (id, position) with the
// reference entity region (KREMENMC_VENT=seed@x0,z0,x1,z1).
func TestVillageEntities(t *testing.T) {
	v := os.Getenv("KREMENMC_VENT")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var x0, z0, x1, z1 int32
	fmt.Sscan(parts[0], &seed)
	fmt.Sscanf(parts[1], "%d,%d,%d,%d", &x0, &z0, &x1, &z1)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: villageStages(t, seed, nil)})
	dir := fmt.Sprintf("../../../../.refcache/26.3/worlds/%d-overworld/world/dimensions/minecraft/overworld/entities", seed)
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			pc, err := p.Get(context.Background(), world.ChunkPos{X: cx, Z: cz}, gen.StatusFull)
			if err != nil {
				t.Fatal(err)
			}
			var g, w []string
			for _, e := range pc.Entities {
				pos := e["Pos"].(nbt.List).Items
				g = append(g, fmt.Sprintf("%v %.2f,%.2f,%.2f", e["id"], pos[0], pos[1], pos[2]))
			}
			r, err := anvil.OpenRegionReadOnly(dir, cx>>5, cz>>5)
			if err == nil {
				if b, err := r.Read(int(cx&31), int(cz&31)); err == nil {
					_, root, _, _ := nbt.Unmarshal(b, false)
					if l, ok := root["Entities"].(nbt.List); ok {
						for _, it := range l.Items {
							e := it.(nbt.Compound)
							pos := e["Pos"].(nbt.List).Items
							w = append(w, fmt.Sprintf("%v %.2f,%.2f,%.2f", e["id"], pos[0], pos[1], pos[2]))
						}
					}
				}
				r.Close()
			}
			sort.Strings(g)
			sort.Strings(w)
			t.Logf("chunk %d,%d\n  kremenmc %v\n  ref  %v", cx, cz, g, w)
		}
	}
}
