// Package feature implements the vanilla 26.3 features chunk status: biome
// feature lists, per-step feature order and seeding, placement modifiers,
// block predicates, providers and the feature types kremenmc supports. Feature
// types it does not support yet are explicit no-ops (see Unsupported).
package feature

import (
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
)

// Stage is the features status of one dimension. It is immutable after
// NewStage and safe for concurrent chunks.
type Stage struct {
	order    [][]*Placed // per step, in vanilla's global order
	inStep   [][][]bool  // [biome][step][order index]
	inBiome  [][]bool    // [biome][placed id]: biome filter
	zoomSeed int64

	// StructureStep, if set, places the structures of a decoration step
	// before that step's features.
	StructureStep func(sc *gen.StageContext, step int)
}

// NewStage builds the features stage of a vanilla dimension
// ("overworld", "the_nether", "the_end").
func NewStage(dim string, seed int64) (*Stage, error) {
	dim = strings.TrimPrefix(dim, "minecraft:")
	rg, err := theRegistry()
	if err != nil {
		return nil, fmt.Errorf("feature: %w", err)
	}
	keys, err := dimOrder(dim)
	if err != nil {
		return nil, err
	}
	s := &Stage{zoomSeed: surface.ObfuscateSeed(seed)}
	s.order = make([][]*Placed, len(keys))
	index := make([]map[string]int, len(keys))
	for step, l := range keys {
		index[step] = map[string]int{}
		for i, k := range l {
			pf, err := rg.placedByKey(k)
			if err != nil {
				return nil, fmt.Errorf("feature: %w", err)
			}
			s.order[step] = append(s.order[step], pf)
			index[step][k] = i
		}
	}
	reg := data.Canonical()
	n := numBiomes()
	s.inStep = make([][][]bool, n)
	s.inBiome = make([][]bool, n)
	for b := range n {
		lists, err := rg.biomeFeatures(reg.BiomeName(data.BiomeID(b)))
		if err != nil {
			continue
		}
		s.inStep[b] = make([][]bool, len(keys))
		for step := range keys {
			s.inStep[b][step] = make([]bool, len(keys[step]))
		}
		for step, l := range lists {
			for _, k := range l {
				pf, err := rg.placedByKey(k)
				if err != nil {
					return nil, fmt.Errorf("feature: biome %s: %w", reg.BiomeName(data.BiomeID(b)), err)
				}
				for pf.id >= len(s.inBiome[b]) {
					s.inBiome[b] = append(s.inBiome[b], false)
				}
				s.inBiome[b][pf.id] = true
				if step < len(keys) {
					if i, ok := index[step][k]; ok {
						s.inStep[b][step][i] = true
					}
				}
			}
		}
	}
	return s, nil
}

func (s *Stage) Status() gen.Status { return gen.StatusFeatures }

func (s *Stage) biomeHas(b data.BiomeID, pf *Placed) bool {
	if int(b) < 0 || int(b) >= len(s.inBiome) {
		return false
	}
	l := s.inBiome[b]
	return pf.id >= 0 && pf.id < len(l) && l[pf.id]
}

// Run is ChunkGenerator.applyBiomeDecoration without structures: every step
// in order, every feature of the step's global order that a biome of the
// chunk or its eight neighbours lists, each seeded with
// setFeatureSeed(decorationSeed, index, step).
func (s *Stage) Run(sc *gen.StageContext) error {
	present := s.presentBiomes(sc)
	var biomes []data.BiomeID
	for b, ok := range present {
		if ok && s.inStep[b] != nil {
			biomes = append(biomes, data.BiomeID(b))
		}
	}
	lv := &Level{rg: sc, sc: sc, st: s, wg: map[world.ChunkPos]*[2][256]int16{}}
	r, decoSeed := sc.Decoration()
	origin := Pos{sc.MinBlockX(), sc.Dim().MinY, sc.MinBlockZ()}
	for step, l := range s.order {
		if s.StructureStep != nil {
			s.StructureStep(sc, step)
		}
		for i, pf := range l {
			want := false
			for _, b := range biomes {
				if s.inStep[b][step][i] {
					want = true
					break
				}
			}
			if !want {
				continue
			}
			r.SetFeatureSeed(decoSeed, int32(i), int32(step))
			pf.place(lv, r, origin, pf)
		}
	}
	return nil
}
