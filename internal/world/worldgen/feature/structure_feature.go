package feature

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// StructureFeatures places the placed features of jigsaw
// feature_pool_elements (village trees, hay, flowers). The features are
// parsed up front, so placement never parses concurrently.
type StructureFeatures struct {
	st *Stage
	m  map[string]*Placed
}

// NewStructureFeatures parses the placed features keys for st.
func NewStructureFeatures(st *Stage, keys []string) (*StructureFeatures, error) {
	rg, err := theRegistry()
	if err != nil {
		return nil, fmt.Errorf("feature: %w", err)
	}
	f := &StructureFeatures{st: st, m: map[string]*Placed{}}
	for _, k := range keys {
		pf, err := rg.placedByKey(k)
		if err != nil {
			return nil, fmt.Errorf("feature: structure feature: %w", err)
		}
		f.m[k] = pf
	}
	return f, nil
}

// Place places key at x,y,z in the chunk sc decorates, with r, as a
// feature placed by a structure piece (no biome check).
func (f *StructureFeatures) Place(sc *gen.StageContext, key string, r *Random, x, y, z int) bool {
	pf := f.m[key]
	if pf == nil {
		return false
	}
	lv := &Level{rg: sc, sc: sc, st: f.st, wg: map[world.ChunkPos]*[2][256]int16{}}
	return pf.place(lv, r, Pos{x, y, z}, nil)
}
