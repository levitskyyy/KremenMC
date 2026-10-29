// Package meta holds per-protocol data produced by cmd/transgen: packet layout
// fingerprints, registry-like enums, synced registry entry names, entity
// metadata keys and the protodef schemas of items, particles and metadata.
package meta

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
)

//go:embed *.json.gz
var files embed.FS

// SD keys used by Packets and Order.
const (
	HandshakeServerbound = "handshaking.toServer"
	StatusClientbound    = "status.toClient"
	StatusServerbound    = "status.toServer"
	LoginClientbound     = "login.toClient"
	LoginServerbound     = "login.toServer"
	ConfigClientbound    = "configuration.toClient"
	ConfigServerbound    = "configuration.toServer"
	PlayClientbound      = "play.toClient"
	PlayServerbound      = "play.toServer"
)

// Enum kinds.
const (
	Component   = "component"
	Metadata    = "metadata"
	Particle    = "particle"
	Parser      = "parser"
	Attribute   = "attribute"
	Sound       = "sound"
	SoundSource = "soundSource"
	Entity      = "entity"
)

type Meta struct {
	Protocol int32    `json:"protocol"`
	Version  string   `json:"version"`
	Packs    []string `json:"packs"`
	Data     string   `json:"data"`
	// Packets maps SD key -> packet name -> layout fingerprint.
	Packets map[string]map[string]string `json:"packets"`
	// Order lists packet names by ID per SD key.
	Order map[string][]string `json:"order"`
	// Opaque maps an opaque schema type to its full fingerprint.
	Opaque       map[string]string          `json:"opaque"`
	Enums        map[string][]string        `json:"enums"`
	Registries   map[string][]string        `json:"registries"`
	MetadataKeys map[string][]string        `json:"metadataKeys"`
	Types        map[string]json.RawMessage `json:"types"`

	idxOnce sync.Once
	idx     map[string]map[string]int32
}

var (
	mu    sync.Mutex
	cache = map[int32]*Meta{}
)

// Load returns the embedded metadata for a protocol.
func Load(protocol int32) (*Meta, error) {
	mu.Lock()
	defer mu.Unlock()
	if m, ok := cache[protocol]; ok {
		return m, nil
	}
	raw, err := files.ReadFile(strconv.Itoa(int(protocol)) + ".json.gz")
	if err != nil {
		return nil, fmt.Errorf("meta: protocol %d not embedded: %w", protocol, err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("meta %d: %w", protocol, err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("meta %d: %w", protocol, err)
	}
	m := &Meta{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("meta %d: %w", protocol, err)
	}
	cache[protocol] = m
	return m, nil
}

// Index returns the position of name in the enum, or -1.
func (m *Meta) Index(kind, name string) int32 {
	m.idxOnce.Do(func() {
		m.idx = make(map[string]map[string]int32, len(m.Enums))
		for k, l := range m.Enums {
			mm := make(map[string]int32, len(l))
			for i, n := range l {
				if _, dup := mm[n]; !dup {
					mm[n] = int32(i)
				}
			}
			m.idx[k] = mm
		}
	})
	if i, ok := m.idx[kind][name]; ok {
		return i
	}
	return -1
}

// EnumMap builds a table translating enum positions of kind from m to to.
// Values missing in to map to -1.
func EnumMap(from, to *Meta, kind string) []int32 {
	src := from.Enums[kind]
	out := make([]int32, len(src))
	for i, n := range src {
		out[i] = to.Index(kind, n)
	}
	return out
}
