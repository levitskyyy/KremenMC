// Package bench ties the pieces together for commands and Go tests: load
// reference chunks (full chunks from refgen worlds or stage-oracle dumps),
// generate the same chunks with a registered kremenmc generator and compare.
package bench

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
	"github.com/levitskyyy/KremenMC/tools/refbench/refserver"
)

type Options struct {
	Ref        *reftest.Ref
	Seed       int64
	Dim        string // overworld, the_nether, the_end
	Spec       string
	Stage      string // biomes, noise, surface, terrain (alias carvers) or full
	Gen        string // registered generator name
	Structures bool
	Workers    int // kremenmc generation goroutines; 0 = GOMAXPROCS
}

// Positions converts chunkspec positions.
func Positions(ps []chunkspec.Pos) []compare.Pos {
	out := make([]compare.Pos, len(ps))
	for i, p := range ps {
		out[i] = compare.Pos{X: p.X, Z: p.Z}
	}
	return out
}

// Reference loads the reference chunks of a spec for a stage.
func Reference(o Options) (compare.Set, []compare.Pos, error) {
	dim := refgen.DimID(o.Dim)
	wdir := refgen.WorldDir(o.Ref, o.Seed, dim, o.Structures)
	var spawn *[2]int
	if m, err := refgen.LoadManifest(wdir); err == nil {
		spawn = m.SpawnPair()
	}
	ps, err := chunkspec.Parse(o.Spec, spawn)
	if err != nil {
		return nil, nil, err
	}
	pos := Positions(ps)
	if o.Stage == "full" {
		set, err := compare.LoadRegion(refserver.DimensionDir(wdir+"/world", dim), pos)
		return set, pos, err
	}
	st, ok := oracle.Canonical(o.Stage)
	if !ok {
		return nil, nil, fmt.Errorf("bench: unknown stage %q", o.Stage)
	}
	path := oracle.DumpPath(oracle.Dir(o.Ref, o.Seed, dim, o.Structures, false), st)
	rcs, err := oracle.ReadDump(path)
	if err != nil {
		return nil, nil, fmt.Errorf("bench: %w (run refbench stages first)", err)
	}
	all := compare.FromRawList(rcs)
	set := compare.Set{}
	var missing []compare.Pos
	for _, p := range pos {
		if c := all[p]; c != nil {
			set[p] = c
		} else {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return set, pos, fmt.Errorf("bench: %d chunks missing from %s (first %v); run refbench stages with this spec", len(missing), path, missing[0])
	}
	return set, pos, nil
}

// Generate runs gen on fresh chunks at ps. For a stage other than full,
// generators implementing gens.Staged stop after that stage; others produce
// full output.
func Generate(ctx context.Context, gen world.Generator, dim world.Dimension, ps []compare.Pos, stage string, workers int) (compare.Set, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	plains, _ := data.Canonical().BiomeByName("plains")
	staged, _ := gen.(gens.Staged)
	status := "minecraft:full"
	if stage != "full" && stage != "" {
		status = "minecraft:" + stage
	}
	set := compare.Set{}
	var mu sync.Mutex
	var firstErr error
	jobs := make(chan compare.Pos)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for p := range jobs {
				c := world.NewChunk(p.X, p.Z, dim, plains)
				var err error
				if staged != nil && stage != "full" && stage != "" {
					err = staged.GenerateStage(c, stage)
				} else {
					gen.Generate(c)
				}
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("bench: chunk %v: %w", p, err)
				}
				set[p] = compare.FromWorld(c, status)
				mu.Unlock()
			}
		})
	}
	for _, p := range ps {
		if ctx.Err() != nil {
			break
		}
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return set, ctx.Err()
}

// Run loads the reference, generates with o.Gen and compares.
func Run(ctx context.Context, o Options) (*compare.Report, error) {
	if o.Stage == "" {
		o.Stage = "full"
	}
	if o.Stage != "full" {
		st, ok := oracle.Canonical(o.Stage)
		if !ok {
			return nil, fmt.Errorf("bench: unknown stage %q", o.Stage)
		}
		o.Stage = st
	}
	ref, pos, err := Reference(o)
	if err != nil {
		return nil, err
	}
	f, err := gens.Lookup(o.Gen)
	if err != nil {
		return nil, err
	}
	dimName := refgen.DimName(refgen.DimID(o.Dim))
	gen := f(o.Seed, dimName)
	if gen == nil {
		return nil, fmt.Errorf("bench: generator %s does not support %s", o.Gen, dimName)
	}
	dim, err := gens.Dimension(dimName)
	if err != nil {
		return nil, err
	}
	got, err := Generate(ctx, gen, dim, pos, o.Stage, o.Workers)
	if err != nil {
		return nil, err
	}
	opts := compare.FullOptions()
	if o.Stage != "full" {
		opts = compare.StageOptions(o.Stage)
	}
	r := compare.Compare(ref, got, pos, opts)
	_, staged := gen.(gens.Staged)
	r.Meta = map[string]string{
		"seed": strconv.FormatInt(o.Seed, 10), "dim": dimName, "stage": o.Stage, "gen": o.Gen, "chunks": o.Spec,
		"structures": strconv.FormatBool(o.Structures),
	}
	if o.Stage != "full" && !staged {
		r.Meta["note"] = "generator-has-no-stage-support:compared-full-output"
	}
	return r, nil
}
