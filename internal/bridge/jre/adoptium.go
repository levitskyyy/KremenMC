package jre

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	maxDownload = 512 << 20
	maxMetadata = 4 << 20
)

type asset struct {
	Binary struct {
		OS           string  `json:"os"`
		Architecture string  `json:"architecture"`
		ImageType    string  `json:"image_type"`
		Package      pkgInfo `json:"package"`
	} `json:"binary"`
	Version struct {
		Semver string `json:"semver"`
	} `json:"version"`
	ReleaseName string `json:"release_name"`
}

type pkgInfo struct {
	Link     string `json:"link"`
	Checksum string `json:"checksum"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
}

func adoptiumOS(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "mac", nil
	case "linux", "windows", "aix":
		return goos, nil
	}
	return "", fmt.Errorf("no Temurin builds for GOOS %q", goos)
}

func adoptiumArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "aarch64", nil
	case "386":
		return "x86-32", nil
	case "arm", "ppc64le", "s390x", "riscv64":
		return goarch, nil
	}
	return "", fmt.Errorf("no Temurin builds for GOARCH %q", goarch)
}

func isMusl() bool {
	m, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(m) > 0
}

func (o *Options) fetchAsset(ctx context.Context) (*asset, error) {
	osName, err := adoptiumOS(o.GOOS)
	if err != nil {
		return nil, err
	}
	arch, err := adoptiumArch(o.GOARCH)
	if err != nil {
		return nil, err
	}
	if osName == "linux" && o.GOOS == runtime.GOOS && isMusl() {
		osName = "alpine-linux"
	}
	q := url.Values{"architecture": {arch}, "image_type": {"jre"}, "os": {osName}, "vendor": {"eclipse"}}
	u := fmt.Sprintf("%s/v3/assets/latest/%d/hotspot?%s", o.APIBase, o.MinVersion, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("adoptium request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adoptium query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adoptium query %s: status %s", u, resp.Status)
	}
	var assets []asset
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadata)).Decode(&assets); err != nil {
		return nil, fmt.Errorf("decode adoptium response: %w", err)
	}
	for i := range assets {
		b := &assets[i].Binary
		if b.ImageType == "jre" && b.OS == osName && b.Architecture == arch && b.Package.Link != "" {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("no Temurin %d JRE for %s/%s", o.MinVersion, osName, arch)
}

// download streams the package into a temp file under RuntimeDir and returns
// its path only if the SHA-256 matches the published checksum.
func (o *Options) download(ctx context.Context, p pkgInfo) (string, error) {
	want, err := hex.DecodeString(strings.TrimSpace(p.Checksum))
	if err != nil || len(want) != sha256.Size {
		return "", fmt.Errorf("package %s has invalid checksum %q", p.Name, p.Checksum)
	}
	if p.Size > maxDownload {
		return "", fmt.Errorf("package %s too large: %d bytes", p.Name, p.Size)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Link, nil)
	if err != nil {
		return "", fmt.Errorf("download request: %w", err)
	}
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", p.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %s", p.Name, resp.Status)
	}

	f, err := os.CreateTemp(o.RuntimeDir, ".jre-download-*")
	if err != nil {
		return "", fmt.Errorf("create download file: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("download %s: %w", p.Name, err)
	}
	if n > maxDownload {
		return "", fmt.Errorf("download %s: exceeds %d bytes", p.Name, maxDownload)
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		return "", fmt.Errorf("download %s: checksum mismatch: got %x, want %x", p.Name, got, want)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write download: %w", err)
	}
	keep = true
	return f.Name(), nil
}
