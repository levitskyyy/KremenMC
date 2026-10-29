package plugin

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func sum256(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func sum512(b []byte) string { h := sha512.Sum512(b); return hex.EncodeToString(h[:]) }

type fakeRepos struct {
	srv      *httptest.Server
	requests atomic.Int32
	files    map[string][]byte
	queries  []string
}

func newFakeRepos(t *testing.T) *fakeRepos {
	r := &fakeRepos{files: map[string][]byte{
		"/files/lp-new.jar":  []byte("luckperms new"),
		"/files/lp-old.jar":  []byte("luckperms old"),
		"/files/lp-beta.jar": []byte("luckperms beta"),
		"/files/via.jar":     []byte("viaversion"),
		"/files/tool.wasm":   []byte("\x00asm wasm plugin"),
		"/files/evil.jar":    []byte("tampered"),
	}}
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	file := func(name string, primary bool, hashOf string) map[string]any {
		return map[string]any{
			"url":      r.srv.URL + "/files/" + name,
			"filename": name,
			"primary":  primary,
			"hashes":   map[string]string{"sha512": sum512(r.files["/files/"+hashOf])},
		}
	}
	mux.HandleFunc("GET /modrinth/project/luckperms/version", func(w http.ResponseWriter, req *http.Request) {
		r.queries = append(r.queries, req.URL.RawQuery)
		if req.Header.Get("User-Agent") == "" {
			http.Error(w, "ua required", http.StatusBadRequest)
			return
		}
		writeJSON(w, []map[string]any{
			{"id": "b", "version_number": "5.4.0-beta", "version_type": "beta", "date_published": "2026-10-01T00:00:00Z",
				"loaders": []string{"paper"}, "game_versions": []string{"26.3"}, "files": []any{file("lp-beta.jar", true, "lp-beta.jar")}},
			{"id": "o", "version_number": "5.3.0", "version_type": "release", "date_published": "2026-01-01T00:00:00Z",
				"loaders": []string{"paper"}, "game_versions": []string{"26.3"}, "files": []any{file("lp-old.jar", true, "lp-old.jar")}},
			{"id": "n", "version_number": "5.4.0", "version_type": "release", "date_published": "2026-09-01T00:00:00Z",
				"loaders": []string{"bukkit", "paper"}, "game_versions": []string{"26.1", "26.3"}, "files": []any{file("sources.zip", false, "lp-new.jar"), file("lp-new.jar", true, "lp-new.jar")}},
			{"id": "f", "version_number": "5.4.0-fabric", "version_type": "release", "date_published": "2026-10-02T00:00:00Z",
				"loaders": []string{"fabric"}, "game_versions": []string{"26.3"}, "files": []any{file("lp-beta.jar", true, "lp-beta.jar")}},
		})
	})
	mux.HandleFunc("GET /modrinth/project/luckperms/version/5.3.0", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, map[string]any{"id": "o", "version_number": "5.3.0", "version_type": "release",
			"loaders": []string{"paper"}, "game_versions": []string{"26.1"}, "files": []any{file("lp-old.jar", true, "lp-old.jar")}})
	})
	mux.HandleFunc("GET /modrinth/project/tampered/version", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, []map[string]any{{"id": "x", "version_number": "1", "version_type": "release",
			"loaders": []string{"paper"}, "game_versions": []string{"26.3"}, "files": []any{file("evil.jar", true, "lp-new.jar")}}})
	})
	mux.HandleFunc("GET /hangar/projects/ViaVersion", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, map[string]any{"namespace": map[string]string{"owner": "ViaVersion", "slug": "ViaVersion"}})
	})
	mux.HandleFunc("GET /hangar/projects/ViaVersion/versions", func(w http.ResponseWriter, req *http.Request) {
		r.queries = append(r.queries, req.URL.RawQuery)
		dl := func(name string) map[string]any {
			return map[string]any{"PAPER": map[string]any{
				"fileInfo":    map[string]any{"name": name, "sha256Hash": sum256(r.files["/files/"+name])},
				"downloadUrl": r.srv.URL + "/files/" + name,
			}}
		}
		writeJSON(w, map[string]any{"result": []any{
			map[string]any{"name": "5.2.0-SNAPSHOT", "channel": map[string]string{"name": "Snapshot"}, "downloads": dl("lp-beta.jar")},
			map[string]any{"name": "5.1.0", "channel": map[string]string{"name": "Release"}, "downloads": dl("via.jar")},
		}})
	})
	mux.HandleFunc("GET /files/", func(w http.ResponseWriter, req *http.Request) {
		b, ok := r.files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(b)
	})
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests.Add(1)
		mux.ServeHTTP(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRepos) fetcher(dir string) *Fetcher {
	return &Fetcher{
		Client:      r.srv.Client(),
		ModrinthAPI: r.srv.URL + "/modrinth",
		HangarAPI:   r.srv.URL + "/hangar",
		GameVersion: "26.3",
		Dir:         filepath.Join(dir, "plugins"),
		LockPath:    filepath.Join(dir, "plugins.lock"),
		Log:         quietLogger(),
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		in   string
		want Source
		err  bool
	}{
		{in: "modrinth:luckperms", want: Source{Kind: "modrinth", Slug: "luckperms"}},
		{in: "modrinth:luckperms@5.4.0", want: Source{Kind: "modrinth", Slug: "luckperms", Version: "5.4.0"}},
		{in: "hangar:ViaVersion/ViaVersion@5.1.0", want: Source{Kind: "hangar", Owner: "ViaVersion", Slug: "ViaVersion", Version: "5.1.0"}},
		{in: "url:https://x.example/a.wasm", want: Source{Kind: "url", URL: "https://x.example/a.wasm"}},
		{in: "hangar:noslug", err: true},
		{in: "modrinth:../etc", err: true},
		{in: "modrinth:a@1/2", err: true},
		{in: "ftp:x", err: true},
		{in: "url:notaurl", err: true},
		{in: "luckperms", err: true},
	}
	for _, tt := range tests {
		got, err := ParseSource(tt.in)
		if (err != nil) != tt.err || err == nil && got != tt.want {
			t.Errorf("ParseSource(%q) = %+v, %v", tt.in, got, err)
		}
	}
}

