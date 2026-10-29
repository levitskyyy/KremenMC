// Package config loads and saves server.properties (vanilla-compatible) and
// kremenmc.toml (engine settings).
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Properties is an ordered Java-style properties file that keeps comments,
// blank lines and unknown keys across a load/save round trip.
type Properties struct {
	lines []propLine
	index map[string]int
}

type propLine struct {
	raw     string // comment/blank line text, or the original kv line until modified
	key     string
	value   string
	isKV    bool
	dirty   bool
	removed bool
}

func NewProperties() *Properties { return &Properties{index: map[string]int{}} }

func ParseProperties(r io.Reader) (*Properties, error) {
	p := NewProperties()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var pending strings.Builder
	var pendingRaw []string
	for sc.Scan() {
		line := sc.Text()
		if pending.Len() == 0 && len(pendingRaw) == 0 {
			trimmed := strings.TrimLeft(line, " \t\f")
			if trimmed == "" || trimmed[0] == '#' || trimmed[0] == '!' {
				p.lines = append(p.lines, propLine{raw: line})
				continue
			}
		}
		pendingRaw = append(pendingRaw, line)
		logical := strings.TrimLeft(line, " \t\f")
		if continues(logical) {
			pending.WriteString(logical[:len(logical)-1])
			continue
		}
		pending.WriteString(logical)
		key, value, err := splitProperty(pending.String())
		if err != nil {
			return nil, fmt.Errorf("properties line %d: %w", len(p.lines)+1, err)
		}
		p.add(propLine{raw: strings.Join(pendingRaw, "\n"), key: key, value: value, isKV: true})
		pending.Reset()
		pendingRaw = nil
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("properties: %w", err)
	}
	if pending.Len() > 0 {
		key, value, err := splitProperty(pending.String())
		if err != nil {
			return nil, err
		}
		p.add(propLine{raw: strings.Join(pendingRaw, "\n"), key: key, value: value, isKV: true})
	}
	return p, nil
}

func (p *Properties) add(l propLine) {
	if i, ok := p.index[l.key]; ok {
		// Java keeps the last occurrence; drop the earlier line.
		p.lines[i] = propLine{removed: true}
	}
	p.index[l.key] = len(p.lines)
	p.lines = append(p.lines, l)
}

// continues reports whether a line ends with an odd number of backslashes.
func continues(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func splitProperty(s string) (string, string, error) {
	i := 0
	var key strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			key.WriteByte(c)
			key.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
			break
		}
		key.WriteByte(c)
		i++
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\f') {
		i++
	}
	if i < len(s) && (s[i] == '=' || s[i] == ':') {
		i++
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\f') {
		i++
	}
	k, err := unescape(key.String())
	if err != nil {
		return "", "", err
	}
	v, err := unescape(s[i:])
	if err != nil {
		return "", "", err
	}
	return k, v, nil
}

func unescape(s string) (string, error) {
	if !strings.ContainsRune(s, '\\') {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 >= len(s) {
				return "", fmt.Errorf("malformed \\u escape in %q", s)
			}
			r, err := strconv.ParseUint(s[i+1:i+5], 16, 16)
			if err != nil {
				return "", fmt.Errorf("malformed \\u escape in %q", s)
			}
			b.WriteRune(rune(r))
			i += 4
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

func escape(s string, isKey bool) string {
	var b strings.Builder
	for i, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		case '=', ':', '#', '!':
			b.WriteByte('\\')
			b.WriteRune(r)
		case ' ':
			if isKey || i == 0 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		default:
			if r == utf8.RuneError {
				b.WriteString(`�`)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func (p *Properties) Get(key string) (string, bool) {
	i, ok := p.index[key]
	if !ok {
		return "", false
	}
	return p.lines[i].value, true
}

// Set updates a key in place or appends it.
func (p *Properties) Set(key, value string) {
	if i, ok := p.index[key]; ok {
		if p.lines[i].value != value {
			p.lines[i].value = value
			p.lines[i].dirty = true
		}
		return
	}
	p.index[key] = len(p.lines)
	p.lines = append(p.lines, propLine{key: key, value: value, isKV: true, dirty: true})
}

func (p *Properties) Delete(key string) {
	i, ok := p.index[key]
	if !ok {
		return
	}
	p.lines[i] = propLine{removed: true}
	delete(p.index, key)
}

func (p *Properties) Keys() []string {
	out := make([]string, 0, len(p.index))
	for _, l := range p.lines {
		if l.isKV {
			out = append(out, l.key)
		}
	}
	return out
}

// Bytes renders the file. Unmodified lines are written verbatim.
func (p *Properties) Bytes() []byte {
	var b bytes.Buffer
	for _, l := range p.lines {
		switch {
		case l.removed:
			continue
		case !l.isKV:
			b.WriteString(l.raw)
		case l.dirty || l.raw == "":
			b.WriteString(escape(l.key, true))
			b.WriteByte('=')
			b.WriteString(escape(l.value, false))
		default:
			b.WriteString(l.raw)
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}
