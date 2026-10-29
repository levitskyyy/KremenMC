// Command kremenmc-pack embeds a plugin.toml into a wasm module as a custom
// section so the plugin ships as a single file.
//
//	kremenmc-pack -manifest plugin.toml -o hello.wasm hello.wasm
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

func main() {
	manifest := flag.String("manifest", "plugin.toml", "manifest to embed")
	out := flag.String("o", "", "output file (default: overwrite input)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: kremenmc-pack [-manifest plugin.toml] [-o out.wasm] in.wasm")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *manifest, *out); err != nil {
		fmt.Fprintln(os.Stderr, "kremenmc-pack:", err)
		os.Exit(1)
	}
}

func run(in, manifest, out string) error {
	if out == "" {
		out = in
	}
	bin, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	m, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	packed, err := wire.EmbedManifest(bin, m)
	if err != nil {
		return err
	}
	return os.WriteFile(out, packed, 0o644)
}
