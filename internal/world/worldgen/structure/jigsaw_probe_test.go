package structure_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

type probeStart struct {
	id     string
	cx, cz int32
	none   bool
	pieces []string
}

// readJigNBT parses kremenmc.probe.jigsaw.JigNBT output.
func readJigNBT(t *testing.T, path string) []probeStart {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no probe data")
	}
	var out []probeStart
	id := ""
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) == 2 && f[0] == "STRUCTURE":
			id = f[1]
		case len(f) >= 3 && (f[0] == "START" || f[0] == "NONE"):
			var s probeStart
			s.id, s.none = id, f[0] == "NONE"
			fmt.Sscan(f[1], &s.cx)
			fmt.Sscan(f[2], &s.cz)
			out = append(out, s)
		case strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "    ") && len(out) > 0:
			out[len(out)-1].pieces = append(out[len(out)-1].pieces, strings.TrimSpace(l))
		}
	}
	return out
}

// TestJigsawProbeStarts compares kremenmc's jigsaw starts with JigNBT probe
// output piece for piece (fresh pieces): testdata/jigsaw/<file>.txt, named
// <seed>_<dim>_<what>.txt. KREMENMC_JIGPROBE limits to one structure id.
func TestJigsawProbeStarts(t *testing.T) {
	files, _ := os.ReadDir("testdata/jigsaw")
	for _, fe := range files {
		name := fe.Name()
		var seed int64
		var dim string
		parts := strings.SplitN(strings.TrimSuffix(name, ".txt"), "_", 3)
		if len(parts) < 3 || !strings.HasSuffix(name, "_starts.txt") {
			continue
		}
		fmt.Sscan(parts[0], &seed)
		dim = parts[1]
		if dim == "nether" {
			dim = "the_nether"
		}
		t.Run(name, func(t *testing.T) {
			ps := readJigNBT(t, "testdata/jigsaw/"+name)
			only := os.Getenv("KREMENMC_JIGPROBE")
			ids := map[string]bool{}
			for _, p := range ps {
				ids[p.id] = true
			}
			for id := range ids {
				if only == "" || only == id {
					structure.EnableJigsaw(id)
				}
			}
			tr, err := vanilla.NewTerrain(dim, seed)
			if err != nil {
				t.Fatal(err)
			}
			ss, err := vanilla.StructureStagesForTest(tr, seed)
			if err != nil {
				t.Fatal(err)
			}
			res := map[string][2]int{}
			for _, p := range ps {
				if only != "" && p.id != only {
					continue
				}
				sts, err := ss.StartsAt(p.cx, p.cz)
				if err != nil {
					t.Fatalf("%s %d,%d: %v", p.id, p.cx, p.cz, err)
				}
				var got []string
				for _, st := range sts {
					if st.ID != p.id {
						continue
					}
					for _, pc := range st.Pieces {
						got = append(got, refPieceKey(pc.Save()))
					}
				}
				r := res[p.id]
				r[1]++
				same := len(got) == len(p.pieces)
				first := -1
				for i := 0; i < len(got) || i < len(p.pieces); i++ {
					if i >= len(got) || i >= len(p.pieces) || got[i] != p.pieces[i] {
						same = false
						first = i
						break
					}
				}
				if same {
					r[0]++
				} else if r[1]-r[0] <= 2 || os.Getenv("KREMENMC_V") != "" {
					w, g := "-", "-"
					if first >= 0 && first < len(p.pieces) {
						w = p.pieces[first]
					}
					if first >= 0 && first < len(got) {
						g = got[first]
					}
					t.Errorf("%s %d,%d: ref %d pieces, kremenmc %d; first diff #%d\n  ref      %s\n  kremenmc %s", p.id, p.cx, p.cz, len(p.pieces), len(got), first, w, g)
				}
				res[p.id] = r
			}
			for id, r := range res {
				t.Logf("%s: %d/%d chunks identical", id, r[0], r[1])
			}
		})
	}
}

// TestJigsawProbeDump writes kremenmc's pieces of one start in JigNBT
// layout (KREMENMC_JIGDUMPAT=id@seed@cx,cz@file).
func TestJigsawProbeDump(t *testing.T) {
	v := os.Getenv("KREMENMC_JIGDUMPAT")
	if v == "" {
		t.Skip()
	}
	p := strings.Split(v, "@")
	var seed int64
	var cx, cz int32
	fmt.Sscan(p[1], &seed)
	fmt.Sscanf(p[2], "%d,%d", &cx, &cz)
	structure.EnableJigsaw(p[0])
	dim := "overworld"
	if len(p) > 4 {
		dim = p[4]
	}
	tr, err := vanilla.NewTerrain(dim, seed)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := vanilla.StructureStagesForTest(tr, seed)
	if err != nil {
		t.Fatal(err)
	}
	draws := structure.JigsawDraws()
	sts, err := ss.StartsAt(cx, cz)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, st := range sts {
		if st.ID != p[0] {
			continue
		}
		fmt.Fprintf(&b, "START %d %d pieces %d draws %d\n", cx, cz, len(st.Pieces), len(draws()))
		for _, pc := range st.Pieces {
			fmt.Fprintf(&b, "  %s\n", refPieceKey(pc.Save()))
		}
	}
	os.WriteFile(p[3], []byte(b.String()), 0o644)
	os.WriteFile(p[3]+".trace", []byte(strings.Join(structure.JigsawTrace(), "\n")+"\n"), 0o644)
}

