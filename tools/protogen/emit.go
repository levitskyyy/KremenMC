package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type packetGen struct {
	sd     *stateDir
	def    *packetDef
	id     int
	goName string
	st     *structT
}

type emitter struct {
	b     strings.Builder
	loops int
}

func (e *emitter) p(format string, args ...any) {
	fmt.Fprintf(&e.b, format, args...)
	e.b.WriteByte('\n')
}

func (e *emitter) loopVar() string {
	v := fmt.Sprintf("i%d", e.loops)
	e.loops++
	return v
}

func zeroOf(n *node) string {
	switch n.k {
	case kPrim:
		if n.base == "bool" {
			return "false"
		}
		return "0"
	case kString:
		return `""`
	case kBytes, kRest, kArray:
		return "nil"
	case kNative:
		if n.goType == "proto.RawNBT" {
			return "nil"
		}
	}
	return n.goType + "{}"
}

func push(es []string, base string) []string {
	out := make([]string, len(es)+1)
	copy(out, es)
	out[len(es)] = base
	return out
}

func (e *emitter) enc(n *node, x string, es []string) {
	switch n.k {
	case kPrim:
		if n.goType != n.base {
			e.p("w.%s(%s(%s))", n.prim, n.base, x)
		} else {
			e.p("w.%s(%s)", n.prim, x)
		}
	case kString:
		e.p("w.String(%s)", x)
	case kBytes:
		e.p("w.ByteArray(%s)", x)
	case kFixedBytes:
		e.p("w.Raw(%s[:])", x)
	case kRest:
		e.p("w.Raw(%s)", x)
	case kUUID:
		e.p("w.UUID(%s)", x)
	case kPos:
		e.p("w.BlockPos(%s)", x)
	case kNative:
		e.p("%s.Encode(w)", x)
	case kStruct:
		if n.st.named {
			e.p("%s.Encode(w)", x)
		} else {
			e.encMembers(n.st, x, push(es, x))
		}
	case kOpt:
		e.p("w.Bool(%s.Ok)", x)
		e.p("if %s.Ok {", x)
		e.enc(n.elem, x+".V", es)
		e.p("}")
	case kArray:
		i := e.loopVar()
		e.p("w.VarInt(int32(len(%s)))", x)
		e.p("for %s := range %s {", i, x)
		e.enc(n.elem, x+"["+i+"]", es)
		e.p("}")
	case kBitfield:
		e.encBits(n.parts, x+".")
	case kHolder:
		e.p("if %s.Value != nil {", x)
		e.p("w.VarInt(0)")
		e.enc(n.elem, "(*"+x+".Value)", es)
		e.p("} else {")
		e.p("w.VarInt(%s.ID + 1)", x)
		e.p("}")
	case kIDSet:
		i := e.loopVar()
		e.p("if %s.IsTag {", x)
		e.p("w.VarInt(0)")
		e.p("w.String(%s.Tag)", x)
		e.p("} else {")
		e.p("w.VarInt(int32(len(%s.IDs) + 1))", x)
		e.p("for _, %s := range %s.IDs {", i, x)
		e.p("w.VarInt(%s)", i)
		e.p("}")
		e.p("}")
	}
}

func bitMethod(w int) string { return fmt.Sprintf("U%d", w) }

func (e *emitter) encBits(parts []bitPart, prefix string) {
	w := bitWidth(parts)
	shift := w
	var terms []string
	for _, p := range parts {
		shift -= p.size
		v := "uint64(" + prefix + p.goName + ")"
		if p.goType == "bool" {
			v = "b2u(" + prefix + p.goName + ")"
		}
		mask := uint64(1)<<p.size - 1
		if p.size == 64 {
			mask = ^uint64(0)
		}
		terms = append(terms, fmt.Sprintf("(%s&%#x)<<%d", v, mask, shift))
	}
	e.p("w.%s(uint%d(%s))", bitMethod(w), w, strings.Join(terms, " | "))
}

func (e *emitter) decBits(parts []bitPart, prefix string) {
	w := bitWidth(parts)
	e.p("if t, err := r.%s(); err != nil {", bitMethod(w))
	e.p("return err")
	e.p("} else {")
	e.p("x := uint64(t)")
	shift := w
	for _, p := range parts {
		shift -= p.size
		mask := uint64(1)<<p.size - 1
		if p.size == 64 {
			mask = ^uint64(0)
		}
		switch {
		case p.goType == "bool":
			e.p("%s%s = x>>%d&1 != 0", prefix, p.goName, shift)
		case p.signed:
			e.p("%s%s = %s(int64(x<<%d) >> %d)", prefix, p.goName, p.goType, 64-shift-p.size, 64-p.size)
		default:
			e.p("%s%s = %s(x >> %d & %#x)", prefix, p.goName, p.goType, shift, mask)
		}
	}
	e.p("}")
}