func TestFetchSync(t *testing.T) {
	repos := newFakeRepos(t)
	dir := t.TempDir()
	f := repos.fetcher(dir)
	wasm := repos.files["/files/tool.wasm"]
	spec := &FetchSpec{Plugins: []Want{
		{Source: "modrinth:luckperms"},
		{Source: "hangar:ViaVersion/ViaVersion"},
		{Source: "url:" + repos.srv.URL + "/files/tool.wasm", SHA256: sum256(wasm)},
	}}
	res, err := f.Sync(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	wantJars := []string{filepath.Join(f.Dir, "lp-new.jar"), filepath.Join(f.Dir, "via.jar")}
	if !slices.Equal(res.Jars, wantJars) || !slices.Equal(res.Wasm, []string{filepath.Join(f.Dir, "tool.wasm")}) {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Fetched) != 3 {
		t.Errorf("fetched = %v", res.Fetched)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "lp-new.jar")); string(b) != "luckperms new" {
		t.Errorf("modrinth picked wrong file: %q", b)
	}
	for _, q := range repos.queries {
		if strings.Contains(q, "loaders") && (!strings.Contains(q, "26.3") || !strings.Contains(q, "paper")) {
			t.Errorf("modrinth query lacks filters: %s", q)
		}
		if strings.Contains(q, "platform=") && !strings.Contains(q, "platformVersion=26.3") {
			t.Errorf("hangar query lacks filters: %s", q)
		}
	}
	lock, err := LoadLockfile(f.LockPath)
	if err != nil || len(lock.Plugins) != 3 {
		t.Fatalf("lock = %+v, %v", lock, err)
	}
	for _, e := range lock.Plugins {
		if e.SHA256 == "" || e.SHA512 == "" || e.Version == "" {
			t.Errorf("incomplete lock entry %+v", e)
		}
	}

	before := repos.requests.Load()
	res, err = f.Sync(context.Background(), spec)
	if err != nil || len(res.Fetched) != 0 || repos.requests.Load() != before {
		t.Fatalf("second sync should use the lock: fetched=%v requests=%d->%d err=%v", res.Fetched, before, repos.requests.Load(), err)
	}

	os.WriteFile(filepath.Join(f.Dir, "via.jar"), []byte("corrupted"), 0o644)
	res, err = f.Sync(context.Background(), spec)
	if err != nil || !slices.Equal(res.Fetched, []string{"hangar:ViaVersion/ViaVersion"}) {
		t.Fatalf("corrupted file not refetched: %+v %v", res, err)
	}

	spec.Plugins = []Want{{Source: "modrinth:luckperms@5.3.0"}}
	res, err = f.Sync(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(res.Removed)
	if !slices.Equal(res.Removed, []string{"lp-new.jar", "tool.wasm", "via.jar"}) {
		t.Errorf("removed = %v", res.Removed)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "lp-old.jar")); err != nil {
		t.Error("pinned version not downloaded")
	}
}

func TestFetchVerification(t *testing.T) {
	repos := newFakeRepos(t)
	tests := []struct {
		name    string
		want    Want
		wantErr error
		errText string
	}{
		{name: "api hash mismatch", want: Want{Source: "modrinth:tampered"}, wantErr: ErrHashMismatch},
		{name: "pinned hash mismatch", want: Want{Source: "modrinth:luckperms", SHA256: sum256([]byte("other"))}, wantErr: ErrHashMismatch},
		{name: "url without hash", want: Want{Source: "url:" + repos.srv.URL + "/files/tool.wasm"}, errText: "require sha256"},
		{name: "plain http url", want: Want{Source: "url:http://example.com/a.jar", SHA256: "00"}, errText: "https"},
		{name: "hangar owner mismatch", want: Want{Source: "hangar:Mallory/ViaVersion"}, errText: "owned by"},
		{name: "unsafe file override", want: Want{Source: "modrinth:luckperms", File: "../escape.jar"}, errText: "refusing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			f := repos.fetcher(dir)
			res, err := f.Sync(context.Background(), &FetchSpec{Plugins: []Want{tt.want}})
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) || tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("err = %v", err)
			}
			if len(res.Jars)+len(res.Wasm) != 0 {
				t.Errorf("failed entry reported as installed: %+v", res)
			}
			ents, _ := os.ReadDir(f.Dir)
			if len(ents) != 0 {
				t.Errorf("files left behind: %v", ents)
			}
		})
	}
}

func TestLoadFetchSpec(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plugins.toml")
	if s, err := LoadFetchSpec(p); err != nil || len(s.Plugins) != 0 {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	os.WriteFile(p, []byte(`
[[plugin]]
source = "modrinth:luckperms@5.4.0"

[[plugin]]
source = "url:https://example.com/a.wasm"
sha512 = "abc"
`), 0o644)
	s, err := LoadFetchSpec(p)
	if err != nil || len(s.Plugins) != 2 || s.Plugins[1].SHA512 != "abc" {
		t.Fatalf("spec = %+v %v", s, err)
	}
	os.WriteFile(p, []byte("[[plugin]]\nsource = \"bogus\"\n"), 0o644)
	if _, err := LoadFetchSpec(p); err == nil {
		t.Error("bad source accepted")
	}
}
