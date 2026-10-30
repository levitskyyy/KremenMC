package structure

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// TestJigsawSurvey lists the pool closure of a start pool: element types,
// projections and processor types (KREMENMC_SURVEY=start pool).
func TestJigsawSurvey(t *testing.T) {
	start := os.Getenv("KREMENMC_SURVEY")
	if start == "" {
		t.Skip()
	}
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := NewAssembler()
	seen := map[string]bool{}
	cnt := map[string]int{}
	queue := strings.Split(start, ",")
	for len(queue) > 0 {
		n := qualify(queue[0])
		queue = queue[1:]
		if seen[n] || n == "minecraft:empty" {
			continue
		}
		seen[n] = true
		p, err := a.pool(n)
		if err != nil {
			t.Log("pool", n, err)
			continue
		}
		if p.fallback != "" {
			queue = append(queue, p.fallback)
		}
		var walk func(e *poolElement)
		walk = func(e *poolElement) {
			cnt["el "+e.kind+" rigid="+map[bool]string{true: "y", false: "n"}[e.rigid]+" legacy="+map[bool]string{true: "y", false: "n"}[e.legacy]]++
			for _, c := range e.list {
				walk(c)
			}
			if e.kind == "feature" {
				cnt["feature "+e.feature]++
			}
			var j struct {
				Processors json.RawMessage `json:"processors"`
			}
			_ = json.Unmarshal(e.raw, &j)
			var name string
			if json.Unmarshal(j.Processors, &name) == nil {
				b, _ := pk.JSON("worldgen/processor_list", qualify(name))
				j.Processors = b
			}
			for _, typ := range procTypes(j.Processors) {
				cnt["proc "+typ]++
			}
			if e.kind == "single" {
				tm, err := LoadTemplate(e.location)
				if err != nil {
					t.Log(err)
					return
				}
				for _, jb := range templateJigsaws(tm) {
					if jb.pool != "" {
						queue = append(queue, jb.pool)
					}
				}
				if len(tm.Entities) > 0 {
					cnt["entities in "+e.location]++
				}
			}
		}
		seenEl := map[*poolElement]bool{}
		for _, e := range p.elements {
			if !seenEl[e] {
				seenEl[e] = true
				walk(e)
			}
		}
	}
	var ks []string
	for k := range cnt {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		t.Logf("%5d %s", cnt[k], k)
	}
	var ps []string
	for k := range seen {
		ps = append(ps, k)
	}
	sort.Strings(ps)
	t.Logf("pools: %s", strings.Join(ps, " "))
}

func procTypes(b []byte) []string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if s, ok := x["processor_type"].(string); ok {
				out = append(out, s)
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// TestJigsawSpreadChunks prints the spread chunks of a structure set within
// a radius of grid cells as probe arguments (KREMENMC_SPREAD=set@seed@r).
func TestJigsawSpreadChunks(t *testing.T) {
	v := os.Getenv("KREMENMC_SPREAD")
	if v == "" {
		t.Skip()
	}
	parts := strings.Split(v, "@")
	var seed int64
	var r int32
	fmt.Sscan(parts[1], &seed)
	fmt.Sscan(parts[2], &r)
	sets, err := LoadSets()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sets {
		if s.Key != qualify(parts[0]) {
			continue
		}
		sp := s.Placement.Spacing
		var out []string
		for gz := -r; gz < r; gz++ {
			for gx := -r; gx < r; gx++ {
				cx, cz := s.Placement.PotentialChunk(seed, gx*sp, gz*sp)
				if s.Placement.IsSpreadChunk(seed, cx, cz) {
					out = append(out, fmt.Sprintf("%d %d", cx, cz))
				}
			}
		}
		fmt.Println(strings.Join(out, " "))
	}
}

// GenerateJigsawForTest runs one jigsaw structure's Generate at a chunk
// with a caller's probe (dimensions the stages do not cover yet).
func GenerateJigsawForTest(key string, seed int64, cx, cz int32, probe Probe, minY, maxY int32) ([]Piece, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	b, ok := pk.JSON("worldgen/structure", key)
	if !ok {
		return nil, fmt.Errorf("no structure %s", key)
	}
	jigsawEnabled[qualify(key)] = true
	typ, err := typeFactories["minecraft:jigsaw"](qualify(key), b)
	if err != nil {
		return nil, err
	}
	var head struct {
		Biomes json.RawMessage `json:"biomes"`
	}
	_ = json.Unmarshal(b, &head)
	ok2 := map[string]bool{}
	for _, n := range biomeList(pk, head.Biomes) {
		ok2[trimNS(n)] = true
	}
	reg := data.Canonical()
	r := largeFeature(seed, cx, cz)
	ctx := &GenContext{Seed: seed, ChunkX: cx, ChunkZ: cz, Random: r, MinY: minY, MaxY: maxY,
		Height: func(x, z int32, hm string) int32 { return probe.Height(x, z, hm) },
		BiomeOK: func(x, y, z int32) bool {
			return ok2[reg.Biomes[probe.Biome(x, y, z)].Name]
		},
	}
	return typ.Generate(ctx)
}

func SetBeardNoJunctions(v bool) { beardNoJunctions = v }

// ColumnTemplateBlocks lists, for each piece of pieces whose box covers
// column x,z, the template blocks at that column (tests).
func ColumnTemplateBlocks(pieces []Piece, x, z int32) []string {
	reg := data.Canonical()
	var out []string
	for i, p := range pieces {
		jp, ok := p.(*JigsawPiece)
		if !ok || x < jp.box.MinX || x > jp.box.MaxX || z < jp.box.MinZ || z > jp.box.MaxZ {
			continue
		}
		for _, t := range jp.parts {
			for _, b := range t.tmpl.Palettes[0] {
				bx, bz := TransformPos(b.X, b.Z, MirrorNone, jp.Rot, 0, 0)
				if jp.X+bx == x && jp.Z+bz == z {
					out = append(out, fmt.Sprintf("#%d %s rigid=%v Y=%d ty=%d %s procs=%s", i, t.Template, jp.el.rigid, jp.Y, b.Y, reg.StateString(b.State), jp.el.procName))
				}
			}
		}
	}
	return out
}

// TemplatePositions is every position a rigid jigsaw piece's templates
// write (structure_void and air of legacy elements excluded).
func TemplatePositions(pieces []Piece) map[[3]int32]bool {
	reg := data.Canonical()
	out := map[[3]int32]bool{}
	for _, p := range pieces {
		jp, ok := p.(*JigsawPiece)
		if !ok || !jp.el.rigid {
			continue
		}
		for _, t := range jp.parts {
			if len(t.tmpl.Palettes) == 0 {
				continue
			}
			for _, b := range t.tmpl.Palettes[0] {
				bl := reg.BlockOf(b.State)
				if bl == nil || bl.Name == "structure_void" || jp.el.legacy && reg.IsAir(b.State) {
					continue
				}
				x, z := TransformPos(b.X, b.Z, MirrorNone, jp.Rot, 0, 0)
				out[[3]int32{jp.X + x, jp.Y + b.Y, jp.Z + z}] = true
			}
		}
	}
	return out
}

func SetLootHook(f func(tmpl string, x, y, z int32, seed int64)) { debugLootHook = f }

type (
	nbtComp = nbt.Compound
	jsonRaw = json.RawMessage
)

var jsonUnmarshal = json.Unmarshal

func mustPack(t *testing.T) *wgdata.Pack {
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	return pk
}