func (e *emitter) dec(n *node, x string, es []string) {
	const ret = "return err"
	switch n.k {
	case kPrim:
		if n.goType != n.base {
			e.p("if t, err := r.%s(); err != nil {", n.prim)
			e.p(ret)
			e.p("} else {")
			e.p("%s = %s(t)", x, n.goType)
			e.p("}")
		} else {
			e.p("if %s, err = r.%s(); err != nil {", x, n.prim)
			e.p(ret)
			e.p("}")
		}
	case kString:
		e.p("if %s, err = r.String(maxString); err != nil {", x)
		e.p(ret)
		e.p("}")
	case kBytes:
		e.p("if %s, err = r.BytesCopy(%s, proto.MaxUncompressedSize); err != nil {", x, x)
		e.p(ret)
		e.p("}")
	case kFixedBytes:
		e.p("if b, err := r.Raw(%d); err != nil {", n.n)
		e.p(ret)
		e.p("} else {")
		e.p("copy(%s[:], b)", x)
		e.p("}")
	case kRest:
		e.p("%s = r.RestCopy(%s)", x, x)
	case kUUID:
		e.p("if %s, err = r.UUID(); err != nil {", x)
		e.p(ret)
		e.p("}")
	case kPos:
		e.p("if %s, err = r.BlockPos(); err != nil {", x)
		e.p(ret)
		e.p("}")
	case kNative:
		e.p("if err = %s.Decode(r); err != nil {", x)
		e.p(ret)
		e.p("}")
	case kStruct:
		if n.st.named {
			e.p("if err = %s.Decode(r); err != nil {", x)
			e.p(ret)
			e.p("}")
		} else {
			e.decMembers(n.st, x, push(es, x))
		}
	case kOpt:
		e.p("if %s.Ok, err = r.Bool(); err != nil {", x)
		e.p(ret)
		e.p("}")
		e.p("if %s.Ok {", x)
		e.dec(n.elem, x+".V", es)
		e.p("} else {")
		e.p("%s.V = %s", x, zeroOf(n.elem))
		e.p("}")
	case kArray:
		i := e.loopVar()
		e.p("if n, err := r.Count(%d); err != nil {", max(n.elem.min, 1))
		e.p(ret)
		e.p("} else {")
		e.p("%s = proto.Resize(%s, n)", x, x)
		e.p("}")
		e.p("for %s := range %s {", i, x)
		e.dec(n.elem, x+"["+i+"]", es)
		e.p("}")
	case kBitfield:
		e.decBits(n.parts, x+".")
	case kHolder:
		e.p("if id, err := r.VarInt(); err != nil {")
		e.p(ret)
		e.p("} else if id != 0 {")
		e.p("%s.ID, %s.Value = id-1, nil", x, x)
		e.p("} else {")
		e.p("%s.ID, %s.Value = 0, new(%s)", x, x, n.elem.goType)
		e.dec(n.elem, "(*"+x+".Value)", es)
		e.p("}")
	case kIDSet:
		i := e.loopVar()
		e.p("if n, err := r.VarInt(); err != nil {")
		e.p(ret)
		e.p("} else if n == 0 {")
		e.p("%s.IsTag, %s.IDs = true, %s.IDs[:0]", x, x, x)
		e.p("if %s.Tag, err = r.String(maxString); err != nil {", x)
		e.p(ret)
		e.p("}")
		e.p("} else {")
		e.p("if n < 0 || int(n-1) > r.Remaining() {")
		e.p("return proto.ErrCountTooBig")
		e.p("}")
		e.p("%s.IsTag, %s.Tag = false, \"\"", x, x)
		e.p("%s.IDs = proto.Resize(%s.IDs, int(n-1))", x, x)
		e.p("for %s := range %s.IDs {", i, x)
		e.p("if %s.IDs[%s], err = r.VarInt(); err != nil {", x, i)
		e.p(ret)
		e.p("}")
		e.p("}")
		e.p("}")
	}
}

func (e *emitter) cmpExpr(sw *switchT, es []string) string {
	return es[len(es)-1-sw.cmp.up] + sw.cmp.sel
}

