package server

import (
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestHasPermissionOr(t *testing.T) {
	perms := newPermissions()
	if err := perms.Register(api.Permission{Name: "go.known", Default: api.DefaultFalse}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		perm string
		def  api.PermissionDefault
		op   bool
		want bool
	}{
		{"unknown uses fallback true", "java.chat", api.DefaultTrue, false, true},
		{"unknown uses fallback op for non-op", "java.admin", api.DefaultOp, false, false},
		{"unknown uses fallback op for op", "java.admin", api.DefaultOp, true, true},
		{"unknown uses fallback notop", "java.guest", api.DefaultNotOp, true, false},
		{"registered default wins over fallback", "go.known", api.DefaultTrue, true, false},
		{"empty permission is always granted", "", api.DefaultFalse, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &permissible{perms: perms, op: func() bool { return tc.op }}
			if got := p.hasPermissionOr(tc.perm, tc.def); got != tc.want {
				t.Fatalf("hasPermissionOr(%q, %v) = %v, want %v", tc.perm, tc.def, got, tc.want)
			}
		})
	}
}
