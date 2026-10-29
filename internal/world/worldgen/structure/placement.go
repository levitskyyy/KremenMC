// Package structure implements vanilla 26.3 structure generation: structure
// sets and their placement, structure starts and references, and the piece
// model structures are built from (see piece.go).
package structure

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Reduction is a frequency_reduction_method.
type Reduction uint8

const (
	ReduceDefault Reduction = iota
	ReduceLegacy1
	ReduceLegacy2
	ReduceLegacy3
)

var reductionNames = map[string]Reduction{
	"default": ReduceDefault, "legacy_type_1": ReduceLegacy1,
	"legacy_type_2": ReduceLegacy2, "legacy_type_3": ReduceLegacy3,
}

// Exclusion is an exclusion_zone: no start within ChunkCount chunks of a
// potential chunk of OtherSet.
type Exclusion struct {
	OtherSet   string
	ChunkCount int32
}

// Placement holds the fields common to every structure placement.
type Placement struct {
	Type         string // "random_spread" or "concentric_rings"
	Salt         int32
	Frequency    float32
	Reduction    Reduction
	LocateOffset [3]int32
	Exclusion    *Exclusion

	// random_spread
	Spacing, Separation int32
	Triangular          bool

	// concentric_rings
	Distance, Spread, Count int32
	PreferredBiomes         string
}

// Set is a structure_set entry.
type Set struct {
	Key        string
	Placement  Placement
	Structures []WeightedStructure
}

// WeightedStructure is one element of a set's structures list.
type WeightedStructure struct {
	Structure string
	Weight    int32
}

// PotentialChunk is RandomSpreadStructurePlacement.getPotentialStructureChunk:
// the one chunk of the spacing-sized cell holding cx,cz that may start the
// set's structure.
func (p *Placement) PotentialChunk(seed int64, cx, cz int32) (int32, int32) {
	gx, gz := floorDiv(cx, p.Spacing), floorDiv(cz, p.Spacing)
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureWithSalt(seed, gx, gz, p.Salt)
	limit := p.Spacing - p.Separation
	ox := p.spread(r, limit)
	oz := p.spread(r, limit)
	return gx*p.Spacing + ox, gz*p.Spacing + oz
}

func (p *Placement) spread(r *rng.WorldgenRandom, limit int32) int32 {
	if p.Triangular {
		return (r.NextIntn(limit) + r.NextIntn(limit)) / 2
	}
	return r.NextIntn(limit)
}

// FrequencyOK is applyAdditionalChunkRestrictions: the frequency reducer
// test for chunk cx,cz (always true at frequency 1).
func (p *Placement) FrequencyOK(seed int64, cx, cz int32) bool {
	if p.Frequency >= 1 {
		return true
	}
	switch p.Reduction {
	case ReduceLegacy1:
		x, z := cx>>4, cz>>4
		r := rng.NewLegacy(int64(x^z<<4) ^ seed)
		r.NextInt()
		return r.NextIntn(int32(1/p.Frequency)) == 0
	case ReduceLegacy2:
		r := rng.NewWorldgenRandom(rng.NewLegacy(0))
		r.SetLargeFeatureWithSalt(seed, cx, cz, 10387320)
		return r.NextFloat() < p.Frequency
	case ReduceLegacy3:
		r := rng.NewWorldgenRandom(rng.NewLegacy(0))
		r.SetLargeFeatureSeed(seed, cx, cz)
		return r.NextDouble() < float64(p.Frequency)
	default:
		r := rng.NewWorldgenRandom(rng.NewLegacy(0))
		r.SetLargeFeatureWithSalt(seed, cx, cz, p.Salt)
		return r.NextFloat() < p.Frequency
	}
}

// LocatePos is getLocatePos: the chunk's minimum block plus locate_offset.
func (p *Placement) LocatePos(cx, cz int32) (x, y, z int32) {
	return cx<<4 + p.LocateOffset[0], p.LocateOffset[1], cz<<4 + p.LocateOffset[2]
}

// IsSpreadChunk reports whether cx,cz passes the random_spread position and
// frequency tests. Exclusion zones need the other set and are checked by
// the caller (State.IsStructureChunk).
func (p *Placement) IsSpreadChunk(seed int64, cx, cz int32) bool {
	px, pz := p.PotentialChunk(seed, cx, cz)
	return px == cx && pz == cz && p.FrequencyOK(seed, cx, cz)
}

func floorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

type setJSON struct {
	Placement struct {
		Type          string   `json:"type"`
		Salt          int32    `json:"salt"`
		Frequency     *float32 `json:"frequency"`
		Reduction     string   `json:"frequency_reduction_method"`
		LocateOffset  []int32  `json:"locate_offset"`
		ExclusionZone *struct {
			ChunkCount int32  `json:"chunk_count"`
			OtherSet   string `json:"other_set"`
		} `json:"exclusion_zone"`
		Spacing         int32           `json:"spacing"`
		Separation      int32           `json:"separation"`
		SpreadType      string          `json:"spread_type"`
		Distance        int32           `json:"distance"`
		Spread          int32           `json:"spread"`
		Count           int32           `json:"count"`
		PreferredBiomes json.RawMessage `json:"preferred_biomes"`
	} `json:"placement"`
	Structures []struct {
		Structure string `json:"structure"`
		Weight    int32  `json:"weight"`
	} `json:"structures"`
}

// ParseSet decodes a structure_set JSON.
func ParseSet(key string, b []byte) (*Set, error) {
	var j setJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("structure: set %s: %w", key, err)
	}
	pj := j.Placement
	p := Placement{Type: trimNS(pj.Type), Salt: pj.Salt, Frequency: 1, Spacing: pj.Spacing, Separation: pj.Separation,
		Distance: pj.Distance, Spread: pj.Spread, Count: pj.Count}
	if pj.Frequency != nil {
		p.Frequency = *pj.Frequency
	}
	if pj.Reduction != "" {
		r, ok := reductionNames[pj.Reduction]
		if !ok {
			return nil, fmt.Errorf("structure: set %s: unknown frequency_reduction_method %q", key, pj.Reduction)
		}
		p.Reduction = r
	}
	if len(pj.LocateOffset) == 3 {
		copy(p.LocateOffset[:], pj.LocateOffset)
	}
	if pj.ExclusionZone != nil {
		p.Exclusion = &Exclusion{OtherSet: pj.ExclusionZone.OtherSet, ChunkCount: pj.ExclusionZone.ChunkCount}
	}
	switch p.Type {
	case "random_spread":
		switch pj.SpreadType {
		case "", "linear":
		case "triangular":
			p.Triangular = true
		default:
			return nil, fmt.Errorf("structure: set %s: unknown spread_type %q", key, pj.SpreadType)
		}
		if p.Spacing <= p.Separation {
			return nil, fmt.Errorf("structure: set %s: spacing %d <= separation %d", key, p.Spacing, p.Separation)
		}
	case "concentric_rings":
		if len(pj.PreferredBiomes) > 0 {
			_ = json.Unmarshal(pj.PreferredBiomes, &p.PreferredBiomes)
		}
	default:
		return nil, fmt.Errorf("structure: set %s: unknown placement %q", key, pj.Type)
	}
	s := &Set{Key: key, Placement: p}
	for _, e := range j.Structures {
		s.Structures = append(s.Structures, WeightedStructure{Structure: e.Structure, Weight: e.Weight})
	}
	return s, nil
}

// LoadSets returns every vanilla structure set, sorted by key.
func LoadSets() ([]*Set, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("structure: %w", err)
	}
	keys := append([]string(nil), pk.Keys("worldgen/structure_set")...)
	sort.Strings(keys)
	var out []*Set
	for _, k := range keys {
		b, ok := pk.JSON("worldgen/structure_set", k)
		if !ok {
			return nil, fmt.Errorf("structure: set %s missing", k)
		}
		s, err := ParseSet(k, b)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func trimNS(s string) string {
	if len(s) > 10 && s[:10] == "minecraft:" {
		return s[10:]
	}
	return s
}

// InExclusionRange reports whether a chunk of other within r chunks
// (square) of cx,cz passes other's spread and frequency tests. Black box
// (probe kremenmc.probe.outpost.Excl, hasStructureChunkInRange): no biome or
// start check is involved.
func InExclusionRange(other *Placement, seed int64, cx, cz, r int32) bool {
	for z := cz - r; z <= cz+r; z++ {
		for x := cx - r; x <= cx+r; x++ {
			if other.IsSpreadChunk(seed, x, z) {
				return true
			}
		}
	}
	return false
}
