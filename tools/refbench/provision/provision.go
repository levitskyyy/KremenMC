// Package provision maintains the reference server cache .refcache/<version>:
// the official bundler jar from Mojang's piston-meta (sha1-checked) and the
// inner server jar plus libraries extracted from it (sha256-checked against
// the bundler's META-INF/versions.list and libraries.list).
//
// Layout (what internal/reftest expects):
//
//	<dir>/bundler.jar
//	<dir>/version.json            piston-meta version metadata (when downloaded)
//	<dir>/server-<version>.jar    from META-INF/versions/<version>/server-<version>.jar
//	<dir>/libraries/<maven path>  from META-INF/libraries/...
package provision

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	DefaultManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	maxMetadata        = 8 << 20
	maxServerJar       = 512 << 20
)

// Options configure Ensure. Zero values use defaults.
type Options struct {
	ManifestURL string
	HTTPClient  *http.Client
	Logger      *slog.Logger
	// Remote re-checks an existing bundler against piston-meta's sha1 even
	// when no version.json is cached (needs network).
	Remote bool
}

// Result describes the provisioned cache.
type Result struct {
	Dir        string
	Bundler    string
	ServerJar  string
	Libraries  []string
	JavaMajor  int  // from version.json; 0 when unknown
	Downloaded bool // bundler fetched from Mojang
	Extracted  int  // files (re)written from the bundler
	Verified   int  // files that already matched their sha256
}

type manifest struct {
	Versions []struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
	} `json:"versions"`
}

type versionMeta struct {
	ID        string `json:"id"`
	Downloads struct {
		Server struct {
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
			URL  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
}

type provisioner struct {
	o       Options
	version string
	dir     string
}

// Ensure makes dir hold a verified cache for version. With a complete cache
// it only reads local files: no network access unless o.Remote is set.
func Ensure(ctx context.Context, dir, version string, o Options) (*Result, error) {
	if o.HTTPClient == nil {
		o.HTTPClient = http.DefaultClient
	}
	if o.ManifestURL == "" {
		o.ManifestURL = DefaultManifestURL
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}
	p := &provisioner{o: o, version: version, dir: dir}
	res := &Result{Dir: dir, Bundler: filepath.Join(dir, "bundler.jar")}
	meta, err := p.ensureBundler(ctx, res)
	if err != nil {
		return nil, err
	}
	if meta != nil {
		res.JavaMajor = meta.JavaVersion.MajorVersion
	}
	if err := p.extract(res); err != nil {
		return nil, fmt.Errorf("provision: extract %s: %w", res.Bundler, err)
	}
	return res, nil
}

func (p *provisioner) ensureBundler(ctx context.Context, res *Result) (*versionMeta, error) {
	metaPath := filepath.Join(p.dir, "version.json")
	sum, sumErr := fileSHA1(res.Bundler)
	if b, err := os.ReadFile(metaPath); err == nil {
		var m versionMeta
		if json.Unmarshal(b, &m) == nil && m.ID == p.version && m.Downloads.Server.SHA1 != "" {
			if sumErr == nil && strings.EqualFold(sum, m.Downloads.Server.SHA1) {
				return &m, nil
			}
		}
	} else if sumErr == nil && !p.o.Remote {
		// Cache created before version.json was recorded: trust the local
		// bundler; its contents are still checked against its own lists.
		p.o.Logger.Info("bundler present without version.json; skipping piston-meta check (use -remote to verify)", "sha1", sum)
		return nil, nil
	}
	m, raw, err := p.fetchMeta(ctx)
	if err != nil {
		return nil, err
	}
	srv := m.Downloads.Server
	if srv.URL == "" || srv.SHA1 == "" {
		return nil, fmt.Errorf("provision: version %s has no server download", p.version)
	}
	if sumErr != nil || !strings.EqualFold(sum, srv.SHA1) {
		if sumErr == nil {
			p.o.Logger.Warn("cached bundler sha1 differs from piston-meta; re-downloading", "have", sum, "want", srv.SHA1)
		}
		p.o.Logger.Info("downloading minecraft server", "version", p.version, "size", srv.Size, "url", srv.URL)
		if err := p.download(ctx, srv.URL, srv.SHA1, srv.Size, res.Bundler); err != nil {
			return nil, err
		}
		res.Downloaded = true
	}
	if err := writeAtomic(metaPath, raw); err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}
	return m, nil
}

func (p *provisioner) fetchMeta(ctx context.Context) (*versionMeta, []byte, error) {
	b, err := p.get(ctx, p.o.ManifestURL)
	if err != nil {
		return nil, nil, fmt.Errorf("provision: version manifest: %w", err)
	}
	var man manifest
	if err := json.Unmarshal(b, &man); err != nil {
		return nil, nil, fmt.Errorf("provision: version manifest: %w", err)
	}
	for _, v := range man.Versions {
		if v.ID != p.version {
			continue
		}
		raw, err := p.get(ctx, v.URL)
		if err != nil {
			return nil, nil, fmt.Errorf("provision: version %s metadata: %w", p.version, err)
		}
		if v.SHA1 != "" {
			s := sha1.Sum(raw)
			if !strings.EqualFold(hex.EncodeToString(s[:]), v.SHA1) {
				return nil, nil, fmt.Errorf("provision: version %s metadata: sha1 mismatch", p.version)
			}
		}
		var m versionMeta
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, nil, fmt.Errorf("provision: version %s metadata: %w", p.version, err)
		}
		m.ID = p.version
		return &m, raw, nil
	}
	return nil, nil, fmt.Errorf("provision: version %s not in Mojang's version manifest", p.version)
}

