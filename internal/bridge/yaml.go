package bridge

import (
	"fmt"
	"strconv"
	"strings"
)

// parseYAML parses the YAML subset used by plugin.yml / paper-plugin.yml:
// block mappings and sequences, flow sequences and mappings of scalars,
// quoted and plain scalars, and literal/folded block scalars. Scalars are
// returned as strings; mappings as map[string]any; sequences as []any.
func parseYAML(src string) (any, error) {
	p := &yamlParser{}
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		p.raw = append(p.raw, raw)
	}
	p.next = 0
	p.skip()
	if p.next >= len(p.raw) {
		return map[string]any{}, nil
	}
	ind, _ := p.peek()
	return p.block(ind)
}

type yamlParser struct {
	raw  []string
	next int
	// overrides lets a "- key: v" sequence item be re-read as a mapping line.
	overrideIdx  int
	overrideText string
	overrideInd  int
	override     bool
}

func (p *yamlParser) line(i int) (indent int, text string) {
	if p.override && i == p.overrideIdx {
		return p.overrideInd, p.overrideText
	}
	l := p.raw[i]
	t := strings.TrimLeft(l, " ")
	return len(l) - len(t), strings.TrimRight(stripComment(t), " \t")
}

func (p *yamlParser) skip() {
	for p.next < len(p.raw) {
		_, t := p.line(p.next)
		if t != "" && t != "---" && !strings.HasPrefix(t, "%") {
			return
		}
		p.next++
	}
}

func (p *yamlParser) peek() (int, string) {
	p.skip()
	if p.next >= len(p.raw) {
		return -1, ""
	}
	return p.line(p.next)
}

