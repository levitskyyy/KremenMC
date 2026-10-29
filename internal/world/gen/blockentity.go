package gen

import (
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// SetBlockEntity stores block entity NBT (without id/x/y/z) at absolute
// coordinates, with the same write-zone rules as SetBlock.
func (c *StageContext) SetBlockEntity(x, y, z int, id string, d nbt.Compound) bool {
	if !c.InWriteZone(x, z) || !c.p.cfg.Dim.Contains(y) {
		return false
	}
	i, _, _ := c.idx(int32(x>>4), int32(z>>4))
	c.grid[i].Chunk.SetBlockEntity(world.BlockEntity{
		X: uint8(x & 15), Y: int32(y), Z: uint8(z & 15), NetType: world.BlockEntityNetType(id), ID: id, Data: d,
	})
	return true
}
