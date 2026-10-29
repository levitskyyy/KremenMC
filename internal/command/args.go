package command

import (
	"math"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

var teamColors = []string{
	"black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple", "gold", "gray",
	"dark_gray", "blue", "green", "aqua", "red", "light_purple", "yellow", "white", "reset",
}

func numberBound(t api.ArgumentType, kind string, v float64, start int, r *reader) error {
	if t.HasMin && v < t.Min {
		r.pos = start
		return r.errf("%s must not be less than %s, found %s", kind, fmtNum(t.Min), fmtNum(v))
	}
	if t.HasMax && v > t.Max {
		r.pos = start
		return r.errf("%s must not be more than %s, found %s", kind, fmtNum(t.Max), fmtNum(v))
	}
	return nil
}

func fmtNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e18 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func (d *Dispatcher) parseArgument(t api.ArgumentType, r *reader, ctx *cmdContext) (any, error) {
	start := r.pos
	switch canonicalParser(t.Parser) {
	case api.ParserBool:
		return r.readBool()
	case api.ParserInteger:
		v, err := r.readInt()
		if err != nil {
			return nil, err
		}
		return v, numberBound(t, "Integer", float64(v), start, r)
	case api.ParserLong:
		v, err := r.readLong()
		if err != nil {
			return nil, err
		}
		return v, numberBound(t, "Long", float64(v), start, r)
	case api.ParserFloat:
		v, err := r.readFloat()
		if err != nil {
			return nil, err
		}
		return v, numberBound(t, "Float", v, start, r)
	case api.ParserDouble:
		v, err := r.readDouble()
		if err != nil {
			return nil, err
		}
		return v, numberBound(t, "Double", v, start, r)
	case api.ParserString:
		switch t.StringMode {
		case api.GreedyPhrase:
			s := r.remaining()
			r.pos = len(r.s)
			return s, nil
		case api.QuotablePhrase:
			return r.readString()
		}
		return r.readUnquoted(), nil
	case api.ParserEntity:
		return d.parseEntityArg(t, r, ctx)
	case api.ParserGameProfile:
		if r.canRead() && r.peek() == '@' {
			s, err := parseSelector(r, d.selectorsAllowed(ctx.sender))
			if err != nil {
				return nil, err
			}
			if s.includesNonPlayers() {
				r.pos = start
				return nil, r.errf(msgOnlyPlayers)
			}
			return &profileArg{sel: s}, nil
		}
		name := r.readUntilSpace()
		if name == "" {
			return nil, r.errf("Expected name")
		}
		return &profileArg{name: name}, nil
	case api.ParserBlockPos:
		return parseCoords(r, coordBlock)
	case api.ParserColumnPos:
		return parseCoords(r, coordColumn)
	case api.ParserVec3:
		return parseCoords(r, coordVec3)
	case api.ParserVec2:
		return parseCoords(r, coordVec2)
	case api.ParserRotation:
		return parseCoords(r, coordRotation)
	case ParserAngle:
		return parseCoords(r, coordAngle)
	case api.ParserBlockState:
		return d.parseBlockState(r)
	case ParserBlockPredicate:
		return d.parseBlockPredicate(r)
	case api.ParserItemStack:
		return d.parseItem(r)
	case ParserItemPredicate:
		return d.parseItemPredicate(r)
	case api.ParserGamemode:
		s := r.readUnquoted()
		g, ok := parseGameMode(s)
		if !ok {
			r.pos = start
			return nil, r.errf("Unknown game mode: %s", s)
		}
		return g, nil
	case api.ParserTime:
		return parseTime(r, t)
	case api.ParserMessage:
		return parseMessage(r, d.selectorsAllowed(ctx.sender))
	case api.ParserComponent:
		return parseComponentArg(r)
	case api.ParserUUID:
		s := r.readWhile(func(c byte) bool {
			return c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		})
		u, err := api.ParseUUID(s)
		if err != nil || strings.Count(s, "-") != 4 {
			r.pos = start
			return nil, r.errf("Invalid UUID")
		}
		return u, nil
	case ParserTeamColor:
		s := r.readUnquoted()
		for _, c := range teamColors {
			if c == s {
				return s, nil
			}
		}
		r.pos = start
		return nil, r.errf("Unknown color '%s'", s)
	case ParserEntityAnchor:
		s := r.readUnquoted()
		if s != "eyes" && s != "feet" {
			r.pos = start
			return nil, r.errf("Invalid entity anchor position %s", s)
		}
		return s, nil
	case ParserHexColor:
		s := r.readUnquoted()
		if _, err := strconv.ParseUint(s, 16, 32); err != nil || len(s) != 6 {
			r.pos = start
			return nil, r.errf("Invalid hex color '%s'", s)
		}
		return s, nil
	case api.ParserResource, api.ParserDimension, api.ParserResourceKey, ParserResourceFull:
		k, err := r.readKey()
		return api.Key(k), err
	case ParserResourceOrTag, ParserResourceOrKey:
		tag := r.canRead() && r.peek() == '#'
		if tag {
			r.skip()
		}
		k, err := r.readKey()
		if tag {
			k = "#" + k
		}
		return api.Key(k), err
	}
	if r.canRead() && (r.peek() == '{' || r.peek() == '[') {
		return skipBalanced(r)
	}
	return r.readUntilSpace(), nil
}

func (d *Dispatcher) parseEntityArg(t api.ArgumentType, r *reader, ctx *cmdContext) (*selector, error) {
	start := r.pos
	s, err := parseSelector(r, d.selectorsAllowed(ctx.sender))
	if err != nil {
		return nil, err
	}
	if t.Single && !s.single() {
		r.pos = start
		if t.PlayersOnly {
			return nil, r.errf(msgOnlyOnePlayer)
		}
		return nil, r.errf(msgOnlyOneEntity)
	}
	if t.PlayersOnly && s.includesNonPlayers() && s.kind != 's' {
		r.pos = start
		return nil, r.errf(msgOnlyPlayers)
	}
	if t.PlayersOnly {
		s.playersOnly = true
	}
	return s, nil
}

// skipBalanced consumes a bracketed SNBT-like value honoring quotes and
// returns its raw text.
func skipBalanced(r *reader) (string, error) {
	start := r.pos
	depth := 0
	for r.canRead() {
		c := r.peek()
		switch c {
		case '"', '\'':
			r.skip()
			if _, err := r.readUntil(c); err != nil {
				return "", err
			}
			continue
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		r.skip()
		if depth == 0 {
			return r.s[start:r.pos], nil
		}
	}
	return "", r.errf("Unexpected end of input")
}

func parseTime(r *reader, t api.ArgumentType) (int64, error) {
	start := r.pos
	f, err := r.readFloat()
	if err != nil {
		return 0, err
	}
	unitStart := r.pos
	mult := 1.0
	switch u := r.readUnquoted(); u {
	case "", "t":
	case "s":
		mult = 20
	case "d":
		mult = 24000
	default:
		r.pos = unitStart
		return 0, r.errf("Invalid unit")
	}
	ticks := int64(math.Round(f * mult))
	minT := int64(0)
	if t.HasMin {
		minT = int64(t.Min)
	}
	if ticks < minT {
		r.pos = start
		return 0, r.errf("Tick count must not be less than %d, found %d", minT, ticks)
	}
	return ticks, nil
}

func readBlockSyntax(r *reader, allowTag bool) (id string, props string, err error) {
	start := r.pos
	if allowTag && r.canRead() && r.peek() == '#' {
		r.skip()
	}
	if _, err = r.readKey(); err != nil {
		return "", "", err
	}
	id = r.s[start:r.pos]
	if r.canRead() && r.peek() == '[' {
		ps := r.pos
		if _, err = skipBalanced(r); err != nil {
			return "", "", err
		}
		props = r.s[ps:r.pos]
	}
	if r.canRead() && r.peek() == '{' {
		if _, err = skipBalanced(r); err != nil {
			return "", "", err
		}
	}
	return id, props, nil
}

func (d *Dispatcher) blocks() api.BlockRegistry {
	if d.server == nil {
		return nil
	}
	return d.server.Blocks()
}

func (d *Dispatcher) parseBlockState(r *reader) (api.BlockState, error) {
	start := r.pos
	id, props, err := readBlockSyntax(r, false)
	if err != nil {
		return nil, err
	}
	reg := d.blocks()
	if reg == nil {
		r.pos = start
		return nil, r.errf("Unknown block type '%s'", id)
	}
	st, perr := reg.Parse(id + props)
	if perr != nil {
		if _, ok := reg.Default(api.Key(id).Normalize()); !ok {
			r.pos = start
			return nil, r.errf("Unknown block type '%s'", id)
		}
		r.pos = start + len(id)
		return nil, r.errf("%s", perr.Error())
	}
	return st, nil
}

type blockPredicate struct {
	block api.Key
	props map[string]string
}

func (p *blockPredicate) test(s api.BlockState) bool {
	if s == nil || s.Type().Normalize() != p.block {
		return false
	}
	for k, v := range p.props {
		if got, ok := s.Property(k); !ok || got != v {
			return false
		}
	}
	return true
}

func (d *Dispatcher) parseBlockPredicate(r *reader) (*blockPredicate, error) {
	start := r.pos
	if r.canRead() && r.peek() == '#' {
		return nil, r.errf("Block tags are not supported")
	}
	st, err := d.parseBlockState(r)
	if err != nil {
		return nil, err
	}
	p := &blockPredicate{block: st.Type().Normalize()}
	text := r.s[start:r.pos]
	if i := strings.IndexByte(text, '['); i >= 0 {
		body := strings.TrimSuffix(text[i+1:], "]")
		if j := strings.IndexByte(body, ']'); j >= 0 {
			body = body[:j]
		}
		p.props = map[string]string{}
		for kv := range strings.SplitSeq(body, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				p.props[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return p, nil
}

func (d *Dispatcher) itemExists(k api.Key) bool {
	if d.server == nil || d.server.Items() == nil {
		return false
	}
	return d.server.Items().Exists(k)
}

func (d *Dispatcher) parseItem(r *reader) (api.ItemStack, error) {
	start := r.pos
	k, err := r.readKey()
	if err != nil {
		return api.ItemStack{}, err
	}
	if !d.itemExists(api.Key(k)) {
		r.pos = start
		return api.ItemStack{}, r.errf("Unknown item '%s'", k)
	}
	for r.canRead() && (r.peek() == '[' || r.peek() == '{') {
		if _, err := skipBalanced(r); err != nil {
			return api.ItemStack{}, err
		}
	}
	return api.Item(api.Key(k), 1), nil
}

type itemPredicate struct {
	any  bool
	item api.Key
}

func (p *itemPredicate) test(s api.ItemStack) bool {
	return !s.IsEmpty() && (p.any || s.Type.Normalize() == p.item)
}

func (d *Dispatcher) parseItemPredicate(r *reader) (*itemPredicate, error) {
	if r.canRead() && r.peek() == '*' {
		r.skip()
		return &itemPredicate{any: true}, nil
	}
	if r.canRead() && r.peek() == '#' {
		return nil, r.errf("Item tags are not supported")
	}
	st, err := d.parseItem(r)
	if err != nil {
		return nil, err
	}
	return &itemPredicate{item: st.Type.Normalize()}, nil
}

type messagePart struct {
	start, end int
	sel        *selector
}

type message struct {
	text  string
	parts []messagePart
}

func parseMessage(r *reader, allowSelectors bool) (*message, error) {
	base := r.pos
	m := &message{text: r.remaining()}
	if allowSelectors {
		for r.canRead() {
			if r.peek() != '@' || !r.canReadN(2) || strings.IndexByte(selectorKinds, r.s[r.pos+1]) < 0 {
				r.skip()
				continue
			}
			start := r.pos
			s, err := parseSelector(r, true)
			if err != nil {
				return nil, err
			}
			m.parts = append(m.parts, messagePart{start: start - base, end: r.pos - base, sel: s})
		}
	}
	r.pos = len(r.s)
	return m, nil
}

func (c *cmdContext) expandMessage(m *message) string {
	if len(m.parts) == 0 {
		return m.text
	}
	var b strings.Builder
	last := 0
	for _, p := range m.parts {
		b.WriteString(m.text[last:p.start])
		es, _ := c.resolveEntities(p.sel)
		b.WriteString(joinNames(es))
		last = p.end
	}
	b.WriteString(m.text[last:])
	return b.String()
}