func (p *provisioner) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.o.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMetadata {
		return nil, fmt.Errorf("GET %s: response too large", url)
	}
	return b, nil
}

func (p *provisioner) download(ctx context.Context, url, wantSHA1 string, size int64, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.o.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("provision: download server jar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provision: download server jar: %s", resp.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".bundler-*.jar")
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	h := sha1.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxServerJar+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("provision: download server jar: %w", err)
	}
	if size > 0 && n != size {
		return fmt.Errorf("provision: server jar is %d bytes, manifest says %d", n, size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA1) {
		return fmt.Errorf("provision: server jar sha1 %s, manifest says %s", got, wantSHA1)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("provision: install server jar: %w", err)
	}
	return nil
}

type bundleEntry struct {
	sha256 string
	path   string // relative to META-INF/<kind>/
}

// extract writes the version jar to <dir>/<basename> and libraries under
// <dir>/libraries, skipping files whose sha256 already matches.
func (p *provisioner) extract(res *Result) error {
	zr, err := zip.OpenReader(res.Bundler)
	if err != nil {
		return err
	}
	defer zr.Close()
	versions, err := readList(&zr.Reader, "META-INF/versions.list")
	if err != nil {
		return err
	}
	libs, err := readList(&zr.Reader, "META-INF/libraries.list")
	if err != nil {
		return err
	}
	if len(versions) != 1 {
		return fmt.Errorf("versions.list has %d entries, want 1", len(versions))
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	one := func(kind string, e bundleEntry, dst string) error {
		if ok, err := matches(dst, e.sha256); err != nil {
			return err
		} else if ok {
			res.Verified++
			return nil
		}
		name := "META-INF/" + kind + "/" + e.path
		f := files[name]
		if f == nil {
			return fmt.Errorf("%s listed but missing", name)
		}
		if err := copyVerified(f, dst, e.sha256); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		p.o.Logger.Info("extracted", "file", dst)
		res.Extracted++
		return nil
	}
	res.ServerJar = filepath.Join(p.dir, path.Base(versions[0].path))
	if err := one("versions", versions[0], res.ServerJar); err != nil {
		return err
	}
	for _, e := range libs {
		dst := filepath.Join(p.dir, "libraries", filepath.FromSlash(e.path))
		if err := one("libraries", e, dst); err != nil {
			return err
		}
		res.Libraries = append(res.Libraries, dst)
	}
	return nil
}

func matches(p, wantSHA256 string) (bool, error) {
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSHA256), nil
}

func readList(zr *zip.Reader, name string) ([]bundleEntry, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, fmt.Errorf("not a bundler jar: %w", err)
	}
	defer f.Close()
	var out []bundleEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s: bad line %q", name, line)
		}
		p := parts[2]
		if p == "" || path.IsAbs(p) || strings.Contains(p, "\\") || path.Clean(p) != p || strings.HasPrefix(p, "../") || p == ".." {
			return nil, fmt.Errorf("%s: unsafe path %q", name, p)
		}
		out = append(out, bundleEntry{sha256: parts[0], path: p})
	}
	return out, sc.Err()
}

func copyVerified(f *zip.File, dst, wantSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	tmp := w.Name()
	defer os.Remove(tmp)
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(w, h), r)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA256) {
		return fmt.Errorf("sha256 %s, bundle lists %s", got, wantSHA256)
	}
	return os.Rename(tmp, dst)
}

func fileSHA1(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
