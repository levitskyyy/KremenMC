// Package gen holds registry blobs produced by tools/datagen.
package gen

import "embed"

//go:embed *.bin.gz
var FS embed.FS

// Canonical is the version the server speaks natively (protocol 777).
const Canonical = "26.3"
