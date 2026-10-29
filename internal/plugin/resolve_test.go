package plugin

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func man(id string, opts ...func(*Manifest)) *Manifest {
	m := &Manifest{ID: id, Version: "1", APIVersion: "1"}
	for _, o := range opts {
		o(m)
	}
	return m
}

func dep(ids ...string) func(*Manifest)    { return func(m *Manifest) { m.Depends = ids } }
func soft(ids ...string) func(*Manifest)   { return func(m *Manifest) { m.SoftDepends = ids } }
func before(ids ...string) func(*Manifest) { return func(m *Manifest) { m.LoadBefore = ids } }

func TestResolve(t *testing.T) {
	tests := []struct {
		name      string
		in        []*Manifest
		order     []string
		failed    map[string]error
		cycleText string
	}{
		{
			name:  "independent sorted by id",
			in:    []*Manifest{man("c"), man("a"), man("b")},
			order: []string{"a", "b", "c"},
		},
		{
			name:  "hard chain",
			in:    []*Manifest{man("a", dep("b")), man("b", dep("c")), man("c")},
			order: []string{"c", "b", "a"},
		},
		{
			name:  "softdepend orders when present and is ignored when absent",
			in:    []*Manifest{man("a", soft("z", "missing")), man("z")},
			order: []string{"z", "a"},
		},
		{
			name:  "loadbefore",
			in:    []*Manifest{man("a"), man("z", before("a"))},
			order: []string{"z", "a"},
		},
		{
			name:   "missing hard dependency fails transitively",
			in:     []*Manifest{man("a", dep("b")), man("b", dep("nope")), man("c")},
			order:  []string{"c"},
			failed: map[string]error{"a": ErrMissingDependency, "b": ErrMissingDependency},
		},
		{
			name:      "hard cycle fails members and dependents",
			in:        []*Manifest{man("a", dep("b")), man("b", dep("a")), man("c", dep("a")), man("d")},
			order:     []string{"d"},
			failed:    map[string]error{"a": ErrDependencyCycle, "b": ErrDependencyCycle, "c": ErrMissingDependency},
			cycleText: "a -> b -> a",
		},
		{
			name:  "soft cycle is broken deterministically",
			in:    []*Manifest{man("a", soft("b")), man("b", soft("a"))},
			order: []string{"a", "b"},
		},
		{
			name:  "mixed cycle keeps hard edge",
			in:    []*Manifest{man("a", dep("b")), man("b", soft("a"))},
			order: []string{"b", "a"},
		},
		{
			name:   "duplicate ids",
			in:     []*Manifest{man("a"), man("a"), man("b")},
			order:  []string{"b"},
			failed: map[string]error{"a": ErrDuplicateID},
		},
		{
			name:  "diamond",
			in:    []*Manifest{man("top", dep("l", "r")), man("l", dep("base")), man("r", dep("base")), man("base")},
			order: []string{"base", "l", "r", "top"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Resolve(tt.in)
			if !slices.Equal(res.Order, tt.order) {
				t.Errorf("order = %v, want %v", res.Order, tt.order)
			}
			if len(res.Failed) != len(tt.failed) {
				t.Errorf("failed = %v, want %v", res.Failed, tt.failed)
			}
			for id, want := range tt.failed {
				if !errors.Is(res.Failed[id], want) {
					t.Errorf("failed[%s] = %v, want %v", id, res.Failed[id], want)
				}
			}
			if tt.cycleText != "" && !strings.Contains(res.Failed["a"].Error(), tt.cycleText) {
				t.Errorf("cycle error %q lacks %q", res.Failed["a"], tt.cycleText)
			}
		})
	}
}

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"valid", `id = "x"
version = "1"
api-version = "1"
depends = ["y"]
[permissions."x.use"]
default = "op"
[commands.x]
aliases = ["xx"]
[limits]
memory-mb = 32`, ""},
		{"bad id", `id = "X Y"
version = "1"
api-version = "1"`, "invalid id"},
		{"missing version", `id = "x"
api-version = "1"`, "version is required"},
		{"unsupported api", `id = "x"
version = "1"
api-version = "99"`, "unsupported api-version"},
		{"unknown key", `id = "x"
version = "1"
api-version = "1"
mian = "typo.wasm"`, "mian"},
		{"self dependency", `id = "x"
version = "1"
api-version = "1"
depends = ["x"]`, "depends on itself"},
		{"bad permission default", `id = "x"
version = "1"
api-version = "1"
[permissions.p]
default = "sometimes"`, "invalid default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseManifest([]byte(tt.in))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if m.Name != m.ID || m.Limits.memoryPages(128) != 32*16 || len(m.APIPermissions()) != 1 {
					t.Errorf("unexpected manifest %+v", m)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
