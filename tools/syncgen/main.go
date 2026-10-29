// Command syncgen extracts the synchronized dynamic registries and all network
// tags from the official server jar into internal/net/regsync/sync.json.gz.
//
// Inputs are the unbundled server jar (META-INF/versions/<ver>/server-<ver>.jar
// inside the downloaded server.jar) and the data generator registry report:
//
//	java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports
//	go run ./tools/syncgen -jar server-26.3.jar -registries generated/reports/registries.json
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// RegistryDataLoader.SYNCHRONIZED_REGISTRIES order.
var synced = []string{
	"worldgen/biome",
	"chat_type",
	"trim_pattern",
	"trim_material",
	"wolf_variant",
	"wolf_sound_variant",
	"pig_variant",
	"pig_sound_variant",
	"frog_variant",
	"cat_variant",
	"cat_sound_variant",
	"cow_sound_variant",
	"cow_variant",
	"chicken_sound_variant",
	"chicken_variant",
	"zombie_nautilus_variant",
	"painting_variant",
	"sulfur_cube_archetype",
	"dimension_type",
	"damage_type",
	"banner_pattern",
	"enchantment",
	"jukebox_song",
	"instrument",
	"test_environment",
	"test_instance",
	"dialog",
	"world_clock",
	"timeline",
	"decorated_pot_pattern",
	"block_transformer",
	"worldgen/block_state_provider",
}

type registry struct {
	Name    string
	Entries []string
}

type tag struct {
	Name string
	IDs  []int32
}

type tagGroup struct {
	Registry string
	Tags     []tag
}

type output struct {
	Registries []registry
	Tags       []tagGroup
}

type report map[string]struct {
	Entries map[string]struct {
		ProtocolID int32 `json:"protocol_id"`
	} `json:"entries"`
}

func main() {
	jarPath := flag.String("jar", "", "unbundled server jar (server-<ver>.jar)")
	regPath := flag.String("registries", "", "data generator reports/registries.json")
	out := flag.String("out", "internal/net/regsync/sync.json.gz", "output file")
	flag.Parse()
	if *jarPath == "" || *regPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*jarPath, *regPath, *out); err != nil {
		log.Fatal(err)
	}
}

func run(jarPath, regPath, outPath string) error {
	raw, err := os.ReadFile(regPath)
	if err != nil {
		return err
	}
	var rep report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fmt.Errorf("registries report: %w", err)
	}
	ids := map[string]map[string]int32{}
	for name, r := range rep {
		m := make(map[string]int32, len(r.Entries))
		for e, v := range r.Entries {
			m[e] = v.ProtocolID
		}
		ids[name] = m
	}
	if err := checkCanonical(ids); err != nil {
		return err
	}

	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	entries := map[string][]string{}
	tagFiles := map[string]map[string]*zip.File{}
	syncedSet := map[string]bool{}
	for _, s := range synced {
		syncedSet[s] = true
	}
	var known []string
	for name := range ids {
		known = append(known, strings.TrimPrefix(name, "minecraft:"))
	}
	known = append(known, synced...)
	skipped := map[string]int{}
	for _, f := range zr.File {
		p, ok := strings.CutPrefix(f.Name, "data/minecraft/")
		if !ok || !strings.HasSuffix(p, ".json") {
			continue
		}
		p = strings.TrimSuffix(p, ".json")
		if t, ok := strings.CutPrefix(p, "tags/"); ok {
			reg, rest := splitRegistry(t, known)
			if reg == "" {
				reg = guessRegistry(t)
				skipped[reg]++
				continue
			}
			if tagFiles[reg] == nil {
				tagFiles[reg] = map[string]*zip.File{}
			}
			tagFiles[reg]["minecraft:"+rest] = f
			continue
		}
		reg, rest := splitRegistry(p, synced)
		if reg != "" {
			entries[reg] = append(entries[reg], rest)
		}
	}

	var o output
	for _, s := range synced {
		es := entries[s]
		sort.Strings(es)
		if s == "worldgen/biome" {
			if es, err = biomeOrder(es); err != nil {
				return err
			}
		}
		full := make([]string, len(es))
		m := make(map[string]int32, len(es))
		for i, e := range es {
			full[i] = "minecraft:" + e
			m[full[i]] = int32(i)
		}
		if len(es) == 0 {
			log.Printf("note: minecraft:%s is empty", s)
		}
		ids["minecraft:"+s] = m
		o.Registries = append(o.Registries, registry{Name: "minecraft:" + s, Entries: full})
	}

	regs := make([]string, 0, len(tagFiles))
	for r := range tagFiles {
		regs = append(regs, r)
	}
	sort.Strings(regs)
	ntags := 0
	for _, r := range regs {
		g, err := resolveTags("minecraft:"+r, tagFiles[r], ids["minecraft:"+r])
		if err != nil {
			return err
		}
		ntags += len(g.Tags)
		o.Tags = append(o.Tags, g)
	}
	for r, n := range skipped {
		log.Printf("skipped %d tags of non-synchronized registry minecraft:%s", n, r)
	}

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(o); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	nent := 0
	for _, r := range o.Registries {
		nent += len(r.Entries)
	}
	log.Printf("wrote %s (%d bytes): %d registries, %d entries, %d tag groups, %d tags",
		outPath, buf.Len(), len(o.Registries), nent, len(o.Tags), ntags)
	return nil
}

