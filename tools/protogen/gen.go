package main

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type kind int

const (
	kPrim kind = iota
	kString
	kBytes
	kFixedBytes
	kRest
	kUUID
	kPos
	kNative
	kOpt
	kArray
	kStruct
	kBitfield
	kHolder
	kIDSet
	kVoid
)

type node struct {
	k       kind
	prim    string
	base    string
	goType  string
	n       int
	elem    *node
	st      *structT
	parts   []bitPart
	mapping map[string]string
	flags   []string
	min     int
}

type bitPart struct {
	name, goName string
	size         int
	signed       bool
	goType       string
}

type memberInfo struct {
	goName string
	t      *node
	part   *bitPart
}

type structT struct {
	goName  string
	members []*member
	byName  map[string]*memberInfo
	fields  map[string]bool
	named   bool
	file    string
}

type member struct {
	goName  string
	t       *node
	sw      *switchT
	parts   []bitPart
	raw     bool
	rawFrom string
}

type switchT struct {
	cmp   cmpRef
	cases []*swCase
}

type swCase struct {
	keys    []string
	def     bool
	void    bool
	t       *node
	goName  string
	members []*member
}

type cmpRef struct {
	up      int
	sel     string
	kind    string
	bit     int
	mapping map[string]string
}

type flagType struct {
	goName string
	base   string
	flags  []string
	file   string
}

type cacheEntry struct {
	n   *node
	err error
}

type gen struct {
	s       *schema
	taken   map[string]bool
	cache   map[string]*cacheEntry
	structs []*structT
	flagTs  []*flagType
	report  []string
}

var errUnsupported = errors.New("unsupported")

type rctx struct {
	sd    *stateDir
	owner string
	field string
	hint  string
	file  string
	scope *rscope
}

type rscope struct {
	parent *rscope
	st     *structT
}

func (c rctx) with(field, hint string) rctx {
	c.field = field
	c.hint = hint
	return c
}

var prims = map[string][3]string{
	"varint":  {"VarInt", "int32", "1"},
	"varlong": {"VarLong", "int64", "1"},
	"u8":      {"U8", "uint8", "1"},
	"i8":      {"I8", "int8", "1"},
	"u16":     {"U16", "uint16", "2"},
	"i16":     {"I16", "int16", "2"},
	"u32":     {"U32", "uint32", "4"},
	"i32":     {"I32", "int32", "4"},
	"u64":     {"U64", "uint64", "8"},
	"i64":     {"I64", "int64", "8"},
	"f32":     {"F32", "float32", "4"},
	"f64":     {"F64", "float64", "8"},
	"bool":    {"Bool", "bool", "1"},
}

var initialisms = map[string]string{
	"id": "ID", "ids": "IDs", "uuid": "UUID", "uuids": "UUIDs", "urls": "URLs", "url": "URL", "nbt": "NBT", "json": "JSON", "ui": "UI", "ip": "IP", "xp": "XP",
}

func words(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' || r == '.' || r == ':' || r == '/' }) {
		rs := []rune(part)
		start := 0
		for i := 1; i < len(rs); i++ {
			lowerToUpper := unicode.IsLower(rs[i-1]) && unicode.IsUpper(rs[i])
			acronymEnd := unicode.IsUpper(rs[i-1]) && unicode.IsUpper(rs[i]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if lowerToUpper || acronymEnd {
				out = append(out, string(rs[start:i]))
				start = i
			}
		}
		out = append(out, string(rs[start:]))
	}
	return out
}

func camel(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		if in, ok := initialisms[strings.ToLower(w)]; ok {
			b.WriteString(in)
			continue
		}
		rs := []rune(w)
		rs[0] = unicode.ToUpper(rs[0])
		b.WriteString(string(rs))
	}
	r := b.String()
	if r == "" || !unicode.IsLetter([]rune(r)[0]) {
		r = "X" + r
	}
	return r
}

