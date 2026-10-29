package feature

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

//go:embed order.txt
var orderTxt string

type unsupportedError struct{ kind, typ string }

func (e *unsupportedError) Error() string { return "unsupported " + e.kind + " " + e.typ }

func unsupported(kind, typ string) error {
	return &unsupportedError{kind, strings.TrimPrefix(typ, "minecraft:")}
}

func asUnsupported(err error, u **unsupportedError) bool { return errors.As(err, u) }

// registry holds parsed placed features. Parsed values are immutable and
// shared by all chunks and worlds.
type registry struct {
	pack *wgdata.Pack

	mu          sync.Mutex
	placed      map[string]*Placed
	tags        map[string]blockSet
	unsupported map[string]int // "kind type" -> placed features affected
}

var (
	regOnce sync.Once
	reg     *registry
	regErr  error
)

func theRegistry() (*registry, error) {
	regOnce.Do(func() {
		p, err := wgdata.Load()
		if err != nil {
			regErr = err
			return
		}
		reg = &registry{pack: p, placed: map[string]*Placed{}, tags: map[string]blockSet{}, unsupported: map[string]int{}}
	})
	return reg, regErr
}

func (rg *registry) noteUnsupported(u *unsupportedError) {
	rg.mu.Lock()
	rg.unsupported[u.kind+" "+u.typ]++
	rg.mu.Unlock()
}

// placedByKey parses a registered placed feature once. Parsing happens
// while building a Stage, never concurrently with placement.
func (rg *registry) placedByKey(key string) (*Placed, error) {
	if !strings.Contains(key, ":") {
		key = "minecraft:" + key
	}
	rg.mu.Lock()
	pf, ok := rg.placed[key]
	rg.mu.Unlock()
	if ok {
		return pf, nil
	}
	raw, ok := rg.pack.JSON("worldgen/placed_feature", key)
	if !ok {
		return nil, fmt.Errorf("placed_feature %s: missing", key)
	}
	pf, err := rg.parseInlinePlaced(key, raw)
	if err != nil {
		return nil, err
	}
	rg.mu.Lock()
	if old, ok := rg.placed[key]; ok {
		pf = old
	} else {
		pf.id = len(rg.placed)
		rg.placed[key] = pf
	}
	rg.mu.Unlock()
	return pf, nil
}

// Unsupported lists feature, modifier and provider types that are placed as
// explicit no-ops, with the number of placed features they disable.
func Unsupported() []string {
	rg, err := theRegistry()
	if err != nil {
		return nil
	}
	rg.mu.Lock()
	defer rg.mu.Unlock()
	var out []string
	for k, n := range rg.unsupported {
		out = append(out, fmt.Sprintf("%s (%d)", k, n))
	}
	slices.Sort(out)
	return out
}

// dimOrder is the per-step feature order of one dimension.
func dimOrder(dim string) ([][]string, error) {
	var steps [][]string
	in := false
	for _, line := range strings.Split(orderTxt, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "dim":
			if in {
				return steps, nil
			}
			in = f[1] == dim
		case "step":
			if in {
				steps = append(steps, f[2:])
			}
		}
	}
	if !in {
		return nil, fmt.Errorf("feature: no feature order for dimension %q", dim)
	}
	return steps, nil
}

// biomeFeatures reads a biome's feature lists per step.
func (rg *registry) biomeFeatures(name string) ([][]string, error) {
	raw, ok := rg.pack.JSON("worldgen/biome", name)
	if !ok {
		return nil, fmt.Errorf("biome %s: missing", name)
	}
	var b struct {
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("biome %s: %w", name, err)
	}
	out := make([][]string, len(b.Features))
	for i, s := range b.Features {
		l, err := stringList(s)
		if err != nil {
			return nil, fmt.Errorf("biome %s step %d: %w", name, i, err)
		}
		out[i] = l
	}
	return out, nil
}

func numBiomes() int { return len(data.Canonical().Biomes) }
