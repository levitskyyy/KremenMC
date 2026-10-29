// Command refprobe compiles the Java probes under tools/refprobe/java against
// the reference server jar and runs one of them. Probes call the reference
// implementation as a black box and print golden vectors; they are a dev-time
// oracle only and are never used by the server at runtime.
//
//	go run ./tools/refprobe <main-class> [args...]
package main

import (
	"fmt"
	"os"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: refprobe <main-class> [args...]")
		os.Exit(2)
	}
	ref, err := reftest.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ref.Run(os.Stdout, os.Args[1], os.Args[2:]...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
