package feature

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// Pos is an absolute block position.
type Pos struct{ X, Y, Z int }

func (p Pos) Add(o Pos) Pos { return Pos{p.X + o.X, p.Y + o.Y, p.Z + o.Z} }

// region is the block access features run against: the pipeline's stage
// view, or a plain map in tests.
type region interface {
	Dim() world.Dimension
	Block(x, y, z int) data.StateID
	SetBlock(x, y, z int, s data.StateID) bool
	Height(h world.Heightmap, x, z int) int
	Biome(x, y, z int) (data.BiomeID, bool)
}

// Level is the WorldGenRegion view features run in: the decorated chunk and
// its neighbours within the features step radius.
type Level struct {
	rg region
	sc *gen.StageContext // nil outside the pipeline
	st *Stage
	wg map[world.ChunkPos]*[2][256]int16
}

func (lv *Level) MinY() int { return lv.rg.Dim().MinY }
func (lv *Level) MaxY() int { return lv.rg.Dim().MinY + lv.rg.Dim().Height }

func (lv *Level) Block(p Pos) data.StateID {
	if p.Y < lv.MinY() || p.Y >= lv.MaxY() {
		return data.Air
	}
	return lv.rg.Block(p.X, p.Y, p.Z)
}

func (lv *Level) SetBlock(p Pos, s data.StateID) bool { return lv.rg.SetBlock(p.X, p.Y, p.Z, s) }

// Height is WorldGenRegion.getHeight: the final heightmaps are live, the
// *_WG ones are frozen at the end of terrain.
func (lv *Level) Height(h world.Heightmap, x, z int) int {
	if lv.sc == nil {
		return lv.rg.Height(h, x, z)
	}
	slot := -1
	switch h {
	case world.WorldSurfaceWG:
		slot = 0
	case world.OceanFloorWG:
		slot = 1
	}
	if slot < 0 {
		return lv.sc.Height(h, x, z)
	}
	cp := world.ChunkPos{X: int32(x >> 4), Z: int32(z >> 4)}
	m, ok := lv.wg[cp]
	if !ok {
		m = lv.frozen(cp)
		lv.wg[cp] = m
	}
	if m == nil {
		return lv.sc.Height(h, x, z)
	}
	return int(m[slot][(z&15)<<4|x&15]) + lv.MinY()
}

// frozen decodes the *_WG heightmaps a terrain stage stored with
// gen.AnvilWGKey; nil when absent.
func (lv *Level) frozen(cp world.ChunkPos) *[2][256]int16 {
	pc, st, ok := lv.sc.Neighbour(cp.X, cp.Z)
	if !ok || st < gen.StatusTerrain && cp != lv.sc.Pos() {
		return nil
	}
	wg, _ := pc.Data[gen.AnvilWGKey].(nbt.Compound)
	if wg == nil {
		return nil
	}
	var out [2][256]int16
	bits := uint(world.HeightmapBits(lv.sc.Dim().Height))
	per := 64 / bits
	mask := uint64(1)<<bits - 1
	for slot, name := range []string{world.WorldSurfaceWG.String(), world.OceanFloorWG.String()} {
		l, _ := wg[name].([]int64)
		if len(l)*int(per) < 256 {
			return nil
		}
		for i := range 256 {
			out[slot][i] = int16(uint64(l[uint(i)/per]) >> (uint(i) % per * bits) & mask)
		}
	}
	return &out
}

// Biome is WorldGenRegion.getBiome: BiomeManager's fuzzy zoom over the
// stored quart biomes.
func (lv *Level) Biome(p Pos) data.BiomeID {
	qx, qy, qz := zoomQuart(lv.st.zoomSeed, int32(p.X), int32(p.Y), int32(p.Z))
	dim := lv.rg.Dim()
	qy = min(max(qy, int32(dim.MinY>>2)), int32((dim.MinY+dim.Height)>>2)-1)
	b, _ := lv.rg.Biome(int(qx)<<2, int(qy)<<2, int(qz)<<2)
	return b
}
