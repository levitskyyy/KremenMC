// Command transgen builds the per-version metadata embedded by
// internal/translate/meta from a protogen schema dump and minecraft-data files.
//
//	go run ./tools/protogen -ver 1.21.11 -patch none -out /tmp/v774 -dump /tmp/774.json
//	go run ./internal/translate/cmd/transgen -dump /tmp/774.json -mcdata ~/minecraft-data/data/pc/1.21.11 \
//	    -protocol 774 -version 1.21.11 -packs 1.21.11 -data 1.21.11
//
// -mcdata must hold entities.json, sounds.json and loginPacket.json (copied from
// the dataPaths.json locations of that version). -opaque-from replaces the
// item/particle/metadata/command schemas with those of another protocol.json.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// opaque types are rewritten once at the canonical end of the chain, so the
// packet fingerprints treat them as tokens.
var opaque = []string{
	"Slot", "UntrustedSlot", "HashedSlot", "ItemStackTemplate", "SlotComponent", "UntrustedSlotComponent",
	"Particle", "entityMetadata", "command_node", "SlotDisplay", "RecipeDisplay",
}

type meta struct {
	Protocol     int                          `json:"protocol"`
	Version      string                       `json:"version"`
	Packs        []string                     `json:"packs"`
	Data         string                       `json:"data"`
	Packets      map[string]map[string]string `json:"packets"`
	Order        map[string][]string          `json:"order"`
	Opaque       map[string]string            `json:"opaque"`
	Enums        map[string][]string          `json:"enums"`
	Registries   map[string][]string          `json:"registries"`
	MetadataKeys map[string][]string          `json:"metadataKeys"`
	Types        map[string]json.RawMessage   `json:"types"`
}

type dumpSD struct {
	Types   map[string]any `json:"types"`
	Packets []struct {
		ID     int    `json:"id"`
		Mojang string `json:"mojang"`
		Def    any    `json:"def"`
	} `json:"packets"`
}

