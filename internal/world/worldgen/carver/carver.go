package carver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Carver is a configured carver (reference WorldCarver).
type Carver interface {
	// IsStart draws whether the source chunk starts this carver.
	IsStart(r rng.Source) bool
	// Carve carves the part of the carver started in source chunk sx,sz
	// that lies in chunk cx,cz.
	Carve(ctx Context, r rng.Source, cx, cz, sx, sz int32, out Output)
}

// Parse decodes a configured carver JSON (worldgen/carver).
func Parse(b []byte) (Carver, error) {
	var h struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("carver: %w", err)
	}
	switch strings.TrimPrefix(h.Type, "minecraft:") {
	case "cave":
		c := &Cave{StartVerticalRadius: ConstFloat(1)}
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("carver: cave: %w", err)
		}
		return c, nil
	case "canyon":
		c := &Canyon{}
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("carver: canyon: %w", err)
		}
		if c.Shape.WidthSmoothness <= 0 {
			return nil, fmt.Errorf("carver: canyon: width_smoothness must be positive")
		}
		return c, nil
	}
	return nil, fmt.Errorf("carver: unknown type %q", h.Type)
}
