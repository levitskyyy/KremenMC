package feature

import "testing"

// TestNetherMore replays testdata/nether_more.txt (kremenmc.probe.feat_nether.Place,
// flat/pits/ceil/end scenes): delta, small and large basalt columns,
// basalt_pillar, projected_random_patchy_square, single_block_pillar and
// chorus_plant.
func TestNetherMore(t *testing.T) { replayNether(t, "testdata/nether_more.txt") }

// TestTreeLeafTicks replays testdata/tree_leaf_ticks.txt: oak, birch and
// fancy oak on grass with and without stone obstacles, blocks and the
// scheduled leaf ticks (the seventh case, fancy oak seed 6002 among stone,
// pins the 5-block free column above a branch cluster).
func TestTreeLeafTicks(t *testing.T) { replayNether(t, "testdata/tree_leaf_ticks.txt") }