// splitRegistry matches the longest registry path that prefixes p.
func splitRegistry(p string, regs []string) (reg, rest string) {
	for _, r := range regs {
		if len(r) > len(reg) && strings.HasPrefix(p, r+"/") {
			reg = r
		}
	}
	if reg == "" {
		return "", ""
	}
	return reg, p[len(reg)+1:]
}

func guessRegistry(p string) string {
	parts := strings.SplitN(p, "/", 3)
	if parts[0] == "worldgen" && len(parts) > 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func biomeOrder(jar []string) ([]string, error) {
	in := map[string]bool{}
	for _, b := range jar {
		in[b] = true
	}
	var out, missing []string
	for _, b := range data.Canonical().Biomes {
		out = append(out, b.Name)
		if !in[b.Name] {
			missing = append(missing, b.Name)
		}
		delete(in, b.Name)
	}
	if len(missing) > 0 || len(in) > 0 {
		var extra []string
		for b := range in {
			extra = append(extra, b)
		}
		sort.Strings(extra)
		return nil, fmt.Errorf("biome mismatch: in internal/data only %v, in jar only %v", missing, extra)
	}
	return out, nil
}

func checkCanonical(ids map[string]map[string]int32) error {
	c := data.Canonical()
	var bad []string
	check := func(reg string, n int, lookup func(string) (int32, bool)) {
		m := ids[reg]
		if len(m) != n {
			bad = append(bad, fmt.Sprintf("%s: report has %d entries, internal/data %d", reg, len(m), n))
		}
		for name, id := range m {
			got, ok := lookup(name)
			if !ok {
				bad = append(bad, fmt.Sprintf("%s: %s missing from internal/data", reg, name))
			} else if got != id {
				bad = append(bad, fmt.Sprintf("%s: %s is %d, internal/data has %d", reg, name, id, got))
			}
		}
	}
	check("minecraft:block", len(c.Blocks), func(n string) (int32, bool) {
		b, ok := c.BlockByName(n)
		if !ok {
			return 0, false
		}
		return int32(b.ID), true
	})
	check("minecraft:item", len(c.Items), func(n string) (int32, bool) {
		id, ok := c.ItemByName(n)
		return int32(id), ok
	})
	check("minecraft:entity_type", len(c.Entities), func(n string) (int32, bool) {
		e, ok := c.EntityByName(n)
		if !ok {
			return 0, false
		}
		return int32(e.ID), true
	})
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("registry report disagrees with internal/data:\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}

type tagFile struct {
	Values []json.RawMessage `json:"values"`
}

type tagValue struct {
	ID       string `json:"id"`
	Required *bool  `json:"required"`
}

func resolveTags(reg string, files map[string]*zip.File, ids map[string]int32) (tagGroup, error) {
	if ids == nil {
		return tagGroup{}, fmt.Errorf("%s: no entry IDs", reg)
	}
	parsed := map[string][]tagValue{}
	for name, f := range files {
		vals, err := readTag(f)
		if err != nil {
			return tagGroup{}, fmt.Errorf("%s tag %s: %w", reg, name, err)
		}
		parsed[name] = vals
	}
	done := map[string][]int32{}
	active := map[string]bool{}
	var resolve func(name string) ([]int32, error)
	resolve = func(name string) ([]int32, error) {
		if r, ok := done[name]; ok {
			return r, nil
		}
		if active[name] {
			return nil, fmt.Errorf("%s: tag cycle through #%s", reg, name)
		}
		active[name] = true
		defer delete(active, name)
		seen := map[int32]bool{}
		out := []int32{}
		add := func(id int32) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		for _, v := range parsed[name] {
			required := v.Required == nil || *v.Required
			if ref, ok := strings.CutPrefix(v.ID, "#"); ok {
				ref = qualify(ref)
				if _, ok := parsed[ref]; !ok {
					if required {
						return nil, fmt.Errorf("%s #%s: missing tag #%s", reg, name, ref)
					}
					continue
				}
				sub, err := resolve(ref)
				if err != nil {
					return nil, err
				}
				for _, id := range sub {
					add(id)
				}
				continue
			}
			id, ok := ids[qualify(v.ID)]
			if !ok {
				if required {
					return nil, fmt.Errorf("%s #%s: unknown entry %s", reg, name, v.ID)
				}
				continue
			}
			add(id)
		}
		done[name] = out
		return out, nil
	}
	names := make([]string, 0, len(parsed))
	for n := range parsed {
		names = append(names, n)
	}
	sort.Strings(names)
	g := tagGroup{Registry: reg}
	for _, n := range names {
		r, err := resolve(n)
		if err != nil {
			return tagGroup{}, err
		}
		g.Tags = append(g.Tags, tag{Name: n, IDs: r})
	}
	return g, nil
}

func readTag(f *zip.File) ([]tagValue, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var tf tagFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return nil, err
	}
	out := make([]tagValue, 0, len(tf.Values))
	for _, r := range tf.Values {
		var v tagValue
		if err := json.Unmarshal(r, &v.ID); err != nil {
			if err := json.Unmarshal(r, &v); err != nil {
				return nil, err
			}
		}
		if v.ID == "" {
			return nil, fmt.Errorf("bad value %s", r)
		}
		out = append(out, v)
	}
	return out, nil
}

func qualify(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}
