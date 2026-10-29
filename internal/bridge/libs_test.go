package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestEnsureLibs(t *testing.T) {
	body := []byte("jar bytes")
	sum := sha256.Sum256(body)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/g/a/1/a-1.jar":
			w.Write(body)
		case "/g/bad/1/bad-1.jar":
			w.Write([]byte("tampered"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := context.Background()
	good := Lib{Group: "g", Artifact: "a", Version: "1", Repo: srv.URL, SHA256: hex.EncodeToString(sum[:])}

	if err := EnsureLibs(ctx, srv.Client(), dir, []Lib{good}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a-1.jar")); string(got) != string(body) {
		t.Fatalf("content %q", got)
	}
	if err := EnsureLibs(ctx, srv.Client(), dir, []Lib{good}, nil); err != nil || hits.Load() != 1 {
		t.Fatalf("cached lib re-downloaded: hits=%d err=%v", hits.Load(), err)
	}

	os.WriteFile(filepath.Join(dir, "a-1.jar"), []byte("corrupt"), 0o644)
	if err := EnsureLibs(ctx, srv.Client(), dir, []Lib{good}, nil); err != nil || hits.Load() != 2 {
		t.Fatalf("corrupt lib not replaced: hits=%d err=%v", hits.Load(), err)
	}

	bad := Lib{Group: "g", Artifact: "bad", Version: "1", Repo: srv.URL, SHA256: hex.EncodeToString(sum[:])}
	if err := EnsureLibs(ctx, srv.Client(), dir, []Lib{bad}, nil); err == nil {
		t.Fatal("expected checksum error")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad-1.jar")); !os.IsNotExist(err) {
		t.Fatal("tampered file left behind")
	}
	unpinned := Lib{Group: "g", Artifact: "a", Version: "1", Repo: srv.URL}
	if err := DownloadVerified(ctx, srv.Client(), unpinned.URL(), filepath.Join(dir, "x.jar"), "", 1<<20); err == nil {
		t.Fatal("expected error for missing pin")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".dl-*"))
	if len(matches) != 0 {
		t.Fatalf("temp files left: %v", matches)
	}
}

func TestLibsArePinned(t *testing.T) {
	for _, l := range Libs {
		if len(l.SHA256) != 64 {
			t.Errorf("%s: missing sha256 pin", l.Coord())
		}
	}
}
