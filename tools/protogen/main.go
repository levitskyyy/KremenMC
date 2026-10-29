// Command protogen generates packet codecs from minecraft-data protocol.json.
//
//	go run ./tools/protogen                       # 26.1 schema patched to 777 -> internal/proto/packets
//	go run ./tools/protogen -ver 1.21.4 -patch none -out internal/translate/v769 -pkg v769
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	var (
		ver      = flag.String("ver", "26.1", "minecraft-data pc version")
		patchArg = flag.String("patch", "auto", `protocol patch to apply ("777", "none", or "auto": 777 when -ver is 26.1)`)
		out      = flag.String("out", "internal/proto/packets", "output directory")
		pkg      = flag.String("pkg", "", "package name (default: base of -out)")
		in       = flag.String("in", "", "local protocol.json instead of fetching")
		protoNum = flag.Int("protocol", 0, "protocol number when using -in without a patch")
		data     = flag.String("data", "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data", "minecraft-data base URL")
		protoImp = flag.String("proto-import", "github.com/levitskyyy/KremenMC/internal/proto", "import path of the proto package")
		quiet    = flag.Bool("q", false, "do not print the raw-packet report")
		dump     = flag.String("dump", "", "also write the patched schema as JSON to this file")
	)
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("protogen: ")

	if *pkg == "" {
		parts := strings.Split(strings.TrimRight(*out, "/"), "/")
		*pkg = parts[len(parts)-1]
	}
	var p *patch
	switch *patchArg {
	case "777":
		p = patch777
	case "auto":
		if *ver == "26.1" {
			p = patch777
		}
	case "none", "":
	default:
		log.Fatalf("unknown patch %q", *patchArg)
	}

	src, protocol, err := load(*in, *data, *ver)
	if err != nil {
		log.Fatal(err)
	}
	if *protoNum != 0 {
		protocol = *protoNum
	}
	s, err := loadSchema(src)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range s.applyFixes(schemaFixes) {
		if !*quiet {
			fmt.Println("fix:", f)
		}
	}
	version := *ver
	if p != nil {
		if err := s.apply(p); err != nil {
			log.Fatal(err)
		}
		protocol, version = p.Protocol, p.Version
		if p == patch777 {
			if err := checkOrder(s, expectedOrder777); err != nil {
				log.Fatal(err)
			}
		}
	}
	if *dump != "" {
		if err := dumpSchema(s, *dump); err != nil {
			log.Fatal(err)
		}
	}
	g := &gen{s: s, taken: map[string]bool{}, cache: map[string]*cacheEntry{}}
	cfg := genConfig{pkg: *pkg, out: *out, protoImport: *protoImp, version: version, mcdataVersion: *ver, protocol: protocol}
	if err := g.generate(cfg); err != nil {
		log.Fatal(err)
	}
	total := 0
	for _, sd := range s.SDs {
		total += len(sd.Packets)
	}
	if !*quiet {
		for _, r := range g.report {
			fmt.Println(r)
		}
	}
	fmt.Printf("protogen: %d packets (%d with raw tails) for %s / protocol %d -> %s\n", total, len(g.report), version, protocol, *out)
}

func checkOrder(s *schema, want map[string][]string) error {
	for _, sd := range s.SDs {
		exp := want[sd.key()]
		if len(exp) != len(sd.Packets) {
			return fmt.Errorf("%s: %d packets, expected %d", sd.key(), len(sd.Packets), len(exp))
		}
		for i, p := range sd.Packets {
			if p.Mojang != exp[i] {
				return fmt.Errorf("%s: id 0x%02X is %s (%s), expected %s", sd.key(), i, p.Mojang, p.Name, exp[i])
			}
		}
	}
	return nil
}

func load(in, base, ver string) ([]byte, int, error) {
	if in != "" {
		b, err := os.ReadFile(in)
		return b, 0, err
	}
	paths, err := fetch(base + "/dataPaths.json")
	if err != nil {
		return nil, 0, err
	}
	var dp struct {
		PC map[string]map[string]string `json:"pc"`
	}
	if err := json.Unmarshal(paths, &dp); err != nil {
		return nil, 0, fmt.Errorf("dataPaths.json: %w", err)
	}
	v, ok := dp.PC[ver]
	if !ok {
		return nil, 0, fmt.Errorf("version %s not in dataPaths.json", ver)
	}
	src, err := fetch(base + "/" + v["protocol"] + "/protocol.json")
	if err != nil {
		return nil, 0, err
	}
	protocol := 0
	if vb, err := fetch(base + "/" + v["version"] + "/version.json"); err == nil {
		var vj struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(vb, &vj) == nil {
			protocol = vj.Version
		}
	}
	return src, protocol, nil
}

func fetch(url string) ([]byte, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func dumpSchema(s *schema, path string) error {
	type pk struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Mojang string `json:"mojang"`
		Def    any    `json:"def"`
	}
	out := map[string]any{"types": s.Types}
	for _, sd := range s.SDs {
		var l []pk
		for i, p := range sd.Packets {
			l = append(l, pk{i, p.Name, p.Mojang, p.Def})
		}
		out[sd.key()] = map[string]any{"types": sd.Types, "packets": l}
	}
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
