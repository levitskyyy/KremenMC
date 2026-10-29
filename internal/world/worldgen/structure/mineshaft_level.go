package structure

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// ZoomedBiome is WorldGenRegion.getBiome: BiomeManager's fuzzy zoom over
// the stored quart biomes (what a structure piece reads).
func (l *stageLevel) ZoomedBiome(x, y, z int32) data.BiomeID {
	qx, qy, qz := surface.ZoomQuart(surface.ObfuscateSeed(l.sc.Seed()), x, y, z)
	dim := l.sc.Dim()
	qy = min(max(qy, int32(dim.MinY>>2)), int32((dim.MinY+dim.Height)>>2)-1)
	b, _ := l.sc.Biome(int(qx)<<2, int(qy)<<2, int(qz)<<2)
	return b
}

// RemoveBlockEntity drops the block entity at x,y,z of the decorated chunk
// (a spawner overwritten by a later mineshaft write).
func (l *stageLevel) RemoveBlockEntity(x, y, z int32) {
	if x>>4 == l.sc.Pos().X && z>>4 == l.sc.Pos().Z {
		l.sc.Chunk().Chunk.RemoveBlockEntity(int(x&15), int(y), int(z&15))
	}
}

// msBlocking is the biome tag minecraft:mineshaft_blocking: a piece whose
// box centre lies in one of its biomes places nothing (MineScene liq3 with
// every biome read answered as deep_dark: no draws, no writes).
var msBlocking = sync.OnceValue(func() map[data.BiomeID]bool {
	out := map[data.BiomeID]bool{}
	pk, err := wgdata.Load()
	if err != nil {
		return out
	}
	names, _ := pk.Tag("worldgen/biome", "mineshaft_blocking")
	reg := data.Canonical()
	for _, n := range names {
		if id, ok := reg.BiomeByName(trimNS(n)); ok {
			out[id] = true
		}
	}
	return out
})
