// Command bridgebuild compiles bridge/java into internal/bridge/embed/jar/kremenmc-bridge.jar.
//
//	go run ./tools/bridgebuild          fetch pinned API jars, javac, package
//	go run ./tools/bridgebuild -fetch    only download the pinned API jars into the cache
//	go run ./tools/bridgebuild -pin     print SHA256 pins for bridge.Libs (verified against the repository .sha1)
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/internal/bridge"
)

const javaRelease = "25"

func main() {
	var (
		root  = flag.String("root", ".", "repository root")
		cache = flag.String("cache", "", "library cache dir (default <user cache dir>/kremenmc-bridgebuild/libs)")
		out   = flag.String("out", "", "output jar (default <root>/internal/bridge/embed/jar/kremenmc-bridge.jar)")
		javac = flag.String("javac", "javac", "javac binary")
		pin   = flag.Bool("pin", false, "print sha256 pins for bridge.Libs and exit")
		fetch = flag.Bool("fetch", false, "only download the API jars into the cache")
	)
	flag.Parse()
	log.SetFlags(0)
	ctx := context.Background()
	client := &http.Client{Timeout: 5 * time.Minute}
	if *cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			log.Fatal(err)
		}
		*cache = filepath.Join(dir, "kremenmc-bridgebuild", "libs")
	}
	if *out == "" {
		*out = filepath.Join(*root, "internal", "bridge", "embed", "jar", "kremenmc-bridge.jar")
	}
	if *pin {
		if err := printPins(ctx, client); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := bridge.EnsureLibs(ctx, client, *cache, bridge.Libs, nil); err != nil {
		log.Fatal(err)
	}
	if *fetch {
		fmt.Println("libraries in", *cache)
		return
	}
	if err := build(*root, *cache, *out, *javac); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", *out)
}

func printPins(ctx context.Context, client *http.Client) error {
	for i, l := range bridge.Libs {
		if i > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		data, err := get(ctx, client, l.URL())
		if err != nil {
			return err
		}
		sum1, err := get(ctx, client, l.URL()+".sha1")
		if err != nil {
			return err
		}
		want := strings.Fields(string(sum1))
		got := sha1.Sum(data)
		if len(want) == 0 || !strings.EqualFold(want[0], hex.EncodeToString(got[:])) {
			return fmt.Errorf("%s: sha1 mismatch with repository", l.Coord())
		}
		s := sha256.Sum256(data)
		fmt.Printf("%s %s\n", l.Coord(), hex.EncodeToString(s[:]))
	}
	return nil
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func build(root, libDir, out, javac string) error {
	srcRoot := filepath.Join(root, "bridge", "java", "src")
	resRoot := filepath.Join(root, "bridge", "java", "resources")
	var sources []string
	err := filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".java") {
			sources = append(sources, p)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("scan sources: %w", err)
	}
	sort.Strings(sources)
	classes, err := os.MkdirTemp("", "bridgebuild-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(classes)

	var cp []string
	for _, l := range bridge.Libs {
		cp = append(cp, filepath.Join(libDir, l.FileName()))
	}
	argFile := filepath.Join(classes, "sources.txt")
	if err := os.WriteFile(argFile, []byte(strings.Join(quoteAll(sources), "\n")), 0o644); err != nil {
		return err
	}
	outDir := filepath.Join(classes, "out")
	cmd := exec.Command(javac, "--release", javaRelease, "-encoding", "UTF-8", "-nowarn", "-Xlint:-options,-removal",
		"-cp", strings.Join(cp, string(os.PathListSeparator)), "-d", outDir, "@"+argFile)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("javac: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := "Manifest-Version: 1.0\r\nMain-Class: kremenmc.bridge.Main\r\nCreated-By: kremenmc bridgebuild\r\n\r\n"
	if err := addFile(zw, "META-INF/MANIFEST.MF", []byte(manifest)); err != nil {
		return err
	}
	for _, dir := range []string{outDir, resRoot} {
		if err := addTree(zw, dir); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = `"` + strings.ReplaceAll(filepath.ToSlash(s), `"`, `\"`) + `"`
	}
	return out
}

func addTree(zw *zip.Writer, dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := addFile(zw, filepath.ToSlash(rel), data); err != nil {
			return err
		}
	}
	return nil
}

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func addFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: epoch})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
