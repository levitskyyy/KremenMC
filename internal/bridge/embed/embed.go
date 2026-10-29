// Package embed carries the compiled Java side of the bridge. The jar is
// produced by `go run ./tools/bridgebuild`; without it the server still
// builds and the bridge reports that it is unavailable.
package embed

import (
	"bytes"
	"embed"
)

//go:embed all:jar
var files embed.FS

// BridgeJar returns the embedded kremenmc-bridge.jar, or nil if it was not built.
func BridgeJar() []byte {
	b, err := files.ReadFile("jar/kremenmc-bridge.jar")
	if err != nil || !bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		return nil
	}
	return b
}
