package vanilla

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Block is the scratch noise chunk's block at x,y,z (structure starts that
// look at the noise column).
func (p *structureProbe) Block(x, y, z int32) data.StateID {
	c := p.chunk(world.ChunkPos{X: x >> 4, Z: z >> 4})
	if c == nil {
		return 0
	}
	return c.Block(int(x&15), int(y), int(z&15))
}
