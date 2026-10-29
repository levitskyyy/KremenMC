// Command datagen converts PrismarineJS minecraft-data JSON into the compact
// registry blobs embedded by internal/data.
//
//	go run ./tools/datagen -ver 26.3
//	go run ./tools/datagen -ver 26.1 -src ~/minecraft-data/data/pc/26.1
//	go run ./tools/datagen -ver 26.3 -reports generated/reports/blocks.json
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type source struct {
	repo, ref, dir string
	protocol       int
	dataVersion    int
	// per-file fallbacks to another version when the primary source lacks the file
	fallback map[string]string
	// biomes added by this version that the data source does not carry yet
	extraBiomes []mcBiome
}

var sources = map[string]source{
	"26.1": {repo: "PrismarineJS/minecraft-data", ref: "master", dir: "data/pc/26.1", protocol: 775, dataVersion: 4786},
	"26.2": {repo: "PrismarineJS/minecraft-data", ref: "pc_26_2", dir: "data/pc/26.2", protocol: 776, dataVersion: 4903},
	// Unmerged PR #1301; blocks/items/entities checked against the 26.3 jar by its author.
	"26.3": {
		repo: "DallasCarraher/minecraft-data", ref: "pc-26.3-support", dir: "data/pc/26.3",
		protocol: 777, dataVersion: 5023,
		fallback: map[string]string{"biomes": "26.2"},
		extraBiomes: []mcBiome{
			{Name: "dappled_forest", Category: "forest", Temperature: 0.7, Precipitation: true, Dimension: "overworld", Color: 7972607},
		},
	},
}

var files = []string{"blocks", "blockCollisionShapes", "items", "entities", "biomes"}

func main() {
	ver := flag.String("ver", "26.3", "minecraft version to generate")
	out := flag.String("out", "internal/data/gen", "output directory")
	src := flag.String("src", "", "local directory with minecraft-data JSON files (skips download)")
	cache := flag.String("cache", filepath.Join(os.TempDir(), "kremenmc-datagen"), "download cache directory")
	reports := flag.String("reports", "", "vanilla generated/reports/blocks.json to verify state IDs against")
	preds := flag.String("predicates", "", "vanilla heightmap predicates (go run ./tools/refbench predicates); default internal/data/testdata/heightmap_predicates_<ver>.tsv.gz when present")
	flag.Parse()
	if *preds == "" {
		if p := filepath.Join("internal", "data", "testdata", "heightmap_predicates_"+*ver+".tsv.gz"); fileExists(p) {
			*preds = p
		}
	}
	if *preds != "" {
		var err error
		if predicates, err = loadPredicates(*preds); err != nil {
			log.Fatal(err)
		}
		log.Printf("heightmap flags from %s (%d entries)", *preds, len(predicates))
	}

	s, ok := sources[*ver]
	if !ok && *src == "" {
		log.Fatalf("unknown version %q and no -src given", *ver)
	}
	in, err := loadInputs(*ver, s, *src, *cache)
	if err != nil {
		log.Fatal(err)
	}
	if *reports != "" {
		if err := verifyReports(in, *reports); err != nil {
			log.Fatal(err)
		}
		log.Printf("state IDs match %s", *reports)
	}
	blob, stats, err := build(*ver, s, in)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	name := filepath.Join(*out, *ver+".bin.gz")
	if err := os.WriteFile(name, blob, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d bytes): %s", name, len(blob), stats)
	if len(changedFlags) > 0 {
		keys := make([]string, 0, len(changedFlags))
		n := 0
		for k, v := range changedFlags {
			keys = append(keys, k)
			n += v
		}
		sort.Strings(keys)
		log.Printf("vanilla predicates changed the flags of %d states:", n)
		for _, k := range keys {
			log.Printf("  %s (%d states)", k, changedFlags[k])
		}
	}
}

type inputs struct {
	blocks   []mcBlock
	shapes   mcShapes
	items    []mcItem
	entities []mcEntity
	biomes   []mcBiome
}

