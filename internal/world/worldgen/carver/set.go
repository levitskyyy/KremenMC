package carver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// SourceRange is the chunk radius the terrain step scans for carver
// sources (reference NoiseBasedChunkGenerator.generateCarvers).
const SourceRange = 8

// Set is the carver setup of a dimension: configured carvers by biome.
// It is immutable and safe for concurrent use.
type Set struct {
	Ctx     Context
	Seed    int64
	byBiome map[string][]Carver
}

// NewSet loads the vanilla configured carvers of every biome.
func NewSet(ctx Context, seed int64) (*Set, error) {
	p, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("carver: %w", err)
	}
	s := &Set{Ctx: ctx, Seed: seed, byBiome: map[string][]Carver{}}
	carvers := map[string]Carver{}
	for _, b := range p.Keys("worldgen/biome") {
		raw, _ := p.JSON("worldgen/biome", b)
		var bj struct {
			Carvers json.RawMessage `json:"carvers"`
		}
		if err := json.Unmarshal(raw, &bj); err != nil {
			return nil, fmt.Errorf("carver: biome %s: %w", b, err)
		}
		ids, err := carverList(p, bj.Carvers)
		if err != nil {
			return nil, fmt.Errorf("carver: biome %s: %w", b, err)
		}
		var list []Carver
		for _, id := range ids {
			c, ok := carvers[id]
			if !ok {
				src, ok := p.JSON("worldgen/carver", id)
				if !ok {
					return nil, fmt.Errorf("carver: biome %s: unknown carver %s", b, id)
				}
				if c, err = Parse(src); err != nil {
					return nil, fmt.Errorf("carver: %s: %w", id, err)
				}
				carvers[id] = c
			}
			list = append(list, c)
		}
		s.byBiome[b] = list
	}
	return s, nil
}

// carverList resolves a biome's carvers field: a list of ids, one id or
// a #tag.
func carverList(p *wgdata.Pack, raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if tag, ok := strings.CutPrefix(one, "#"); ok {
			l, ok := p.Tag("worldgen/carver", tag)
			if !ok {
				return nil, fmt.Errorf("unknown carver tag %s", one)
			}
			return l, nil
		}
		return []string{qualify(one)}, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	for i := range ids {
		ids[i] = qualify(ids[i])
	}
	return ids, nil
}

func qualify(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// Carvers returns the carvers of a biome (shared, do not modify).
func (s *Set) Carvers(biome string) []Carver { return s.byBiome[biome] }

// BiomeFunc returns the biome at quart coordinates qx, qz and quart y 0,
// as the dimension's biome source computes it.
type BiomeFunc func(qx, qz int32) string

// CarveChunk marks into out every position of chunk cx,cz carved by
// carvers started in chunks within SourceRange. Each source's carvers come
// from the biome at its minimum corner (quart y 0) and are seeded with
// setLargeFeatureSeed(seed + index, sx, sz).
func (s *Set) CarveChunk(cx, cz int32, biomeAt BiomeFunc, out Output) {
	var r rng.Legacy
	for sx := cx - SourceRange; sx <= cx+SourceRange; sx++ {
		for sz := cz - SourceRange; sz <= cz+SourceRange; sz++ {
			list := s.byBiome[biomeAt(sx<<2, sz<<2)]
			for i, c := range list {
				largeFeatureSeed(&r, s.Seed+int64(i), sx, sz)
				if c.IsStart(&r) {
					c.Carve(s.Ctx, &r, cx, cz, sx, sz, out)
				}
			}
		}
	}
}

func largeFeatureSeed(r *rng.Legacy, seed int64, x, z int32) {
	r.SetSeed(seed)
	a := r.NextLong()
	b := r.NextLong()
	r.SetSeed(int64(x)*a ^ int64(z)*b ^ seed)
}