func (e *emitter) switchChain(sw *switchT, es []string, body func(cs *swCase)) {
	x := e.cmpExpr(sw, es)
	var def *swCase
	first := true
	for _, cs := range sw.cases {
		if cs.def {
			def = cs
			continue
		}
		cond := sw.cmp.conds(x, cs.keys)
		if first {
			e.p("if %s {", cond)
			first = false
		} else {
			e.p("} else if %s {", cond)
		}
		if !cs.void {
			body(cs)
		}
	}
	if def != nil {
		if first {
			e.p("{")
			first = false
		} else {
			e.p("} else {")
		}
		body(def)
	}
	if !first {
		e.p("}")
	}
}

func (e *emitter) zeroMembers(members []*member, base string) {
	for _, m := range members {
		switch {
		case m.raw:
			e.p("%s.%s = nil", base, m.goName)
		case m.parts != nil:
			for _, p := range m.parts {
				if p.goType == "bool" {
					e.p("%s.%s = false", base, p.goName)
				} else {
					e.p("%s.%s = 0", base, p.goName)
				}
			}
		case m.sw != nil:
			for _, cs := range m.sw.cases {
				if cs.t != nil {
					e.p("%s.%s = %s", base, cs.goName, zeroOf(cs.t))
				}
				e.zeroMembers(cs.members, base)
			}
		default:
			e.p("%s.%s = %s", base, m.goName, zeroOf(m.t))
		}
	}
}

func (e *emitter) caseEnc(cs *swCase, base string, es []string) {
	if cs.members != nil {
		e.encMembers(&structT{members: cs.members}, base, es)
		return
	}
	e.enc(cs.t, base+"."+cs.goName, es)
}

func (e *emitter) caseDec(cs *swCase, base string, es []string) {
	if cs.members != nil {
		e.decMembers(&structT{members: cs.members}, base, es)
		return
	}
	e.dec(cs.t, base+"."+cs.goName, es)
}

func (e *emitter) encMembers(st *structT, base string, es []string) {
	for _, m := range st.members {
		switch {
		case m.raw:
			e.p("w.Raw(%s.%s)", base, m.goName)
		case m.parts != nil:
			e.encBits(m.parts, base+".")
		case m.sw != nil:
			e.switchChain(m.sw, es, func(cs *swCase) { e.caseEnc(cs, base, es) })
		default:
			e.enc(m.t, base+"."+m.goName, es)
		}
	}
}

func (e *emitter) decMembers(st *structT, base string, es []string) {
	for _, m := range st.members {
		switch {
		case m.raw:
			e.p("%s.%s = r.RestCopy(%s.%s)", base, m.goName, base, m.goName)
		case m.parts != nil:
			e.decBits(m.parts, base+".")
		case m.sw != nil:
			e.zeroMembers([]*member{m}, base)
			e.switchChain(m.sw, es, func(cs *swCase) { e.caseDec(cs, base, es) })
		default:
			e.dec(m.t, base+"."+m.goName, es)
		}
	}
}

func (e *emitter) structDecl(st *structT, doc string) {
	if doc != "" {
		e.p("// %s", doc)
	}
	e.p("type %s struct {", st.goName)
	e.fieldDecls(st.members)
	e.p("}")
	e.p("")
}

func (e *emitter) fieldDecls(members []*member) {
	for _, m := range members {
		switch {
		case m.raw:
			e.p("// %s holds the undecoded fields from %s to the end of the packet.", m.goName, m.rawFrom)
			e.p("%s []byte", m.goName)
		case m.parts != nil:
			for _, p := range m.parts {
				e.p("%s %s", p.goName, p.goType)
			}
		case m.sw != nil:
			for _, cs := range m.sw.cases {
				if cs.t != nil {
					e.p("%s %s", cs.goName, cs.t.goType)
				}
				e.fieldDecls(cs.members)
			}
		default:
			e.p("%s %s", m.goName, m.t.goType)
		}
	}
}

func (e *emitter) methods(st *structT, recv string) {
	e.loops = 0
	e.p("func (%s *%s) Encode(w *proto.Writer) {", recv, st.goName)
	e.encMembers(st, recv, []string{recv})
	e.p("}")
	e.p("")
	e.p("func (%s *%s) Decode(r *proto.Reader) error {", recv, st.goName)
	e.p("var err error")
	e.decMembers(st, recv, []string{recv})
	e.p("return err")
	e.p("}")
	e.p("")
}

func (e *emitter) flagDecl(f *flagType) {
	e.p("type %s %s", f.goName, f.base)
	e.p("")
	if len(f.flags) == 0 {
		return
	}
	e.p("const (")
	for i, fl := range f.flags {
		e.p("%s%s %s = 1 << %d", f.goName, camel(fl), f.goName, i)
	}
	e.p(")")
	e.p("")
}

