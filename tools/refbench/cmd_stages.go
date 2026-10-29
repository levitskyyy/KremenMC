package main

import (
	"context"
	"io"
	"os"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
)

func cmdStages(ctx context.Context, args []string) error {
	fs := newFlags("stages")
	seed := fs.Int64("seed", 0, "world seed")
	dim := fs.String("dim", "overworld", "overworld, the_nether or the_end")
	spec := fs.String("chunks", "border", "chunk spec")
	stages := fs.String("stages", "biomes,noise,surface,terrain", "stages to dump")
	structures := fs.Bool("structures", true, "generate-structures")
	real := fs.Bool("real", false, "request the vanilla dimension instead of the kremenmc:stage copy")
	threads := fs.Int("threads", 0, "worldgen worker threads (0 = server default)")
	mem := fs.String("memory", "4G", "server heap")
	keep := fs.Bool("keep", false, "keep the oracle server instances")
	verbose := fs.Bool("v", false, "mirror the server console")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ref, err := reftest.Open()
	if err != nil {
		return err
	}
	var echo io.Writer
	if *verbose {
		echo = os.Stderr
	}
	var spawn *[2]int
	if m, err := refgen.LoadManifest(refgen.WorldDir(ref, *seed, *dim, *structures)); err == nil {
		spawn = m.SpawnPair()
	}
	_, err = oracle.Run(ctx, oracle.Options{
		Ref: ref, Seed: *seed, Dim: *dim, Spec: *spec, Spawn: spawn, Stages: splitList(*stages),
		Structures: *structures, Real: *real, Threads: *threads, Memory: *mem, Keep: *keep, Log: os.Stdout, Echo: echo,
	})
	return err
}

func cmdPredicates(ctx context.Context, args []string) error {
	fs := newFlags("predicates")
	out := fs.String("out", "internal/data/testdata/heightmap_predicates_26.3.tsv.gz", "output file (.gz compresses)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ref, err := reftest.Open()
	if err != nil {
		return err
	}
	return oracle.HeightmapPredicates(ctx, ref, *out, os.Stdout)
}
