package provision

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func sum256(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func fakeBundler(t *testing.T, libSHA string) []byte {
	t.Helper()
	jar, lib := []byte("server jar bytes"), []byte("library bytes")
	if libSHA == "" {
		libSHA = sum256(lib)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, b []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	add("META-INF/versions.list", []byte(sum256(jar)+"\t9.9\t9.9/server-9.9.jar\n"))
	add("META-INF/libraries.list", []byte(libSHA+"\tcom.example:lib:1\tcom/example/lib/1/lib-1.jar\n"))
	add("META-INF/versions/9.9/server-9.9.jar", jar)
	add("META-INF/libraries/com/example/lib/1/lib-1.jar", lib)
	zw.Close()
	return buf.Bytes()
}

type fakeMojang struct {
	*httptest.Server
	hits atomic.Int64
}

func newMojang(t *testing.T, bundler []byte, advertised []byte) *fakeMojang {
	f := &fakeMojang{}
	s1 := sha1.Sum(advertised)
	var meta []byte
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		switch r.URL.Path {
		case "/manifest.json":
			m := sha1.Sum(meta)
			fmt.Fprintf(w, `{"versions":[{"id":"9.9","url":"%s/9.9.json","sha1":"%x"}]}`, f.URL, m)
		case "/9.9.json":
			w.Write(meta)
		case "/server.jar":
			w.Write(bundler)
		default:
			http.NotFound(w, r)
		}
	}))
	meta = fmt.Appendf(nil, `{"id":"9.9","downloads":{"server":{"sha1":"%x","size":%d,"url":"%s/server.jar"}},"javaVersion":{"majorVersion":25}}`, s1, len(advertised), f.URL)
	t.Cleanup(f.Close)
	return f
}

func TestEnsure(t *testing.T) {
	dir := t.TempDir()
	b := fakeBundler(t, "")
	m := newMojang(t, b, b)
	opts := Options{ManifestURL: m.URL + "/manifest.json"}
	res, err := Ensure(context.Background(), dir, "9.9", opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Downloaded || res.Extracted != 2 || res.JavaMajor != 25 || filepath.Base(res.ServerJar) != "server-9.9.jar" {
		t.Fatalf("first run: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "libraries", "com/example/lib/1/lib-1.jar")); err != nil {
		t.Fatal(err)
	}

	m.Close() // a complete cache must not touch the network
	hits := m.hits.Load()
	res, err = Ensure(context.Background(), dir, "9.9", opts)
	if err != nil || res.Downloaded || res.Extracted != 0 || res.Verified != 2 || m.hits.Load() != hits {
		t.Fatalf("cached run: %+v %v", res, err)
	}

	// A damaged library is re-extracted from the bundler.
	os.WriteFile(res.Libraries[0], []byte("broken"), 0o644)
	if res, err = Ensure(context.Background(), dir, "9.9", opts); err != nil || res.Extracted != 1 {
		t.Fatalf("repair run: %+v %v", res, err)
	}
}

func TestEnsureRejectsBadHashes(t *testing.T) {
	b := fakeBundler(t, "")
	m := newMojang(t, b, append([]byte("x"), b...)) // advertised sha1 differs
	_, err := Ensure(context.Background(), t.TempDir(), "9.9", Options{ManifestURL: m.URL + "/manifest.json"})
	if err == nil || !strings.Contains(err.Error(), "bytes") && !strings.Contains(err.Error(), "sha1") {
		t.Fatalf("bundler hash: %v", err)
	}
	bad := fakeBundler(t, strings.Repeat("0", 64))
	m = newMojang(t, bad, bad)
	_, err = Ensure(context.Background(), t.TempDir(), "9.9", Options{ManifestURL: m.URL + "/manifest.json"})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("library hash: %v", err)
	}
}
