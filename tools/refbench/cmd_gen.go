package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/provision"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
)

func cacheDir() (string, error) {
	if d := os.Getenv("KREMENMC_REF_DIR"); d != "" {
		return d, nil
	}
	root, err := reftest.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".refcache", reftest.Version), nil
}

func cmdProvision(ctx context.Context, args []string) error {
	fs := newFlags("provision")
	remote := fs.Bool("remote", false, "verify an existing bundler against piston-meta (network)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	res, err := provision.Ensure(ctx, dir, reftest.Version, provision.Options{
		Remote: *remote, Logger: log, HTTPClient: &http.Client{Timeout: 15 * time.Minute},
	})
	if err != nil {
		return err
	}
	fmt.Printf("reference %s in %s: server jar %s, %d libraries; downloaded=%v extracted=%d verified=%d\n",
		reftest.Version, res.Dir, filepath.Base(res.ServerJar), len(res.Libraries), res.Downloaded, res.Extracted, res.Verified)
	return nil
}

func cmdGen(ctx context.Context, args []string) error {
	fs := newFlags("gen")
	seed := fs.Int64("seed", 0, "world seed")
	dim := fs.String("dim", "overworld", "overworld, the_nether or the_end")
	spec := fs.String("chunks", "border", "chunk spec (see package chunkspec)")
	order := fs.String("order", "sequential", "sequential (one target at a time, reproducible) or batch")
	threads := fs.Int("threads", 1, "worldgen worker threads in the reference server (0 = server default)")
	structures := fs.Bool("structures", true, "generate-structures")
	fresh := fs.Bool("fresh", false, "delete the existing reference world first")
	mem := fs.String("memory", "4G", "server heap")
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
	_, err = refgen.Generate(ctx, refgen.Options{
		Ref: ref, Seed: *seed, Dim: *dim, Spec: *spec, Structures: *structures, Order: *order,
		Threads: *threads, Memory: *mem, Fresh: *fresh, Log: os.Stdout, Echo: echo,
	})
	return err
}
