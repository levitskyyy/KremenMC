package jre

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	java25 = "#!/bin/sh\necho 'openjdk version \"25.0.1\" 2025-10-21' 1>&2\n"
	java8  = "#!/bin/sh\necho 'java version \"1.8.0_402\"' 1>&2\n"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		feature int
		full    string
		wantErr bool
	}{
		{in: `openjdk version "25.0.5" 2026-10-20 LTS`, feature: 25, full: "25.0.5"},
		{in: `java version "1.8.0_402"`, feature: 8, full: "1.8.0_402"},
		{in: `openjdk version "21" 2023-09-19`, feature: 21, full: "21"},
		{in: `openjdk version "25-ea"`, feature: 25, full: "25-ea"},
		{in: `java version "17.0.2"`, feature: 17, full: "17.0.2"},
		{
			in:      "Picked up JAVA_TOOL_OPTIONS: -Dfoo.version=\"1\"\nopenjdk version \"25.0.1\" 2025-10-21\nOpenJDK Runtime Environment",
			feature: 25, full: "25.0.1",
		},
		{in: "The operation couldn't be completed. Unable to locate a Java Runtime.", wantErr: true},
		{in: `openjdk version "ea"`, wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		feature, full, err := ParseVersion(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseVersion(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if feature != tt.feature || full != tt.full {
			t.Errorf("ParseVersion(%q) = %d, %q; want %d, %q", tt.in, feature, full, tt.feature, tt.full)
		}
	}
}

type entry struct {
	name string
	body string
	mode int64
	typ  byte
	link string
}

func file(name, body string, mode int64) entry {
	return entry{name: name, body: body, mode: mode, typ: tar.TypeReg}
}

func symlink(name, target string) entry {
	return entry{name: name, typ: tar.TypeSymlink, link: target}
}

func makeTarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typ, Linkname: e.link, Size: int64(len(e.body))}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if h.Mode == 0 {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// makeZip builds a Windows-style zip with no unix mode bits.
func makeZip(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fakeAPI struct {
	srv      *httptest.Server
	requests atomic.Int32
}

// newFakeAPI serves an Adoptium-like assets endpoint whose package link
// redirects to the archive. An empty checksum means "use the real one".
func newFakeAPI(t *testing.T, archive []byte, name, checksum string) *fakeAPI {
	t.Helper()
	if checksum == "" {
		sum := sha256.Sum256(archive)
		checksum = hex.EncodeToString(sum[:])
	}
	f := &fakeAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/assets/latest/25/hotspot", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("image_type") != "jre" || q.Get("vendor") != "eclipse" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		var a asset
		a.Binary.OS = q.Get("os")
		a.Binary.Architecture = q.Get("architecture")
		a.Binary.ImageType = "jre"
		a.Binary.Package = pkgInfo{Link: f.srv.URL + "/redirect/" + name, Checksum: checksum, Name: name, Size: int64(len(archive))}
		a.ReleaseName = "jdk-25.0.1+8"
		_ = json.NewEncoder(w).Encode([]asset{a})
	})
	mux.HandleFunc("GET /redirect/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blob/"+r.PathValue("name"), http.StatusFound)
	})
	mux.HandleFunc("GET /blob/{name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func baseOptions(t *testing.T, api *fakeAPI) Options {
	o := Options{
		RuntimeDir: filepath.Join(t.TempDir(), "runtime"),
		MinVersion: 25,
		GOOS:       runtime.GOOS,
		GOARCH:     "arm64",
		SkipSystem: true,
		LookPath:   func(string) (string, error) { return "", errors.New("not found") },
		Getenv:     func(string) string { return "" },
	}
	if api != nil {
		o.APIBase = api.srv.URL
		o.HTTPClient = api.srv.Client()
	}
	return o
}

func skipExecOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake java is a shell script")
	}
}

