package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/levitskyyy/KremenMC/api"
)

// SupportedAPIVersions lists the api-version values this host accepts.
var SupportedAPIVersions = []string{"1"}

type Manifest struct {
	ID          string                        `toml:"id"`
	Name        string                        `toml:"name"`
	Version     string                        `toml:"version"`
	APIVersion  string                        `toml:"api-version"`
	Main        string                        `toml:"main"`
	Description string                        `toml:"description"`
	Authors     []string                      `toml:"authors"`
	Website     string                        `toml:"website"`
	Depends     []string                      `toml:"depends"`
	SoftDepends []string                      `toml:"softdepends"`
	LoadBefore  []string                      `toml:"loadbefore"`
	Permissions map[string]ManifestPermission `toml:"permissions"`
	Commands    map[string]ManifestCommand    `toml:"commands"`
	Limits      ManifestLimits                `toml:"limits"`
}

type ManifestPermission struct {
	Description string          `toml:"description"`
	Default     string          `toml:"default"`
	Children    map[string]bool `toml:"children"`
}

type ManifestCommand struct {
	Description string   `toml:"description"`
	Usage       string   `toml:"usage"`
	Aliases     []string `toml:"aliases"`
	Permission  string   `toml:"permission"`
}

// ManifestLimits lets a plugin ask for less than the server maximum; values
// above the server limits are clamped.
type ManifestLimits struct {
	MemoryMB      int `toml:"memory-mb"`
	CallTimeoutMS int `toml:"call-timeout-ms"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return nil, fmt.Errorf("manifest: %s", sm.String())
		}
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func (m *Manifest) Validate() error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("manifest: invalid id %q (want [a-z0-9][a-z0-9_.-]{0,63})", m.ID)
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	if m.Version == "" {
		return fmt.Errorf("manifest %s: version is required", m.ID)
	}
	if m.APIVersion == "" {
		return fmt.Errorf("manifest %s: api-version is required", m.ID)
	}
	if !slices.Contains(SupportedAPIVersions, m.APIVersion) {
		return fmt.Errorf("manifest %s: unsupported api-version %q", m.ID, m.APIVersion)
	}
	for _, list := range [][]string{m.Depends, m.SoftDepends, m.LoadBefore} {
		for _, d := range list {
			if d == m.ID {
				return fmt.Errorf("manifest %s: depends on itself", m.ID)
			}
			if !idPattern.MatchString(d) {
				return fmt.Errorf("manifest %s: invalid dependency id %q", m.ID, d)
			}
		}
	}
	for name, p := range m.Permissions {
		if p.Default == "" {
			continue
		}
		if _, ok := api.ParsePermissionDefault(p.Default); !ok {
			return fmt.Errorf("manifest %s: permission %s: invalid default %q", m.ID, name, p.Default)
		}
	}
	if m.Limits.MemoryMB < 0 || m.Limits.CallTimeoutMS < 0 {
		return fmt.Errorf("manifest %s: negative limits", m.ID)
	}
	return nil
}

func (m *Manifest) Info(runtime string) api.PluginInfo {
	return api.PluginInfo{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Authors:     slices.Clone(m.Authors),
		Runtime:     runtime,
		Depends:     slices.Clone(m.Depends),
		SoftDepends: slices.Clone(m.SoftDepends),
	}
}

func (m *Manifest) APIPermissions() []api.Permission {
	out := make([]api.Permission, 0, len(m.Permissions))
	for name, p := range m.Permissions {
		def := api.DefaultOp
		if p.Default != "" {
			def, _ = api.ParsePermissionDefault(p.Default)
		}
		out = append(out, api.Permission{Name: name, Description: p.Description, Default: def, Children: p.Children})
	}
	slices.SortFunc(out, func(a, b api.Permission) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return out
}

func (l ManifestLimits) memoryPages(maxMB int) uint32 {
	mb := maxMB
	if l.MemoryMB > 0 && l.MemoryMB < mb {
		mb = l.MemoryMB
	}
	return uint32(mb) * 16 // 64 KiB pages
}

func (l ManifestLimits) callTimeout(max time.Duration) time.Duration {
	if l.CallTimeoutMS > 0 {
		if d := time.Duration(l.CallTimeoutMS) * time.Millisecond; d < max {
			return d
		}
	}
	return max
}
