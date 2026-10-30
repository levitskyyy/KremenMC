package gen

import (
	"context"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world"
)

// WorldGenerator adapts a Pipeline to world.Generator. Each call runs the
// pipeline to StatusFull and copies the result into the world's chunk.
type WorldGenerator struct {
	P *Pipeline
}

func (g WorldGenerator) Generate(c *world.Chunk) {
	if err := g.GenerateContext(context.Background(), c); err != nil {
		g.P.cfg.Logf("gen: chunk %d,%d: %v", c.X, c.Z, err)
	}
}

// GenerateContext stops waiting when ctx ends; the work is abandoned if
// nobody else needs it.
func (g WorldGenerator) GenerateContext(ctx context.Context, c *world.Chunk) error {
	pc, err := g.P.Get(ctx, c.Pos(), StatusFull)
	if err != nil {
		return err
	}
	pc.promote(c, g.P)
	return nil
}

// GenerateStage stops at the named status ("biomes", "terrain", ...), for
// the reference bench. The copy is taken when the status is reached; below
// StatusInitializeLight a concurrent request for a neighbour's features may
// still be writing into the chunk, so callers must not mix stage and full
// requests on one pipeline.
func (g WorldGenerator) GenerateStage(c *world.Chunk, stage string) error {
	st, ok := ParseStatus(stage)
	if !ok {
		return fmt.Errorf("gen: unknown status %q", stage)
	}
	pc, err := g.P.Get(context.Background(), c.Pos(), st)
	if err != nil {
		return err
	}
	if st == StatusFull {
		pc.promote(c, g.P)
	} else {
		pc.Promote(c)
	}
	return nil
}