func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"), strings.HasSuffix(s, "zes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "ss"), strings.HasSuffix(s, "us"):
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	}
	return s + "Entry"
}

func (g *gen) uniq(name string) string {
	n := name
	for i := 2; g.taken[n]; i++ {
		n = name + strconv.Itoa(i)
	}
	g.taken[n] = true
	return n
}

func (st *structT) field(name string) string {
	n := name
	for i := 2; st.fields[n]; i++ {
		n = name + strconv.Itoa(i)
	}
	st.fields[n] = true
	return n
}

func (g *gen) newStruct(goName, file string, named bool) *structT {
	st := &structT{goName: g.uniq(goName), byName: map[string]*memberInfo{}, fields: map[string]bool{}, named: named, file: file}
	g.structs = append(g.structs, st)
	return st
}

type snapshot struct{ structs, flags int }

func (g *gen) snap() snapshot { return snapshot{len(g.structs), len(g.flagTs)} }

func (g *gen) rollback(s snapshot) {
	for _, st := range g.structs[s.structs:] {
		delete(g.taken, st.goName)
	}
	for _, f := range g.flagTs[s.flags:] {
		delete(g.taken, f.goName)
	}
	g.structs = g.structs[:s.structs]
	g.flagTs = g.flagTs[:s.flags]
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func (g *gen) lookup(sd *stateDir, name string) (any, string, bool) {
	if sd != nil {
		if d, ok := sd.Types[name]; ok {
			return d, sd.key() + ":" + name, true
		}
	}
	d, ok := g.s.Types[name]
	return d, ":" + name, ok
}

func (g *gen) resolve(t any, c rctx) (*node, error) {
	switch x := t.(type) {
	case string:
		return g.resolveName(x, c)
	case []any:
		if len(x) != 2 {
			return nil, fmt.Errorf("%w: malformed type", errUnsupported)
		}
		kindName, _ := x[0].(string)
		args := x[1]
		switch kindName {
		case "container":
			list, _ := args.([]any)
			st := g.newStruct(c.hint, c.file, false)
			if err := g.resolveFields(list, st, c, &rscope{parent: c.scope, st: st}); err != nil {
				return nil, err
			}
			return &node{k: kStruct, goType: st.goName, st: st, min: structMin(st)}, nil
		case "array":
			a := asMap(args)
			if a["countType"] != "varint" {
				return nil, fmt.Errorf("%w: array count %v", errUnsupported, a["count"])
			}
			elem, err := g.resolve(a["type"], c.with(c.field, singular(c.hint)))
			if err != nil {
				return nil, err
			}
			if elem.k == kPrim && elem.prim == "U8" && elem.goType == "uint8" {
				return &node{k: kBytes, goType: "[]byte", min: 1}, nil
			}
			return &node{k: kArray, goType: "[]" + elem.goType, elem: elem, min: 1}, nil
		case "option":
			elem, err := g.resolve(args, c)
			if err != nil {
				return nil, err
			}
			return &node{k: kOpt, goType: "proto.Opt[" + elem.goType + "]", elem: elem, min: 1}, nil
		case "mapper":
			a := asMap(args)
			base, err := g.resolve(a["type"], c)
			if err != nil {
				return nil, err
			}
			if base.k != kPrim {
				return nil, fmt.Errorf("%w: mapper over %v", errUnsupported, a["type"])
			}
			n := *base
			n.mapping = map[string]string{}
			for k, v := range asMap(a["mappings"]) {
				n.mapping[fmt.Sprint(v)] = k
			}
			return &n, nil
		case "bitflags":
			a := asMap(args)
			return g.bitflags(a, c.hint, c.file)
		case "bitfield":
			list, _ := args.([]any)
			parts, err := bitParts(list)
			if err != nil {
				return nil, err
			}
			st := g.newStruct(c.hint, c.file, false)
			st.members = append(st.members, &member{parts: parts})
			return &node{k: kBitfield, goType: st.goName, parts: parts, st: st, min: bitWidth(parts) / 8}, nil
		case "buffer":
			a := asMap(args)
			if a["countType"] == "varint" {
				return &node{k: kBytes, goType: "[]byte", min: 1}, nil
			}
			if n, ok := a["count"].(float64); ok {
				return &node{k: kFixedBytes, goType: fmt.Sprintf("[%d]byte", int(n)), n: int(n), min: int(n)}, nil
			}
			return nil, fmt.Errorf("%w: buffer %v", errUnsupported, a)
		case "pstring":
			if asMap(args)["countType"] == "varint" {
				return &node{k: kString, goType: "string", min: 1}, nil
			}
			return nil, fmt.Errorf("%w: pstring", errUnsupported)
		case "registryEntryHolder":
			a := asMap(args)
			ow := asMap(a["otherwise"])
			elem, err := g.resolve(ow["type"], c.with(c.field, c.hint+camel(fmt.Sprint(ow["name"]))))
			if err != nil {
				return nil, err
			}
			return &node{k: kHolder, goType: "proto.Holder[" + elem.goType + "]", elem: elem, min: 1}, nil
		case "switch":
			if c.scope == nil {
				return nil, fmt.Errorf("%w: switch outside container", errUnsupported)
			}
			st := g.newStruct(c.hint, c.file, false)
			sw, err := g.resolveSwitch(asMap(args), c, c.scope, "Value", st, false)
			if err != nil {
				return nil, err
			}
			sw.cmp.up++
			st.members = append(st.members, &member{sw: sw})
			return &node{k: kStruct, goType: st.goName, st: st}, nil
		case "registryEntryHolderSet":
			a := asMap(args)
			if asMap(a["base"])["type"] != "string" || asMap(a["otherwise"])["type"] != "varint" {
				return nil, fmt.Errorf("%w: holder set", errUnsupported)
			}
			return &node{k: kIDSet, goType: "proto.IDSet", min: 1}, nil
		}
		return nil, fmt.Errorf("%w: %s", errUnsupported, kindName)
	}
	return nil, fmt.Errorf("%w: type %T", errUnsupported, t)
}

func bitParts(list []any) ([]bitPart, error) {
	var parts []bitPart
	for _, e := range list {
		m := asMap(e)
		size := int(m["size"].(float64))
		signed, _ := m["signed"].(bool)
		p := bitPart{name: m["name"].(string), goName: camel(m["name"].(string)), size: size, signed: signed}
		switch {
		case size == 1 && !signed:
			p.goType = "bool"
		case signed && size <= 32:
			p.goType = "int32"
		case signed:
			p.goType = "int64"
		case size <= 32:
			p.goType = "uint32"
		default:
			p.goType = "uint64"
		}
		parts = append(parts, p)
	}
	switch bitWidth(parts) {
	case 8, 16, 32, 64:
		return parts, nil
	}
	return nil, fmt.Errorf("%w: bitfield width %d", errUnsupported, bitWidth(parts))
}

func bitWidth(parts []bitPart) int {
	w := 0
	for _, p := range parts {
		w += p.size
	}
	return w
}

func (g *gen) bitflags(a map[string]any, goName, file string) (*node, error) {
	p, ok := prims[fmt.Sprint(a["type"])]
	if !ok {
		return nil, fmt.Errorf("%w: bitflags base %v", errUnsupported, a["type"])
	}
	var flags []string
	for _, f := range a["flags"].([]any) {
		flags = append(flags, f.(string))
	}
	ft := &flagType{goName: g.uniq(goName), base: p[1], flags: flags, file: file}
	g.flagTs = append(g.flagTs, ft)
	min, _ := strconv.Atoi(p[2])
	return &node{k: kPrim, prim: p[0], base: p[1], goType: ft.goName, flags: flags, min: min}, nil
}

func (g *gen) resolveName(name string, c rctx) (*node, error) {
	if p, ok := prims[name]; ok {
		min, _ := strconv.Atoi(p[2])
		return &node{k: kPrim, prim: p[0], base: p[1], goType: p[1], min: min}, nil
	}
	switch name {
	case "string":
		return &node{k: kString, goType: "string", min: 1}, nil
	case "restBuffer":
		return &node{k: kRest, goType: "[]byte"}, nil
	case "void":
		return &node{k: kVoid}, nil
	case "UUID":
		return &node{k: kUUID, goType: "[16]byte", min: 16}, nil
	case "position":
		return &node{k: kPos, goType: "proto.BlockPos", min: 8}, nil
	case "ByteArray":
		return &node{k: kBytes, goType: "[]byte", min: 1}, nil
	case "anonymousNbt":
		if rawNBTFields[c.owner+"."+c.field] {
			return &node{k: kNative, goType: "proto.RawNBT", min: 1}, nil
		}
		return &node{k: kNative, goType: "proto.Text", min: 1}, nil
	}
	if gt, ok := nativeTypes[name]; ok {
		return &node{k: kNative, goType: gt, min: 1}, nil
	}
	if opaqueTypes[name] {
		return nil, fmt.Errorf("%w: opaque %s", errUnsupported, name)
	}
	def, key, ok := g.lookup(c.sd, name)
	if !ok {
		return nil, fmt.Errorf("%w: unknown type %s", errUnsupported, name)
	}
	if def == "native" {
		return nil, fmt.Errorf("%w: native %s", errUnsupported, name)
	}
	if ce, ok := g.cache[key]; ok {
		return ce.n, ce.err
	}
	nc := rctx{sd: c.sd, owner: name, hint: camel(name), file: "types"}
	if key[0] == ':' {
		nc.sd = nil
	}
	arr, _ := def.([]any)
	s := g.snap()
	if len(arr) == 2 && arr[0] == "container" {
		st := g.newStruct(camel(name), "types", true)
		n := &node{k: kStruct, goType: st.goName, st: st, min: 1}
		ce := &cacheEntry{n: n}
		g.cache[key] = ce
		list, _ := arr[1].([]any)
		if err := g.resolveFields(list, st, nc, &rscope{st: st}); err != nil {
			g.rollback(s)
			ce.n, ce.err = nil, fmt.Errorf("%s: %w", name, err)
			return nil, ce.err
		}
		n.min = structMin(st)
		return n, nil
	}
	if len(arr) == 2 && arr[0] == "bitflags" {
		n, err := g.bitflags(asMap(arr[1]), camel(name), "types")
		g.cache[key] = &cacheEntry{n: n, err: err}
		return n, err
	}
	ce := &cacheEntry{}
	g.cache[key] = ce
	n, err := g.resolve(def, nc)
	if err != nil {
		g.rollback(s)
		err = fmt.Errorf("%s: %w", name, err)
	}
	ce.n, ce.err = n, err
	return n, err
}

func structMin(st *structT) int {
	m := 0
	for _, mb := range st.members {
		switch {
		case mb.t != nil:
			m += mb.t.min
		case mb.parts != nil:
			m += bitWidth(mb.parts) / 8
		}
	}
	return m
}

func (g *gen) resolveFields(list []any, st *structT, c rctx, sc *rscope) error {
	for _, e := range list {
		if err := g.resolveField(asMap(e), st, c, sc); err != nil {
			return err
		}
	}
	return nil
}

func (g *gen) resolveField(f map[string]any, st *structT, c rctx, sc *rscope) error {
	name, _ := f["name"].(string)
	typ := f["type"]
	if anon, _ := f["anon"].(bool); anon {
		arr, _ := typ.([]any)
		if len(arr) == 2 && arr[0] == "container" {
			list, _ := arr[1].([]any)
			return g.resolveFields(list, st, c, sc)
		}
		if len(arr) == 2 && arr[0] == "switch" {
			sw, err := g.resolveSwitch(asMap(arr[1]), c, sc, "", st, true)
			if err != nil {
				return err
			}
			st.members = append(st.members, &member{sw: sw})
			return nil
		}
		if len(arr) == 2 && arr[0] == "bitfield" {
			list, _ := arr[1].([]any)
			parts, err := bitParts(list)
			if err != nil {
				return err
			}
			for i := range parts {
				parts[i].goName = st.field(parts[i].goName)
				st.byName[parts[i].name] = &memberInfo{goName: parts[i].goName, part: &parts[i]}
			}
			st.members = append(st.members, &member{parts: parts})
			return nil
		}
		return fmt.Errorf("%w: anonymous %v", errUnsupported, typ)
	}
	goName := camel(name)
	if rn, ok := fieldRenames[c.owner+"."+name]; ok {
		goName = rn
	}
	fc := c.with(name, st.goName+goName)
	fc.scope = sc
	if arr, ok := typ.([]any); ok && len(arr) == 2 && arr[0] == "switch" {
		sw, err := g.resolveSwitch(asMap(arr[1]), fc, sc, goName, st, false)
		if err != nil {
			return err
		}
		st.members = append(st.members, &member{sw: sw})
		return nil
	}
	n, err := g.resolve(typ, fc)
	if err != nil {
		return err
	}
	if n.k == kVoid {
		return nil
	}
	if n.k == kRest {
		goName = "Data"
		if name != "" {
			goName = camel(name)
		}
	}
	gn := st.field(goName)
	st.members = append(st.members, &member{goName: gn, t: n})
	st.byName[name] = &memberInfo{goName: gn, t: n}
	return nil
}

func (g *gen) resolveCmp(path string, sc *rscope) (cmpRef, error) {
	var ref cmpRef
	segs := strings.Split(path, "/")
	for len(segs) > 0 && segs[0] == ".." {
		if sc.parent == nil {
			return ref, fmt.Errorf("%w: compareTo %s escapes type", errUnsupported, path)
		}
		sc = sc.parent
		ref.up++
		segs = segs[1:]
	}
	if len(segs) == 0 {
		return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
	}
	mi := sc.st.byName[segs[0]]
	if mi == nil {
		return ref, fmt.Errorf("%w: compareTo %s not found", errUnsupported, path)
	}
	ref.sel = "." + mi.goName
	for _, s := range segs[1:] {
		switch {
		case mi.part != nil:
			return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
		case mi.t.k == kBitfield:
			found := false
			for i := range mi.t.parts {
				if mi.t.parts[i].name == s {
					ref.sel += "." + mi.t.parts[i].goName
					mi = &memberInfo{part: &mi.t.parts[i]}
					found = true
					break
				}
			}
			if !found {
				return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
			}
		case mi.t.flags != nil:
			for i, fl := range mi.t.flags {
				if fl == s {
					ref.kind, ref.bit = "flag", i
					return ref, nil
				}
			}
			return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
		case mi.t.k == kStruct:
			next := mi.t.st.byName[s]
			if next == nil {
				return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
			}
			ref.sel += "." + next.goName
			mi = next
		default:
			return ref, fmt.Errorf("%w: compareTo %s", errUnsupported, path)
		}
	}
	switch {
	case mi.part != nil && mi.part.goType == "bool":
		ref.kind = "bool"
	case mi.part != nil:
		ref.kind = "int"
	case mi.t.k == kPrim && mi.t.base == "bool":
		ref.kind = "bool"
	case mi.t.k == kPrim:
		ref.kind = "int"
		ref.mapping = mi.t.mapping
	case mi.t.k == kString:
		ref.kind = "string"
	case mi.t.k == kOpt && mi.t.elem.k == kPrim && mi.t.elem.base != "bool":
		ref.kind = "optint"
		ref.mapping = mi.t.elem.mapping
	default:
		return ref, fmt.Errorf("%w: compareTo %s kind", errUnsupported, path)
	}
	return ref, nil
}

func (g *gen) resolveSwitch(a map[string]any, c rctx, sc *rscope, goName string, st *structT, anon bool) (*switchT, error) {
	cmp, err := g.resolveCmp(fmt.Sprint(a["compareTo"]), sc)
	if err != nil {
		return nil, err
	}
	sw := &switchT{cmp: cmp}
	fields := asMap(a["fields"])
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, ea := strconv.Atoi(keys[i])
		b, eb := strconv.Atoi(keys[j])
		if ea == nil && eb == nil {
			return a < b
		}
		return keys[i] < keys[j]
	})
	type group struct {
		cs *swCase
		t  any
	}
	var groups []*group
	var voids []string
	for _, k := range keys {
		t := fields[k]
		if t == "void" {
			voids = append(voids, k)
			continue
		}
		found := false
		for _, gr := range groups {
			if reflect.DeepEqual(gr.t, t) {
				gr.cs.keys = append(gr.cs.keys, k)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, &group{cs: &swCase{keys: []string{k}}, t: t})
		}
	}
	if d, ok := a["default"]; ok && d != "void" {
		groups = append(groups, &group{cs: &swCase{def: true}, t: d})
		if len(voids) > 0 {
			sw.cases = append(sw.cases, &swCase{keys: voids, void: true})
		}
	}
	for _, gr := range groups {
		cs := gr.cs
		for _, k := range cs.keys {
			if _, err := cmp.cond("x", k); err != nil {
				return nil, err
			}
		}
		label := "Default"
		if !cs.def {
			label = camel(cs.keys[0])
		}
		if anon {
			arr, _ := gr.t.([]any)
			if len(arr) != 2 || arr[0] != "container" {
				return nil, fmt.Errorf("%w: anonymous switch case %v", errUnsupported, gr.t)
			}
			list, _ := arr[1].([]any)
			saved := st.members
			st.members = nil
			err := g.resolveFields(list, st, c, sc)
			cs.members = st.members
			st.members = saved
			if err != nil {
				return nil, err
			}
			sw.cases = append(sw.cases, cs)
			continue
		}
		name := goName
		if len(groups) > 1 {
			name += label
		}
		n, err := g.resolve(gr.t, c.with(c.field, st.goName+name))
		if err != nil {
			return nil, err
		}
		cs.t = n
		cs.goName = st.field(name)
		if len(groups) == 1 {
			st.byName[c.field] = &memberInfo{goName: cs.goName, t: n}
		}
		sw.cases = append(sw.cases, cs)
	}
	return sw, nil
}

