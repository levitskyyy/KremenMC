package density

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
)

// Settings is the part of a noise_settings entry the density layer uses.
type Settings struct {
	Key          string
	MinY, Height int32
	SeaLevel     int32
	Legacy       bool
	DefaultBlock string
	DefaultFluid string
	Router       map[string]*Node
	Aquifers     map[string]*Node
}

// RouterFields are the 26.3 noise router fields.
var RouterFields = []string{"temperature", "vegetation", "continents", "erosion", "depth", "ridges", "chunk_surface_level", "final_density"}

// AquiferFields are the 26.3 aquifer configuration functions.
var AquiferFields = []string{"barrier", "exclusion", "fluid_level_floodedness", "fluid_level_spread", "lava", "surface_level"}

// LoadSettings parses a noise_settings entry.
func LoadSettings(reg Registry, key string) (*Settings, error) {
	b, ok := reg.JSON("worldgen/noise_settings", key)
	if !ok {
		return nil, fmt.Errorf("density: unknown noise settings %s", key)
	}
	var raw struct {
		Noise struct {
			MinY   int32 `json:"min_y"`
			Height int32 `json:"height"`
		} `json:"noise"`
		SeaLevel int32                      `json:"sea_level"`
		Block    string                     `json:"default_block"`
		Fluid    string                     `json:"default_fluid"`
		Legacy   bool                       `json:"legacy_random_source"`
		Router   map[string]json.RawMessage `json:"noise_router"`
		Aquifers map[string]json.RawMessage `json:"aquifers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("density: %s: %w", key, err)
	}
	s := &Settings{Key: key, MinY: raw.Noise.MinY, Height: raw.Noise.Height, SeaLevel: raw.SeaLevel, Legacy: raw.Legacy, DefaultBlock: raw.Block, DefaultFluid: raw.Fluid,
		Router: map[string]*Node{}, Aquifers: map[string]*Node{}}
	for _, f := range RouterFields {
		n, err := Parse(raw.Router[f])
		if err != nil {
			return nil, fmt.Errorf("density: %s router %s: %w", key, f, err)
		}
		s.Router[f] = n
	}
	for k, v := range raw.Aquifers {
		n, err := Parse(v)
		if err != nil {
			return nil, fmt.Errorf("density: %s aquifer %s: %w", key, k, err)
		}
		s.Aquifers[k] = n
	}
	return s, nil
}

// Router is a compiled noise router for one seed.
type Router struct {
	Settings *Settings
	State    *State
	Fields   map[string]Sampler
	Aquifers map[string]Sampler

	temperature, vegetation, continents, erosion, depth, ridges Sampler
}

// NewRouter compiles every router and aquifer function for a seed.
func NewRouter(reg Registry, settingsKey string, seed int64) (*Router, error) {
	s, err := LoadSettings(reg, settingsKey)
	if err != nil {
		return nil, err
	}
	st := NewState(reg, seed, s.Legacy)
	c := &compiler{s: st, refs: map[string]Sampler{}, nodes: map[*Node]Sampler{}}
	r := &Router{Settings: s, State: st, Fields: map[string]Sampler{}, Aquifers: map[string]Sampler{}}
	for _, f := range RouterFields {
		r.Fields[f] = c.node(s.Router[f])
	}
	for k, n := range s.Aquifers {
		r.Aquifers[k] = c.node(n)
	}
	if c.err != nil {
		return nil, c.err
	}
	r.temperature, r.vegetation, r.continents = r.Fields["temperature"], r.Fields["vegetation"], r.Fields["continents"]
	r.erosion, r.depth, r.ridges = r.Fields["erosion"], r.Fields["depth"], r.Fields["ridges"]
	return r, nil
}

// ClimateTarget is the reference Climate.Sampler.sample at a quart
// position: each climate field sampled at the block (q<<2) and quantized.
func (r *Router) ClimateTarget(c *Ctx, qx, qy, qz int32) climate.Target {
	x, y, z := qx<<2, qy<<2, qz<<2
	return climate.Target{
		Temperature:     climate.Quantize(r.temperature.Value(c, x, y, z)),
		Humidity:        climate.Quantize(r.vegetation.Value(c, x, y, z)),
		Continentalness: climate.Quantize(r.continents.Value(c, x, y, z)),
		Erosion:         climate.Quantize(r.erosion.Value(c, x, y, z)),
		Depth:           climate.Quantize(r.depth.Value(c, x, y, z)),
		Weirdness:       climate.Quantize(r.ridges.Value(c, x, y, z)),
	}
}

type routerKey struct {
	seed     int64
	settings string
}

var routers sync.Map

// CachedRouter returns a shared compiled router for (vanilla data, settings,
// seed). Routers are immutable and safe for concurrent use.
func CachedRouter(settings string, seed int64) (*Router, error) {
	k := routerKey{seed, settings}
	if r, ok := routers.Load(k); ok {
		return r.(*Router), nil
	}
	reg, err := Vanilla()
	if err != nil {
		return nil, err
	}
	r, err := NewRouter(reg, settings, seed)
	if err != nil {
		return nil, err
	}
	v, _ := routers.LoadOrStore(k, r)
	return v.(*Router), nil
}

// ClimateTarget samples the climate target point at a quart position for a
// world seed and noise settings key (e.g. "minecraft:overworld").
func ClimateTarget(seed int64, settings string, qx, qy, qz int32) (climate.Target, error) {
	r, err := CachedRouter(settings, seed)
	if err != nil {
		return climate.Target{}, err
	}
	return r.ClimateTarget(nil, qx, qy, qz), nil
}
