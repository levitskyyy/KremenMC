// Package oracle drives the stage oracle (tools/refprobe/java/kremenmc/probe/bench/
// StageOracle.java): the official server boots in-process and its own chunk
// system generates each requested chunk to a given status, which the probe
// dumps as vanilla chunk NBT.
//
// 26.3 merged noise, surface and carvers into one TERRAIN status, so the
// sub-stages are isolated with data-pack variants (pure data, no code):
//
//	biomes   vanilla world, chunks requested at BIOMES
//	noise    TERRAIN with an empty material_rule and no biome carvers (noise fill + aquifers only)
//	surface  TERRAIN without biome carvers (noise + material rules)
//	terrain  vanilla TERRAIN (noise + material rules + carvers); alias "carvers"
//
// Requests go to a datapack dimension kremenmc:stage that copies the target
// dimension's generator from the normal world preset, so chunks generated
// during world creation (spawn search) never contaminate the dumps; -real
// requests the vanilla dimension instead (used to validate the copy).
package oracle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
	"github.com/levitskyyy/KremenMC/tools/refbench/refserver"
)

const ProbeClass = "kremenmc.probe.bench.StageOracle"

// StageDim is the datapack dimension that copies the target dimension.
const StageDim = "kremenmc:stage"

// Stages in pipeline order.
var Stages = []string{"biomes", "noise", "surface", "terrain"}

type stageDef struct {
	variant string // datapack variant
	status  string // vanilla chunk status requested
}

var stageDefs = map[string]stageDef{
	"biomes":  {"vanilla", "biomes"},
	"noise":   {"noise", "terrain"},
	"surface": {"surface", "terrain"},
	"terrain": {"vanilla", "terrain"},
}

// Canonical maps aliases to stage names ("carvers" is "terrain").
func Canonical(stage string) (string, bool) {
	if stage == "carvers" {
		stage = "terrain"
	}
	_, ok := stageDefs[stage]
	return stage, ok
}

type Options struct {
	Ref        *reftest.Ref
	Seed       int64
	Dim        string
	Spec       string
	Spawn      *[2]int // for the spawn chunk spec term
	Stages     []string
	Structures bool
	Real       bool // request the vanilla dimension instead of the copy
	Threads    int
	Memory     string
	Keep       bool // keep the server instances
	Log        io.Writer
	Echo       io.Writer
}

type Manifest struct {
	Version    string            `json:"version"`
	Seed       int64             `json:"seed"`
	Dimension  string            `json:"dimension"`
	Requested  string            `json:"requestedDimension"`
	Structures bool              `json:"structures"`
	Stages     map[string]Record `json:"stages"`
}

type Record struct {
	Variant string          `json:"variant"`
	Status  string          `json:"status"`
	Spec    string          `json:"spec"`
	Chunks  []chunkspec.Pos `json:"chunks"` // all chunks in the dump
	Updated time.Time       `json:"updated"`
	Ms      int64           `json:"ms"`
}

// Dir is where dumps for a seed/dimension live.
func Dir(ref *reftest.Ref, seed int64, dim string, structures, real bool) string {
	name := fmt.Sprintf("%d-%s", seed, refgen.DimName(dim))
	if !structures {
		name += "-nostructures"
	}
	if real {
		name += "-real"
	}
	return filepath.Join(ref.Dir, "stages", name)
}

// DumpPath is the dump file of one stage.
func DumpPath(dir, stage string) string { return filepath.Join(dir, stage+".dump") }

