// Package reftest locates the reference (official) 26.3 server used as a
// test oracle and runs Java probes against it. Tests compare against golden
// files committed under testdata; set KREMENMC_REF=1 to regenerate them from the
// live reference. Nothing here is imported by the server at runtime.
package reftest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const Version = "26.3"

// Ref is an unpacked reference server: the inner server jar and its libraries.
type Ref struct {
	Dir  string // .refcache/<version>
	Jar  string
	Libs []string
	root string
}

// Root returns the module root (directory holding go.mod).
func Root() (string, error) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("reftest: go.mod not found")
		}
		dir = parent
	}
}

// Open finds the reference cache in $KREMENMC_REF_DIR or <root>/.refcache/26.3.
func Open() (*Ref, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := os.Getenv("KREMENMC_REF_DIR")
	if dir == "" {
		dir = filepath.Join(root, ".refcache", Version)
	}
	r := &Ref{Dir: dir, Jar: filepath.Join(dir, "server-"+Version+".jar"), root: root}
	if _, err := os.Stat(r.Jar); err != nil {
		return nil, fmt.Errorf("reftest: reference jar missing (run go run ./tools/refbench provision): %w", err)
	}
	err = filepath.WalkDir(filepath.Join(dir, "libraries"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jar") {
			r.Libs = append(r.Libs, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reftest: libraries: %w", err)
	}
	return r, nil
}

// Classpath is the reference jar plus libraries.
func (r *Ref) Classpath() string {
	return strings.Join(append([]string{r.Jar}, r.Libs...), string(os.PathListSeparator))
}

// Compile builds the probe package holding class (plus kremenmc/probe/common)
// into a fresh temporary directory, so concurrent runs and broken probes in
// other packages do not interfere.
func (r *Ref) Compile(class string) (string, error) {
	src := filepath.Join(r.root, "tools", "refprobe", "java")
	pkg := class[:max(strings.LastIndex(class, "."), 0)]
	dirs := []string{filepath.Join(src, filepath.FromSlash(strings.ReplaceAll(pkg, ".", "/")))}
	if common := filepath.Join(src, "kremenmc", "probe", "common"); common != dirs[0] {
		dirs = append(dirs, common)
	}
	var files []string
	for _, d := range dirs {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".java") {
				files = append(files, filepath.Join(d, e.Name()))
			}
		}
	}
	if len(files) == 0 {
		return "", fmt.Errorf("reftest: no probe sources for %s", class)
	}
	out, err := os.MkdirTemp(r.Dir, "probe-classes-")
	if err != nil {
		return "", err
	}
	args := append([]string{"--release", "25", "-nowarn", "-cp", r.Classpath(), "-d", out}, files...)
	cmd := exec.Command("javac", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(out)
		return "", fmt.Errorf("reftest: javac: %w\n%s", err, stderr.String())
	}
	return out, nil
}

// Run compiles the probe package and runs class with args, writing stdout to w.
func (r *Ref) Run(w io.Writer, class string, args ...string) error {
	classes, err := r.Compile(class)
	if err != nil {
		return err
	}
	defer os.RemoveAll(classes)
	cp := classes + string(os.PathListSeparator) + r.Classpath()
	heap := os.Getenv("KREMENMC_REF_HEAP")
	if heap == "" {
		heap = "4g"
	}
	cmd := exec.Command("java", append([]string{"-Xmx" + heap, "-cp", cp, class}, args...)...)
	cmd.Dir = r.Dir
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reftest: %s: %w\n%s", class, err, tail(stderr.String(), 4000))
	}
	return nil
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// Golden returns testdata/<name> from the calling package's directory. With
// KREMENMC_REF=1 it first regenerates the file by running the probe class.
func Golden(t testing.TB, name, class string, args ...string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("KREMENMC_REF") == "1" {
		ref, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := ref.Run(&buf, class, args...); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing; regenerate with KREMENMC_REF=1: %v", path, err)
	}
	return b
}