func isSeqItem(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

func (p *yamlParser) block(ind int) (any, error) {
	_, t := p.peek()
	if isSeqItem(t) {
		return p.sequence(ind)
	}
	return p.mapping(ind)
}

func (p *yamlParser) mapping(ind int) (map[string]any, error) {
	out := map[string]any{}
	for {
		li, t := p.peek()
		if li != ind || isSeqItem(t) {
			if li > ind {
				return nil, fmt.Errorf("yaml line %d: unexpected indentation", p.next+1)
			}
			return out, nil
		}
		key, rest, ok := splitKey(t)
		if !ok {
			return nil, fmt.Errorf("yaml line %d: expected key: value", p.next+1)
		}
		lineNo := p.next
		p.next++
		if p.override && lineNo == p.overrideIdx {
			p.override = false
		}
		v, err := p.value(ind, rest, lineNo)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
}

func (p *yamlParser) value(ind int, rest string, lineNo int) (any, error) {
	switch {
	case rest == "":
		ni, nt := p.peek()
		if ni > ind {
			return p.block(ni)
		}
		if ni == ind && isSeqItem(nt) {
			return p.sequence(ind)
		}
		return nil, nil
	case rest[0] == '|' || rest[0] == '>':
		return p.blockScalar(ind, rest), nil
	case rest[0] == '[' || rest[0] == '{':
		v, tail, err := parseFlow(rest)
		if err != nil {
			return nil, fmt.Errorf("yaml line %d: %w", lineNo+1, err)
		}
		if strings.TrimSpace(tail) != "" {
			return nil, fmt.Errorf("yaml line %d: trailing %q", lineNo+1, tail)
		}
		return v, nil
	}
	return p.scalar(ind, rest, lineNo)
}

// scalar handles a scalar that may continue on more-indented lines.
func (p *yamlParser) scalar(ind int, s string, lineNo int) (any, error) {
	if s[0] == '"' || s[0] == '\'' {
		for !closedQuote(s) {
			if p.next >= len(p.raw) {
				return nil, fmt.Errorf("yaml line %d: unterminated string", lineNo+1)
			}
			s += " " + strings.TrimSpace(p.raw[p.next])
			p.next++
		}
		v, _, err := unquote(s)
		return v, err
	}
	for {
		ni, nt := p.peek()
		if ni <= ind || ni < 0 {
			break
		}
		if _, _, ok := splitKey(nt); ok {
			break
		}
		s += " " + nt
		p.next++
	}
	return s, nil
}

func closedQuote(s string) bool {
	_, rest, err := unquote(s)
	return err == nil && strings.TrimSpace(rest) == ""
}

func (p *yamlParser) blockScalar(ind int, header string) string {
	folded := header[0] == '>'
	chomp := ""
	if strings.Contains(header, "-") {
		chomp = "-"
	} else if strings.Contains(header, "+") {
		chomp = "+"
	}
	var lines []string
	bodyInd := -1
	for p.next < len(p.raw) {
		l := p.raw[p.next]
		t := strings.TrimLeft(l, " ")
		li := len(l) - len(t)
		if strings.TrimSpace(l) == "" {
			lines = append(lines, "")
			p.next++
			continue
		}
		if bodyInd < 0 {
			if li <= ind {
				break
			}
			bodyInd = li
		}
		if li < bodyInd {
			break
		}
		lines = append(lines, l[bodyInd:])
		p.next++
	}
	trailing := 0
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailing++
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			if folded && l != "" && lines[i-1] != "" {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(l)
	}
	switch chomp {
	case "":
		if len(lines) > 0 {
			b.WriteByte('\n')
		}
	case "+":
		b.WriteString(strings.Repeat("\n", trailing+1))
	}
	return b.String()
}

func (p *yamlParser) sequence(ind int) ([]any, error) {
	out := []any{}
	for {
		li, t := p.peek()
		if li != ind || !isSeqItem(t) {
			return out, nil
		}
		rest := strings.TrimLeft(strings.TrimPrefix(t, "-"), " ")
		lineNo := p.next
		if rest == "" {
			p.next++
			ni, _ := p.peek()
			if ni > ind {
				v, err := p.block(ni)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			} else {
				out = append(out, nil)
			}
			continue
		}
		if _, _, ok := splitKey(rest); ok && rest[0] != '"' && rest[0] != '\'' && rest[0] != '[' && rest[0] != '{' {
			_, full := p.line(lineNo)
			p.override, p.overrideIdx, p.overrideText = true, lineNo, rest
			p.overrideInd = ind + len(full) - len(rest)
			m, err := p.mapping(p.overrideInd)
			p.override = false
			if err != nil {
				return nil, err
			}
			out = append(out, m)
			continue
		}
		p.next++
		v, err := p.value(ind, rest, lineNo)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '[' || s[i-1] == ',' || s[i-1] == '{' || s[i-1] == ':' {
				q = c
			}
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

// splitKey splits "key: rest" or "key:" at the first unquoted ": ".
func splitKey(t string) (key, rest string, ok bool) {
	if t == "" {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		k, tail, err := unquote(t)
		if err != nil {
			return "", "", false
		}
		tail = strings.TrimLeft(tail, " ")
		if !strings.HasPrefix(tail, ":") {
			return "", "", false
		}
		return k, strings.TrimSpace(tail[1:]), true
	}
	if t[0] == '[' || t[0] == '{' {
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == ':' && (i+1 == len(t) || t[i+1] == ' ' || t[i+1] == '\t') {
			return strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:]), true
		}
	}
	return "", "", false
}

// unquote parses a leading quoted string and returns the remainder.
func unquote(s string) (string, string, error) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if q == '\'' {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return b.String(), s[i+1:], nil
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			return b.String(), s[i+1:], nil
		case '\\':
			if i+1 >= len(s) {
				return "", "", fmt.Errorf("bad escape")
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case 'u', 'U', 'x':
				n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[s[i]]
				if i+n >= len(s) {
					return "", "", fmt.Errorf("bad escape")
				}
				r, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
				if err != nil {
					return "", "", fmt.Errorf("bad escape: %w", err)
				}
				b.WriteRune(rune(r))
				i += n
			default:
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("unterminated string")
}

func parseFlow(s string) (any, string, error) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return nil, "", fmt.Errorf("unexpected end of flow collection")
	}
	switch s[0] {
	case '[':
		out := []any{}
		s = strings.TrimLeft(s[1:], " ")
		for {
			if strings.HasPrefix(s, "]") {
				return out, s[1:], nil
			}
			v, rest, err := parseFlow(s)
			if err != nil {
				return nil, "", err
			}
			out = append(out, v)
			s = strings.TrimLeft(rest, " ")
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " ")
			} else if !strings.HasPrefix(s, "]") {
				return nil, "", fmt.Errorf("expected , or ] in flow sequence")
			}
		}
	case '{':
		out := map[string]any{}
		s = strings.TrimLeft(s[1:], " ")
		for {
			if strings.HasPrefix(s, "}") {
				return out, s[1:], nil
			}
			k, rest, err := parseFlow(s)
			if err != nil {
				return nil, "", err
			}
			ks, _ := k.(string)
			s = strings.TrimLeft(rest, " ")
			var v any
			if strings.HasPrefix(s, ":") {
				if v, rest, err = parseFlow(s[1:]); err != nil {
					return nil, "", err
				}
				s = strings.TrimLeft(rest, " ")
			}
			out[ks] = v
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " ")
			} else if !strings.HasPrefix(s, "}") {
				return nil, "", fmt.Errorf("expected , or } in flow mapping")
			}
		}
	case '"', '\'':
		v, rest, err := unquote(s)
		return v, rest, err
	}
	end := strings.IndexAny(s, ",]}")
	if c := strings.Index(s, ": "); c >= 0 && (end < 0 || c < end) {
		end = c
	}
	if end < 0 {
		end = len(s)
	}
	return strings.TrimSpace(s[:end]), s[end:], nil
}
