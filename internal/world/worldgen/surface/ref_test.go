package surface

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

// Pinned cases: inputs are the reference noise fill (before the material
// pass) captured by kremenmc.probe.surface.Trace; expected outputs are hashes of
// the reference results with the vanilla rule ("full") and with an empty
// rule ("ext", extensions only). KREMENMC_REF=1 regenerates them after checking
// that our output equals the reference block for block.

type refCase struct {
	name, settings, dim string
	seed                int64
	chunks              []chunkspec.Pos
}

func borderFarChunks(t testing.TB) []chunkspec.Pos {
	ps, err := chunkspec.Parse("corner:0,0+corner:512,-512+corner:1000000,1000000", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []chunkspec.Pos
	for g := range 3 {
		idx := []int{5, 6, 9, 10}
		if g == 2 {
			idx = []int{5, 10}
		}
		for _, i := range idx {
			out = append(out, ps[g*16+i])
		}
	}
	return out
}

func around(cs ...[2]int32) []chunkspec.Pos {
	var out []chunkspec.Pos
	for _, c := range cs {
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				out = append(out, chunkspec.Pos{X: c[0] + dx, Z: c[1] + dz})
			}
		}
	}
	return out
}

func refCases(t testing.TB) []refCase {
	bf := borderFarChunks(t)
	return []refCase{
		{"overworld-0", "minecraft:overworld", "overworld", 0, bf},
		{"overworld-8266389061079214669", "minecraft:overworld", "overworld", 8266389061079214669, bf},
		{"nether-0", "minecraft:nether", "the_nether", 0, bf[:4]},
		{"end-0", "minecraft:end", "the_end", 0, bf[:4]},
		{"extensions-0", "minecraft:overworld", "overworld", 0, around([2]int32{0, -220}, [2]int32{-148, 148})},
		{"icebergs-8266389061079214669", "minecraft:overworld", "overworld", 8266389061079214669,
			[]chunkspec.Pos{{X: -1875, Z: -6249}, {X: -1874, Z: -6249}}},
	}
}

