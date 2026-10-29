package world

import (
	"math/bits"

	"github.com/levitskyyy/KremenMC/internal/data"
)

type Heightmap uint8

// Order matches the vanilla Heightmap.Types enum; the value is the network ID.
const (
	WorldSurfaceWG Heightmap = iota
	WorldSurface
	OceanFloorWG
	OceanFloor
	MotionBlocking
	MotionBlockingNoLeaves
)

// tracked heightmaps, indexed by position in this table
var heightmapKinds = [...]Heightmap{WorldSurface, OceanFloor, MotionBlocking, MotionBlockingNoLeaves}

const numHeightmaps = len(heightmapKinds)

// clientHeightmaps are sent in Chunk Data (WORLD_SURFACE, MOTION_BLOCKING, MOTION_BLOCKING_NO_LEAVES).
var clientHeightmaps = [...]int{0, 2, 3}

var heightmapNames = map[Heightmap]string{
	WorldSurfaceWG:         "WORLD_SURFACE_WG",
	WorldSurface:           "WORLD_SURFACE",
	OceanFloorWG:           "OCEAN_FLOOR_WG",
	OceanFloor:             "OCEAN_FLOOR",
	MotionBlocking:         "MOTION_BLOCKING",
	MotionBlockingNoLeaves: "MOTION_BLOCKING_NO_LEAVES",
}

func (h Heightmap) String() string { return heightmapNames[h] }

func heightmapSlot(h Heightmap) int {
	for i, k := range heightmapKinds {
		if k == h {
			return i
		}
	}
	return -1
}

// occupiedMask returns a bitmask over heightmapKinds slots for which st counts as occupied.
func occupiedMask(st data.StateID) uint8 {
	f := data.Flags(st)
	var m uint8
	if f&data.FlagAir == 0 {
		m |= 1
	}
	if f&data.FlagSolid != 0 {
		m |= 2
	}
	if f&(data.FlagSolid|data.FlagFluid) != 0 {
		m |= 4
		if f&data.FlagLeaves == 0 {
			m |= 8
		}
	}
	return m
}

// Height returns the first free y above the highest occupied block for the
// heightmap at local x,z, as an absolute coordinate (MinY when the column is empty).
func (c *Chunk) Height(h Heightmap, x, z int) int {
	s := heightmapSlot(h)
	if s < 0 {
		return c.Dim.MinY
	}
	return int(c.heights[s][z<<4|x]) + c.Dim.MinY
}

func (c *Chunk) updateHeights(x, y, z int, st data.StateID) {
	m := occupiedMask(st)
	col := z<<4 | x
	rel := uint16(y - c.Dim.MinY + 1)
	var rescan uint8
	for i := range heightmapKinds {
		h := &c.heights[i][col]
		if m&(1<<i) != 0 {
			if rel > *h {
				*h = rel
			}
		} else if rel == *h {
			rescan |= 1 << i
		}
	}
	if rescan == 0 {
		return
	}
	for yy := y - 1; yy >= c.Dim.MinY && rescan != 0; yy-- {
		om := occupiedMask(c.Sections[(yy-c.Dim.MinY)>>4].Block(x, yy&15, z))
		for i := range heightmapKinds {
			if rescan&(1<<i) != 0 && om&(1<<i) != 0 {
				c.heights[i][col] = uint16(yy - c.Dim.MinY + 1)
				rescan &^= 1 << i
			}
		}
	}
	for i := range heightmapKinds {
		if rescan&(1<<i) != 0 {
			c.heights[i][col] = 0
		}
	}
}

// RecalcHeightmaps rebuilds all tracked heightmaps from block data.
func (c *Chunk) RecalcHeightmaps() {
	const all = 1<<numHeightmaps - 1
	for col := range 256 {
		x, z := col&15, col>>4
		var done uint8
		for i := range heightmapKinds {
			c.heights[i][col] = 0
		}
		for si := len(c.Sections) - 1; si >= 0 && done != all; si-- {
			s := &c.Sections[si]
			if v, ok := s.Blocks.Single(); ok && data.IsAir(data.StateID(v)) {
				continue
			}
			for ly := 15; ly >= 0 && done != all; ly-- {
				om := occupiedMask(s.Block(x, ly, z)) &^ done
				if om == 0 {
					continue
				}
				rel := uint16(si<<4 + ly + 1)
				for i := range heightmapKinds {
					if om&(1<<i) != 0 {
						c.heights[i][col] = rel
					}
				}
				done |= om
			}
		}
	}
}

// HeightmapBits is ceil(log2(height+1)).
func HeightmapBits(height int) uint8 { return uint8(bits.Len(uint(height))) }

// PackHeightmap packs a tracked heightmap into longs for disk or network.
func (c *Chunk) PackHeightmap(dst []uint64, h Heightmap) []uint64 {
	s := heightmapSlot(h)
	b := HeightmapBits(c.Dim.Height)
	n := longsFor(256, b)
	dst = append(dst[:0], make([]uint64, n)...)
	if s < 0 {
		return dst
	}
	pl := 64 / int(b)
	for i, v := range c.heights[s] {
		dst[i/pl] |= uint64(v) << (uint(i%pl) * uint(b))
	}
	return dst
}
