// Package selfcheck verifies that a kremenmc generation backend is
// deterministic: the same chunk set must produce identical chunk data
// (blocks, biomes, heightmaps, block entities, entities) regardless of worker
// count, request order, and save/restart in the middle of generation. It also
// measures generation speedup by worker count.
//
// Backends implement Backend. WorldBackend drives the current world.World +
// world.Generator; the staged pipeline (internal/world/gen) is meant to
// provide its own Backend. Equality is checked by diffing chunk data, not
// by the race detector.
package selfcheck

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
)

// Session is one backend lifetime (as if one server process): it owns its
// generator state and, when opened with a directory, its Anvil storage.
type Session interface {
	// Load brings the chunk to full status (generating neighbours as the
	// backend needs) and returns once it is final.
	Load(ctx context.Context, p compare.Pos) error
	// Snapshot returns the comparable data of loaded chunks.
	Snapshot(ps []compare.Pos) (compare.Set, error)
	// Save persists all generated chunks; a later session on the same
	// directory must continue from them.
	Save() error
	Close() error
}

type Backend interface {
	Name() string
	// Open starts a fresh session with the given generation worker count;
	// dir is the storage directory, "" for memory only.
	Open(dir string, workers int) (Session, error)
}

// WorldBackend runs world.World with a registered generator factory. Every
// session builds a new generator instance.
type WorldBackend struct {
	Gen     string
	Factory gens.Factory
	Seed    int64
	Dim     string // overworld, the_nether, the_end
}

func (b *WorldBackend) Name() string { return "world+" + b.Gen }

func (b *WorldBackend) Open(dir string, workers int) (Session, error) {
	dim, err := gens.Dimension(b.Dim)
	if err != nil {
		return nil, err
	}
	g := b.Factory(b.Seed, b.Dim)
	if g == nil {
		return nil, fmt.Errorf("selfcheck: %s does not support %s", b.Gen, b.Dim)
	}
	cfg := world.Config{Dim: dim, Generator: g, Workers: workers, Seed: b.Seed}
	var st *anvil.Storage
	if dir != "" {
		st = anvil.NewStorage(dir, dim)
		cfg.Storage = st
	}
	w, err := world.New(cfg)
	if err != nil {
		return nil, err
	}
	return &worldSession{w: w, st: st}, nil
}

type worldSession struct {
	w  *world.World
	st *anvil.Storage
}

func (s *worldSession) Load(ctx context.Context, p compare.Pos) error {
	_, err := s.w.LoadChunk(ctx, world.ChunkPos{X: p.X, Z: p.Z})
	return err
}

func (s *worldSession) Snapshot(ps []compare.Pos) (compare.Set, error) {
	set := compare.Set{}
	for _, p := range ps {
		c := s.w.Chunk(world.ChunkPos{X: p.X, Z: p.Z})
		if c == nil {
			return nil, fmt.Errorf("selfcheck: chunk %v not loaded", p)
		}
		c.RLock()
		set[p] = compare.FromWorld(c, "minecraft:full")
		c.RUnlock()
	}
	return set, nil
}

func (s *worldSession) Save() error { return s.w.SaveAll() }

func (s *worldSession) Close() error {
	err := s.w.Close()
	if s.st != nil {
		err = errors.Join(err, s.st.Close())
	}
	return err
}

// Orders maps order names to permutations of a chunk set.
var Orders = []string{"row-major", "column-major", "spiral", "reverse-spiral", "random", "far-near", "burst"}