// biomeSource builds the biome source the reference uses for a settings
// entry of a vanilla dimension.
func biomeSource(t testing.TB, settings, dim string, seed int64) biome.Source {
	t.Helper()
	if dim == "overworld" && settings != "minecraft:overworld" {
		r, err := density.CachedRouter(settings, seed)
		if err != nil {
			t.Fatal(err)
		}
		l, err := biome.Preset("overworld")
		if err != nil {
			t.Fatal(err)
		}
		return &biome.MultiNoise{Router: r, List: l}
	}
	src, err := biome.ForDimension(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func stateHash(w *world.Chunk) uint64 {
	reg := data.Canonical()
	h := fnv.New64a()
	var b []byte
	for s := range w.Sections {
		for i := range 4096 {
			b = reg.AppendState(b[:0], data.StateID(w.Sections[s].Blocks.Get(i)))
			b = append(b, 0)
			h.Write(b)
		}
	}
	return h.Sum64()
}

func dimOf(s *System) world.Dimension {
	return world.Dimension{MinY: int(s.MinY), Height: int(s.Height), HasSkylight: s.Settings != "minecraft:nether"}
}

// cloneChunk copies blocks of a world chunk.
func cloneChunk(w *world.Chunk) *world.Chunk {
	plains, _ := data.Canonical().BiomeByName("plains")
	c := world.NewChunk(w.X, w.Z, w.Dim, plains)
	vals := make([]uint32, 4096)
	for i := range w.Sections {
		vals = w.Sections[i].Blocks.Values(vals)
		c.Sections[i].SetAllBlocks(vals)
	}
	c.RecalcHeightmaps()
	return c
}

// Input files: gzip of records x, z int32; palette (uint16 count, uint16
// length-prefixed strings); sections*4096 uint8 palette indices
// (y<<8|z<<4|x within a section).

func writeInputs(path string, chunks []*world.Chunk) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	reg := data.Canonical()
	for _, c := range chunks {
		binary.Write(zw, binary.BigEndian, [2]int32{c.X, c.Z})
		idx := map[data.StateID]uint8{}
		var pal []string
		var blocks []byte
		for s := range c.Sections {
			for i := range 4096 {
				st := data.StateID(c.Sections[s].Blocks.Get(i))
				k, ok := idx[st]
				if !ok {
					if len(pal) == 255 {
						return fmt.Errorf("chunk %d,%d: palette too large", c.X, c.Z)
					}
					k = uint8(len(pal))
					idx[st] = k
					pal = append(pal, reg.StateString(st))
				}
				blocks = append(blocks, k)
			}
		}
		binary.Write(zw, binary.BigEndian, uint16(len(pal)))
		for _, p := range pal {
			binary.Write(zw, binary.BigEndian, uint16(len(p)))
			zw.Write([]byte(p))
		}
		zw.Write(blocks)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func readInputs(t testing.TB, path string, dim world.Dimension) []*world.Chunk {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(zr)
	reg := data.Canonical()
	plains, _ := reg.BiomeByName("plains")
	var out []*world.Chunk
	for {
		var pos [2]int32
		if err := binary.Read(r, binary.BigEndian, &pos); err == io.EOF {
			return out
		} else if err != nil {
			t.Fatal(err)
		}
		var n uint16
		binary.Read(r, binary.BigEndian, &n)
		ids := make([]uint32, n)
		for i := range ids {
			s, err := readUTF(r)
			if err != nil {
				t.Fatal(err)
			}
			id, ok := reg.ParseState(s)
			if !ok {
				t.Fatalf("unknown state %s", s)
			}
			ids[i] = uint32(id)
		}
		c := world.NewChunk(pos[0], pos[1], dim, plains)
		raw := make([]byte, 4096)
		vals := make([]uint32, 4096)
		for s := range c.Sections {
			if _, err := io.ReadFull(r, raw); err != nil {
				t.Fatal(err)
			}
			for i, b := range raw {
				vals[i] = ids[b]
			}
			c.Sections[s].SetAllBlocks(vals)
		}
		c.RecalcHeightmaps()
		out = append(out, c)
	}
}

type pin struct {
	full, ext uint64
}

func readPins(t testing.TB) map[string]pin {
	b, err := os.ReadFile(filepath.Join("testdata", "pinned.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]pin{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(l)
		if len(f) != 5 || strings.HasPrefix(l, "#") {
			continue
		}
		full, _ := strconv.ParseUint(f[3], 16, 64)
		ext, _ := strconv.ParseUint(f[4], 16, 64)
		out[f[0]+" "+f[1]+" "+f[2]] = pin{full, ext}
	}
	return out
}

func runCase(t testing.TB, rc refCase, in []*world.Chunk) (full, ext []*world.Chunk) {
	t.Helper()
	sys, err := New(mustPack(t), rc.settings, rc.seed)
	if err != nil {
		t.Fatal(err)
	}
	bs := NewSourceBiomes(biomeSource(t, rc.settings, rc.dim, rc.seed), sys.MinY, sys.Height)
	ctx := NewCtx()
	for _, c := range in {
		f, e := cloneChunk(c), cloneChunk(c)
		if err := sys.Build(ctx, f, bs); err != nil {
			t.Fatal(err)
		}
		if err := sys.Extensions(ctx, e, bs); err != nil {
			t.Fatal(err)
		}
		full, ext = append(full, f), append(ext, e)
	}
	return full, ext
}

func TestPinned(t *testing.T) {
	if os.Getenv("KREMENMC_REF") == "1" {
		regenerate(t)
	}
	pins := readPins(t)
	n := 0
	for _, rc := range refCases(t) {
		t.Run(rc.name, func(t *testing.T) {
			sys, err := Vanilla(rc.settings, rc.seed)
			if err != nil {
				t.Fatal(err)
			}
			in := readInputs(t, filepath.Join("testdata", rc.name+".in.gz"), dimOf(sys))
			if len(in) != len(rc.chunks) {
				t.Fatalf("%d input chunks, want %d", len(in), len(rc.chunks))
			}
			full, ext := runCase(t, rc, in)
			for i, c := range in {
				k := fmt.Sprintf("%s %d %d", rc.name, c.X, c.Z)
				p, ok := pins[k]
				if !ok {
					t.Fatalf("no pin for %s", k)
				}
				if h := stateHash(full[i]); h != p.full {
					t.Errorf("%s: material pass hash %016x, want %016x", k, h, p.full)
				}
				if h := stateHash(ext[i]); h != p.ext {
					t.Errorf("%s: extensions hash %016x, want %016x", k, h, p.ext)
				}
				n++
			}
		})
	}
	t.Logf("%d pinned chunks", n)
}

// probeRun runs kremenmc.probe.surface.Trace for chunks and returns the noise,
// surface and ext records by position.
func probeRun(t testing.TB, ref *reftest.Ref, settings, dim string, seed int64, chunks []chunkspec.Pos) map[chunkspec.Pos][3]*probeChunk {
	t.Helper()
	dir := t.TempDir()
	args := []string{settings, dim, strconv.FormatInt(seed, 10), dir, "all-both"}
	for _, c := range chunks {
		args = append(args, fmt.Sprintf("%d,%d", c.X, c.Z))
	}
	var out bytes.Buffer
	if err := ref.Run(&out, "kremenmc.probe.surface.Trace", args...); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("probe: %s", out.String())
	}
	pcs, err := readProbeChunks(filepath.Join(dir, "blocks.bin.gz"))
	if err != nil {
		t.Fatal(err)
	}
	res := map[chunkspec.Pos][3]*probeChunk{}
	for _, pc := range pcs {
		k := chunkspec.Pos{X: pc.X, Z: pc.Z}
		r := res[k]
		switch pc.Tag {
		case "noise":
			r[0] = pc
		case "surface":
			r[1] = pc
		case "ext":
			r[2] = pc
		}
		res[k] = r
	}
	return res
}

func diffProbe(w *world.Chunk, pc *probeChunk) int {
	reg := data.Canonical()
	bad := 0
	for s := range w.Sections {
		for i := range 4096 {
			if reg.StateString(data.StateID(w.Sections[s].Blocks.Get(i))) != pc.Palette[pc.Blocks[s*4096+i]] {
				bad++
			}
		}
	}
	return bad
}

func regenerate(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Fatal(err)
	}
	var pins strings.Builder
	pins.WriteString("# case x z full ext (fnv64a of state strings; see ref_test.go)\n")
	for _, rc := range refCases(t) {
		sys, err := Vanilla(rc.settings, rc.seed)
		if err != nil {
			t.Fatal(err)
		}
		dim := dimOf(sys)
		got := probeRun(t, ref, rc.settings, rc.dim, rc.seed, rc.chunks)
		var in []*world.Chunk
		for _, p := range rc.chunks {
			r := got[p]
			if r[0] == nil || r[1] == nil || r[2] == nil {
				t.Fatalf("%s: probe missed chunk %v", rc.name, p)
			}
			in = append(in, r[0].worldChunk(t, dim))
		}
		full, ext := runCase(t, rc, in)
		for i, p := range rc.chunks {
			r := got[p]
			if d := diffProbe(full[i], r[1]); d != 0 {
				t.Fatalf("%s %v: %d blocks differ from the reference material pass", rc.name, p, d)
			}
			if d := diffProbe(ext[i], r[2]); d != 0 {
				t.Fatalf("%s %v: %d blocks differ from the reference extensions", rc.name, p, d)
			}
			fmt.Fprintf(&pins, "%s %d %d %016x %016x\n", rc.name, p.X, p.Z, stateHash(full[i]), stateHash(ext[i]))
		}
		if err := writeInputs(filepath.Join("testdata", rc.name+".in.gz"), in); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join("testdata", "pinned.txt"), []byte(pins.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOracleDumps checks every chunk of the stage-oracle dumps in the
// reference cache (refbench stages -structures=false): our material pass
// over the reference noise fill must equal the "surface" dump, and the
// extensions alone the "noise" dump (the bench's empty-rule variant).
// It needs KREMENMC_REF=1 and the cache.
func TestOracleDumps(t *testing.T) {
	if os.Getenv("KREMENMC_REF") != "1" {
		t.Skip("set KREMENMC_REF=1 (needs .refcache stage dumps)")
	}
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	dirs, _ := filepath.Glob(filepath.Join(ref.Dir, "stages", "*-nostructures"))
	if len(dirs) == 0 {
		t.Skip("no -nostructures stage dumps")
	}
	for _, dir := range dirs {
		base := strings.TrimSuffix(filepath.Base(dir), "-nostructures")
		i := strings.IndexByte(base, '-')
		seed, err := strconv.ParseInt(base[:i], 10, 64)
		if err != nil {
			continue
		}
		dimName := base[i+1:]
		settings, err := SettingsFor(dimName)
		if err != nil {
			continue
		}
		surf, err1 := oracle.ReadDump(oracle.DumpPath(dir, "surface"))
		noiseD, err2 := oracle.ReadDump(oracle.DumpPath(dir, "noise"))
		if err1 != nil || err2 != nil {
			continue
		}
		t.Run(base, func(t *testing.T) {
			var chunks []chunkspec.Pos
			for _, c := range surf {
				chunks = append(chunks, chunkspec.Pos{X: c.X, Z: c.Z})
			}
			sys, err := Vanilla(settings, seed)
			if err != nil {
				t.Fatal(err)
			}
			dim := dimOf(sys)
			got := probeRun(t, ref, settings, dimName, seed, chunks)
			var in []*world.Chunk
			for _, p := range chunks {
				in = append(in, got[p][0].worldChunk(t, dim))
			}
			full, ext := runCase(t, refCase{base, settings, dimName, seed, chunks}, in)
			byPos := func(cs []*anvilRaw) map[chunkspec.Pos]*anvilRaw {
				m := map[chunkspec.Pos]*anvilRaw{}
				for _, c := range cs {
					m[chunkspec.Pos{X: c.X, Z: c.Z}] = c
				}
				return m
			}
			sm, nm := byPos(surf), byPos(noiseD)
			total, badF, badE := 0, 0, 0
			for i, p := range chunks {
				f, e := rawDiff(full[i], sm[p]), rawDiff(ext[i], nm[p])
				total += len(full[i].Sections) * 4096
				badF += f
				badE += e
				if f+e > 0 {
					t.Errorf("chunk %v: %d blocks differ from the surface dump, %d from the noise dump", p, f, e)
				}
			}
			t.Logf("%s: %d chunks, %d blocks: material pass %d mismatches, extensions %d mismatches", base, len(chunks), total, badF, badE)
		})
	}
}

type anvilRaw = anvil.RawChunk

func rawDiff(w *world.Chunk, rc *anvil.RawChunk) int {
	if rc == nil {
		return 0 // chunk not in that dump
	}
	reg := data.Canonical()
	bad := 0
	for s := range w.Sections {
		for i := range 4096 {
			x, y, z := i&15, i>>8, (i>>4)&15
			if reg.StateString(data.StateID(w.Sections[s].Blocks.Get(i))) != rc.Block(x, w.Dim.MinY+s*16+y, z) {
				bad++
			}
		}
	}
	return bad
}
