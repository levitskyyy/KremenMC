package entity

import "testing"

func TestBoxAt(t *testing.T) {
	got := BoxAt(1, 64, -2, 0.5, 1.5)
	want := AABB{0.75, 64, -2.25, 1.25, 65.5, -1.75}
	if got != want {
		t.Fatalf("BoxAt = %+v, want %+v", got, want)
	}
}

func TestAABBTransforms(t *testing.T) {
	a := AABB{0, 0, 0, 1, 2, 3}
	tests := []struct {
		name string
		got  AABB
		want AABB
	}{
		{"offset", a.Offset(1, -1, 0.5), AABB{1, -1, 0.5, 2, 1, 3.5}},
		{"grow", a.Grow(0.5, 1, 0), AABB{-0.5, -1, 0, 1.5, 3, 3}},
		{"shrink", a.Grow(-0.25, 0, 0), AABB{0.25, 0, 0, 0.75, 2, 3}},
		{"stretch positive", a.Stretch(2, 1, 0), AABB{0, 0, 0, 3, 3, 3}},
		{"stretch negative", a.Stretch(-2, -1, -3), AABB{-2, -1, -3, 1, 2, 3}},
		{"stretch mixed", a.Stretch(1, 0, -1), AABB{0, 0, -1, 2, 2, 3}},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %+v, want %+v", tt.name, tt.got, tt.want)
		}
	}
}

func TestIntersects(t *testing.T) {
	a := AABB{0, 0, 0, 1, 1, 1}
	tests := []struct {
		name string
		o    AABB
		want bool
	}{
		{"same", a, true},
		{"overlap corner", AABB{0.5, 0.5, 0.5, 2, 2, 2}, true},
		{"contained", AABB{0.25, 0.25, 0.25, 0.75, 0.75, 0.75}, true},
		{"touch x face", AABB{1, 0, 0, 2, 1, 1}, false},
		{"touch y face", AABB{0, 1, 0, 1, 2, 1}, false},
		{"touch z face", AABB{0, 0, -1, 1, 1, 0}, false},
		{"apart", AABB{3, 3, 3, 4, 4, 4}, false},
		{"overlap x only", AABB{0.5, 2, 0, 1.5, 3, 1}, false},
	}
	for _, tt := range tests {
		if got := a.Intersects(tt.o); got != tt.want {
			t.Errorf("%s: Intersects = %v, want %v", tt.name, got, tt.want)
		}
		if got := tt.o.Intersects(a); got != tt.want {
			t.Errorf("%s: reverse Intersects = %v, want %v", tt.name, got, tt.want)
		}
	}
}
