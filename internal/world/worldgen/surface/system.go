// Package surface implements the material pass of the reference 26.3
// TERRAIN status (MaterialSystem.buildSurface): data-driven material rules
// and conditions (worldgen/material_rule, worldgen/material_condition) plus
// the hardcoded eroded badlands and frozen ocean extensions, applied to a
// chunk after the noise fill.
//
// A System is compiled once per noise settings and seed; it is immutable
// and safe for concurrent use. Per-goroutine scratch state lives in Ctx.
package surface

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

const noState = ^data.StateID(0)

// System is the reference MaterialSystem for one noise settings entry and
// seed, with its material rule compiled.
type System struct {
	Settings     string
	Seed         int64
	MinY, Height int32
	SeaLevel     int32
	DefaultBlock data.StateID

	reg   *data.Registry
	state *density.State
	pos   rng.Positional
	pr    posRandom

	surfaceNoise, secondaryNoise, clayOffset         noise.Noise
	badlandsPillar, badlandsPillarRoof, badlandsSurf noise.Noise
	icebergPillar, icebergPillarRoof, icebergSurf    noise.Noise
	bands                                            [192]data.StateID
	chunkSurface                                     density.Sampler

	rule      ruleEval
	biomeObf  int64
	climates  []climate // by data.BiomeID
	slots     int       // memo slots used by compiled conditions
	noises2d  map[string]noise.Noise
	stateMemo map[string]data.StateID

	sPackedIce, sSnowBlock, sAir, sWater, sIce data.StateID
	sTerracotta                                data.StateID
	erodedBadlands, frozenOcean, deepFrozen    data.BiomeID
}

var vanilla struct {
	once sync.Once
	pack *wgdata.Pack
	err  error
}

func vanillaPack() (*wgdata.Pack, error) {
	vanilla.once.Do(func() { vanilla.pack, vanilla.err = wgdata.Load() })
	return vanilla.pack, vanilla.err
}

type sysKey struct {
	settings string
	seed     int64
}

var systems sync.Map

// Vanilla returns the shared System for a vanilla noise settings key
// ("minecraft:overworld", "minecraft:nether", ...) and seed.
func Vanilla(settings string, seed int64) (*System, error) {
	k := sysKey{settings, seed}
	if s, ok := systems.Load(k); ok {
		return s.(*System), nil
	}
	p, err := vanillaPack()
	if err != nil {
		return nil, err
	}
	s, err := New(p, settings, seed)
	if err != nil {
		return nil, err
	}
	v, _ := systems.LoadOrStore(k, s)
	return v.(*System), nil
}

// SettingsFor maps a vanilla dimension to its noise settings key.
func SettingsFor(dim string) (string, error) {
	switch dim {
	case "overworld", "minecraft:overworld":
		return "minecraft:overworld", nil
	case "the_nether", "minecraft:the_nether":
		return "minecraft:nether", nil
	case "the_end", "minecraft:the_end":
		return "minecraft:end", nil
	}
	return "", fmt.Errorf("surface: unknown dimension %q", dim)
}

