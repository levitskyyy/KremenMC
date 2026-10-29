package structure

import (
	"slices"
	"testing"
)

// TestJigsawStartDraws checks the start draws read from the reference draw
// log of the plains village at chunk -13,-28 (seed 8266389061079214669):
// rotation nextInt(4) = 0, element nextInt(204) = 83 (plains_meeting_point_1),
// then the start piece's 11 jigsaws shuffled into the reference junction
// order.
func TestJigsawStartDraws(t *testing.T) {
	r := largeFeature(8266389061079214669, -13, -28)
	if rot, el := r.NextIntn(4), r.NextIntn(204); rot != 0 || el != 83 {
		t.Fatalf("rot %d el %d", rot, el)
	}
	a := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i := len(a) - 1; i > 0; i-- {
		k := r.NextIntn(int32(i + 1))
		a[i], a[k] = a[k], a[i]
	}
	if want := []int{9, 3, 5, 0, 2, 6, 8, 7, 1, 4, 10}; !slices.Equal(a, want) {
		t.Fatalf("jigsaw order %v, want %v", a, want)
	}
}
