package gen

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// BlockReader reads blocks at absolute coordinates.
type BlockReader interface {
	Block(x, y, z int) data.StateID
}

// PostProcessFunc returns the state the block st at x,y,z takes from its
// neighbours (vanilla post-processing on FULL: the shape update over every
// side, air when the block cannot stay). A mark whose update reads a chunk
// that is no longer in memory is left as placed.
type PostProcessFunc func(r BlockReader, x, y, z int, st data.StateID) data.StateID

// promoteView reads the promoted chunk for its own columns and the sealed
// proto-chunks around it for the rest. Sealed chunks are never written
// again, so the reads are race-free and independent of promotion order.
type promoteView struct {
	dst    *world.Chunk
	nb     [9]*ProtoChunk
	missed bool
}

func (v *promoteView) Block(x, y, z int) data.StateID {
	cx, cz := int32(x>>4), int32(z>>4)
	if cx == v.dst.X && cz == v.dst.Z {
		return v.dst.Block(x&15, y, z&15)
	}
	dx, dz := cx-v.dst.X, cz-v.dst.Z
	if dx < -1 || dx > 1 || dz < -1 || dz > 1 {
		v.missed = true
		return data.Air
	}
	pc := v.nb[(dz+1)*3+dx+1]
	if pc == nil {
		v.missed = true
		return data.Air
	}
	return pc.Chunk.Block(x&15, y, z&15)
}

// postProcess applies the configured shape update to every position c
// marked for post-processing, in section and mark order, writing into dst.
func (p *Pipeline) postProcess(c *ProtoChunk, dst *world.Chunk) {
	f := p.cfg.PostProcess
	if f == nil {
		return
	}
	v := &promoteView{dst: dst}
	loaded := false
	for si, l := range c.PostProcessing {
		y0 := c.Dim.MinY + si<<4
		for _, m := range l {
			lx, y, lz := int(m&15), y0+int(m>>4&15), int(m>>8&15)
			st := dst.Block(lx, y, lz)
			if st == data.Air {
				continue
			}
			if !loaded {
				loaded = true
				for dz := int32(-1); dz <= 1; dz++ {
					for dx := int32(-1); dx <= 1; dx++ {
						if dx != 0 || dz != 0 {
							if n := p.Peek(world.ChunkPos{X: c.X + dx, Z: c.Z + dz}); n != nil && n.Status() >= StatusInitializeLight {
								v.nb[(dz+1)*3+dx+1] = n
							}
						}
					}
				}
			}
			x, z := int(c.X)<<4|lx, int(c.Z)<<4|lz
			v.missed = false
			ns := f(v, x, y, z, st)
			if ns != st && !v.missed {
				dst.SetBlock(lx, y, lz, ns)
			}
		}
	}
}
