package surface

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
)

// SourceBiomes serves stored biomes computed from a biome source exactly as
// the BIOMES status fills them (whole chunks through biome.FillChunk), for
// use outside a generation pipeline. It caches filled chunks; it is not safe
// for concurrent use.
type SourceBiomes struct {
	src          biome.Source
	minQ, quarts int32
	reg          *data.Registry
	chunks       map[[2]int32][]data.BiomeID
	lastK        [2]int32
	last         []data.BiomeID
	fill         func(cx, cz int32) []data.BiomeID
}

// NewSharedSourceBiomes serves biome columns from fill, typically a cache
// shared between chunks. fill must return exactly what NewSourceBiomes would
// compute: col[qy<<4|qz<<2|qx], qy relative to the dimension's bottom.
func NewSharedSourceBiomes(minY int32, fill func(cx, cz int32) []data.BiomeID) *SourceBiomes {
	return &SourceBiomes{minQ: minY >> 2, fill: fill, chunks: map[[2]int32][]data.BiomeID{}}
}

// NewSourceBiomes serves biomes of src for a dimension of the given height.
func NewSourceBiomes(src biome.Source, minY, height int32) *SourceBiomes {
	return &SourceBiomes{src: src, minQ: minY >> 2, quarts: height >> 2, reg: data.Canonical(), chunks: map[[2]int32][]data.BiomeID{}}
}

func (b *SourceBiomes) chunk(cx, cz int32) []data.BiomeID {
	k := [2]int32{cx, cz}
	if b.last != nil && b.lastK == k {
		return b.last
	}
	col, ok := b.chunks[k]
	if !ok && b.fill != nil {
		col = b.fill(cx, cz)
		if len(b.chunks) > 256 {
			clear(b.chunks)
		}
		b.chunks[k] = col
	} else if !ok {
		col = make([]data.BiomeID, b.quarts*16)
		biome.FillChunk(b.src, cx, cz, b.minQ>>2, b.quarts>>2, func(qx, qy, qz int32, name string) {
			id, _ := b.reg.BiomeByName(name)
			col[qy<<4|qz<<2|qx] = id
		})
		if len(b.chunks) > 256 {
			clear(b.chunks)
		}
		b.chunks[k] = col
	}
	b.lastK, b.last = k, col
	return col
}

func (b *SourceBiomes) QuartBiome(qx, qy, qz int32) data.BiomeID {
	col := b.chunk(qx>>2, qz>>2)
	return col[(qy-b.minQ)<<4|(qz&3)<<2|qx&3]
}

// BlockBiomes reads stored biomes by block position, e.g. a generation
// pipeline's stage view (gen.StageContext) covering the chunk's neighbours.
type BlockBiomes interface {
	Biome(x, y, z int) (data.BiomeID, bool)
}

// RegionBiomes adapts a BlockBiomes view.
func RegionBiomes(v BlockBiomes) Biomes { return regionBiomes{v} }

type regionBiomes struct{ v BlockBiomes }

func (r regionBiomes) QuartBiome(qx, qy, qz int32) data.BiomeID {
	id, _ := r.v.Biome(int(qx)<<2, int(qy)<<2, int(qz)<<2)
	return id
}
