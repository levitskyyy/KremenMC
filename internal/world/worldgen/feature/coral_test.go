package feature

import "testing"

// TestFeatOceanPlace replays testdata/feat_ocean_place.txt (tools/refprobe
// kremenmc.probe.feat_ocean.Place): coral shapes and decoration.
func TestFeatOceanPlace(t *testing.T) { replayPlace(t, "testdata/feat_ocean_place.txt") }
