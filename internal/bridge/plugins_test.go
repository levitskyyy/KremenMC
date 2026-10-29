package bridge

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const samplePluginYML = `# comment
name: Essentials
main: com.earth2me.essentials.Essentials
version: "2.21.0"
api-version: '1.21'
author: md_5
authors: [zenexer, "snowleo"]
description: >
  Provides an essential,
  core set of commands.
depend: [Vault]
softdepend:
  - LuckPerms
  - WorldEdit # trailing comment
folia-supported: true
libraries:
  - com.example:lib:1.0
commands:
  home:
    description: Teleport home
    usage: /<command> [name]
    aliases: [homes, h]
    permission: essentials.home
  Spawn:
    aliases: spawnpoint
permissions:
  essentials.home:
    default: true
`

func TestParseDescriptor(t *testing.T) {
	d, err := ParseDescriptor(samplePluginYML, false)
	if err != nil {
		t.Fatal(err)
	}
	want := PluginDesc{
		Name:           "Essentials",
		Main:           "com.earth2me.essentials.Essentials",
		Version:        "2.21.0",
		APIVersion:     "1.21",
		Description:    "Provides an essential, core set of commands.\n",
		Authors:        []string{"md_5", "zenexer", "snowleo"},
		Depend:         []string{"Vault"},
		SoftDepend:     []string{"LuckPerms", "WorldEdit"},
		Libraries:      []string{"com.example:lib:1.0"},
		FoliaSupported: true,
		Commands: []CommandSpec{
			{Plugin: "Essentials", Name: "home", Aliases: []string{"homes", "h"}, Description: "Teleport home", Usage: "/<command> [name]", Permission: "essentials.home"},
			{Plugin: "Essentials", Name: "spawn", Aliases: []string{"spawnpoint"}},
		},
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("got  %+v\nwant %+v", d, want)
	}
}

func TestParseDescriptorErrors(t *testing.T) {
	tests := map[string]string{
		"no name":    "main: a.B\n",
		"no main":    "name: X\n",
		"bad name":   "name: a/b\nmain: a.B\n",
		"not a map":  "- a\n- b\n",
		"bad indent": "name: X\nmain: a.B\n   oops: 1\n",
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDescriptor(src, false); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParsePaperDescriptor(t *testing.T) {
	src := `name: Modern
version: 1.0
main: dev.modern.Main
bootstrapper: dev.modern.Boot
api-version: '26.3'
dependencies:
  server:
    Vault:
      load: BEFORE
      required: false
    LuckPerms:
      load: AFTER
      required: true
    Optional:
      required: false
`
	d, err := ParseDescriptor(src, true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Paper || d.Bootstrapper != "dev.modern.Boot" || d.APIVersion != "26.3" {
		t.Fatalf("got %+v", d)
	}
	if !reflect.DeepEqual(d.Depend, []string{"LuckPerms"}) || !reflect.DeepEqual(d.SoftDepend, []string{"Optional"}) || !reflect.DeepEqual(d.LoadBefore, []string{"Vault"}) {
		t.Fatalf("deps: %+v", d)
	}
}

func TestYAMLSubset(t *testing.T) {
	src := `a: 1
b:
- x
- k: v
  k2: v2
-
  nested: true
c: {p: 1, q: [2, 3]}
d: |
  line1
  line2
e: 'it''s'
f: "tab\tquote\""
g:
h: plain value
  continued
`
	v, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"a": "1",
		"b": []any{"x", map[string]any{"k": "v", "k2": "v2"}, map[string]any{"nested": "true"}},
		"c": map[string]any{"p": "1", "q": []any{"2", "3"}},
		"d": "line1\nline2\n",
		"e": "it's",
		"f": "tab\tquote\"",
		"g": nil,
		"h": "plain value continued",
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got  %#v\nwant %#v", v, want)
	}
}

func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func pluginYML(name string, extra ...string) string {
	return "name: " + name + "\nmain: test." + name + "\nversion: 1\n" + strings.Join(extra, "\n") + "\n"
}

func TestDiscoverPlugins(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "a.jar"), map[string]string{"plugin.yml": pluginYML("Alpha", "depend: [Beta]", "softdepend: [Missing]")})
	writeJar(t, filepath.Join(dir, "b.jar"), map[string]string{"plugin.yml": pluginYML("Beta", "softdepend: [Gamma]")})
	writeJar(t, filepath.Join(dir, "c.jar"), map[string]string{"plugin.yml": pluginYML("Gamma", "loadbefore: [Delta]")})
	writeJar(t, filepath.Join(dir, "d.jar"), map[string]string{"plugin.yml": pluginYML("Delta")})
	writeJar(t, filepath.Join(dir, "e.jar"), map[string]string{"plugin.yml": pluginYML("Orphan", "depend: [Nope]")})
	writeJar(t, filepath.Join(dir, "f.jar"), map[string]string{"plugin.yml": pluginYML("Delta")})
	writeJar(t, filepath.Join(dir, "g.jar"), map[string]string{"readme.txt": "no descriptor"})
	writeJar(t, filepath.Join(dir, "h.jar"), map[string]string{
		"paper-plugin.yml": "name: Paperish\nmain: test.P\nversion: 2\nfolia-supported: true\n",
		"plugin.yml":       pluginYML("Paperish", "commands:\n  pp:\n    description: x"),
	})
	os.WriteFile(filepath.Join(dir, "broken.jar"), []byte("not a zip"), 0o644)

	ps, errs := DiscoverPlugins(dir)
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	want := []string{"Gamma", "Beta", "Alpha", "Delta", "Paperish"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("order: got %v want %v", names, want)
	}
	if len(errs) != 4 {
		t.Fatalf("want 4 errors (orphan, duplicate, no descriptor, broken), got %v", errs)
	}
	last := ps[len(ps)-1]
	if !last.Paper || !last.FoliaSupported || len(last.Commands) != 1 || last.Commands[0].Name != "pp" {
		t.Fatalf("paper plugin: %+v", last)
	}
}

func TestLoadOrderCycle(t *testing.T) {
	m := map[string]PluginDesc{
		"A": {Name: "A", SoftDepend: []string{"B"}},
		"B": {Name: "B", SoftDepend: []string{"A"}},
	}
	order, errs := loadOrder(m)
	if len(order) != 2 || len(errs) != 1 {
		t.Fatalf("got %v %v", order, errs)
	}
}
