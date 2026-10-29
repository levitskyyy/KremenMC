package bridge

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// PluginDesc is the metadata of a Java plugin jar.
type PluginDesc struct {
	File           string
	Name           string
	Main           string
	Version        string
	APIVersion     string
	Description    string
	Authors        []string
	Website        string
	Depend         []string
	SoftDepend     []string
	LoadBefore     []string
	Libraries      []string
	Commands       []CommandSpec
	FoliaSupported bool
	// Paper is set when the descriptor came from paper-plugin.yml.
	Paper bool
	// Bootstrapper and Loader are paper-plugin.yml entry points the bridge does not run.
	Bootstrapper string
	Loader       string
}

const maxDescriptorSize = 1 << 20

// ReadPluginJar reads paper-plugin.yml (preferred, like Paper) or plugin.yml from a jar.
func ReadPluginJar(path string) (PluginDesc, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return PluginDesc{}, fmt.Errorf("open plugin %s: %w", path, err)
	}
	defer zr.Close()
	var paperYML, bukkitYML *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case "paper-plugin.yml":
			paperYML = f
		case "plugin.yml":
			bukkitYML = f
		}
	}
	var d PluginDesc
	switch {
	case paperYML != nil:
		d, err = readDescriptor(paperYML, true)
	case bukkitYML != nil:
		d, err = readDescriptor(bukkitYML, false)
	default:
		return PluginDesc{}, fmt.Errorf("plugin %s: no plugin.yml or paper-plugin.yml", path)
	}
	if err != nil {
		return PluginDesc{}, fmt.Errorf("plugin %s: %w", path, err)
	}
	if paperYML != nil && bukkitYML != nil && len(d.Commands) == 0 {
		if bd, err := readDescriptor(bukkitYML, false); err == nil {
			d.Commands = bd.Commands
		}
	}
	d.File = path
	return d, nil
}

func readDescriptor(f *zip.File, paper bool) (PluginDesc, error) {
	rc, err := f.Open()
	if err != nil {
		return PluginDesc{}, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxDescriptorSize+1))
	if err != nil {
		return PluginDesc{}, err
	}
	if len(data) > maxDescriptorSize {
		return PluginDesc{}, fmt.Errorf("%s too large", f.Name)
	}
	return ParseDescriptor(string(data), paper)
}