// conds returns the condition matching any of the case keys.
func (r cmpRef) conds(expr string, keys []string) string {
	var parts []string
	for _, k := range keys {
		c, _ := r.cond(expr, k)
		parts = append(parts, c)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ") || (") + ")"
}

func (r cmpRef) cond(expr, key string) (string, error) {
	switch r.kind {
	case "bool", "flag":
		e := expr
		if r.kind == "flag" {
			e = fmt.Sprintf("%s&(1<<%d) != 0", expr, r.bit)
		}
		switch key {
		case "true", "1":
			return e, nil
		case "false", "0":
			return "!(" + e + ")", nil
		}
	case "int":
		if v, ok := r.mapping[key]; ok {
			return expr + " == " + v, nil
		}
		if _, err := strconv.ParseInt(key, 10, 64); err == nil {
			return expr + " == " + key, nil
		}
		switch key {
		case "true":
			return expr + " != 0", nil
		case "false":
			return expr + " == 0", nil
		}
	case "string":
		return expr + " == " + strconv.Quote(key), nil
	case "optint":
		lit := key
		if v, ok := r.mapping[key]; ok {
			lit = v
		} else if _, err := strconv.ParseInt(key, 10, 64); err != nil {
			break
		}
		return expr + ".Ok && " + expr + ".V == " + lit, nil
	}
	return "", fmt.Errorf("%w: switch key %q for %s compare", errUnsupported, key, r.kind)
}
