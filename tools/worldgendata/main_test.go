package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

const cache = "../../.refcache/26.3"

// heavySkip, when set, skips the tests that re-read the whole jar.
var heavySkip string

func TestNormalize(t *testing.T) {
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte{10, 0, 0, 0})
	w.Close()
	tests := []struct {
		name string
		in   []byte
		want string
		err  bool
	}{
		{"a.json", []byte("{\n  \"a\": 1.50,\n  \"b\": [ 1, 2 ]\n}\n"), `{"a":1.50,"b":[1,2]}`, false},
		{"pack.mcmeta", []byte("{ \"pack\": {} }"), `{"pack":{}}`, false},
		{"bad.json", []byte("{"), "", true},
		{"x.nbt", gz.Bytes(), "\x0a\x00\x00\x00", false},
		{"raw.nbt", []byte{10, 0, 0, 0}, "\x0a\x00\x00\x00", false},
		{"other.txt", []byte("keep  me"), "keep  me", false},
	}
	for _, tt := range tests {
		got, err := normalize(tt.name, tt.in)
		if (err != nil) != tt.err || (!tt.err && string(got) != tt.want) {
			t.Errorf("normalize(%s) = %q,%v want %q,err=%v", tt.name, got, err, tt.want, tt.err)
		}
	}
}

func refInputs(t *testing.T) (jar, reports string) {
	t.Helper()
	if heavySkip != "" {
		t.Skip(heavySkip)
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	jar = filepath.Join(cache, "server-26.3.jar")
	reports = filepath.Join(cache, "reports", "reports")
	if _, err := os.Stat(jar); err != nil {
		t.Skip("no reference jar in .refcache")
	}
	if _, err := os.Stat(filepath.Join(reports, "registries.json")); err != nil {
		t.Skip("no data generator reports in .refcache; run go run ./tools/worldgendata")
	}
	return jar, reports
}

// Two collections must agree, and encoding them must reproduce the
// committed artifact byte for byte.
func TestDeterministic(t *testing.T) {
	jar, reports := refInputs(t)
	a, err := collect(jar, reports)
	if err != nil {
		t.Fatal(err)
	}
	b, err := collect(jar, reports)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("runs collected %d and %d files", len(a), len(b))
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			t.Fatalf("%s differs between runs", k)
		}
	}
	out, err := wgdata.Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../internal/data/gen/worldgen/worldgen-26.3.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, out) {
		t.Error("committed artifact differs from a fresh encode; run go run ./tools/worldgendata")
	}
}

func TestEmbeddedMatchesJar(t *testing.T) {
	jar, reports := refInputs(t)
	files, err := collect(jar, reports)
	if err != nil {
		t.Fatal(err)
	}
	p, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Paths()) != len(files) {
		t.Errorf("embedded has %d files, jar+reports %d", len(p.Paths()), len(files))
	}
	for path, want := range files {
		got, ok := p.File(path)
		if !ok || !bytes.Equal(got, want) {
			t.Errorf("%s: embedded differs (%d bytes, %v)", path, len(got), ok)
		}
	}

	zr, err := zip.OpenReader(jar)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	count := map[string]int{}
	for _, f := range zr.File {
		r, ok := strings.CutPrefix(f.Name, "data/minecraft/")
		if !ok || f.FileInfo().IsDir() || strings.HasPrefix(r, "tags/") || strings.HasPrefix(r, "datapacks/") {
			continue
		}
		dir := filepath.Dir(r)
		for {
			count[dir]++
			if !strings.Contains(dir, "/") {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	for _, reg := range p.Registries() {
		if got, want := len(p.Keys(reg)), count[reg]; got != want {
			t.Errorf("Keys(%q) = %d, jar has %d files", reg, got, want)
		}
	}
}
