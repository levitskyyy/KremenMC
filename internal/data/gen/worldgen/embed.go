// Package worldgen embeds the vanilla data container produced by
// tools/worldgendata. Read it through internal/world/worldgen/wgdata.
package worldgen

import _ "embed"

//go:embed worldgen-26.3.bin
var Data []byte