// biomeStageProbe answers biome questions from the biomes stage of a
// scratch chunk (Nether: the structure stages are overworld only).
type biomeStageProbe struct {
	tr    *vanilla.Terrain
	dim   world.Dimension
	cache map[world.ChunkPos]*world.Chunk
}

func (p *biomeStageProbe) Biome(x, y, z int32) data.BiomeID {
	cp := world.ChunkPos{X: x >> 4, Z: z >> 4}
	c := p.cache[cp]
	if c == nil {
		c = world.NewChunk(cp.X, cp.Z, p.dim, 0)
		if err := p.tr.GenerateStage(c, "biomes"); err != nil {
			panic(err)
		}
		p.cache[cp] = c
	}
	return c.Biome(int(x&15), int(y), int(z&15))
}

func (p *biomeStageProbe) Height(x, z int32, hm string) int32 { return 0 }

// TestJigsawProbeNether compares Nether jigsaw starts (bastion remnants)
// with JigNBT probe output (testdata/jigsaw/nether/<seed>.txt).
func TestJigsawProbeNether(t *testing.T) {
	files, _ := os.ReadDir("testdata/jigsaw/nether")
	for _, fe := range files {
		var seed int64
		fmt.Sscan(strings.TrimSuffix(fe.Name(), ".txt"), &seed)
		ps := readJigNBT(t, "testdata/jigsaw/nether/"+fe.Name())
		tr, err := vanilla.NewTerrain("the_nether", seed)
		if err != nil {
			t.Fatal(err)
		}
		pr := &biomeStageProbe{tr: tr, dim: world.Dimension{MinY: 0, Height: 256}, cache: map[world.ChunkPos]*world.Chunk{}}
		ok, n := 0, 0
		for _, p := range ps {
			pcs, err := structure.GenerateJigsawForTest(p.id, seed, p.cx, p.cz, pr, 0, 256)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, pc := range pcs {
				got = append(got, refPieceKey(pc.Save()))
			}
			n++
			if strings.Join(got, "\n") == strings.Join(p.pieces, "\n") {
				ok++
				continue
			}
			first := 0
			for first < len(got) && first < len(p.pieces) && got[first] == p.pieces[first] {
				first++
			}
			if n-ok <= 3 {
				w, g := "-", "-"
				if first < len(p.pieces) {
					w = p.pieces[first]
				}
				if first < len(got) {
					g = got[first]
				}
				t.Errorf("%s %d,%d: ref %d pieces, kremenmc %d; first diff #%d\n  ref      %s\n  kremenmc %s", p.id, p.cx, p.cz, len(p.pieces), len(got), first, w, g)
			}
		}
		t.Logf("seed %d: %d/%d chunks identical", seed, ok, n)
	}
}

func TestJigsawColumn(t *testing.T) {
	v := os.Getenv("KREMENMC_JCOL") // id@seed@cx,cz@x,z
	if v == "" {
		t.Skip()
	}
	p := strings.Split(v, "@")
	var seed int64
	var cx, cz, x, z int32
	fmt.Sscan(p[1], &seed)
	fmt.Sscanf(p[2], "%d,%d", &cx, &cz)
	fmt.Sscanf(p[3], "%d,%d", &x, &z)
	structure.EnableJigsaw(p[0])
	tr, _ := vanilla.NewTerrain("overworld", seed)
	ss, _ := vanilla.StructureStagesForTest(tr, seed)
	sts, _ := ss.StartsAt(cx, cz)
	for _, st := range sts {
		if st.ID == p[0] {
			for _, l := range structure.ColumnTemplateBlocks(st.Pieces, x, z) {
				t.Log(l)
			}
		}
	}
}

func TestJigsawRefPieceNBT(t *testing.T) {
	v := os.Getenv("KREMENMC_JNBT") // id@cx,cz@index
	if v == "" {
		t.Skip()
	}
	p := strings.Split(v, "@")
	var i int
	fmt.Sscan(p[2], &i)
	ref := refStarts(t, refWorldRegion)
	c := ref[p[0]+"@"+p[1]]
	if c == nil {
		t.Fatal("no start")
	}
	ch := c["Children"].(nbt.List).Items[i].(nbt.Compound)
	for k, v := range ch {
		if k != "junctions" {
			t.Logf("%s = %v", k, v)
		}
	}
}
