package main

import (
	"context"
	"fmt"
	"os"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/bench"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

func saveReport(path string, r *compare.Report) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := r.WriteJSON(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func cmdCompare(ctx context.Context, args []string) error {
	fs := newFlags("compare")
	seed := fs.Int64("seed", 0, "world seed")
	dim := fs.String("dim", "overworld", "overworld, the_nether or the_end")
	spec := fs.String("chunks", "border", "chunk spec")
	gen := fs.String("gen", "terrain", "kremenmc generator (see refbench gens)")
	stage := fs.String("stage", "full", "biomes, noise, surface, terrain (alias carvers) or full")
	structures := fs.Bool("structures", true, "reference generated with structures")
	workers := fs.Int("workers", 0, "kremenmc generation goroutines (0 = GOMAXPROCS)")
	jsonOut := fs.String("json", "", "write the full report as JSON to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ref, err := reftest.Open()
	if err != nil {
		return err
	}
	r, err := bench.Run(ctx, bench.Options{
		Ref: ref, Seed: *seed, Dim: *dim, Spec: *spec, Stage: *stage, Gen: *gen, Structures: *structures, Workers: *workers,
	})
	if err != nil {
		return err
	}
	r.Print(os.Stdout)
	return saveReport(*jsonOut, r)
}

func cmdDiff(ctx context.Context, args []string) error {
	fs := newFlags("diff")
	a := fs.String("a", "", "reference dump file")
	b := fs.String("b", "", "dump file to compare against a")
	spec := fs.String("chunks", "", "restrict to a chunk spec (default: all chunks of a)")
	stage := fs.String("stage", "terrain", "stage options to compare with (biomes compares only biomes)")
	jsonOut := fs.String("json", "", "write the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ra, err := oracle.ReadDump(*a)
	if err != nil {
		return err
	}
	rb, err := oracle.ReadDump(*b)
	if err != nil {
		return err
	}
	var order []compare.Pos
	if *spec != "" {
		ps, err := chunkspec.Parse(*spec, nil)
		if err != nil {
			return err
		}
		order = bench.Positions(ps)
	}
	opts := compare.StageOptions(*stage)
	opts.BlockEntities, opts.Data = true, true
	r := compare.Compare(compare.FromRawList(ra), compare.FromRawList(rb), order, opts)
	r.Meta = map[string]string{"a": *a, "b": *b}
	r.Print(os.Stdout)
	if err := saveReport(*jsonOut, r); err != nil {
		return err
	}
	if !r.Equal() {
		return fmt.Errorf("dumps differ")
	}
	return nil
}

func cmdGens(context.Context, []string) error {
	for _, n := range gens.Names() {
		fmt.Println(n)
	}
	return nil
}
