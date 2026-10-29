package feature

import "testing"

// TestEndStage checks the the_end section of order.txt resolves.
func TestEndStage(t *testing.T) {
	if _, err := NewStage("the_end", 0); err != nil {
		t.Fatal(err)
	}
}
