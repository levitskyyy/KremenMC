// Command worldgendata packs vanilla data-pack contents and data generator
// reports from the official server jar into the container embedded by
// internal/world/worldgen/wgdata.
//
//	go run ./tools/worldgendata [-cache .refcache/26.3] [-force]
//
// Included: everything under data/minecraft/ of the inner server jar (JSON
// minified, structure NBT stored decompressed; the built-in feature packs
// under datapacks/ keep their paths), and every report produced by
// --reports except the JSON-RPC schema.
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var skipReports = map[string]bool{
	"json-rpc-api-schema.json": true,
}

func main() {
	cache := flag.String("cache", ".refcache/26.3", "reference cache with bundler.jar and server-26.3.jar")
	out := flag.String("out", "internal/data/gen/worldgen/worldgen-26.3.bin", "output container")
	force := flag.Bool("force", false, "rerun the data generator even if reports exist")
	flag.Parse()

	reports, err := ensureReports(*cache, *force)
	if err != nil {
		log.Fatal(err)
	}
	files, err := collect(filepath.Join(*cache, "server-26.3.jar"), reports)
	if err != nil {
		log.Fatal(err)
	}
	b, err := wgdata.Encode(files)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		log.Fatal(err)
	}
	printSummary(files, len(b), *out)
}

// ensureReports runs the official data generator unless its output exists
// and returns the directory holding the reports.
func ensureReports(cache string, force bool) (string, error) {
	base, err := filepath.Abs(filepath.Join(cache, "reports"))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "reports")
	if _, err := os.Stat(filepath.Join(dir, "registries.json")); err == nil && !force {
		return dir, nil
	}
	bundler, err := filepath.Abs(filepath.Join(cache, "bundler.jar"))
	if err != nil {
		return "", err
	}
	work := filepath.Join(filepath.Dir(base), "dgwork")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("java", "-DbundlerMainClass=net.minecraft.data.Main", "-jar", bundler, "--reports", "--output", base)
	cmd.Dir = work
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	log.Printf("running data generator in %s", work)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("data generator: %w", err)
	}
	return dir, nil
}

// collect reads the jar and reports into container paths.
func collect(jarPath, reportsDir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "data/minecraft/") || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		b, err := normalize(f.Name, raw)
		if err != nil {
			return nil, err
		}
		files[f.Name] = b
	}

	err = filepath.WalkDir(reportsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(reportsDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipReports[rel] || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b, err := normalize(rel, raw)
		if err != nil {
			return err
		}
		files["reports/"+rel] = b
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reports: %w", err)
	}
	if _, ok := files["reports/registries.json"]; !ok {
		return nil, fmt.Errorf("reports: registries.json missing in %s", reportsDir)
	}
	return files, nil
}

func normalize(name string, raw []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, ".json"), strings.HasSuffix(name, ".mcmeta"):
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return buf.Bytes(), nil
	case strings.HasSuffix(name, ".nbt") && len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b:
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		b, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return b, nil
	}
	return raw, nil
}

func printSummary(files map[string][]byte, size int, out string) {
	type stat struct{ n, bytes int }
	groups := map[string]*stat{}
	total := 0
	for p, b := range files {
		g := p
		if r, ok := strings.CutPrefix(p, "data/minecraft/"); ok {
			parts := strings.SplitN(r, "/", 3)
			g = parts[0]
			if (g == "worldgen" || g == "tags") && len(parts) > 2 {
				g += "/" + parts[1]
			}
		} else if r, ok := strings.CutPrefix(p, "reports/"); ok {
			g = "reports/" + strings.SplitN(r, "/", 2)[0]
		}
		s := groups[g]
		if s == nil {
			s = &stat{}
			groups[g] = s
		}
		s.n++
		s.bytes += len(b)
		total += len(b)
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		fmt.Printf("%-48s %6d files %10d bytes\n", g, groups[g].n, groups[g].bytes)
	}
	fmt.Printf("wrote %s: %d files, %d raw bytes, %d bytes packed\n", out, len(files), total, size)
}
