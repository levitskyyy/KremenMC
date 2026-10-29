package net

import (
	"testing"
)

func TestServerHash(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Notch", "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48"},
		{"jeb_", "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1"},
		{"simon", "88e16a1019277b15d58faf0541e11910eb756f6"},
	} {
		if got := serverHash([]byte(tc.in), nil); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.in, got, tc.want)
		}
	}
}

func TestOfflineUUID(t *testing.T) {
	u := OfflineUUID("Notch")
	if got := uuidString(u); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("got %s", got)
	}
}

func TestValidName(t *testing.T) {
	for name, ok := range map[string]bool{"Notch": true, "a_b_9": true, "": false, "seventeen_chars__": false, "bad name": false, "ünï": false} {
		if validName(name) != ok {
			t.Errorf("%q: want %v", name, ok)
		}
	}
}
