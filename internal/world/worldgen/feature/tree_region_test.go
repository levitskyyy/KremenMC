package feature_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
	"github.com/levitskyyy/KremenMC/tools/refbench/refserver"
)

// TestTreeRegion decorates chunks centre-only (neighbours at terrain, no
// structures) in kremenmc and in the reference (kremenmc.probe.feat_region.Region)
// and writes both traces to TREEREGION_DIR (java.txt is reused unless
// KREMENMC_REF=1). TREEREGION=seed:cx,cz[;cx,cz...], TREEREGION_KEEP=a,b.
// TREEREGION_FIND=seed:x0,z0,x1,z1,biome lists chunks of a biome instead.
func TestTreeRegion(t *testing.T) {
	if f := os.Getenv("TREEREGION_FIND"); f != "" {
		findBiome(t, f)
		return
	}
	spec := os.Getenv("TREEREGION")
	if spec == "" {
		t.Skip()
	}
	dir := os.Getenv("TREEREGION_DIR")
	if dir == "" {
		t.Fatal("TREEREGION_DIR")
	}
	seedS, cs, _ := strings.Cut(spec, ":")
	seed, _ := strconv.ParseInt(seedS, 10, 64)
	var chunks []world.ChunkPos
	for _, c := range strings.Split(cs, ";") {
		a, b, _ := strings.Cut(c, ",")
		x, _ := strconv.Atoi(a)
		z, _ := strconv.Atoi(b)
		chunks = append(chunks, world.ChunkPos{X: int32(x), Z: int32(z)})
	}
	keep := strings.Split(os.Getenv("TREEREGION_KEEP"), ",")
	jpath := filepath.Join(dir, "java.txt")
	if _, err := os.Stat(jpath); err != nil || os.Getenv("KREMENMC_REF") == "1" {
		runRegionProbe(t, seed, chunks, keep, dir, jpath)
	}
	gpath := filepath.Join(dir, "go.txt")
	f, err := os.Create(gpath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := feature.NewStage("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	var cur world.ChunkPos
	stages := []gen.Stage{
		gen.StageFunc{S: gen.StatusBiomes, F: func(ctx *gen.StageContext) error {
			return tr.GenerateStage(ctx.Chunk().Chunk, "biomes")
		}},
		gen.StageFunc{S: gen.StatusTerrain, F: func(ctx *gen.StageContext) error {
			pc := ctx.Chunk()
			if err := tr.GenerateStage(pc.Chunk, "terrain"); err != nil {
				return err
			}
			if pc.Data == nil {
				pc.Data = nbt.Compound{}
			}
			pc.Data[gen.AnvilWGKey] = anvil.PackWG(pc.Chunk)
			return nil
		}},
		gen.StageFunc{S: gen.StatusFeatures, F: func(ctx *gen.StageContext) error {
			if ctx.Pos() != cur {
				return nil // the pipeline's decoration order; the probe leaves neighbours at terrain
			}
			return fs.RunTraced(ctx, w, keep)
		}},
	}
	for _, c := range chunks {
		p, err := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		cur = c
		if _, err := p.Get(context.Background(), c, gen.StatusFeatures); err != nil {
			t.Fatal(err)
		}
		p.Close()
	}
}

func runRegionProbe(t *testing.T, seed int64, chunks []world.ChunkPos, keep []string, dir, out string) {
	ref, err := reftest.Open()
	if err != nil {
		t.Fatal(err)
	}
	const class = "kremenmc.probe.feat_region.Region"
	classes, err := ref.Compile(class)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(classes)
	inst := filepath.Join(dir, "inst")
	os.RemoveAll(inst)
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	var cfg strings.Builder
	fmt.Fprintf(&cfg, "out %s\n", out)
	for _, c := range chunks {
		fmt.Fprintf(&cfg, "chunk %d %d\n", c.X, c.Z)
	}
	for _, k := range keep {
		if k != "" {
			fmt.Fprintf(&cfg, "keep %s\n", k)
		}
	}
	cfgPath := filepath.Join(dir, "region.cfg")
	if err := os.WriteFile(cfgPath, []byte(cfg.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	srv, err := refserver.Start(ctx, refserver.Config{
		Classpath: append([]string{classes, ref.Jar}, ref.Libs...),
		MainClass: class,
		MainArgs:  []string{cfgPath},
		Dir:       inst,
		Seed:      seed,
		Memory:    "2G",
		JVMArgs:   []string{"--add-opens", "java.base/java.lang=ALL-UNNAMED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, line, err := srv.WaitLine(ctx, 0, func(l string) bool {
		return strings.Contains(l, "kremenmc-oracle: ok") || strings.Contains(l, "kremenmc-oracle: failed")
	})
	if err != nil || !strings.Contains(line, "ok") {
		srv.Kill()
		t.Fatalf("%v %s\n%s", err, line, srv.Tail(60))
	}
	select {
	case <-srv.Exited():
	case <-time.After(time.Minute):
		srv.Kill()
	}
}

func findBiome(t *testing.T, spec string) {
	seedS, rest, _ := strings.Cut(spec, ":")
	seed, _ := strconv.ParseInt(seedS, 10, 64)
	f := strings.Split(rest, ",")
	var v [4]int
	for i := range 4 {
		v[i], _ = strconv.Atoi(f[i])
	}
	want := "minecraft:" + f[4]
	if f[4] == "any" {
		tr, _ := vanilla.NewTerrain("overworld", seed)
		seen := map[string]int{}
		for cx := v[0]; cx <= v[2]; cx += 4 {
			for cz := v[1]; cz <= v[3]; cz += 4 {
				c := world.NewChunk(int32(cx), int32(cz), world.Overworld, 0)
				_ = tr.GenerateStage(c, "biomes")
				n := data.Canonical().BiomeName(c.Biome(8, 72, 8))
				if seen[n] < 12 {
					fmt.Printf("biome %s %d,%d\n", n, cx, cz)
				}
				seen[n]++
			}
		}
		return
	}
	tr, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	for cx := v[0]; cx <= v[2]; cx++ {
		for cz := v[1]; cz <= v[3]; cz++ {
			ok := true
			for dx := -1; dx <= 1 && ok; dx++ {
				for dz := -1; dz <= 1 && ok; dz++ {
					c := world.NewChunk(int32(cx+dx), int32(cz+dz), world.Overworld, 0)
					if err := tr.GenerateStage(c, "biomes"); err != nil {
						t.Fatal(err)
					}
					for _, q := range [][2]int{{0, 0}, {15, 0}, {0, 15}, {15, 15}, {8, 8}} {
						for _, y := range []int{64, 80} {
							if reg.BiomeName(c.Biome(q[0], y, q[1])) != want {
								ok = false
							}
						}
					}
				}
			}
			if ok {
				fmt.Printf("chunk %d,%d\n", cx, cz)
			} else if os.Getenv("TREEREGION_V") != "" {
				c := world.NewChunk(int32(cx), int32(cz), world.Overworld, 0)
				_ = tr.GenerateStage(c, "biomes")
				fmt.Printf("%d,%d %s\n", cx, cz, reg.BiomeName(c.Biome(8, 70, 8)))
			}
		}
	}
}
