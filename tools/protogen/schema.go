package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

var states = []string{"handshaking", "status", "login", "configuration", "play"}
var dirs = []string{"toClient", "toServer"}

type schema struct {
	Types map[string]any
	SDs   []*stateDir
}

type stateDir struct {
	State, Dir string
	Types      map[string]any
	Packets    []*packetDef
}

func (sd *stateDir) key() string { return sd.State + "." + sd.Dir }

type packetDef struct {
	Name   string
	Mojang string
	Def    any
}

func loadSchema(data []byte) (*schema, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse protocol.json: %w", err)
	}
	types, _ := root["types"].(map[string]any)
	if types == nil {
		return nil, fmt.Errorf("protocol.json: missing types")
	}
	s := &schema{Types: types}
	for _, st := range states {
		sm, _ := root[st].(map[string]any)
		for _, dr := range dirs {
			sd := &stateDir{State: st, Dir: dr, Types: map[string]any{}}
			s.SDs = append(s.SDs, sd)
			dm, _ := sm[dr].(map[string]any)
			tm, _ := dm["types"].(map[string]any)
			if tm == nil {
				continue
			}
			if err := sd.load(tm, types); err != nil {
				return nil, fmt.Errorf("%s: %w", sd.key(), err)
			}
		}
	}
	return s, nil
}

func (sd *stateDir) load(tm, global map[string]any) error {
	for k, v := range tm {
		if k != "packet" && !strings.HasPrefix(k, "packet_") {
			sd.Types[k] = v
		}
	}
	pk, ok := tm["packet"].([]any)
	if !ok {
		return nil
	}
	fields := pk[1].([]any)
	mapper := fields[0].(map[string]any)["type"].([]any)[1].(map[string]any)["mappings"].(map[string]any)
	sw := fields[1].(map[string]any)["type"].([]any)[1].(map[string]any)["fields"].(map[string]any)
	type idName struct {
		id   int
		name string
	}
	var ids []idName
	for hex, name := range mapper {
		id, err := strconv.ParseInt(strings.TrimPrefix(hex, "0x"), 16, 32)
		if err != nil {
			return fmt.Errorf("bad packet id %q", hex)
		}
		ids = append(ids, idName{int(id), name.(string)})
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].id < ids[j].id })
	for i, e := range ids {
		if e.name == "legacy_server_list_ping" {
			continue
		}
		if e.id != i {
			return fmt.Errorf("non-contiguous packet id 0x%02x for %s", e.id, e.name)
		}
		tn, _ := sw[e.name].(string)
		var def any = tn
		ok := tn == "void"
		if !ok {
			def, ok = tm[tn]
		}
		if !ok {
			def, ok = global[tn]
		}
		if !ok {
			return fmt.Errorf("packet %s: type %q not found", e.name, tn)
		}
		mj := mojangNames[sd.key()][e.name]
		if mj == "" {
			mj = e.name
		}
		sd.Packets = append(sd.Packets, &packetDef{Name: e.name, Mojang: mj, Def: def})
	}
	return nil
}

func (s *schema) sd(key string) *stateDir {
	for _, sd := range s.SDs {
		if sd.key() == key {
			return sd
		}
	}
	return nil
}

func (sd *stateDir) index(name string) int {
	for i, p := range sd.Packets {
		if p.Name == name {
			return i
		}
	}
	return -1
}

type patchOp struct {
	SD     string
	Op     string
	Packet string
	After  string
	Mojang string
	Def    string
	Note   string
}

type patch struct {
	Protocol int
	Version  string
	Ops      []patchOp
}

func parseDef(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *schema) apply(p *patch) error {
	for _, op := range p.Ops {
		if err := s.applyOp(op); err != nil {
			return fmt.Errorf("patch %d %s %s %s: %w", p.Protocol, op.Op, op.SD, op.Packet, err)
		}
	}
	return nil
}

func (s *schema) applyOp(op patchOp) error {
	var def any
	if op.Def != "" {
		d, err := parseDef(op.Def)
		if err != nil {
			return err
		}
		def = d
	}
	if op.Op == "type" {
		if op.SD == "" {
			s.Types[op.Packet] = def
			return nil
		}
		sd := s.sd(op.SD)
		if sd == nil {
			return fmt.Errorf("unknown state/direction")
		}
		sd.Types[op.Packet] = def
		return nil
	}
	sd := s.sd(op.SD)
	if sd == nil {
		return fmt.Errorf("unknown state/direction")
	}
	switch op.Op {
	case "replace":
		i := sd.index(op.Packet)
		if i < 0 {
			return fmt.Errorf("packet not found")
		}
		sd.Packets[i].Def = def
	case "insert":
		if sd.index(op.Packet) >= 0 {
			return fmt.Errorf("packet exists")
		}
		i := sd.index(op.After)
		if i < 0 {
			return fmt.Errorf("anchor %q not found", op.After)
		}
		pd := &packetDef{Name: op.Packet, Mojang: op.Mojang, Def: def}
		if pd.Mojang == "" {
			pd.Mojang = op.Packet
		}
		sd.Packets = append(sd.Packets[:i+1], append([]*packetDef{pd}, sd.Packets[i+1:]...)...)
	case "remove":
		i := sd.index(op.Packet)
		if i < 0 {
			return fmt.Errorf("packet not found")
		}
		sd.Packets = append(sd.Packets[:i], sd.Packets[i+1:]...)
	default:
		return fmt.Errorf("unknown op")
	}
	return nil
}

// fieldFix replaces the type of a container field when it still has the
// expected (wrong) type. SD "" targets a global type. Field is a dotted path;
// a "[]" suffix descends into an array's element container.
type fieldFix struct {
	SD, Packet, Field string
	From, To          string
	Note              string
}

func containerField(def any, name string) map[string]any {
	c, ok := def.([]any)
	if !ok || len(c) != 2 || c[0] != "container" {
		return nil
	}
	for _, fe := range c[1].([]any) {
		if fm := fe.(map[string]any); fm["name"] == name {
			return fm
		}
	}
	return nil
}

func (s *schema) applyFixes(fixes []fieldFix) []string {
	var applied []string
	for _, f := range fixes {
		var def any
		if f.SD == "" {
			def = s.Types[f.Packet]
		} else if sd := s.sd(f.SD); sd != nil {
			if i := sd.index(f.Packet); i >= 0 {
				def = sd.Packets[i].Def
			}
		}
		from, err1 := parseDef(f.From)
		to, err2 := parseDef(f.To)
		if err1 != nil || err2 != nil {
			panic(fmt.Sprintf("bad fix %s/%s", f.Packet, f.Field))
		}
		segs := strings.Split(f.Field, ".")
		for i, seg := range segs {
			name := strings.TrimSuffix(seg, "[]")
			fm := containerField(def, name)
			if fm == nil {
				break
			}
			if i == len(segs)-1 {
				if reflect.DeepEqual(fm["type"], from) {
					fm["type"] = to
					applied = append(applied, fmt.Sprintf("%s %s.%s: %s", f.SD, f.Packet, f.Field, f.Note))
				}
				break
			}
			def = fm["type"]
			if strings.HasSuffix(seg, "[]") {
				arr, _ := def.([]any)
				if len(arr) != 2 || arr[0] != "array" {
					break
				}
				def = arr[1].(map[string]any)["type"]
			}
		}
	}
	return applied
}