var stateGo = map[string][2]string{
	"handshaking":   {"Handshake", "Handshake"},
	"status":        {"Status", "Status"},
	"login":         {"Login", "Login"},
	"configuration": {"Config", "Configuration"},
	"play":          {"", "Play"},
}

func dirGo(d string) string {
	if d == "toClient" {
		return "Clientbound"
	}
	return "Serverbound"
}

func packetGoName(sd *stateDir, p *packetDef) string {
	prefix := stateGo[sd.State][0]
	name := camel(p.Mojang)
	if prefix != "" && strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
		name = name[len(prefix):]
	}
	return dirGo(sd.Dir) + prefix + name
}

func fileFor(sd *stateDir) string {
	if sd.State == "play" {
		return "play_" + strings.ToLower(dirGo(sd.Dir))
	}
	return strings.ToLower(stateGo[sd.State][1])
}

func derefDef(g *gen, sd *stateDir, def any) []any {
	if def == "void" {
		return nil
	}
	for i := 0; i < 8; i++ {
		name, ok := def.(string)
		if !ok {
			break
		}
		d, _, found := g.lookup(sd, name)
		if !found {
			break
		}
		def = d
	}
	arr, _ := def.([]any)
	if len(arr) == 2 && arr[0] == "container" {
		list, _ := arr[1].([]any)
		return list
	}
	return []any{map[string]any{"name": "value", "type": def}}
}

func (g *gen) resolvePacket(pg *packetGen) {
	st := &structT{goName: pg.goName, byName: map[string]*memberInfo{}, fields: map[string]bool{"ID": true, "Encode": true, "Decode": true}, file: fileFor(pg.sd)}
	pg.st = st
	c := rctx{sd: pg.sd, owner: pg.def.Name, hint: pg.goName, file: st.file}
	sc := &rscope{st: st}
	for _, f := range derefDef(g, pg.sd, pg.def.Def) {
		fm := asMap(f)
		s := g.snap()
		nm := len(st.members)
		if err := g.resolveField(fm, st, c, sc); err != nil {
			g.rollback(s)
			st.members = st.members[:nm]
			from := "the anonymous field"
			if n, ok := fm["name"].(string); ok {
				from = camel(n)
			}
			st.members = append(st.members, &member{raw: true, goName: st.field("Raw"), rawFrom: from})
			g.report = append(g.report, fmt.Sprintf("%-46s raw from %-18s (%v)", pg.goName, from, err))
			return
		}
	}
}

type genConfig struct {
	pkg, out, protoImport  string
	version, mcdataVersion string
	protocol               int
}