// Order returns ps rearranged by name. "burst" is a random permutation that
// Run issues concurrently instead of one at a time.
func Order(name string, ps []compare.Pos, seed uint64) ([]compare.Pos, error) {
	out := slices.Clone(ps)
	var cx, cz float64
	for _, p := range ps {
		cx += float64(p.X)
		cz += float64(p.Z)
	}
	cx /= float64(max(1, len(ps)))
	cz /= float64(max(1, len(ps)))
	dist := func(p compare.Pos) float64 { return math.Hypot(float64(p.X)-cx, float64(p.Z)-cz) }
	ring := func(p compare.Pos) float64 {
		return math.Max(math.Abs(float64(p.X)-cx), math.Abs(float64(p.Z)-cz))
	}
	angle := func(p compare.Pos) float64 { return math.Atan2(float64(p.Z)-cz, float64(p.X)-cx) }
	switch name {
	case "row-major":
		slices.SortFunc(out, func(a, b compare.Pos) int { return cmp.Or(cmp.Compare(a.Z, b.Z), cmp.Compare(a.X, b.X)) })
	case "column-major":
		slices.SortFunc(out, func(a, b compare.Pos) int { return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z)) })
	case "spiral", "reverse-spiral":
		slices.SortStableFunc(out, func(a, b compare.Pos) int {
			return cmp.Or(cmp.Compare(math.Round(ring(a)), math.Round(ring(b))), cmp.Compare(angle(a), angle(b)))
		})
		if name == "reverse-spiral" {
			slices.Reverse(out)
		}
	case "random", "burst":
		r := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	case "far-near":
		slices.SortStableFunc(out, func(a, b compare.Pos) int { return cmp.Compare(dist(a), dist(b)) })
		inter := make([]compare.Pos, 0, len(out))
		for i, j := 0, len(out)-1; i <= j; i, j = i+1, j-1 {
			inter = append(inter, out[i])
			if i != j {
				inter = append(inter, out[j])
			}
		}
		out = inter
	default:
		return nil, fmt.Errorf("selfcheck: unknown order %q (have %v)", name, Orders)
	}
	return out, nil
}