func TestEnsureDownload(t *testing.T) {
	skipExecOnWindows(t)
	top := "jdk-25.0.1+8-jre/"
	tests := []struct {
		name    string
		goos    string
		archive func(t *testing.T) []byte
		check   []string // relative paths that must exist in the install
		noExec  []string
	}{
		{
			name: "tar.gz linux layout",
			goos: "linux",
			archive: func(t *testing.T) []byte {
				return makeTarGz(t, []entry{
					{name: top, typ: tar.TypeDir, mode: 0o755},
					file(top+"bin/java", java25, 0o755),
					file(top+"lib/modules", "data", 0o644),
					file(top+"legal/java.base/LICENSE", "license", 0o444),
					symlink(top+"legal/java.sql/LICENSE", "../java.base/LICENSE"),
				})
			},
			check:  []string{"lib/modules", "legal/java.sql/LICENSE"},
			noExec: []string{"lib/modules"},
		},
		{
			name: "macOS Contents/Home normalized",
			goos: "darwin",
			archive: func(t *testing.T) []byte {
				return makeTarGz(t, []entry{
					file(top+"Contents/Info.plist", "<plist/>", 0o644),
					file(top+"Contents/Home/bin/java", java25, 0o755),
					file(top+"Contents/Home/lib/libjli.dylib", "lib", 0o644),
					symlink(top+"Contents/MacOS/libjli.dylib", "../Home/lib/libjli.dylib"),
				})
			},
			check: []string{"lib/libjli.dylib"},
		},
		{
			name: "windows zip without unix modes",
			goos: "windows",
			archive: func(t *testing.T) []byte {
				return makeZip(t, []entry{
					{name: top + "bin/java.exe", body: java25},
					{name: top + "release", body: "JAVA_VERSION=25"},
				})
			},
			check:  []string{"release"},
			noExec: []string{"release"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t, tt.archive(t), "pkg", "")
			o := baseOptions(t, api)
			o.GOOS = tt.goos
			rt, err := Ensure(context.Background(), o)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			exe := "java"
			if tt.goos == "windows" {
				exe = "java.exe"
			}
			wantPath := filepath.Join(o.RuntimeDir, "jre-25", "bin", exe)
			if rt.JavaPath != wantPath || rt.Source != SourceProvisioned || rt.Version != 25 || rt.FullVersion != "25.0.1" {
				t.Fatalf("got %+v, want java at %s", rt, wantPath)
			}
			fi, err := os.Stat(wantPath)
			if err != nil || fi.Mode()&0o111 == 0 {
				t.Fatalf("java not executable: %v %v", fi, err)
			}
			for _, p := range tt.check {
				if _, err := os.Stat(filepath.Join(o.RuntimeDir, "jre-25", p)); err != nil {
					t.Errorf("missing %s: %v", p, err)
				}
			}
			for _, p := range tt.noExec {
				fi, err := os.Stat(filepath.Join(o.RuntimeDir, "jre-25", p))
				if err != nil || fi.Mode()&0o111 != 0 {
					t.Errorf("%s: want non-executable, got %v %v", p, fi, err)
				}
			}
			assertOnlyInstall(t, o.RuntimeDir, "jre-25")
		})
	}
}

func TestEnsureRejectsBadArchives(t *testing.T) {
	good := file("jdk/bin/java", java25, 0o755)
	tests := []struct {
		name     string
		entries  []entry
		checksum string
		wantErr  string
	}{
		{name: "checksum mismatch", entries: []entry{good}, checksum: strings.Repeat("ab", 32), wantErr: "checksum mismatch"},
		{name: "path traversal", entries: []entry{good, file("../evil", "x", 0o644)}, wantErr: "path traversal"},
		{name: "nested traversal", entries: []entry{good, file("jdk/../../evil", "x", 0o644)}, wantErr: "path traversal"},
		{name: "absolute path", entries: []entry{good, file("/tmp/evil", "x", 0o644)}, wantErr: "absolute path"},
		{name: "escaping symlink", entries: []entry{good, symlink("jdk/lib/evil", "../../../etc/passwd")}, wantErr: "escapes"},
		{name: "absolute symlink", entries: []entry{good, symlink("jdk/lib/evil", "/etc/passwd")}, wantErr: "absolute"},
		{
			name: "chained symlink escape",
			entries: []entry{
				good,
				symlink("jdk/d/up", ".."),
				symlink("jdk/d/up/esc", "../.."),
			},
			wantErr: "escapes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t, makeTarGz(t, tt.entries), "pkg.tar.gz", tt.checksum)
			o := baseOptions(t, api)
			_, err := Ensure(context.Background(), o)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Ensure err = %v, want containing %q", err, tt.wantErr)
			}
			assertOnlyInstall(t, o.RuntimeDir, "")
			if _, err := os.Lstat(filepath.Join(filepath.Dir(o.RuntimeDir), "evil")); err == nil {
				t.Fatal("entry escaped runtime dir")
			}
		})
	}
}

// assertOnlyInstall checks no temp, lock or download files were left behind.
func assertOnlyInstall(t *testing.T, dir, want string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if want == "" && len(names) != 0 || want != "" && (len(names) != 1 || names[0] != want) {
		t.Fatalf("runtime dir contains %v, want only %q", names, want)
	}
}