// ParseDescriptor parses the contents of plugin.yml or paper-plugin.yml.
func ParseDescriptor(src string, paper bool) (PluginDesc, error) {
	v, err := parseYAML(src)
	if err != nil {
		return PluginDesc{}, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return PluginDesc{}, errors.New("descriptor is not a mapping")
	}
	d := PluginDesc{
		Name:           yStr(m["name"]),
		Main:           yStr(m["main"]),
		Version:        yStr(m["version"]),
		APIVersion:     yStr(m["api-version"]),
		Description:    yStr(m["description"]),
		Website:        yStr(m["website"]),
		Authors:        yStrs(m["authors"]),
		Libraries:      yStrs(m["libraries"]),
		FoliaSupported: yStr(m["folia-supported"]) == "true",
		Paper:          paper,
		Bootstrapper:   yStr(m["bootstrapper"]),
		Loader:         yStr(m["loader"]),
	}
	if a := yStr(m["author"]); a != "" {
		d.Authors = append([]string{a}, d.Authors...)
	}
	if paper {
		deps, _ := m["dependencies"].(map[string]any)
		server, _ := deps["server"].(map[string]any)
		for _, name := range sortedKeys(server) {
			spec, _ := server[name].(map[string]any)
			required := yStr(spec["required"]) != "false"
			switch strings.ToUpper(yStr(spec["load"])) {
			case "AFTER", "":
				if required {
					d.Depend = append(d.Depend, name)
				} else {
					d.SoftDepend = append(d.SoftDepend, name)
				}
			case "BEFORE":
				d.LoadBefore = append(d.LoadBefore, name)
			}
		}
	} else {
		d.Depend = yStrs(m["depend"])
		d.SoftDepend = yStrs(m["softdepend"])
		d.LoadBefore = yStrs(m["loadbefore"])
	}
	cmds, _ := m["commands"].(map[string]any)
	for _, name := range sortedKeys(cmds) {
		c, _ := cmds[name].(map[string]any)
		d.Commands = append(d.Commands, CommandSpec{
			Plugin:      d.Name,
			Name:        strings.ToLower(name),
			Aliases:     yStrs(c["aliases"]),
			Description: yStr(c["description"]),
			Usage:       yStr(c["usage"]),
			Permission:  yStr(c["permission"]),
		})
	}
	sort.Slice(d.Commands, func(i, j int) bool { return d.Commands[i].Name < d.Commands[j].Name })
	if d.Name == "" {
		return d, errors.New("descriptor has no name")
	}
	if d.Main == "" {
		return d, errors.New("descriptor has no main")
	}
	if strings.ContainsAny(d.Name, " /\\") || d.Name == "." || d.Name == ".." {
		return d, fmt.Errorf("invalid plugin name %q", d.Name)
	}
	return d, nil
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func yStr(v any) string {
	s, _ := v.(string)
	return s
}

func yStrs(v any) []string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// DiscoverPlugins reads every *.jar in dir and returns the loadable plugins in
// load order (depend, softdepend and loadbefore respected). Problems with
// individual jars are returned alongside the usable plugins.
func DiscoverPlugins(dir string) ([]PluginDesc, []error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.jar"))
	if err != nil {
		return nil, []error{err}
	}
	sort.Strings(paths)
	var errs []error
	byName := map[string]PluginDesc{}
	for _, p := range paths {
		d, err := ReadPluginJar(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := byName[d.Name]; dup {
			errs = append(errs, fmt.Errorf("plugin %s: duplicate name %q (also in %s)", p, d.Name, prev.File))
			continue
		}
		byName[d.Name] = d
	}
	order, oerrs := loadOrder(byName)
	return order, append(errs, oerrs...)
}

func loadOrder(byName map[string]PluginDesc) ([]PluginDesc, []error) {
	var errs []error
	for changed := true; changed; {
		changed = false
		for _, name := range sortedKeys(anyMap(byName)) {
			for _, dep := range byName[name].Depend {
				if _, ok := byName[dep]; !ok {
					errs = append(errs, fmt.Errorf("plugin %s: missing dependency %s", name, dep))
					delete(byName, name)
					changed = true
					break
				}
			}
		}
	}
	after := map[string][]string{}
	for name, d := range byName {
		for _, dep := range slices.Concat(d.Depend, d.SoftDepend) {
			if _, ok := byName[dep]; ok && dep != name {
				after[name] = append(after[name], dep)
			}
		}
		for _, b := range d.LoadBefore {
			if _, ok := byName[b]; ok && b != name {
				after[b] = append(after[b], name)
			}
		}
	}
	var out []PluginDesc
	state := map[string]int{}
	var visit func(string, []string) bool
	visit = func(n string, stack []string) bool {
		switch state[n] {
		case 1:
			errs = append(errs, fmt.Errorf("plugin dependency cycle: %s -> %s", strings.Join(stack, " -> "), n))
			return false
		case 2:
			return true
		}
		state[n] = 1
		deps := after[n]
		sort.Strings(deps)
		for _, d := range deps {
			if !visit(d, append(stack, n)) {
				// Break the cycle and keep loading in a deterministic order.
				continue
			}
		}
		state[n] = 2
		out = append(out, byName[n])
		return true
	}
	for _, name := range sortedKeys(anyMap(byName)) {
		visit(name, nil)
	}
	return out, errs
}

func anyMap(m map[string]PluginDesc) map[string]any {
	out := make(map[string]any, len(m))
	for k := range m {
		out[k] = nil
	}
	return out
}
