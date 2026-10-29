package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/levitskyyy/KremenMC/tools/refbench/bench"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
	"github.com/levitskyyy/KremenMC/tools/refbench/selfcheck"
)

func cmdSelfcheck(ctx context.Context, args []string) error {
	fs := newFlags("selfcheck")
	backend := fs.String("backend", "world", "world (world.World + a gens generator) or pipeline (internal/world/gen with a stage set)")
	gen := fs.String("gen", "terrain", "gens generator (world backend) or stage set (pipeline backend: synthetic)")
	storage := fs.String("storage", "file", "pipeline save/restart storage: file or anvil")
	cache := fs.Int("cache", 0, "pipeline chunk cache size with storage (0 = default; small values force eviction)")
	seed := fs.Int64("seed", 0, "world seed")
	dim := fs.String("dim", "overworld", "dimension")
	spec := fs.String("chunks", "border+far", "determinism chunk set (spawn is not available here)")
	speedSpec := fs.String("speed-chunks", "rect:-12,-12,11,11", "chunk set for the speed table")
	workers := fs.String("workers", "", "worker counts (default 1,2,4,8,GOMAXPROCS)")
	orders := fs.String("orders", "", "request orders (default all)")
	noSpeed := fs.Bool("no-speed", false, "skip the speed table")
	jsonOut := fs.String("json", "", "write the result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var b selfcheck.Backend
	switch *backend {
	case "world":
		f, err := gens.Lookup(*gen)
		if err != nil {
			return err
		}
		b = &selfcheck.WorldBackend{Gen: *gen, Factory: f, Seed: *seed, Dim: *dim}
	case "pipeline":
		pb, err := selfcheck.NewPipelineBackend(*gen, *seed, *dim)
		if err != nil {
			return err
		}
		pb.Storage, pb.CacheSize = *storage, *cache
		b = pb
	default:
		return fmt.Errorf("selfcheck: backend %q: want world or pipeline", *backend)
	}
	ps, err := chunkspec.Parse(*spec, nil)
	if err != nil {
		return err
	}
	sps, err := chunkspec.Parse(*speedSpec, nil)
	if err != nil {
		return err
	}
	var ws []int
	for _, w := range splitList(*workers) {
		n, err := strconv.Atoi(w)
		if err != nil || n < 1 {
			return fmt.Errorf("bad worker count %q", w)
		}
		ws = append(ws, n)
	}
	res, err := selfcheck.Run(ctx, selfcheck.Options{
		Backend:   b,
		Chunks:    bench.Positions(ps),
		SpeedSet:  bench.Positions(sps),
		Workers:   ws,
		Orders:    splitList(*orders),
		SkipSpeed: *noSpeed,
		Log:       os.Stderr,
	})
	if err != nil {
		return err
	}
	res.Print(os.Stdout)
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	if !res.OK() {
		return fmt.Errorf("selfcheck: chunk data differs between runs")
	}
	return nil
}