// Run generates the requested stages and merges them into the dumps.
func Run(ctx context.Context, o Options) (*Manifest, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	dim := refgen.DimID(o.Dim)
	switch dim {
	case "minecraft:overworld", "minecraft:the_nether", "minecraft:the_end":
	default:
		return nil, fmt.Errorf("oracle: unknown dimension %q", o.Dim)
	}
	chunks, err := chunkspec.Parse(o.Spec, o.Spawn)
	if err != nil {
		return nil, err
	}
	byVariant := map[string][]string{}
	var variants []string
	for _, s := range o.Stages {
		c, ok := Canonical(s)
		if !ok {
			return nil, fmt.Errorf("oracle: unknown stage %q (want %s)", s, strings.Join(Stages, ", "))
		}
		v := stageDefs[c].variant
		if byVariant[v] == nil {
			variants = append(variants, v)
		}
		if !slices.Contains(byVariant[v], c) {
			byVariant[v] = append(byVariant[v], c)
		}
	}
	dir := Dir(o.Ref, o.Seed, dim, o.Structures, o.Real)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manifest{Stages: map[string]Record{}}
	if b, err := os.ReadFile(filepath.Join(dir, "stages.json")); err == nil {
		if err := json.Unmarshal(b, m); err != nil {
			return nil, fmt.Errorf("oracle: stages.json: %w", err)
		}
	}
	m.Version, m.Seed, m.Dimension, m.Structures = reftest.Version, o.Seed, dim, o.Structures
	m.Requested = StageDim
	if o.Real {
		m.Requested = dim
	}
	classes, err := o.Ref.Compile(ProbeClass)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(classes)
	for _, v := range variants {
		if err := runVariant(ctx, o, dim, dir, v, byVariant[v], chunks, classes, m); err != nil {
			return nil, err
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "stages.json"), append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func runVariant(ctx context.Context, o Options, dim, dir, variant string, stages []string, chunks []chunkspec.Pos, classes string, m *Manifest) error {
	inst := filepath.Join(dir, "work", variant)
	if err := os.RemoveAll(inst); err != nil {
		return err
	}
	pack := filepath.Join(inst, "world", "datapacks", "kremenmc_stage")
	if err := WriteDatapack(o.Ref.Jar, pack, dim, variant, o.Real); err != nil {
		return err
	}
	var cfg strings.Builder
	fmt.Fprintf(&cfg, "dim %s\n", m.Requested)
	parts := map[string]string{}
	for _, s := range stages {
		parts[s] = filepath.Join(inst, s+".part")
		fmt.Fprintf(&cfg, "stage %s %s %s\n", s, stageDefs[s].status, parts[s])
	}
	for _, c := range chunks {
		fmt.Fprintf(&cfg, "chunk %d %d\n", c.X, c.Z)
	}
	cfgPath := filepath.Join(inst, "oracle.cfg")
	if err := os.WriteFile(cfgPath, []byte(cfg.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(o.Log, "stage oracle: variant %s, stages %v, %d chunks in %s\n", variant, stages, len(chunks), m.Requested)
	began := time.Now()
	srv, err := refserver.Start(ctx, refserver.Config{
		Classpath:  append([]string{classes, o.Ref.Jar}, o.Ref.Libs...),
		MainClass:  ProbeClass,
		MainArgs:   []string{cfgPath},
		Dir:        inst,
		Seed:       o.Seed,
		Structures: o.Structures,
		Memory:     o.Memory,
		Threads:    o.Threads,
		JVMArgs:    []string{"--add-opens", "java.base/java.lang=ALL-UNNAMED"},
		Props:      map[string]string{"initial-enabled-packs": "vanilla,file/kremenmc_stage"},
		Echo:       o.Echo,
	})
	if err != nil {
		return err
	}
	_, line, err := srv.WaitLine(ctx, 0, func(l string) bool {
		return strings.Contains(l, "kremenmc-oracle: ok") || strings.Contains(l, "kremenmc-oracle: failed")
	})
	if err != nil {
		srv.Kill()
		return fmt.Errorf("oracle: %w\n%s", err, srv.Tail(40))
	}
	select {
	case <-srv.Exited():
	case <-time.After(3 * time.Minute):
		srv.Kill()
	case <-ctx.Done():
		srv.Kill()
		return ctx.Err()
	}
	if !strings.Contains(line, "kremenmc-oracle: ok") {
		return fmt.Errorf("oracle: %s\n%s", line, srv.Tail(40))
	}
	for _, s := range stages {
		fresh, err := ReadDump(parts[s])
		if err != nil {
			return err
		}
		want := stageDefs[s].status
		for _, c := range fresh {
			if c.Status != "minecraft:"+want {
				fmt.Fprintf(o.Log, "warning: %s chunk %d,%d has status %s, requested %s (already generated further)\n", s, c.X, c.Z, c.Status, want)
			}
		}
		all, err := mergeDump(DumpPath(dir, s), parts[s])
		if err != nil {
			return err
		}
		m.Stages[s] = Record{Variant: variant, Status: want, Spec: o.Spec, Chunks: all, Updated: time.Now().UTC(), Ms: time.Since(began).Milliseconds()}
		fmt.Fprintf(o.Log, "  %s: %d chunks (%d in dump) -> %s\n", s, len(fresh), len(all), DumpPath(dir, s))
	}
	if !o.Keep {
		_ = os.RemoveAll(inst)
	}
	return nil
}

// mergeDump adds the records of part to the dump at dst (replacing records
// of the same chunk) and returns the chunk list.
func mergeDump(dst, part string) ([]chunkspec.Pos, error) {
	recs := map[chunkspec.Pos][]byte{}
	var order []chunkspec.Pos
	add := func(p string) error {
		return eachRecord(p, func(b []byte) error {
			_, root, _, err := nbt.Unmarshal(b, false)
			if err != nil {
				return err
			}
			x, _ := root["xPos"].(int32)
			z, _ := root["zPos"].(int32)
			k := chunkspec.Pos{X: x, Z: z}
			if _, ok := recs[k]; !ok {
				order = append(order, k)
			}
			recs[k] = slices.Clone(b)
			return nil
		})
	}
	if err := add(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := add(part); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	for _, k := range order {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(recs[k])))
		zw.Write(n[:])
		zw.Write(recs[k])
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst+".tmp", buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return order, os.Rename(dst+".tmp", dst)
}

func eachRecord(p string, fn func([]byte) error) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReader(f))
	if err != nil {
		return fmt.Errorf("oracle: %s: %w", p, err)
	}
	for {
		var n [4]byte
		if _, err := io.ReadFull(zr, n[:]); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("oracle: %s: %w", p, err)
		}
		l := binary.BigEndian.Uint32(n[:])
		if l > 64<<20 {
			return fmt.Errorf("oracle: %s: record of %d bytes", p, l)
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(zr, b); err != nil {
			return fmt.Errorf("oracle: %s: %w", p, err)
		}
		if err := fn(b); err != nil {
			return fmt.Errorf("oracle: %s: %w", p, err)
		}
	}
}

// ReadDump decodes every chunk of a dump file.
func ReadDump(p string) ([]*anvil.RawChunk, error) {
	var out []*anvil.RawChunk
	err := eachRecord(p, func(b []byte) error {
		_, root, _, err := nbt.Unmarshal(b, false)
		if err != nil {
			return err
		}
		c, err := anvil.DecodeRaw(root)
		if err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// WriteDatapack writes the kremenmc_stage data pack for a variant: the kremenmc:stage
// dimension (a copy of dim from the normal world preset) and, for the noise
// and surface variants, biomes without carvers and an empty material rule.
//
// With inPlace the empty material rule overrides the vanilla noise settings
// (needed when requesting the vanilla dimension); otherwise it goes into
// kremenmc:stage_noise used only by kremenmc:stage, so world creation's spawn search
// in the overworld still sees vanilla terrain.
func WriteDatapack(serverJar, dir, dim, variant string, inPlace bool) error {
	zr, err := zip.OpenReader(serverJar)
	if err != nil {
		return fmt.Errorf("oracle: %w", err)
	}
	defer zr.Close()
	read := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, fmt.Errorf("oracle: %s: %w", name, err)
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	write := func(rel string, b []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0o644)
	}
	var version struct {
		Pack struct {
			Data int `json:"data_major"`
		} `json:"pack_version"`
	}
	b, err := read("version.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &version); err != nil {
		return fmt.Errorf("oracle: version.json: %w", err)
	}
	f := version.Pack.Data
	meta := fmt.Sprintf(`{"pack":{"description":"kremenmc stage oracle (%s)","pack_format":%d,"min_format":%d,"max_format":%d}}`, variant, f, f, f)
	if err := write("pack.mcmeta", []byte(meta)); err != nil {
		return err
	}
	var preset struct {
		Dimensions map[string]json.RawMessage `json:"dimensions"`
	}
	if b, err = read("data/minecraft/worldgen/world_preset/normal.json"); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &preset); err != nil {
		return fmt.Errorf("oracle: normal preset: %w", err)
	}
	stem, ok := preset.Dimensions[dim]
	if !ok {
		return fmt.Errorf("oracle: normal preset has no %s", dim)
	}
	if variant == "vanilla" {
		return write("data/kremenmc/dimension/stage.json", stem)
	}
	for _, zf := range zr.File {
		if !strings.HasPrefix(zf.Name, "data/minecraft/worldgen/biome/") || path.Ext(zf.Name) != ".json" {
			continue
		}
		b, err := read(zf.Name)
		if err != nil {
			return err
		}
		var biome map[string]json.RawMessage
		if err := json.Unmarshal(b, &biome); err != nil {
			return fmt.Errorf("oracle: %s: %w", zf.Name, err)
		}
		biome["carvers"] = json.RawMessage("[]")
		if b, err = json.Marshal(biome); err != nil {
			return err
		}
		if err := write(zf.Name, b); err != nil {
			return err
		}
	}
	if variant != "noise" {
		return write("data/kremenmc/dimension/stage.json", stem)
	}
	var gen struct {
		Generator struct {
			Settings string `json:"settings"`
		} `json:"generator"`
	}
	if err := json.Unmarshal(stem, &gen); err != nil || gen.Generator.Settings == "" {
		return fmt.Errorf("oracle: %s generator settings: %v", dim, err)
	}
	ns, name, _ := strings.Cut(gen.Generator.Settings, ":")
	rel := "data/" + ns + "/worldgen/noise_settings/" + name + ".json"
	if b, err = read(rel); err != nil {
		return err
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(b, &settings); err != nil {
		return fmt.Errorf("oracle: %s: %w", rel, err)
	}
	settings["material_rule"] = json.RawMessage(`{"type":"minecraft:sequence","sequence":[]}`)
	if b, err = json.Marshal(settings); err != nil {
		return err
	}
	if inPlace {
		if err := write(rel, b); err != nil {
			return err
		}
		return write("data/kremenmc/dimension/stage.json", stem)
	}
	if err := write("data/kremenmc/worldgen/noise_settings/stage_noise.json", b); err != nil {
		return err
	}
	var st map[string]any
	if err := json.Unmarshal(stem, &st); err != nil {
		return err
	}
	st["generator"].(map[string]any)["settings"] = "kremenmc:stage_noise"
	if stem, err = json.Marshal(st); err != nil {
		return err
	}
	return write("data/kremenmc/dimension/stage.json", stem)
}

// HeightmapPredicates writes vanilla's heightmap predicates for every block
// state (see HeightmapPredicates.java for the format) to out, from a
// started reference server (the predicates depend on loaded block tags).
func HeightmapPredicates(ctx context.Context, ref *reftest.Ref, out string, log io.Writer) error {
	if log == nil {
		log = io.Discard
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	inst := filepath.Join(ref.Dir, "stages", "work-predicates")
	if err := os.RemoveAll(inst); err != nil {
		return err
	}
	defer os.RemoveAll(inst)
	if err := os.MkdirAll(inst, 0o755); err != nil {
		return err
	}
	plain := filepath.Join(inst, "predicates.tsv")
	cfg := filepath.Join(inst, "oracle.cfg")
	if err := os.WriteFile(cfg, []byte("dim minecraft:overworld\nheightmaps "+plain+"\n"), 0o644); err != nil {
		return err
	}
	classes, err := ref.Compile(ProbeClass)
	if err != nil {
		return err
	}
	defer os.RemoveAll(classes)
	srv, err := refserver.Start(ctx, refserver.Config{
		Classpath: append([]string{classes, ref.Jar}, ref.Libs...),
		MainClass: ProbeClass, MainArgs: []string{cfg}, Dir: inst,
		JVMArgs: []string{"--add-opens", "java.base/java.lang=ALL-UNNAMED"},
	})
	if err != nil {
		return err
	}
	_, line, err := srv.WaitLine(ctx, 0, func(l string) bool {
		return strings.Contains(l, "kremenmc-oracle: ok") || strings.Contains(l, "kremenmc-oracle: failed")
	})
	if err != nil {
		srv.Kill()
		return fmt.Errorf("oracle: %w", err)
	}
	select {
	case <-srv.Exited():
	case <-time.After(3 * time.Minute):
		srv.Kill()
	}
	if !strings.Contains(line, "kremenmc-oracle: ok") {
		return fmt.Errorf("oracle: %s\n%s", line, srv.Tail(40))
	}
	b, err := os.ReadFile(plain)
	if err != nil {
		return err
	}
	if strings.HasSuffix(out, ".gz") {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(b)
		if err := zw.Close(); err != nil {
			return err
		}
		b = buf.Bytes()
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(log, "wrote %s\n", out)
	return nil
}
