package structure

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// NewDimStages is NewStages for the Nether or the End: only structure sets
// with a structure whose biomes meet the dimension's biome tag ("is_nether",
// "is_end") take part, as vanilla's possibleStructureSets (probe
// kremenmc.probe.nstruct.NStarts: the Nether has nether_complexes,
// nether_fossils and ruined_portals). A set may hold a structure kremenmc
// does not implement; picking it generates nothing there.
func NewDimStages(seed int64, probe Probe, biomeTag string) (*Stages, error) {
	s, err := NewStages(seed, probe)
	if err != nil {
		return nil, err
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("structure: %w", err)
	}
	names, _ := pk.Tag("worldgen/biome", biomeTag)
	reg := data.Canonical()
	dimBiomes := map[data.BiomeID]bool{}
	for _, n := range names {
		if id, ok := reg.BiomeByName(trimNS(n)); ok {
			dimBiomes[id] = true
		}
	}
	possible := func(key string) bool {
		b, ok := pk.JSON("worldgen/structure", trimNS(key))
		if !ok {
			return false
		}
		var head struct {
			Biomes json.RawMessage `json:"biomes"`
		}
		if json.Unmarshal(b, &head) != nil {
			return false
		}
		for _, n := range biomeList(pk, head.Biomes) {
			if id, ok := reg.BiomeByName(trimNS(n)); ok && dimBiomes[id] {
				return true
			}
		}
		return false
	}
	keys := make([]string, 0, len(s.all))
	for k := range s.all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Not yet verified against the reference in this dimension.
	delete(s.defs, "minecraft:ruined_portal_nether")
	s.sets = nil
	for _, k := range keys {
		set := s.all[k]
		if set.Placement.Type != "random_spread" {
			continue
		}
		var ws []WeightedStructure
		typed := false
		for _, w := range set.Structures {
			if possible(w.Structure) {
				ws = append(ws, w)
				typed = typed || s.defs[qualify(w.Structure)] != nil
			}
		}
		if typed {
			s.sets = append(s.sets, &Set{Key: set.Key, Placement: set.Placement, Structures: ws})
		}
	}
	s.weighted = true
	return s, nil
}

// pickStart is the start of a weighted multi-structure set: the large
// feature random of the chunk draws nextInt(total weight) and the entry it
// lands on is tried (with its own fresh random); one that does not
// generate leaves the list and the rest are drawn again. Fitted on
// nether_complexes (fortress 2, bastion_remnant 3) over 400 reference
// cells: a first draw below 2 is always a fortress, otherwise a bastion,
// or a fortress where the bastion fails.
func (s *Stages) pickStart(set *Set, cx, cz int32) (*Start, error) {
	ws := append([]WeightedStructure(nil), set.Structures...)
	r := largeFeature(s.seed, cx, cz)
	for len(ws) > 0 {
		i := 0
		if len(ws) > 1 {
			total := int32(0)
			for _, w := range ws {
				total += w.Weight
			}
			v := r.NextIntn(total)
			for v >= ws[i].Weight {
				v -= ws[i].Weight
				i++
			}
		}
		if s.defs[qualify(ws[i].Structure)] == nil {
			return nil, nil
		}
		st, err := s.tryStructure(ws[i], cx, cz)
		if err != nil || st != nil {
			return st, err
		}
		ws = append(ws[:i], ws[i+1:]...)
	}
	return nil, nil
}
