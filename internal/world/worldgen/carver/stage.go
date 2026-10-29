package carver

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/biome"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
)

// Stage carves chunks in the pipeline's terrain status (after the noise
// fill and surface, which an earlier stage of the same status must have
// written). Biomes of source chunks come from the biome source, as in the
// reference, so no neighbour data is read.
type Stage struct {
	Set    *Set
	Biomes biome.Source
	// NewAquifer returns the aquifer for a chunk; nil carves everything
	// to air.
	NewAquifer func(cx, cz int32) Aquifer

	pool sync.Pool
}

type stageState struct {
	mask *Mask
	dctx density.Ctx
}

func (s *Stage) Status() gen.Status { return gen.StatusTerrain }

func (s *Stage) Run(ctx *gen.StageContext) error {
	st, _ := s.pool.Get().(*stageState)
	if st == nil {
		st = &stageState{mask: NewMask(s.Set.Ctx.MinY, s.Set.Ctx.Height)}
	}
	defer s.pool.Put(st)
	st.mask.Reset()
	p := ctx.Pos()
	s.Set.CarveChunk(p.X, p.Z, func(qx, qz int32) string { return s.Biomes.Biome(&st.dctx, nil, qx, 0, qz) }, st.mask)
	var aq Aquifer = airAquifer{}
	if s.NewAquifer != nil {
		aq = s.NewAquifer(p.X, p.Z)
	}
	st.mask.Apply(p.X, p.Z, aq, ctx.Chunk().Chunk)
	return nil
}

type airAquifer struct{}

func (airAquifer) Substance(int, int, int) (data.StateID, bool) { return data.Air, true }
