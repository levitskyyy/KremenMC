package translate

import "testing"

func TestNoLayoutGaps(t *testing.T) {
	for _, p := range Default().Supported() {
		tg, err := getTarget(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range tg.gaps {
			t.Errorf("%d: %s", p, g)
		}
	}
}
