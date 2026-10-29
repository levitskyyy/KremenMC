package chunkspec

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	spawn := &[2]int{100, -40}
	tests := []struct {
		spec  string
		spawn *[2]int
		n     int
		first Pos
		err   string
	}{
		{spec: "border", n: 32, first: Pos{-2, -2}},
		{spec: "corner:512,-512", n: 16, first: Pos{30, -34}},
		{spec: "corner:0,0,1", n: 4, first: Pos{-1, -1}},
		{spec: "far", n: 48, first: Pos{1873, 6248}},
		{spec: "spawn", spawn: spawn, n: 36, first: Pos{3, -6}},
		{spec: "spawn", err: "needs the world spawn"},
		{spec: "rect:0,0,2,1+chunk:1,1+chunk:5,5", n: 7, first: Pos{0, 0}},
		{spec: "all", spawn: spawn, n: 32 + 36 + 48, first: Pos{-2, -2}},
		{spec: "spiral:0,0,2", n: 25, first: Pos{0, 0}},
		{spec: "rect:2,0,1,0", err: "bad rectangle"},
		{spec: "nope", err: "unknown term"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := Parse(tt.spec, tt.spawn)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.n || got[0] != tt.first {
				t.Fatalf("got %d chunks starting %v, want %d starting %v", len(got), got[0], tt.n, tt.first)
			}
		})
	}
	if !NeedsSpawn("border+all") || NeedsSpawn("border+far") {
		t.Fatal("NeedsSpawn")
	}
}
