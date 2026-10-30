package feature

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

// TestEndSpikesForSeed compares endSpikesFor with
// EndSpikeFeature.getSpikesForLevel for the 10 seeds of
// testdata/end_spikes.txt (tools/refprobe kremenmc.probe.feat_nether.Spikes).
func TestEndSpikesForSeed(t *testing.T) {
	f, err := os.Open("testdata/end_spikes.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var seed int64
		var i int
		var s endSpikeDef
		if _, err := fmt.Sscan(sc.Text(), &seed, &i, &s.x, &s.z, &s.radius, &s.height, &s.guarded); err != nil {
			t.Fatal(err)
		}
		if got := endSpikesFor(seed)[i]; got != s {
			t.Errorf("seed %d spike %d: got %+v want %+v", seed, i, got, s)
		}
		n++
	}
	if n != 100 {
		t.Fatalf("%d spikes", n)
	}
}

// TestEndPlace replays testdata/end_place.txt (kremenmc.probe.feat_nether.Place):
// end_island and end_platform.
func TestEndPlace(t *testing.T) { replayNether(t, "testdata/end_place.txt") }