// loadAll loads ps one by one in order, or all at once when burst is set.
func loadAll(ctx context.Context, s Session, ps []compare.Pos, burst bool) error {
	if !burst {
		for _, p := range ps {
			if err := s.Load(ctx, p); err != nil {
				return err
			}
		}
		return nil
	}
	var wg sync.WaitGroup
	errs := make([]error, len(ps))
	for i, p := range ps {
		wg.Go(func() { errs[i] = s.Load(ctx, p) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

type Options struct {
	Backend     Backend
	Chunks      []compare.Pos // determinism set (should include chunk and region borders)
	Workers     []int         // worker counts for the worker and speed checks; default 1,2,4,8,GOMAXPROCS
	Orders      []string      // default Orders
	SpeedSet    []compare.Pos // speed set (default: Chunks)
	OrderSeed   uint64
	TempDir     string // for save/restart storage; default os.MkdirTemp
	SkipSpeed   bool
	SkipRestart bool
	Log         io.Writer
}

type Check struct {
	Name   string          `json:"name"`
	Equal  bool            `json:"equal"`
	Millis int64           `json:"ms"`
	Blocks compare.Counter `json:"blocks"`
	Biomes compare.Counter `json:"biomes"`
	Detail *compare.Report `json:"detail,omitempty"` // only for failures
	Err    string          `json:"error,omitempty"`
}

type SpeedRow struct {
	Workers      int     `json:"workers"`
	Chunks       int     `json:"chunks"`
	Seconds      float64 `json:"seconds"`
	ChunksPerSec float64 `json:"chunksPerSec"`
	Speedup      float64 `json:"speedup"`
	Equal        bool    `json:"equalToWorkers1"`
}

type Result struct {
	Backend string     `json:"backend"`
	Chunks  int        `json:"chunks"`
	Checks  []Check    `json:"checks"`
	Speed   []SpeedRow `json:"speed,omitempty"`
}

// OK reports whether every check produced identical chunk data.
func (r *Result) OK() bool {
	for _, c := range r.Checks {
		if !c.Equal {
			return false
		}
	}
	for _, s := range r.Speed {
		if !s.Equal {
			return false
		}
	}
	return true
}

// DefaultWorkers is 1, 2, 4, 8 and GOMAXPROCS (deduplicated).
func DefaultWorkers() []int {
	ws := []int{1, 2, 4, 8, runtime.GOMAXPROCS(0)}
	slices.Sort(ws)
	return slices.Compact(ws)
}

// generate runs one session over ps and snapshots ps plus extra.
func generate(ctx context.Context, b Backend, dir string, workers int, ps []compare.Pos, burst bool) (compare.Set, time.Duration, error) {
	s, err := b.Open(dir, workers)
	if err != nil {
		return nil, 0, err
	}
	defer s.Close()
	t := time.Now()
	if err := loadAll(ctx, s, ps, burst); err != nil {
		return nil, 0, err
	}
	d := time.Since(t)
	set, err := s.Snapshot(ps)
	return set, d, err
}

// Run executes all checks.
func Run(ctx context.Context, o Options) (*Result, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if len(o.Workers) == 0 {
		o.Workers = DefaultWorkers()
	}
	if len(o.Orders) == 0 {
		o.Orders = Orders
	}
	if o.OrderSeed == 0 {
		o.OrderSeed = 0x5eed
	}
	if len(o.SpeedSet) == 0 {
		o.SpeedSet = o.Chunks
	}
	res := &Result{Backend: o.Backend.Name(), Chunks: len(o.Chunks)}
	opts := compare.FullOptions()
	rowMajor, _ := Order("row-major", o.Chunks, 0)
	fmt.Fprintf(o.Log, "baseline: %s, 1 worker, row-major, %d chunks\n", res.Backend, len(o.Chunks))
	base, d, err := generate(ctx, o.Backend, "", 1, rowMajor, false)
	if err != nil {
		return nil, fmt.Errorf("selfcheck: baseline: %w", err)
	}
	res.Checks = append(res.Checks, Check{Name: "baseline workers=1 row-major", Equal: true, Millis: d.Milliseconds()})
	record := func(name string, got compare.Set, d time.Duration, err error) {
		c := Check{Name: name, Millis: d.Milliseconds()}
		if err != nil {
			c.Err = err.Error()
		} else {
			r := compare.Compare(base, got, o.Chunks, opts)
			c.Equal, c.Blocks, c.Biomes = r.Equal(), r.Blocks, r.Biomes
			if !c.Equal {
				r.Chunks = nil
				c.Detail = r
			}
		}
		status := "ok"
		if !c.Equal {
			status = "DIFFERENT"
			if c.Err != "" {
				status = "ERROR " + c.Err
			}
		}
		fmt.Fprintf(o.Log, "  %-40s %s (%d ms)\n", name, status, c.Millis)
		res.Checks = append(res.Checks, c)
	}
	for _, w := range o.Workers {
		ord, _ := Order("burst", o.Chunks, o.OrderSeed)
		got, d, err := generate(ctx, o.Backend, "", w, ord, true)
		record(fmt.Sprintf("workers=%d burst", w), got, d, err)
	}
	maxW := slices.Max(o.Workers)
	for _, name := range o.Orders {
		ord, err := Order(name, o.Chunks, o.OrderSeed)
		if err != nil {
			return nil, err
		}
		got, d, err := generate(ctx, o.Backend, "", maxW, ord, name == "burst")
		record(fmt.Sprintf("order=%s workers=%d", name, maxW), got, d, err)
	}
	if !o.SkipRestart {
		splits := map[string]func([]compare.Pos) ([]compare.Pos, []compare.Pos){
			"random-half": func(ps []compare.Pos) ([]compare.Pos, []compare.Pos) {
				r, _ := Order("random", ps, o.OrderSeed)
				return r[:len(r)/2], r[len(r)/2:]
			},
			"west-half": func(ps []compare.Pos) ([]compare.Pos, []compare.Pos) {
				xs := make([]int32, len(ps))
				for i, p := range ps {
					xs[i] = p.X
				}
				slices.Sort(xs)
				mid := xs[len(xs)/2]
				var a, b []compare.Pos
				for _, p := range ps {
					if p.X < mid {
						a = append(a, p)
					} else {
						b = append(b, p)
					}
				}
				return a, b
			},
		}
		for _, name := range []string{"random-half", "west-half"} {
			first, rest := splits[name](o.Chunks)
			got, d, err := restart(ctx, o, maxW, first, rest)
			record("save-restart "+name, got, d, err)
		}
	}
	if !o.SkipSpeed {
		var ref compare.Set
		var t1 float64
		ord, _ := Order("burst", o.SpeedSet, o.OrderSeed)
		fmt.Fprintf(o.Log, "speed: %d chunks, burst\n", len(o.SpeedSet))
		for _, w := range o.Workers {
			got, d, err := generate(ctx, o.Backend, "", w, ord, true)
			if err != nil {
				return nil, fmt.Errorf("selfcheck: speed workers=%d: %w", w, err)
			}
			row := SpeedRow{Workers: w, Chunks: len(o.SpeedSet), Seconds: d.Seconds(), ChunksPerSec: float64(len(o.SpeedSet)) / d.Seconds(), Equal: true}
			if ref == nil {
				ref, t1 = got, d.Seconds()
			} else {
				row.Equal = compare.Compare(ref, got, o.SpeedSet, opts).Equal()
			}
			row.Speedup = t1 / d.Seconds()
			res.Speed = append(res.Speed, row)
		}
	}
	return res, nil
}

// restart generates first in one session, saves and closes it, then opens a
// fresh session on the same storage, generates rest and snapshots everything.
func restart(ctx context.Context, o Options, workers int, first, rest []compare.Pos) (compare.Set, time.Duration, error) {
	dir, err := os.MkdirTemp(o.TempDir, "selfcheck-restart-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	t := time.Now()
	s, err := o.Backend.Open(dir, workers)
	if err != nil {
		return nil, 0, err
	}
	if err := loadAll(ctx, s, first, false); err != nil {
		s.Close()
		return nil, 0, err
	}
	if err := s.Save(); err != nil {
		s.Close()
		return nil, 0, err
	}
	if err := s.Close(); err != nil {
		return nil, 0, err
	}
	s, err = o.Backend.Open(dir, workers)
	if err != nil {
		return nil, 0, err
	}
	defer s.Close()
	if err := loadAll(ctx, s, rest, false); err != nil {
		return nil, 0, err
	}
	if err := loadAll(ctx, s, first, false); err != nil {
		return nil, 0, err
	}
	d := time.Since(t)
	set, err := s.Snapshot(append(slices.Clone(first), rest...))
	return set, d, err
}

// Print writes the check list and the speed table.
func (r *Result) Print(w io.Writer) {
	fmt.Fprintf(w, "selfcheck %s, %d chunks\n", r.Backend, r.Chunks)
	for _, c := range r.Checks {
		st := "identical"
		if c.Err != "" {
			st = "ERROR: " + c.Err
		} else if !c.Equal {
			st = fmt.Sprintf("DIFFERENT (blocks %.4f%%, biomes %.4f%%)", c.Blocks.Pct(), c.Biomes.Pct())
		}
		fmt.Fprintf(w, "  %-40s %-12s %6d ms\n", c.Name, st, c.Millis)
		if c.Detail != nil {
			for _, m := range c.Detail.First[:min(5, len(c.Detail.First))] {
				fmt.Fprintf(w, "      %s %d %d %d %s -> %s\n", m.Kind, m.X, m.Y, m.Z, m.Ref, m.Got)
			}
		}
	}
	if len(r.Speed) > 0 {
		fmt.Fprintf(w, "\n  %7s  %7s  %9s  %10s  %8s  %s\n", "workers", "chunks", "seconds", "chunks/s", "speedup", "same data")
		for _, s := range r.Speed {
			fmt.Fprintf(w, "  %7d  %7d  %9.3f  %10.1f  %7.2fx  %v\n", s.Workers, s.Chunks, s.Seconds, s.ChunksPerSec, s.Speedup, s.Equal)
		}
	}
}