func main() {
	var (
		dumpPath   = flag.String("dump", "", "protogen -dump output")
		opaqueFrom = flag.String("opaque-from", "", "protocol.json whose opaque type schemas replace the dump's")
		mcdata     = flag.String("mcdata", "", "directory with entities.json, sounds.json, loginPacket.json")
		protocol   = flag.Int("protocol", 0, "protocol number")
		version    = flag.String("version", "", "version name")
		packs      = flag.String("packs", "", "comma-separated minecraft:core known pack versions")
		dataVer    = flag.String("data", "", "internal/data registry version")
		out        = flag.String("out", "internal/translate/meta", "output directory")
	)
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("transgen: ")
	if *dumpPath == "" || *mcdata == "" || *protocol == 0 || *version == "" {
		log.Fatal("-dump, -mcdata, -protocol and -version are required")
	}
	var dump map[string]json.RawMessage
	readJSON(*dumpPath, &dump)
	var types map[string]any
	if err := json.Unmarshal(dump["types"], &types); err != nil {
		log.Fatal(err)
	}
	sds := map[string]*dumpSD{}
	for k, v := range dump {
		if k == "types" {
			continue
		}
		var sd dumpSD
		if err := json.Unmarshal(v, &sd); err != nil {
			log.Fatalf("%s: %v", k, err)
		}
		sds[k] = &sd
	}

	m := &meta{
		Protocol: *protocol, Version: *version, Data: *dataVer,
		Packets: map[string]map[string]string{}, Order: map[string][]string{},
		Opaque: map[string]string{}, Enums: map[string][]string{},
		Registries: map[string][]string{}, MetadataKeys: map[string][]string{},
		Types: map[string]json.RawMessage{},
	}
	for p := range strings.SplitSeq(*packs, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m.Packs = append(m.Packs, p)
		}
	}
	if len(m.Packs) == 0 {
		m.Packs = []string{*version}
	}

	isOpaque := map[string]bool{}
	for _, o := range opaque {
		isOpaque[o] = true
	}
	for key, sd := range sds {
		fp := map[string]string{}
		var order []string
		for _, p := range sd.Packets {
			e := expand(p.Def, types, sd.Types, isOpaque, nil)
			fp[p.Mojang] = hash(e)
			order = append(order, p.Mojang)
		}
		m.Packets[key] = fp
		m.Order[key] = order
	}

	// interpreter schemas: closure of the opaque roots over global and play types
	schemaTypes := map[string]any{}
	for k, v := range types {
		schemaTypes[k] = v
	}
	for _, key := range []string{"play.toServer", "play.toClient"} {
		if sd := sds[key]; sd != nil {
			for k, v := range sd.Types {
				schemaTypes[k] = v
			}
		}
	}
	if *opaqueFrom != "" {
		var pj struct {
			Types map[string]any `json:"types"`
			Play  struct {
				ToClient struct {
					Types map[string]any `json:"types"`
				} `json:"toClient"`
				ToServer struct {
					Types map[string]any `json:"types"`
				} `json:"toServer"`
			} `json:"play"`
		}
		readJSON(*opaqueFrom, &pj)
		src := map[string]any{}
		for k, v := range pj.Types {
			src[k] = v
		}
		for k, v := range pj.Play.ToServer.Types {
			src[k] = v
		}
		for k, v := range pj.Play.ToClient.Types {
			src[k] = v
		}
		seen := map[string]bool{}
		for _, o := range opaque {
			closure(o, src, seen)
		}
		for k := range seen {
			if v, ok := src[k]; ok {
				schemaTypes[k] = v
			}
		}
	}
	seen := map[string]bool{}
	for _, o := range opaque {
		closure(o, schemaTypes, seen)
	}
	for k := range seen {
		v, ok := schemaTypes[k]
		if !ok || v == "native" {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			log.Fatal(err)
		}
		m.Types[k] = b
	}
	for _, o := range opaque {
		if v, ok := schemaTypes[o]; ok {
			m.Opaque[o] = hash(expand(v, schemaTypes, nil, nil, nil))
		}
	}

	m.Enums["component"] = mapperOf(schemaTypes["SlotComponentType"])
	m.Enums["metadata"] = findMapper(schemaTypes["entityMetadataEntry"], "boolean", "byte")
	m.Enums["particle"] = findMapper(schemaTypes["Particle"], "block", "flame")
	m.Enums["parser"] = findMapper(schemaTypes["command_node"], "brigadier:bool", "brigadier:integer")
	if sd := sds["play.toClient"]; sd != nil {
		for _, p := range sd.Packets {
			if p.Mojang == "update_attributes" {
				m.Enums["attribute"] = findMapper(inline(p.Def, schemaTypes, sd.Types, nil), "generic.max_health", "generic.armor")
				if m.Enums["attribute"] == nil {
					m.Enums["attribute"] = findMapper(inline(p.Def, schemaTypes, sd.Types, nil), "max_health", "armor")
				}
			}
			if p.Mojang == "sound" {
				m.Enums["soundSource"] = findMapper(inline(p.Def, schemaTypes, sd.Types, nil), "master", "voice")
			}
		}
	}
	for k, v := range m.Enums {
		if v == nil {
			log.Printf("warning: enum %s not found", k)
			delete(m.Enums, k)
		}
	}

	var sounds []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	readJSON(filepath.Join(*mcdata, "sounds.json"), &sounds)
	sort.Slice(sounds, func(i, j int) bool { return sounds[i].ID < sounds[j].ID })
	for i, s := range sounds {
		if s.ID != sounds[0].ID+i {
			log.Fatalf("sounds.json: id gap at %s", s.Name)
		}
		m.Enums["sound"] = append(m.Enums["sound"], s.Name)
	}

	var ents []struct {
		ID           int      `json:"id"`
		Name         string   `json:"name"`
		MetadataKeys []string `json:"metadataKeys"`
	}
	readJSON(filepath.Join(*mcdata, "entities.json"), &ents)
	sort.Slice(ents, func(i, j int) bool { return ents[i].ID < ents[j].ID })
	for _, e := range ents {
		m.Enums["entity"] = append(m.Enums["entity"], e.Name)
		m.MetadataKeys[e.Name] = e.MetadataKeys
	}

	var login struct {
		DimensionCodec map[string]struct {
			Entries []struct {
				Key string `json:"key"`
			} `json:"entries"`
		} `json:"dimensionCodec"`
	}
	readJSON(filepath.Join(*mcdata, "loginPacket.json"), &login)
	for reg, v := range login.DimensionCodec {
		names := make([]string, len(v.Entries))
		for i, e := range v.Entries {
			names[i] = e.Key
		}
		m.Registries[reg] = names
	}

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(m); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	name := filepath.Join(*out, strconv.Itoa(*protocol)+".json.gz")
	if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("transgen: %s (%d bytes), %d schema types, enums %v\n", name, buf.Len(), len(m.Types), enumSizes(m.Enums))
}