func writeJava(t *testing.T, dir, script string) string {
	t.Helper()
	p := filepath.Join(dir, "bin", "java")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnsureReusesInstalled(t *testing.T) {
	skipExecOnWindows(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request %s", r.URL)
	}))
	defer srv.Close()
	o := baseOptions(t, nil)
	o.APIBase = srv.URL
	javaPath := writeJava(t, filepath.Join(o.RuntimeDir, "jre-25"), java25)

	rt, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if rt.JavaPath != javaPath || rt.Source != SourceProvisioned || rt.Version != 25 {
		t.Fatalf("got %+v", rt)
	}
}

func TestEnsureSystemJava(t *testing.T) {
	skipExecOnWindows(t)
	tarball := makeTarGz(t, []entry{file("jdk/bin/java", java25, 0o755)})
	tests := []struct {
		name       string
		script     string
		viaHome    bool
		wantSource string
		wantHTTP   bool
	}{
		{name: "JAVA_HOME ok", script: java25, viaHome: true, wantSource: SourceSystem},
		{name: "PATH ok", script: java25, wantSource: SourceSystem},
		{name: "too old falls back to download", script: java8, viaHome: true, wantSource: SourceProvisioned, wantHTTP: true},
		{name: "macOS stub falls back to download", script: "#!/bin/sh\necho 'Unable to locate a Java Runtime.' 1>&2\nexit 1\n", wantSource: SourceProvisioned, wantHTTP: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t, tarball, "pkg.tar.gz", "")
			o := baseOptions(t, api)
			o.SkipSystem = false
			home := t.TempDir()
			sys := writeJava(t, home, tt.script)
			if tt.viaHome {
				o.Getenv = func(k string) string {
					if k == "JAVA_HOME" {
						return home
					}
					return ""
				}
			} else {
				o.LookPath = func(name string) (string, error) {
					if name != "java" {
						return "", errors.New("not found")
					}
					return sys, nil
				}
			}
			rt, err := Ensure(context.Background(), o)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if rt.Source != tt.wantSource || rt.Version != 25 {
				t.Fatalf("got %+v, want source %s", rt, tt.wantSource)
			}
			if tt.wantSource == SourceSystem && rt.JavaPath != sys {
				t.Fatalf("JavaPath = %s, want %s", rt.JavaPath, sys)
			}
			if got := api.requests.Load() > 0; got != tt.wantHTTP {
				t.Fatalf("HTTP used = %v, want %v", got, tt.wantHTTP)
			}
		})
	}
}

func TestEnsureReplacesBrokenInstall(t *testing.T) {
	skipExecOnWindows(t)
	api := newFakeAPI(t, makeTarGz(t, []entry{file("jdk/bin/java", java25, 0o755)}), "pkg.tar.gz", "")
	o := baseOptions(t, api)
	writeJava(t, filepath.Join(o.RuntimeDir, "jre-25"), java8)

	rt, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if rt.Version != 25 || rt.Source != SourceProvisioned {
		t.Fatalf("got %+v", rt)
	}
	assertOnlyInstall(t, o.RuntimeDir, "jre-25")
}

func TestCleanName(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "jdk/bin/java", want: "jdk/bin/java"},
		{in: "./jdk/", want: "jdk"},
		{in: "./", want: ""},
		{in: `jdk\bin\java.exe`, want: "jdk/bin/java.exe"},
		{in: "../x", wantErr: true},
		{in: "a/../../x", wantErr: true},
		{in: "/etc/passwd", wantErr: true},
		{in: `C:\x`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := cleanName(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("cleanName(%q) = %q, %v; want %q, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestPlatformMapping(t *testing.T) {
	arches := map[string]string{
		"amd64": "x64", "arm64": "aarch64", "386": "x86-32", "arm": "arm",
		"ppc64le": "ppc64le", "s390x": "s390x", "riscv64": "riscv64",
	}
	for in, want := range arches {
		if got, err := adoptiumArch(in); err != nil || got != want {
			t.Errorf("adoptiumArch(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	oses := map[string]string{"darwin": "mac", "linux": "linux", "windows": "windows"}
	for in, want := range oses {
		if got, err := adoptiumOS(in); err != nil || got != want {
			t.Errorf("adoptiumOS(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := adoptiumOS("plan9"); err == nil {
		t.Error("adoptiumOS(plan9) succeeded")
	}
}
