package vanilla

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
)

// biomeCache shares computed biome columns between the biomes stage, the
// terrain stage and the surface rules of neighbouring chunks, which would
// otherwise each recompute them. Bounded: a full shard is dropped.
type biomeCache struct {
	src          biome.Source
	minQ, quarts int32
	shards       [64]biomeShard
}

type biomeShard struct {
	mu sync.Mutex
	m  map[[2]int32]biomeCol
}

type biomeCol struct {
	col []data.BiomeID
	ok  bool // every biome name resolved
}

const biomeShardCap = 32

func newBiomeCache(src biome.Source, minY, height int32) *biomeCache {
	b := &biomeCache{src: src, minQ: minY >> 2, quarts: height >> 2}
	for i := range b.shards {
		b.shards[i].m = make(map[[2]int32]biomeCol, biomeShardCap)
	}
	return b
}

func (b *biomeCache) get(cx, cz int32) biomeCol {
	k := [2]int32{cx, cz}
	sh := &b.shards[(uint32(cx)*0x9E3779B1^uint32(cz)*0x85EBCA77)>>26]
	sh.mu.Lock()
	v, hit := sh.m[k]
	sh.mu.Unlock()
	if hit {
		return v
	}
	reg := data.Canonical()
	v = biomeCol{col: make([]data.BiomeID, b.quarts*16), ok: true}
	biome.FillChunk(b.src, cx, cz, b.minQ>>2, b.quarts>>2, func(qx, qy, qz int32, name string) {
		id, ok := reg.BiomeByName(name)
		if !ok {
			v.ok = false
		}
		v.col[qy<<4|qz<<2|qx] = id
	})
	sh.mu.Lock()
	if len(sh.m) >= biomeShardCap {
		clear(sh.m)
	}
	sh.m[k] = v
	sh.mu.Unlock()
	return v
}

func (b *biomeCache) column(cx, cz int32) []data.BiomeID { return b.get(cx, cz).col }

// fill stores c's biomes in the same write order as biome.FillWorldChunk.
func (b *biomeCache) fill(c *world.Chunk) error {
	if int32(c.Dim.MinY)>>2 != b.minQ || int32(c.Dim.Height)>>2 != b.quarts {
		return biome.FillWorldChunk(b.src, c)
	}
	v := b.get(c.X, c.Z)
	if !v.ok {
		return biome.FillWorldChunk(b.src, c)
	}
	for s := range b.quarts >> 2 {
		for x := range int32(4) {
			for y := range int32(4) {
				for z := range int32(4) {
					qy := s<<2 | y
					c.SetBiome(int(x)*4, c.Dim.MinY+int(qy)*4, int(z)*4, v.col[qy<<4|z<<2|x])
				}
			}
		}
	}
	return nil
}
