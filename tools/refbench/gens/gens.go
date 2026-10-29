// Package gens is the registry of kremenmc generators the bench can run. A
// generator package registers itself from init (or a bench-only file):
//
//	func init() { gens.Register("vanilla", func(seed int64, dim string) world.Generator { ... }) }
//
// and the refbench command imports it for side effects.
package gens

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/generator"
)

// Factory builds a generator for a seed and dimension ("overworld",
// "the_nether", "the_end"). It returns nil when the dimension is unsupported.
type Factory func(seed int64, dim string) world.Generator

// Staged is optionally implemented by generators that can stop after a
// stage-oracle stage ("biomes", "noise", "surface", "terrain"), leaving the
// chunk as vanilla's proto-chunk would be at that point.
type Staged interface {
	GenerateStage(c *world.Chunk, stage string) error
}

var (
	mu  sync.RWMutex
	reg = map[string]Factory{}
)

// Register adds a generator; it panics on duplicate names.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := reg[name]; dup {
		panic("gens: duplicate generator " + name)
	}
	reg[name] = f
}

// Lookup returns a registered factory.
func Lookup(name string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := reg[name]
	if !ok {
		return nil, fmt.Errorf("gens: unknown generator %q (have %v)", name, slices.Sorted(maps.Keys(reg)))
	}
	return f, nil
}

// Names lists registered generators.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	return slices.Sorted(maps.Keys(reg))
}

// Dimension returns the vertical bounds of a vanilla dimension.
func Dimension(dim string) (world.Dimension, error) {
	switch dim {
	case "overworld", "minecraft:overworld":
		return world.Overworld, nil
	case "the_nether", "minecraft:the_nether":
		return world.Nether, nil
	case "the_end", "minecraft:the_end":
		return world.End, nil
	}
	return world.Dimension{}, fmt.Errorf("gens: unknown dimension %q", dim)
}

func init() {
	Register("terrain", func(seed int64, dim string) world.Generator {
		if dim != "overworld" && dim != "minecraft:overworld" {
			return nil
		}
		return generator.NewTerrain(seed)
	})
	Register("flat", func(int64, string) world.Generator { return generator.DefaultFlat() })
}
