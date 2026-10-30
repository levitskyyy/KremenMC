package feature

import "testing"

// TestFallenTreeGround replays testdata/trees_fallen_ground.txt
// (kremenmc.probe.feat_nether.Place): fallen oaks on a floor with one-block
// steps and fallen birches among stone, where the log row has to find a
// resting place and at least half of it must rest on something.
func TestFallenTreeGround(t *testing.T) { replayNether(t, "testdata/trees_fallen_ground.txt") }
