package structure_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

// writeShScene writes a kremenmc.probe.temple.TScene input: the region
// filled by fill, a stronghold start holding children, placed for chunks.
func writeShScene(t *testing.T, path string, seed int64, rg structure.Box, fill func(x, y, z int32) data.StateID, children []nbt.Compound, chunks [][2]int32) {
	reg := data.Canonical()
	si := structure.DecoIndexForTest("minecraft:stronghold")
	var b strings.Builder
	fmt.Fprintf(&b, "SEED %d %d %d\n", seed, si[1], si[0])
	fmt.Fprintf(&b, "REGION %d %d %d %d %d %d\n", rg.MinX, rg.MinY, rg.MinZ, rg.MaxX, rg.MaxY, rg.MaxZ)
	pal := map[data.StateID]int{}
	var palList []data.StateID
	var rle strings.Builder
	last, run := -1, 0
	for y := rg.MinY; y <= rg.MaxY; y++ {
		for z := rg.MinZ; z <= rg.MaxZ; z++ {
			for x := rg.MinX; x <= rg.MaxX; x++ {
				s := fill(x, y, z)
				i, ok := pal[s]
				if !ok {
					i = len(palList)
					pal[s] = i
					palList = append(palList, s)
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
		}
	}
	fmt.Fprintf(&rle, " %d*%d", last, run)
	fmt.Fprintf(&b, "PAL %d\n", len(palList))
	for _, s := range palList {
		fmt.Fprintln(&b, reg.StateString(s))
	}
	fmt.Fprintf(&b, "DATA%s\n", rle.String())
	items := make([]any, len(children))
	for i, c := range children {
		items[i] = c
	}
	c0 := chunks[0]
	st := nbt.Compound{"Children": nbt.List{Type: nbt.TagCompound, Items: items}, "ChunkX": c0[0], "ChunkZ": c0[1], "id": "minecraft:stronghold", "references": int32(0)}
	fmt.Fprintf(&b, "START %s\n", snbt(st))
	var cs []string
	for _, c := range chunks {
		cs = append(cs, fmt.Sprintf("%d,%d", c[0], c[1]))
	}
	fmt.Fprintf(&b, "CHUNKS %s\n", strings.Join(cs, " "))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shPieceNBT is a piece compound at min corner x,y,z facing O (0 south,
// 1 west, 2 north, 3 east) with the size of its id.
func shPieceNBT(id string, x, y, z, o int32, w, h, d int32, extra nbt.Compound) nbt.Compound {
	var bb []int32
	if o == 0 || o == 2 {
		bb = []int32{x, y, z, x + w - 1, y + h - 1, z + d - 1}
	} else {
		bb = []int32{x, y, z, x + d - 1, y + h - 1, z + w - 1}
	}
	c := nbt.Compound{"id": "minecraft:" + id, "BB": bb, "GD": int32(1), "O": o, "EntryDoor": "OPENING"}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

var shKindSizes = map[string][3]int32{
	"shstart": {5, 11, 5}, "shs": {5, 5, 7}, "shph": {9, 5, 11}, "shlt": {5, 5, 5}, "shrt": {5, 5, 5}, "shrc": {11, 7, 11},
	"shssd": {5, 11, 8}, "shsd": {5, 11, 5}, "sh5c": {10, 9, 11}, "shcc": {5, 5, 7}, "shli": {14, 11, 15}, "shpr": {11, 8, 16}, "shfc": {5, 5, 3},
}

// TestShSceneGen (KREMENMC_SHSCENE=out@spec[@scramble[@all]]) writes a
// TScene input with one piece per cell along x (24 apart) on solid stone,
// or on a pseudo-random block mix with a scramble seed; "all" places every
// chunk of the region in order instead of one full-clip pass. spec is a
// list of pieces "id:O:door:k=v,k=v;..." (values ending in b are bytes,
// else ints).
func TestShSceneGen(t *testing.T) {
	v := os.Getenv("KREMENMC_SHSCENE")
	if v == "" {
		t.Skip("KREMENMC_SHSCENE not set")
	}
	args := strings.Split(v, "@")
	out, spec := args[0], args[1]
	reg := data.Canonical()
	stone, _ := reg.ParseState("minecraft:stone")
	fill := func(x, y, z int32) data.StateID { return stone }
	if len(args) > 2 && args[2] != "" {
		// a pseudo-random mix to probe the replace conditions
		var seed int64
		fmt.Sscan(args[2], &seed)
		picks := []string{"minecraft:air", "minecraft:air", "minecraft:cave_air", "minecraft:water", "minecraft:water[level=3]", "minecraft:lava",
			"minecraft:stone", "minecraft:stone", "minecraft:stone", "minecraft:bedrock", "minecraft:cobweb", "minecraft:glass",
			"minecraft:oak_leaves[distance=7,persistent=true,waterlogged=false]", "minecraft:short_grass", "minecraft:seagrass",
			"minecraft:chest[facing=north,type=single,waterlogged=false]", "minecraft:spawner", "minecraft:end_portal_frame[eye=false,facing=north]",
			"minecraft:stone_bricks", "minecraft:oak_planks", "minecraft:gravel", "minecraft:obsidian"}
		var ids []data.StateID
		for _, n := range picks {
			st, ok := reg.ParseState(n)
			if !ok {
				t.Fatalf("state %s", n)
			}
			ids = append(ids, st)
		}
		fill = func(x, y, z int32) data.StateID {
			h := uint64(seed) ^ uint64(x)*0x9E3779B97F4A7C15 ^ uint64(y)*0xC2B2AE3D27D4EB4F ^ uint64(z)*0x165667B19E3779F9
			h ^= h >> 29
			h *= 0xBF58476D1CE4E5B9
			h ^= h >> 32
			return ids[h%uint64(len(ids))]
		}
	}
	var kids []nbt.Compound
	for i, one := range strings.Split(spec, ";") {
		f := strings.Split(one, ":")
		var o int32
		fmt.Sscan(f[1], &o)
		sz := shKindSizes[f[0]]
		ex := nbt.Compound{"EntryDoor": f[2]}
		if len(f) > 3 && f[3] != "" {
			for _, kv := range strings.Split(f[3], ",") {
				k, val, _ := strings.Cut(kv, "=")
				if strings.HasSuffix(val, "b") {
					var n int8
					fmt.Sscan(strings.TrimSuffix(val, "b"), &n)
					ex[k] = n
				} else {
					var n int32
					fmt.Sscan(val, &n)
					ex[k] = n
				}
			}
		}
		if f[0] == "shfc" {
			if s, ok := ex["Steps"].(int32); ok {
				sz[2] = s
			}
		}
		if f[0] == "shli" {
			if t, ok := ex["Tall"].(int8); ok && t == 0 {
				sz[1] = 6
			}
		}
		kids = append(kids, shPieceNBT(f[0], int32(i)*24+4, 0, 4, o, sz[0], sz[1], sz[2], ex))
	}
	n := int32(len(kids))
	rg := structure.Box{MinX: 0, MinY: -2, MinZ: 0, MaxX: n*24 - 1, MaxY: 14, MaxZ: 23}
	chunks := [][2]int32{{0, 0}}
	if len(args) > 3 && args[3] == "all" {
		chunks = nil
		for cx := rg.MinX >> 4; cx <= rg.MaxX>>4; cx++ {
			for cz := rg.MinZ >> 4; cz <= rg.MaxZ>>4; cz++ {
				chunks = append(chunks, [2]int32{cx, cz})
			}
		}
	}
	writeShScene(t, out, 8266389061079214669, rg, fill, kids, chunks)
}

// TestShDrawDebug (KREMENMC_SHDBG=scene.txt@scene.log) replays a full-clip
// stronghold scene and reports the first placement draw that differs from
// the probe log's.
func TestShDrawDebug(t *testing.T) {
	v := os.Getenv("KREMENMC_SHDBG")
	if v == "" {
		t.Skip("KREMENMC_SHDBG not set")
	}
	in, logf, _ := strings.Cut(v, "@")
	b, err := os.ReadFile(logf)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "R0 ") && strings.Contains(l, "nextFloat") {
			f := strings.Fields(l)
			want = append(want, f[4]+" "+l[strings.Index(l, "@")+1:])
		}
	}
	s := readLScene(t, in)
	st, err := structure.DecodeStart(s.start)
	if err != nil {
		t.Fatal(err)
	}
	lv := &llevel{s: s, m: map[[3]int32]data.StateID{}, be: map[[3]int32]nbt.Compound{}}
	n, cur := 0, -1
	bad := false
	structure.SetShTrace(func(what string, v any) {
		if bad {
			return
		}
		g := fmt.Sprint(v)
		if n >= len(want) || !strings.HasPrefix(want[n], g+" ") {
			w := "end"
			if n < len(want) {
				w = want[n]
			}
			t.Errorf("draw %d (piece %d op %s): got %s, want %s", n, cur, what, g, w)
			bad = true
		}
		n++
	})
	defer structure.SetShTrace(nil)
	r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
	c := s.chunks[0]
	deco := r.SetDecorationSeed(s.seed, c[0]<<4, c[1]<<4)
	r.SetFeatureSeed(deco, s.index, s.step)
	clip := structure.Box{MinX: s.rg.MinX, MinY: -64, MinZ: s.rg.MinZ, MaxX: s.rg.MaxX, MaxY: 319, MaxZ: s.rg.MaxZ}
	for i, p := range st.Pieces {
		cur = i
		n0 := n
		if err := p.Place(lv, r, clip, c[0], c[1]); err != nil {
			t.Fatal(err)
		}
		t.Logf("piece %d %s: %d draws", i, p.Type(), n-n0)
	}
	t.Logf("%d/%d float draws matched", n, len(want))
}

// TestStrongholdScenes replays the TScene goldens testdata/stronghold/
// scene_*: every piece variant on solid stone (one full-clip pass), on a
// scrambled block mix (full clip), per chunk on stone, and three other
// orientations per chunk on a scrambled mix; blocks, loot seeds and the
// placement random's state per chunk must be identical.
func TestStrongholdScenes(t *testing.T) {
	files, _ := filepath.Glob("testdata/stronghold/scene_*.txt.gz")
	if len(files) == 0 {
		t.Fatal("no scenes")
	}
	for _, f := range files {
		in := strings.TrimSuffix(f, ".gz")
		t.Run(filepath.Base(in), func(t *testing.T) { runLScene(t, in, strings.TrimSuffix(in, ".txt")+".out") })
	}
}
