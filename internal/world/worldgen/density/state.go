package density

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Registry resolves density function and noise JSON by key.
type Registry interface {
	JSON(registry, key string) ([]byte, bool)
}

var (
	packOnce sync.Once
	pack     *wgdata.Pack
	packErr  error
)

// Vanilla returns the embedded vanilla data pack.
func Vanilla() (Registry, error) {
	packOnce.Do(func() { pack, packErr = wgdata.Load() })
	return pack, packErr
}

// State is the reference RandomState: per-seed noise instances, positional
// randoms and the compiled samplers. It is immutable after NewState
// returns, apart from internal memoization done during construction.
type State struct {
	Seed   int64
	Legacy bool
	reg    Registry
	pos    rng.Positional
	noises map[string]noise.Noise
	nodes  map[string]*Node
	nextID int
}

// NewState creates the random state for a world seed. legacy selects the
// legacy random source (noise_settings legacy_random_source).
func NewState(reg Registry, seed int64, legacy bool) *State {
	alg := rng.AlgorithmXoroshiro
	if legacy {
		alg = rng.AlgorithmLegacy
	}
	return &State{Seed: seed, Legacy: legacy, reg: reg, pos: alg.NewInstance(seed).ForkPositional(),
		noises: map[string]noise.Noise{}, nodes: map[string]*Node{}}
}

// Positional is the state's positional random factory.
func (s *State) Positional() rng.Positional { return s.pos }

// NoiseParams loads a noise definition.
func (s *State) NoiseParams(key string) (noise.NormalParams, error) {
	var p noise.NormalParams
	b, ok := s.reg.JSON("worldgen/noise", key)
	if !ok {
		return p, fmt.Errorf("density: unknown noise %s", key)
	}
	err := json.Unmarshal(b, &p)
	return p, err
}

// Noise returns the instance of a registered noise for this seed.
func (s *State) Noise(key string) (noise.Noise, error) {
	if n, ok := s.noises[key]; ok {
		return n, nil
	}
	p, err := s.NoiseParams(key)
	if err != nil {
		return nil, err
	}
	nn, err := noise.NewNormalNoise(p)
	if err != nil {
		return nil, fmt.Errorf("density: noise %s: %w", key, err)
	}
	n := nn.Create(s.pos.FromHashOf(key))
	s.noises[key] = n
	return n, nil
}

// Function loads (and memoizes) a registered density function.
func (s *State) Function(key string) (*Node, error) {
	if n, ok := s.nodes[key]; ok {
		return n, nil
	}
	b, ok := s.reg.JSON("worldgen/density_function", key)
	if !ok {
		return nil, fmt.Errorf("density: unknown density function %s", key)
	}
	n, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("density: %s: %w", key, err)
	}
	s.nodes[key] = n
	return n, nil
}

// legacyBiomeNoise is the legacy-random-source special case: the nether
// biome noises use the pre-1.18 sequential initializer seeded with the
// world seed (temperature) and seed+1 (vegetation).
func (s *State) legacyBiomeNoise(key string) (noise.Noise, error) {
	if n, ok := s.noises["legacy:"+key]; ok {
		return n, nil
	}
	p, err := s.NoiseParams(key)
	if err != nil {
		return nil, err
	}
	nn, err := noise.NewNormalNoise(p)
	if err != nil {
		return nil, err
	}
	sd := s.Seed
	if key == "minecraft:nether/vegetation" {
		sd++
	}
	n, err := nn.CreateForLegacyNetherBiome(rng.NewLegacy(sd))
	if err != nil {
		return nil, err
	}
	s.noises["legacy:"+key] = n
	return n, nil
}