func enumSizes(e map[string][]string) string {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%d ", k, len(e[k]))
	}
	return strings.TrimSpace(sb.String())
}

func readJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
}

func hash(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatal(err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

// expand inlines named types (except opaque ones) and drops names and switch
// paths, so equal results mean equal wire layouts. Mappers keep only their
// underlying type and value count.
func expand(t any, global, local map[string]any, opq map[string]bool, stack []string) any {
	switch v := t.(type) {
	case string:
		if opq[v] {
			return "@" + v
		}
		for _, s := range stack {
			if s == v {
				return "@rec:" + v
			}
		}
		if d, ok := local[v]; ok {
			return expand(d, global, local, opq, append(stack, v))
		}
		if d, ok := global[v]; ok && d != "native" {
			return expand(d, global, local, opq, append(stack, v))
		}
		return v
	case []any:
		if len(v) == 2 {
			if s, ok := v[0].(string); ok && s == "mapper" {
				if o, ok := v[1].(map[string]any); ok {
					mp, _ := o["mappings"].(map[string]any)
					return []any{"mapper", expand(o["type"], global, local, opq, stack), len(mp)}
				}
			}
		}
		out := make([]any, len(v))
		for i := range v {
			out[i] = expand(v[i], global, local, opq, stack)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			if k == "name" || k == "anon" || k == "compareTo" {
				continue
			}
			out[k] = expand(x, global, local, opq, stack)
		}
		return out
	}
	return t
}

// inline expands named types without normalizing anything.
func inline(t any, global, local map[string]any, stack []string) any {
	switch v := t.(type) {
	case string:
		for _, s := range stack {
			if s == v {
				return v
			}
		}
		if d, ok := local[v]; ok {
			return inline(d, global, local, append(stack, v))
		}
		if d, ok := global[v]; ok && d != "native" {
			return inline(d, global, local, append(stack, v))
		}
		return v
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = inline(v[i], global, local, stack)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = inline(x, global, local, stack)
		}
		return out
	}
	return t
}

func closure(name string, types map[string]any, seen map[string]bool) {
	if seen[name] {
		return
	}
	d, ok := types[name]
	if !ok {
		return
	}
	seen[name] = true
	var walk func(any)
	walk = func(x any) {
		switch v := x.(type) {
		case string:
			if _, ok := types[v]; ok {
				closure(v, types, seen)
			}
		case []any:
			for _, y := range v {
				walk(y)
			}
		case map[string]any:
			for k, y := range v {
				if k == "mappings" {
					continue
				}
				walk(y)
			}
		}
	}
	walk(d)
}

func mapperOf(t any) []string {
	v, ok := t.([]any)
	if !ok || len(v) != 2 || v[0] != "mapper" {
		return nil
	}
	o, _ := v[1].(map[string]any)
	mp, _ := o["mappings"].(map[string]any)
	out := make([]string, len(mp))
	for k, name := range mp {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(out) {
			return nil
		}
		out[i], _ = name.(string)
	}
	return out
}

// findMapper returns the first mapper in t whose values include all of want.
func findMapper(t any, want ...string) []string {
	if l := mapperOf(t); l != nil {
		ok := true
		for _, w := range want {
			found := false
			for _, n := range l {
				if n == w {
					found = true
					break
				}
			}
			ok = ok && found
		}
		if ok {
			return l
		}
	}
	switch v := t.(type) {
	case []any:
		for _, y := range v {
			if r := findMapper(y, want...); r != nil {
				return r
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if r := findMapper(v[k], want...); r != nil {
				return r
			}
		}
	}
	return nil
}
