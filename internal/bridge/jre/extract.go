package jre

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const maxExtract = 2 << 30

// extract unpacks a .tar.gz or .zip archive (detected by magic bytes) into
// dst. All writes go through os.Root so nothing can land outside dst.
func extract(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("read archive header: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open extract root: %w", err)
	}
	defer root.Close()
	x := &extractor{root: root}

	switch {
	case bytes.HasPrefix(magic[:], []byte{0x1f, 0x8b}):
		return x.tarGz(f)
	case bytes.Equal(magic[:], []byte("PK\x03\x04")):
		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat archive: %w", err)
		}
		return x.zip(f, fi.Size())
	}
	return fmt.Errorf("unknown archive format (magic %x)", magic)
}

type extractor struct {
	root    *os.Root
	written int64
}

func (x *extractor) tarGz(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		perm := fs.FileMode(h.Mode) & 0o777
		switch h.Typeflag {
		case tar.TypeDir:
			err = x.dir(name, perm)
		case tar.TypeReg:
			err = x.file(name, perm, tr)
		case tar.TypeSymlink:
			err = x.symlink(name, h.Linkname)
		case tar.TypeLink:
			var old string
			if old, err = cleanName(h.Linkname); err == nil {
				err = x.hardlink(name, old)
			}
		}
		if err != nil {
			return err
		}
	}
}

func (x *extractor) zip(r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		name, err := cleanName(f.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			err = x.dir(name, zipPerm(f, name))
		case mode&fs.ModeSymlink != 0:
			err = x.zipSymlink(f, name)
		case mode.IsRegular():
			err = x.zipFile(f, name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (x *extractor) zipFile(f *zip.File, name string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("zip %s: %w", name, err)
	}
	defer rc.Close()
	return x.file(name, zipPerm(f, name), rc)
}

func (x *extractor) zipSymlink(f *zip.File, name string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("zip %s: %w", name, err)
	}
	defer rc.Close()
	target, err := io.ReadAll(io.LimitReader(rc, 4096))
	if err != nil {
		return fmt.Errorf("zip %s: %w", name, err)
	}
	return x.symlink(name, string(target))
}

// zipPerm uses unix mode bits when the archive carries them; Windows-made
// zips do not, so anything under a bin/ directory is made executable.
func zipPerm(f *zip.File, name string) fs.FileMode {
	if f.CreatorVersion>>8 == 3 {
		if p := f.Mode().Perm(); p != 0 {
			return p
		}
	}
	if f.Mode().IsDir() || slices.Contains(strings.Split(path.Dir(name), "/"), "bin") {
		return 0o755
	}
	return 0o644
}

func (x *extractor) dir(name string, perm fs.FileMode) error {
	p := filepath.FromSlash(name)
	if err := x.root.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}
	if err := x.root.Chmod(p, perm|0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

func (x *extractor) parent(name string) error {
	if d := path.Dir(name); d != "." {
		if err := x.root.MkdirAll(filepath.FromSlash(d), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

func (x *extractor) file(name string, perm fs.FileMode, r io.Reader) error {
	if err := x.parent(name); err != nil {
		return err
	}
	p := filepath.FromSlash(name)
	_ = x.root.Remove(p)
	f, err := x.root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	n, err := io.Copy(f, io.LimitReader(r, maxExtract-x.written+1))
	x.written += n
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if x.written > maxExtract {
		return fmt.Errorf("archive expands beyond %d bytes", maxExtract)
	}
	if err := x.root.Chmod(p, perm|0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

func (x *extractor) symlink(name, target string) error {
	t := strings.ReplaceAll(target, `\`, "/")
	if t == "" || path.IsAbs(t) || hasVolume(t) {
		return fmt.Errorf("symlink %s: absolute or empty target %q", name, target)
	}
	if j := path.Join(path.Dir(name), t); j == ".." || strings.HasPrefix(j, "../") {
		return fmt.Errorf("symlink %s: target %q escapes archive root", name, target)
	}
	if err := x.parent(name); err != nil {
		return err
	}
	p := filepath.FromSlash(name)
	_ = x.root.Remove(p)
	if err := x.root.Symlink(filepath.FromSlash(t), p); err != nil {
		return fmt.Errorf("symlink %s: %w", name, err)
	}
	return nil
}

func (x *extractor) hardlink(name, old string) error {
	if err := x.parent(name); err != nil {
		return err
	}
	p := filepath.FromSlash(name)
	_ = x.root.Remove(p)
	if err := x.root.Link(filepath.FromSlash(old), p); err != nil {
		return fmt.Errorf("link %s: %w", name, err)
	}
	return nil
}

func hasVolume(p string) bool {
	return len(p) >= 2 && p[1] == ':'
}

// cleanName validates an archive entry name and returns it slash-separated
// and cleaned, or "" for entries naming the root itself.
func cleanName(raw string) (string, error) {
	name := strings.ReplaceAll(raw, `\`, "/")
	if path.IsAbs(name) || hasVolume(name) {
		return "", fmt.Errorf("archive entry %q: absolute path", raw)
	}
	if slices.Contains(strings.Split(name, "/"), "..") {
		return "", fmt.Errorf("archive entry %q: path traversal", raw)
	}
	name = path.Clean(name)
	if name == "." {
		return "", nil
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("archive entry %q: invalid path", raw)
	}
	return name, nil
}

// installRoot strips the archive's single top-level directory and, for the
// macOS bundle layout, descends into Contents/Home.
func installRoot(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read extracted runtime: %w", err)
	}
	if len(ents) == 1 && ents[0].IsDir() {
		dir = filepath.Join(dir, ents[0].Name())
	}
	if fi, err := os.Stat(filepath.Join(dir, "Contents", "Home")); err == nil && fi.IsDir() {
		dir = filepath.Join(dir, "Contents", "Home")
	}
	return dir, nil
}

// verifySymlinks resolves every symlink under root and rejects any that ends
// up outside it, catching escapes built from chains the lexical check misses.
func verifySymlinks(root string) error {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve runtime root: %w", err)
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		r, err := filepath.EvalSymlinks(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resolve symlink %s: %w", p, err)
		}
		if rel, err := filepath.Rel(base, r); err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("symlink %s escapes runtime root", p)
		}
		return nil
	})
}