func (g *gen) generate(cfg genConfig) error {
	var pgs []*packetGen
	for _, sd := range g.s.SDs {
		for i, p := range sd.Packets {
			pg := &packetGen{sd: sd, def: p, id: i, goName: packetGoName(sd, p)}
			if g.taken[pg.goName] {
				return fmt.Errorf("duplicate packet name %s", pg.goName)
			}
			g.taken[pg.goName] = true
			pgs = append(pgs, pg)
		}
	}
	for _, pg := range pgs {
		g.resolvePacket(pg)
	}
	files := map[string]*emitter{}
	get := func(f string) *emitter {
		if files[f] == nil {
			files[f] = &emitter{}
		}
		return files[f]
	}
	for _, pg := range pgs {
		e := get(pg.st.file)
		doc := fmt.Sprintf("%s is minecraft:%s (%s, %s, 0x%02X).", pg.goName, pg.def.Mojang, stateGo[pg.sd.State][1], strings.ToLower(dirGo(pg.sd.Dir)), pg.id)
		e.structDecl(pg.st, doc)
		e.p("func (*%s) ID() int32 { return 0x%02X }", pg.goName, pg.id)
		e.p("")
		e.methods(pg.st, "p")
	}
	for _, st := range g.structs {
		e := get(st.file)
		e.structDecl(st, "")
		if st.named {
			e.methods(st, "v")
		}
	}
	for _, f := range g.flagTs {
		get(f.file).flagDecl(f)
	}
	get("packets").b.WriteString(g.common(pgs, cfg))

	if err := os.MkdirAll(cfg.out, 0o755); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(cfg.out, "*_gen.go"))
	for _, f := range old {
		os.Remove(f)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name].b.String()
		var hdr strings.Builder
		fmt.Fprintf(&hdr, "// Code generated by protogen from minecraft-data %s", cfg.mcdataVersion)
		if cfg.version != cfg.mcdataVersion {
			fmt.Fprintf(&hdr, " patched to %s", cfg.version)
		}
		fmt.Fprintf(&hdr, " (protocol %d). DO NOT EDIT.\n\npackage %s\n\n", cfg.protocol, cfg.pkg)
		var imports []string
		if name == "packets" {
			imports = append(imports, `"errors"`, `"fmt"`)
		}
		if strings.Contains(body, "proto.") {
			imports = append(imports, fmt.Sprintf("%q", cfg.protoImport))
		}
		if len(imports) > 0 {
			fmt.Fprintf(&hdr, "import (\n%s\n)\n\n", strings.Join(imports, "\n"))
		}
		src := hdr.String() + body
		out, err := format.Source([]byte(src))
		if err != nil {
			dump := filepath.Join(os.TempDir(), "protogen_"+name+".go")
			os.WriteFile(dump, []byte(src), 0o644)
			return fmt.Errorf("format %s (raw source in %s): %w", name, dump, err)
		}
		if err := os.WriteFile(filepath.Join(cfg.out, name+"_gen.go"), out, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (g *gen) common(pgs []*packetGen, cfg genConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, `const (
	Protocol = %d
	Version  = %q
)

const maxString = 32767

type State uint8

const (
	Handshake State = iota
	Status
	Login
	Configuration
	Play
)

type Direction uint8

const (
	Clientbound Direction = iota
	Serverbound
)

type Packet interface {
	ID() int32
	Encode(w *proto.Writer)
	Decode(r *proto.Reader) error
}

var (
	ErrUnknownPacket = errors.New("packets: unknown packet id")
	ErrTrailingData  = errors.New("packets: trailing data after packet")
)

type entry struct {
	name string
	new  func() Packet
}

func b2u(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// New returns a zero packet for the given state, direction and ID, or nil.
func New(s State, d Direction, id int32) Packet {
	if int(s) >= len(registry) || int(d) > 1 {
		return nil
	}
	t := registry[s][d]
	if id < 0 || int(id) >= len(t) {
		return nil
	}
	return t[id].new()
}

// Name returns the official packet name (without namespace), or "".
func Name(s State, d Direction, id int32) string {
	if int(s) >= len(registry) || int(d) > 1 {
		return ""
	}
	t := registry[s][d]
	if id < 0 || int(id) >= len(t) {
		return ""
	}
	return t[id].name
}

// Count returns the number of packets defined for a state and direction.
func Count(s State, d Direction) int {
	if int(s) >= len(registry) || int(d) > 1 {
		return 0
	}
	return len(registry[s][d])
}

// Marshal appends the packet ID and body to w.
func Marshal(w *proto.Writer, p Packet) {
	w.VarInt(p.ID())
	p.Encode(w)
}

// Unmarshal decodes a packet (ID followed by body) and rejects trailing bytes.
func Unmarshal(s State, d Direction, data []byte) (Packet, error) {
	r := proto.Reader{B: data}
	id, err := r.VarInt()
	if err != nil {
		return nil, fmt.Errorf("packet id: %%w", err)
	}
	p := New(s, d, id)
	if p == nil {
		return nil, fmt.Errorf("%%w: 0x%%02X", ErrUnknownPacket, id)
	}
	if err := DecodeInto(p, data[r.Off:]); err != nil {
		return nil, fmt.Errorf("decode %%s: %%w", Name(s, d, id), err)
	}
	return p, nil
}

// DecodeInto decodes a packet body (without ID) into p.
func DecodeInto(p Packet, body []byte) error {
	return DecodeBody(p, &proto.Reader{B: body})
}

// DecodeBody decodes the rest of r into p and rejects trailing bytes. With a
// caller-owned reader and a reused packet this does not allocate for
// fixed-size packets.
func DecodeBody(p Packet, r *proto.Reader) error {
	if err := p.Decode(r); err != nil {
		return err
	}
	if r.Remaining() != 0 {
		return ErrTrailingData
	}
	return nil
}

`, cfg.protocol, cfg.version)
	b.WriteString("var registry = [5][2][]entry{\n")
	for _, st := range states {
		fmt.Fprintf(&b, "%s: {\n", stateGo[st][1])
		for _, d := range dirs {
			fmt.Fprintf(&b, "%s: {\n", dirGo(d))
			for _, pg := range pgs {
				if pg.sd.State == st && pg.sd.Dir == d {
					fmt.Fprintf(&b, "{%q, func() Packet { return new(%s) }},\n", pg.def.Mojang, pg.goName)
				}
			}
			b.WriteString("},\n")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return b.String()
}
