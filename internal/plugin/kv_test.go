package plugin

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileKV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "kv.bin")
	kv, err := OpenFileKV(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		op      string
		key     string
		val     string
		wantErr error
	}{
		{"set", "a/1", "x", nil},
		{"set", "a/2", "yy", nil},
		{"set", "b", "zzz", nil},
		{"set", "a/1", "replaced", nil},
		{"del", "b", "", nil},
		{"set", "big", string(make([]byte, 100)), ErrKVFull},
	}
	for _, s := range steps {
		switch s.op {
		case "set":
			err = kv.Set(s.key, []byte(s.val))
		case "del":
			err = kv.Delete(s.key)
		}
		if !errors.Is(err, s.wantErr) {
			t.Fatalf("%s %s: err = %v, want %v", s.op, s.key, err, s.wantErr)
		}
	}
	re, err := OpenFileKV(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := re.Get("a/1"); !ok || string(v) != "replaced" {
		t.Errorf("a/1 = %q %v", v, ok)
	}
	if _, ok, _ := re.Get("b"); ok {
		t.Error("deleted key persisted")
	}
	if keys, _ := re.Keys("a/"); !slices.Equal(keys, []string{"a/1", "a/2"}) {
		t.Errorf("keys = %v", keys)
	}
}