// New builds the system for a noise settings entry of reg and a seed.
func New(reg Registry, settings string, seed int64) (*System, error) {
	b, ok := reg.JSON("worldgen/noise_settings", settings)
	if !ok {
		return nil, fmt.Errorf("surface: unknown noise settings %s", settings)
	}
	var raw struct {
		Noise struct {
			MinY   int32 `json:"min_y"`
			Height int32 `json:"height"`
		} `json:"noise"`
		SeaLevel     int32           `json:"sea_level"`
		Legacy       bool            `json:"legacy_random_source"`
		DefaultBlock json.RawMessage `json:"default_block"`
		MaterialRule json.RawMessage `json:"material_rule"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("surface: %s: %w", settings, err)
	}
	s := &System{Settings: settings, Seed: seed, MinY: raw.Noise.MinY, Height: raw.Noise.Height, SeaLevel: raw.SeaLevel,
		reg: data.Canonical(), noises2d: map[string]noise.Noise{}, stateMemo: map[string]data.StateID{}}
	var err error
	ds, err := parseState(raw.DefaultBlock)
	if err != nil {
		return nil, err
	}
	if s.DefaultBlock, err = s.stateID(ds); err != nil {
		return nil, err
	}
	s.state = density.NewState(reg, seed, raw.Legacy)
	s.pos = s.state.Positional()
	s.pr = newPosRandom(s.pos)
	s.biomeObf = ObfuscateSeed(seed)
	for _, n := range []struct {
		dst *noise.Noise
		key string
	}{
		{&s.surfaceNoise, "surface"}, {&s.secondaryNoise, "surface_secondary"}, {&s.clayOffset, "clay_bands_offset"},
		{&s.badlandsPillar, "badlands_pillar"}, {&s.badlandsPillarRoof, "badlands_pillar_roof"}, {&s.badlandsSurf, "badlands_surface"},
		{&s.icebergPillar, "iceberg_pillar"}, {&s.icebergPillarRoof, "iceberg_pillar_roof"}, {&s.icebergSurf, "iceberg_surface"},
	} {
		if *n.dst, err = s.state.Noise("minecraft:" + n.key); err != nil {
			return nil, err
		}
	}
	if err := s.initStates(); err != nil {
		return nil, err
	}
	s.makeBands(s.pos.FromHashOf("minecraft:clay_bands"))
	set, err := density.LoadSettings(reg, settings)
	if err != nil {
		return nil, err
	}
	if s.chunkSurface, err = s.state.Compile(set.Router["chunk_surface_level"]); err != nil {
		return nil, err
	}
	if err := s.loadClimates(reg); err != nil {
		return nil, err
	}
	rule, err := ParseRule(reg, raw.MaterialRule)
	if err != nil {
		return nil, err
	}
	c := &compiler{s: s, reg: reg, rules: map[*Rule]ruleEval{}, conds: map[*Condition]condEval{}}
	if s.rule, err = c.rule(rule); err != nil {
		return nil, err
	}
	s.slots = c.slots
	return s, nil
}

func (s *System) stateID(str string) (data.StateID, error) {
	if id, ok := s.stateMemo[str]; ok {
		return id, nil
	}
	id, ok := s.reg.ParseState(str)
	if !ok {
		return 0, fmt.Errorf("surface: unknown block state %s", str)
	}
	s.stateMemo[str] = id
	return id, nil
}

func (s *System) mustState(str string) data.StateID {
	id, err := s.stateID(str)
	if err != nil {
		panic(err)
	}
	return id
}

func (s *System) initStates() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	s.sPackedIce = s.mustState("minecraft:packed_ice")
	s.sSnowBlock = s.mustState("minecraft:snow_block")
	s.sAir = s.mustState("minecraft:air")
	s.sWater = s.mustState("minecraft:water")
	s.sIce = s.mustState("minecraft:ice")
	s.sTerracotta = s.mustState("minecraft:terracotta")
	return nil
}

func (s *System) loadClimates(reg Registry) error {
	biomes := s.reg.Biomes
	s.climates = make([]climate, len(biomes))
	for i, b := range biomes {
		c, err := loadClimate(reg, qualify(b.Name))
		if err != nil {
			continue // biomes without data (none in vanilla) never match temperature
		}
		s.climates[i] = c
	}
	var ok bool
	if s.erodedBadlands, ok = s.reg.BiomeByName("eroded_badlands"); !ok {
		return fmt.Errorf("surface: no eroded_badlands biome")
	}
	if s.frozenOcean, ok = s.reg.BiomeByName("frozen_ocean"); !ok {
		return fmt.Errorf("surface: no frozen_ocean biome")
	}
	if s.deepFrozen, ok = s.reg.BiomeByName("deep_frozen_ocean"); !ok {
		return fmt.Errorf("surface: no deep_frozen_ocean biome")
	}
	return nil
}

// makeBands is the reference clay band table generation.
func (s *System) makeBands(r rng.Source) {
	terracotta := s.sTerracotta
	for i := range s.bands {
		s.bands[i] = terracotta
	}
	orange := s.mustState("minecraft:orange_terracotta")
	for i := 0; i < len(s.bands); i++ {
		i += int(r.NextIntn(5)) + 1
		if i < len(s.bands) {
			s.bands[i] = orange
		}
	}
	run := func(base int, st data.StateID) {
		n := int(r.NextIntBetweenInclusive(6, 15))
		for range n {
			w := base + int(r.NextIntn(3))
			start := int(r.NextIntn(int32(len(s.bands))))
			for k := 0; start+k < len(s.bands) && k < w; k++ {
				s.bands[start+k] = st
			}
		}
	}
	run(1, s.mustState("minecraft:yellow_terracotta"))
	run(2, s.mustState("minecraft:brown_terracotta"))
	run(1, s.mustState("minecraft:red_terracotta"))
	white, gray := s.mustState("minecraft:white_terracotta"), s.mustState("minecraft:light_gray_terracotta")
	n := int(r.NextIntBetweenInclusive(9, 15))
	for i, j := 0, 0; i < n && j < len(s.bands); i, j = i+1, j+int(r.NextIntn(16))+4 {
		s.bands[j] = white
		if j-1 > 0 && r.NextBool() {
			s.bands[j-1] = gray
		}
		if j+1 < len(s.bands) && r.NextBool() {
			s.bands[j+1] = gray
		}
	}
}

// Band is the reference MaterialSystem.getBand.
func (s *System) Band(x, y, z int32) data.StateID {
	off := int32(math.Floor(float64(s.clayOffset.Get(float64(x), 0, float64(z)))*4 + 0.5))
	return s.bands[((y+off)%192+192)%192]
}

// SurfaceDepth is the reference MaterialSystem.getSurfaceDepth.
func (s *System) SurfaceDepth(x, z int32) int32 {
	n := float64(s.surfaceNoise.Get(float64(x), 0, float64(z)))
	r := s.pr.at(x, 0, z)
	return int32(math.Floor(n*2.75 + 3 + r.nextDouble()*0.25))
}

// SurfaceSecondary is the reference MaterialSystem.getSurfaceSecondary.
func (s *System) SurfaceSecondary(x, z int32) float64 {
	return max(0, float64(s.secondaryNoise.Get(float64(x), 0, float64(z))))
}

// Bands returns the clay band table.
func (s *System) Bands() []data.StateID { return s.bands[:] }
