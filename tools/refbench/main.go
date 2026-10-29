// Command refbench is the reference comparison bench for vanilla-exact world
// generation. The official 26.3 server is used only as a dev-time oracle
// (loopback, offline, no players); nothing here is used by kremenmc at runtime.
//
//	refbench provision [-remote]
//	refbench gen      -seed S -dim D -chunks SPEC [-order sequential|batch] [-threads N] [-structures=false] [-fresh]
//	refbench stages   -seed S -dim D -chunks SPEC [-stages biomes,noise,surface,terrain] [-structures=false] [-real]
//	refbench compare  -seed S -dim D -chunks SPEC -gen NAME [-stage biomes|noise|surface|terrain|full] [-json FILE]
//	refbench diff     -a DUMP -b DUMP [-chunks SPEC]
//	refbench selfcheck -gen NAME -seed S -chunks SPEC [-workers 1,2,4,8] [-json FILE]
//	refbench gens
//	refbench export   -seed S -out DIR
//
// Chunk specs are described in package chunkspec (presets: border, spawn, far, all).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: refbench <command> [flags]

commands:
  provision   ensure .refcache/26.3 (bundler from piston-meta, inner jar + libraries)
  gen         generate reference chunks with the official server
  stages      dump stage-wise proto-chunks (biomes, noise, surface, terrain) via the stage oracle
  compare     compare a kremenmc generator against reference chunks or stage dumps
  diff        compare two stage dumps (e.g. oracle copy dimension vs real dimension)
  selfcheck   Go-vs-Go determinism: workers, request orders, save/restart, speedup table
  gens        list registered kremenmc generators
  predicates  dump vanilla heightmap predicates of every block state (for tools/datagen)
  export      copy a reference overworld into a kremenmc world directory (region/ + kremenmc-level.json)

run "refbench <command> -h" for flags`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cmds := map[string]func(context.Context, []string) error{
		"provision":  cmdProvision,
		"gen":        cmdGen,
		"stages":     cmdStages,
		"compare":    cmdCompare,
		"diff":       cmdDiff,
		"selfcheck":  cmdSelfcheck,
		"gens":       cmdGens,
		"export":     cmdExport,
		"predicates": cmdPredicates,
	}
	f, ok := cmds[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	if err := f(ctx, os.Args[2:]); err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintln(os.Stderr, "refbench:", err)
		}
		os.Exit(1)
	}
}

func newFlags(name string) *flag.FlagSet {
	return flag.NewFlagSet("refbench "+name, flag.ContinueOnError)
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
