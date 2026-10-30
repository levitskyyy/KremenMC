package vanilla

import (
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

// structureFeatures adapts feature.StructureFeatures to structure.FeaturePlacer.
type structureFeatures struct{ f *feature.StructureFeatures }

func (s structureFeatures) PlaceFeature(sc *gen.StageContext, key string, r *rng.WorldgenRandom, x, y, z int32) bool {
	return s.f.Place(sc, key, r, int(x), int(y), int(z))
}

// Stages returns the gen.Pipeline stages of a vanilla dimension: biomes and
// terrain from t, then the features status. Statuses without a stage pass
// through.
func (t *Terrain) Stages(dim string, seed int64) ([]gen.Stage, error) {
	fs, err := feature.NewStage(dim, seed)
	if err != nil {
		return nil, err
	}
	ss, err := t.structureStages(dim, seed)
	if err != nil {
		return nil, err
	}
	var pre []gen.Stage
	if ss != nil {
		fs.StructureStep = ss.PlaceStep
		pre = []gen.Stage{ss.StartsStage(), ss.ReferencesStage()}
		keys, err := structure.PoolFeatures()
		if err != nil {
			return nil, err
		}
		sf, err := feature.NewStructureFeatures(fs, keys)
		if err != nil {
			return nil, err
		}
		ss.Features = structureFeatures{sf}
	}
	return append(pre,
		gen.StageFunc{S: gen.StatusBiomes, F: func(ctx *gen.StageContext) error {
			return t.GenerateStage(ctx.Chunk().Chunk, "biomes")
		}},
		gen.StageFunc{S: gen.StatusTerrain, F: func(ctx *gen.StageContext) error {
			pc := ctx.Chunk()
			var beard func(x, y, z int32) float32
			if ss != nil {
				if b := ss.BeardFor(ctx); b != nil {
					beard = b.Value
				}
			}
			if err := t.GenerateStageMarked(pc.Chunk, "terrain", beard, pc.MarkPostProcess); err != nil {
				return err
			}
			if pc.Data == nil {
				pc.Data = nbt.Compound{}
			}
			pc.Data[gen.AnvilWGKey] = anvil.PackWG(pc.Chunk)
			return nil
		}},
		fs,
	), nil
}
