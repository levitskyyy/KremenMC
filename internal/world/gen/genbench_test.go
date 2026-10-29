package gen_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

// vanillaPipeline mirrors internal/server.vanillaPipeline.
func vanillaPipeline(seed int64, workers int, st *anvil.Storage, logf func(string, ...any)) (*gen.Pipeline, error) {
	t, err := vanilla.NewTerrain("overworld", seed)
	if err != nil {
		return nil, err
	}
	stages, err := t.Stages("overworld", seed)
	if err != nil {
		return nil, err
	}
	for i, s := range stages {
		s := s
		stages[i] = gen.StageFunc{S: s.Status(), F: func(sc *gen.StageContext) error {
			runsMu.Lock()
			runs[runKey{sc.Pos(), s.Status()}]++
			runsMu.Unlock()
			return s.Run(sc)
		}}
	}
	return gen.New(gen.Config{
		Seed: seed, Dim: world.Overworld, Workers: workers, Stages: stages,
		Storage: protoStorage{&gen.AnvilStorage{St: st}}, CacheSize: 2048, Logf: logf,
	})
}

type runKey struct {
	p  world.ChunkPos
	st gen.Status
}

var (
	runsMu sync.Mutex
	runs   = map[runKey]int{}
)

type protoStorage struct{ *gen.AnvilStorage }

func (p protoStorage) SaveProto(c *gen.ProtoChunk) error {
	if c.Status() == gen.StatusFull {
		return nil
	}
	return p.AnvilStorage.SaveProto(c)
}

// TestGenBench generates a square of full chunks around spawn through the
// server's world + vanilla pipeline path. Opt-in: KREMENMC_GENBENCH=1.
// KREMENMC_GENBENCH_R sets the radius (default 10 = 21x21), KREMENMC_GENBENCH_CPU and
// KREMENMC_GENBENCH_MEM write pprof profiles.
func TestGenBench(t *testing.T) {
	if os.Getenv("KREMENMC_GENBENCH") == "" {
		t.Skip("set KREMENMC_GENBENCH=1")
	}
	r := 10
	if v, err := strconv.Atoi(os.Getenv("KREMENMC_GENBENCH_R")); err == nil {
		r = v
	}
	workers, _ := strconv.Atoi(os.Getenv("KREMENMC_GENBENCH_WORKERS"))
	const seed = 8266389061079214669
	if f := os.Getenv("KREMENMC_GENBENCH_CPU"); f != "" {
		out, err := os.Create(f)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		pprof.StartCPUProfile(out)
		defer pprof.StopCPUProfile()
	}
	if os.Getenv("KREMENMC_GENBENCH_MEM") != "" {
		runtime.MemProfileRate = 64 << 10
	}
	dir := t.TempDir()
	st := anvil.NewStorage(dir, world.Overworld)
	t0 := time.Now()
	p, err := vanillaPipeline(seed, workers, st, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	ww, err := world.New(world.Config{Dim: world.Overworld, Generator: gen.WorldGenerator{P: p}, Storage: st, Seed: seed, Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	setup := time.Since(t0)
	if os.Getenv("KREMENMC_GENBENCH_WARM") != "" {
		t1 := time.Now()
		if _, err := ww.LoadChunk(context.Background(), world.ChunkPos{X: 1000, Z: 1000}); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("genbench: warm-up chunk took %v\n", time.Since(t1).Round(time.Millisecond))
	}
	t0 = time.Now()
	ww.AddTicket(world.ChunkPos{}, r, world.TicketForced)
	want := (2*r + 1) * (2*r + 1)
	base := ww.Loaded()
	var first time.Duration
	for ww.Loaded()-base < want {
		if first == 0 && ww.Loaded() > base {
			first = time.Since(t0)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if first == 0 {
		first = time.Since(t0)
	}
	total := time.Since(t0)
	h := sha256.New()
	var b [4]byte
	var dump *os.File
	if f := os.Getenv("KREMENMC_GENBENCH_DUMP"); f != "" {
		dump, _ = os.Create(f)
		defer dump.Close()
	}
	var blocks *bufio.Writer
	if f := os.Getenv("KREMENMC_GENBENCH_BLOCKS"); f != "" {
		bf, _ := os.Create(f)
		defer bf.Close()
		blocks = bufio.NewWriter(bf)
		defer blocks.Flush()
	}
	spawner, _ := data.DefaultState("spawner")
	mossy, _ := data.DefaultState("mossy_cobblestone")
	var nSpawner, nMossy, nSpawnerBE, nChestBE int
	for x := int32(-r); x <= int32(r); x++ {
		for z := int32(-r); z <= int32(r); z++ {
			ch := sha256.New()
			defer func() {
				if dump != nil {
					fmt.Fprintf(dump, "%d %d %x\n", x, z, ch.Sum(nil)[:8])
				}
			}()
			c := ww.Chunk(world.ChunkPos{X: x, Z: z})
			c.RLock()
			for _, be := range c.BlockEntities {
				switch be.ID {
				case "minecraft:mob_spawner":
					nSpawnerBE++
				case "minecraft:chest":
					nChestBE++
				}
			}
			for y := world.Overworld.MinY; y < world.Overworld.MinY+world.Overworld.Height; y++ {
				for i := range 256 {
					st := c.Block(i&15, y, i>>4)
					switch st {
					case spawner:
						nSpawner++
					case mossy:
						nMossy++
					}
					binary.LittleEndian.PutUint32(b[:], uint32(st))
					if blocks != nil && (x == int32(-r) || x == int32(r) || z == int32(-r) || z == int32(r)) {
						fmt.Fprintf(blocks, "%d %d %d %d %d %d\n", x, z, int(x)*16+i&15, y, int(z)*16+i>>4, st)
					}
					h.Write(b[:])
					ch.Write(b[:])
				}
			}
			c.RUnlock()
		}
	}
	if f := os.Getenv("KREMENMC_GENBENCH_MEM"); f != "" {
		out, _ := os.Create(f)
		pprof.Lookup("allocs").WriteTo(out, 0)
		out.Close()
	}
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("genbench: workers=%d GOMAXPROCS=%d chunks=%d setup=%v first=%v total=%v rate=%.1f chunks/s peakRSS=%dMB totalAlloc=%dMB numGC=%d hash=%x\n",
		workers, runtime.GOMAXPROCS(0), want, setup.Round(time.Millisecond), first.Round(time.Millisecond), total.Round(time.Millisecond),
		float64(want)/total.Seconds(), ru.Maxrss>>20, ms.TotalAlloc>>20, ms.NumGC, h.Sum(nil)[:12])
	fmt.Printf("genbench: spawners=%d mossy=%d spawnerBE=%d chestBE=%d\n", nSpawner, nMossy, nSpawnerBE, nChestBE)
	runsMu.Lock()
	var again []string
	for k, n := range runs {
		if n > 1 && k.st >= gen.StatusTerrain {
			again = append(again, fmt.Sprintf("%v@%v:%d", k.p, k.st, n))
		}
	}
	runsMu.Unlock()
	sort.Strings(again)
	fmt.Printf("genbench: steps run more than once: %d %v\n", len(again), again[:min(len(again), 60)])
	st0 := p.Stats()
	fmt.Printf("genbench: steps=%v loads=%d busy=%v\n", st0.Steps, st0.Loads, time.Duration(st0.BusyNanos).Round(time.Millisecond))
	ww.Close()
	p.Close()
}