func loadInputs(ver string, s source, src, cache string) (*inputs, error) {
	get := func(file string) ([]byte, error) {
		if src != "" {
			return os.ReadFile(filepath.Join(src, file+".json"))
		}
		v, sc := ver, s
		if fb, ok := s.fallback[file]; ok {
			v, sc = fb, sources[fb]
		}
		return fetch(sc, v, file, cache)
	}
	in := &inputs{}
	targets := map[string]any{
		"blocks":               &in.blocks,
		"blockCollisionShapes": &in.shapes,
		"items":                &in.items,
		"entities":             &in.entities,
		"biomes":               &in.biomes,
	}
	for _, f := range files {
		b, err := get(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if err := json.Unmarshal(b, targets[f]); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
	}
	return in, nil
}

func fetch(s source, ver, file, cache string) ([]byte, error) {
	path := filepath.Join(cache, ver, file+".json")
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s.json", s.repo, s.ref, s.dir, file)
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
	return b, nil
}

type mcState struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	NumValues int      `json:"num_values"`
	Values    []string `json:"values"`
}

type mcBlock struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Hardness     *float64  `json:"hardness"`
	Resistance   float64   `json:"resistance"`
	Diggable     bool      `json:"diggable"`
	Transparent  bool      `json:"transparent"`
	EmitLight    int       `json:"emitLight"`
	FilterLight  int       `json:"filterLight"`
	DefaultState int       `json:"defaultState"`
	MinStateID   int       `json:"minStateId"`
	MaxStateID   int       `json:"maxStateId"`
	States       []mcState `json:"states"`
	BoundingBox  string    `json:"boundingBox"`
}

type mcShapes struct {
	Blocks map[string]json.RawMessage `json:"blocks"`
	Shapes map[string][][6]float64    `json:"shapes"`
}

type mcItem struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	StackSize int    `json:"stackSize"`
}

type mcEntity struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Type     string  `json:"type"`
	Category string  `json:"category"`
}

type mcBiome struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	Temperature   float64 `json:"temperature"`
	Precipitation bool    `json:"has_precipitation"`
	Dimension     string  `json:"dimension"`
	Color         int     `json:"color"`
}

func (s mcState) values() []string {
	if s.Type == "bool" {
		return []string{"true", "false"}
	}
	return s.Values
}

type reportBlock struct {
	States []struct {
		ID         int               `json:"id"`
		Default    bool              `json:"default"`
		Properties map[string]string `json:"properties"`
	} `json:"states"`
}

