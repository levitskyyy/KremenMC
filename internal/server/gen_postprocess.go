package server

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// GenPostProcess is the generation pipeline's post-processing hook: a
// marked block takes the shape its neighbours give it, as after a
// neighbour update.
func GenPostProcess(r gen.BlockReader, x, y, z int, st data.StateID) data.StateID {
	return shapeAt(r, x, y, z, st)
}
