package feature

import "github.com/levitskyyy/KremenMC/internal/world/gen"

// presentBiomes are the biomes whose features a chunk runs: those of the
// chunk and its eight neighbours (kremenmc.probe.feat_region.Region: a chunk
// runs the features of a biome found only in a diagonal neighbour).
func (s *Stage) presentBiomes(sc *gen.StageContext) []bool {
	present := make([]bool, len(s.inStep))
	var vals [64]uint32
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			pc, _, ok := sc.Neighbour(sc.Pos().X+dx, sc.Pos().Z+dz)
			if !ok || pc == nil {
				continue
			}
			for i := range pc.Sections {
				for _, v := range pc.Sections[i].Biomes.Values(vals[:]) {
					if int(v) < len(present) {
						present[v] = true
					}
				}
			}
		}
	}
	return present
}