func verifyReports(in *inputs, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var rep map[string]reportBlock
	if err := json.Unmarshal(b, &rep); err != nil {
		return fmt.Errorf("parse reports: %w", err)
	}
	var bad []string
	for _, blk := range in.blocks {
		r, ok := rep["minecraft:"+blk.Name]
		if !ok {
			bad = append(bad, blk.Name+": missing in reports")
			continue
		}
		for _, st := range r.States {
			want := stateIndex(blk, st.Properties) + blk.MinStateID
			if st.ID != want {
				bad = append(bad, fmt.Sprintf("%s%v: reports %d, data %d", blk.Name, st.Properties, st.ID, want))
				break
			}
			if st.Default && st.ID != blk.DefaultState {
				bad = append(bad, fmt.Sprintf("%s: default reports %d, data %d", blk.Name, st.ID, blk.DefaultState))
			}
		}
	}
	if len(rep) != len(in.blocks) {
		bad = append(bad, fmt.Sprintf("block count: reports %d, data %d", len(rep), len(in.blocks)))
	}
	if len(bad) > 0 {
		if len(bad) > 20 {
			bad = append(bad[:20], fmt.Sprintf("... %d more", len(bad)-20))
		}
		return fmt.Errorf("state ID mismatch:\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}

func stateIndex(b mcBlock, props map[string]string) int {
	idx, stride := 0, 1
	for i := len(b.States) - 1; i >= 0; i-- {
		vals := b.States[i].values()
		for j, v := range vals {
			if v == props[b.States[i].Name] {
				idx += j * stride
				break
			}
		}
		stride *= len(vals)
	}
	return idx
}

// Flag bits; must match internal/data.
const (
	flagAir = 1 << iota
	flagSolid
	flagLiquid
	flagFluid
	flagLeaves
	flagFullCube
	flagTransparent
	flagReplaceable
)

var litLevel = map[string]int{
	"furnace": 13, "smoker": 13, "blast_furnace": 13,
	"redstone_ore": 9, "deepslate_redstone_ore": 9,
	"redstone_torch": 7, "redstone_wall_torch": 7,
	"redstone_lamp": 15, "campfire": 15, "soul_campfire": 10,
	"copper_bulb": 15, "waxed_copper_bulb": 15,
	"exposed_copper_bulb": 12, "waxed_exposed_copper_bulb": 12,
	"weathered_copper_bulb": 8, "waxed_weathered_copper_bulb": 8,
	"oxidized_copper_bulb": 4, "waxed_oxidized_copper_bulb": 4,
}

var alwaysWater = map[string]bool{
	"bubble_column": true, "kelp": true, "kelp_plant": true, "seagrass": true, "tall_seagrass": true,
}

var replaceable = map[string]bool{
	"air": true, "cave_air": true, "void_air": true, "water": true, "lava": true,
	"short_grass": true, "tall_grass": true, "fern": true, "large_fern": true, "dead_bush": true,
	"seagrass": true, "tall_seagrass": true, "snow": true, "vine": true, "glow_lichen": true,
	"light": true, "structure_void": true, "fire": true, "soul_fire": true, "bush": true,
	"short_dry_grass": true, "tall_dry_grass": true, "leaf_litter": true, "resin_clump": true,
	"sculk_vein": true, "crimson_roots": true, "warped_roots": true, "nether_sprouts": true,
	"hanging_roots": true, "firefly_bush": false,
}

func stateEmission(b mcBlock, props map[string]string) int {
	atoi := func(s string) int {
		n := 0
		fmt.Sscanf(s, "%d", &n)
		return n
	}
	switch {
	case b.Name == "light":
		return atoi(props["level"])
	case b.Name == "sea_pickle":
		if props["waterlogged"] != "true" {
			return 0
		}
		return 3 + 3*atoi(props["pickles"])
	case b.Name == "respawn_anchor":
		return []int{0, 3, 7, 11, 15}[atoi(props["charges"])]
	case b.Name == "cave_vines" || b.Name == "cave_vines_plant":
		if props["berries"] == "true" {
			return 14
		}
		return 0
	case strings.HasSuffix(b.Name, "candle") && props["candles"] != "":
		if props["lit"] != "true" {
			return 0
		}
		return 3 * atoi(props["candles"])
	case strings.HasSuffix(b.Name, "candle_cake"):
		if props["lit"] == "true" {
			return 3
		}
		return 0
	}
	if v, ok := props["lit"]; ok {
		if v != "true" {
			return 0
		}
		if l, ok := litLevel[b.Name]; ok {
			return l
		}
	}
	return b.EmitLight
}

type writer struct{ b []byte }

func (w *writer) u8(v int)     { w.b = append(w.b, byte(v)) }
func (w *writer) uv(v int)     { w.b = binary.AppendUvarint(w.b, uint64(v)) }
func (w *writer) str(s string) { w.uv(len(s)); w.b = append(w.b, s...) }
func (w *writer) f32(v float64) {
	w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(float32(v)))
}
func (w *writer) f64(v float64) { w.b = binary.LittleEndian.AppendUint64(w.b, math.Float64bits(v)) }

func (w *writer) bool(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

func build(ver string, s source, in *inputs) ([]byte, string, error) {
	sort.Slice(in.blocks, func(i, j int) bool { return in.blocks[i].ID < in.blocks[j].ID })
	nStates := 0
	for i, b := range in.blocks {
		if b.ID != i {
			return nil, "", fmt.Errorf("block ids not dense at %s", b.Name)
		}
		if b.MinStateID != nStates {
			return nil, "", fmt.Errorf("state ids not contiguous at %s", b.Name)
		}
		n := 1
		for _, st := range b.States {
			n *= len(st.values())
		}
		if b.MaxStateID-b.MinStateID+1 != n {
			return nil, "", fmt.Errorf("%s: %d states, properties give %d", b.Name, b.MaxStateID-b.MinStateID+1, n)
		}
		nStates += n
	}

	// coordinate table + shape table
	coordIdx := map[float64]int{}
	var coords []float64
	shapeKeys := make([]string, 0, len(in.shapes.Shapes))
	for k := range in.shapes.Shapes {
		shapeKeys = append(shapeKeys, k)
	}
	sort.Slice(shapeKeys, func(i, j int) bool { return atoiS(shapeKeys[i]) < atoiS(shapeKeys[j]) })
	shapeID := map[string]int{}
	for i, k := range shapeKeys {
		shapeID[k] = i
		for _, box := range in.shapes.Shapes[k] {
			for _, c := range box {
				if _, ok := coordIdx[c]; !ok {
					coordIdx[c] = len(coords)
					coords = append(coords, c)
				}
			}
		}
	}
	if len(coords) > 255 {
		return nil, "", fmt.Errorf("too many distinct shape coordinates: %d", len(coords))
	}

	w := &writer{}
	w.b = append(w.b, "KRMCDAT1"...)
	w.str(ver)
	w.uv(s.protocol)
	w.uv(s.dataVersion)

	w.uv(len(coords))
	for _, c := range coords {
		w.f64(c)
	}
	w.uv(len(shapeKeys))
	for _, k := range shapeKeys {
		boxes := in.shapes.Shapes[k]
		w.uv(len(boxes))
		for _, box := range boxes {
			for _, c := range box {
				w.u8(coordIdx[c])
			}
		}
	}

	w.uv(len(in.blocks))
	w.uv(nStates)
	for _, b := range in.blocks {
		w.str(b.Name)
		w.uv(b.DefaultState - b.MinStateID)
		hard := -1.0
		if b.Hardness != nil {
			hard = *b.Hardness
		}
		w.f32(hard)
		w.f32(b.Resistance)
		w.bool(b.Diggable)
		w.uv(len(b.States))
		for _, st := range b.States {
			w.str(st.Name)
			vals := st.values()
			w.uv(len(vals))
			for _, v := range vals {
				w.str(v)
			}
		}
		n := b.MaxStateID - b.MinStateID + 1
		perState, err := shapesFor(in.shapes.Blocks[b.Name], n)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", b.Name, err)
		}
		props := map[string]string{}
		for i := 0; i < n; i++ {
			decode(b, i, props)
			sk := fmt.Sprint(perState[i])
			sid, ok := shapeID[sk]
			if !ok {
				return nil, "", fmt.Errorf("%s: unknown shape %s", b.Name, sk)
			}
			flags := stateFlags(b, props, in.shapes.Shapes[sk])
			w.u8(flags)
			w.u8(stateEmission(b, props)<<4 | b.FilterLight&15)
			w.uv(sid)
		}
	}

	sort.Slice(in.items, func(i, j int) bool { return in.items[i].ID < in.items[j].ID })
	w.uv(len(in.items))
	for i, it := range in.items {
		if it.ID != i {
			return nil, "", fmt.Errorf("item ids not dense at %s", it.Name)
		}
		w.str(it.Name)
		w.uv(it.StackSize)
	}

	sort.Slice(in.entities, func(i, j int) bool { return in.entities[i].ID < in.entities[j].ID })
	w.uv(len(in.entities))
	for i, e := range in.entities {
		if e.ID != i {
			return nil, "", fmt.Errorf("entity ids not dense at %s", e.Name)
		}
		w.str(e.Name)
		w.f32(e.Width)
		w.f32(e.Height)
		w.str(e.Type)
		w.str(e.Category)
	}

	biomes := append([]mcBiome(nil), in.biomes...)
	for _, eb := range s.extraBiomes {
		dup := false
		for _, b := range biomes {
			dup = dup || b.Name == eb.Name
		}
		if !dup {
			biomes = append(biomes, eb)
		}
	}
	// vanilla datapack registries are ordered by identifier
	sort.Slice(biomes, func(i, j int) bool { return biomes[i].Name < biomes[j].Name })
	w.uv(len(biomes))
	for _, b := range biomes {
		w.str(b.Name)
		w.str(b.Category)
		w.f32(b.Temperature)
		w.bool(b.Precipitation)
		w.str(b.Dimension)
		w.uv(b.Color)
	}

	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.Header.ModTime = time.Time{}
	if _, err := zw.Write(w.b); err != nil {
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	stats := fmt.Sprintf("%d blocks, %d states, %d shapes, %d items, %d entities, %d biomes",
		len(in.blocks), nStates, len(shapeKeys), len(in.items), len(in.entities), len(biomes))
	return gz.Bytes(), stats, nil
}

func shapesFor(raw json.RawMessage, n int) ([]int, error) {
	out := make([]int, n)
	if len(raw) == 0 {
		return out, nil
	}
	var one int
	if err := json.Unmarshal(raw, &one); err == nil {
		for i := range out {
			out[i] = one
		}
		return out, nil
	}
	var many []int
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	if len(many) != n {
		return nil, fmt.Errorf("%d shapes for %d states", len(many), n)
	}
	return many, nil
}

func decode(b mcBlock, idx int, props map[string]string) {
	for i := len(b.States) - 1; i >= 0; i-- {
		vals := b.States[i].values()
		props[b.States[i].Name] = vals[idx%len(vals)]
		idx /= len(vals)
	}
}

func stateFlags(b mcBlock, props map[string]string, boxes [][6]float64) int {
	f := 0
	switch b.Name {
	case "air", "cave_air", "void_air":
		f |= flagAir
	case "water", "lava":
		f |= flagLiquid | flagFluid
	}
	if alwaysWater[b.Name] || props["waterlogged"] == "true" {
		f |= flagFluid
	}
	if strings.HasSuffix(b.Name, "_leaves") {
		f |= flagLeaves
	}
	if b.Transparent {
		f |= flagTransparent
	}
	if replaceable[b.Name] {
		f |= flagReplaceable
	}
	if len(boxes) == 1 && boxes[0] == [6]float64{0, 0, 0, 1, 1, 1} {
		f |= flagFullCube
	}
	if blocksMotion(b.Name, boxes) {
		f |= flagSolid
	}
	if m, ok := predicateMask(b.Name, props); ok {
		old := f
		f = applyPredicates(f, m)
		if old != f {
			changedFlags[b.Name+" "+flagDiff(old, f)]++
		}
	}
	return f
}

// changedFlags counts states whose flags the vanilla predicates corrected.
var changedFlags = map[string]int{}

func flagDiff(old, f int) string {
	var out []string
	for _, b := range []struct {
		bit  int
		name string
	}{{flagAir, "air"}, {flagSolid, "solid"}, {flagFluid, "fluid"}, {flagLeaves, "leaves"}} {
		if old&b.bit != f&b.bit {
			if f&b.bit != 0 {
				out = append(out, "+"+b.name)
			} else {
				out = append(out, "-"+b.name)
			}
		}
	}
	return strings.Join(out, ",")
}

// predicates maps "minecraft:name" or "minecraft:name[k=v,...]" (sorted) to
// vanilla's heightmap mask "WS OF MB MBNL air fluid" (from the reference
// server, see tools/refbench predicates).
var predicates map[string]string

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func loadPredicates(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		r = zr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		k, v, ok := strings.Cut(line, "\t")
		if ok && len(v) == 6 {
			m[k] = v
		}
	}
	return m, nil
}

func predicateMask(name string, props map[string]string) (string, bool) {
	if predicates == nil {
		return "", false
	}
	full := "minecraft:" + name
	if m, ok := predicates[full]; ok {
		return m, true
	}
	kv := make([]string, 0, len(props))
	for k, v := range props {
		kv = append(kv, k+"="+v)
	}
	sort.Strings(kv)
	m, ok := predicates[full+"["+strings.Join(kv, ",")+"]"]
	return m, ok
}

// applyPredicates sets the flags heightmaps use so that WORLD_SURFACE =
// !air, OCEAN_FLOOR = solid, MOTION_BLOCKING = solid || fluid and
// MOTION_BLOCKING_NO_LEAVES = MOTION_BLOCKING && !leaves reproduce vanilla.
// Leaves therefore marks states excluded from MOTION_BLOCKING_NO_LEAVES
// (waterlogged leaves are not).
func applyPredicates(f int, m string) int {
	set := func(bit int, on bool) {
		if on {
			f |= bit
		} else {
			f &^= bit
		}
	}
	mb, mbnl := m[2] == '1', m[3] == '1'
	set(flagAir, m[4] == '1')
	set(flagSolid, m[1] == '1')
	set(flagFluid, m[5] == '1')
	set(flagLeaves, mb && !mbnl)
	return f
}

// blocksMotion mirrors BlockStateBase.calculateSolid: a non-empty collision
// shape whose bounds are large on average or a full block tall.
func blocksMotion(name string, boxes [][6]float64) bool {
	if name == "cobweb" || name == "bamboo_sapling" || len(boxes) == 0 {
		return false
	}
	mn := [3]float64{1e9, 1e9, 1e9}
	mx := [3]float64{-1e9, -1e9, -1e9}
	for _, b := range boxes {
		for a := 0; a < 3; a++ {
			mn[a] = min(mn[a], b[a])
			mx[a] = max(mx[a], b[a+3])
		}
	}
	sx, sy, sz := mx[0]-mn[0], mx[1]-mn[1], mx[2]-mn[2]
	return (sx+sy+sz)/3 >= 0.7291666666666666 || sy >= 1.0
}

func atoiS(s string) int {
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}
