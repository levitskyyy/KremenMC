package command

import (
	"fmt"
	"strconv"
	"strings"
)

// SyntaxError is a brigadier-style parse error with the cursor it occurred at.
type SyntaxError struct {
	Message string
	Input   string
	Cursor  int // -1 when the error has no input context
}

func (e *SyntaxError) Error() string {
	if e.Cursor < 0 {
		return e.Message
	}
	return fmt.Sprintf("%s at position %d: %s", e.Message, e.Cursor, e.Context())
}

// Context renders the last ten characters before the cursor followed by
// "<--[HERE]", as brigadier does.
func (e *SyntaxError) Context() string {
	if e.Cursor < 0 {
		return ""
	}
	c := min(e.Cursor, len(e.Input))
	var b strings.Builder
	if c > 10 {
		b.WriteString("...")
	}
	b.WriteString(e.Input[max(0, c-10):c])
	b.WriteString("<--[HERE]")
	return b.String()
}

type reader struct {
	s   string
	pos int
}

func (r *reader) canRead() bool       { return r.pos < len(r.s) }
func (r *reader) canReadN(n int) bool { return r.pos+n <= len(r.s) }
func (r *reader) peek() byte          { return r.s[r.pos] }
func (r *reader) skip()               { r.pos++ }
func (r *reader) remaining() string   { return r.s[r.pos:] }

func (r *reader) errAt(cursor int, format string, args ...any) *SyntaxError {
	return &SyntaxError{Message: fmt.Sprintf(format, args...), Input: r.s, Cursor: cursor}
}

func (r *reader) errf(format string, args ...any) *SyntaxError {
	return r.errAt(r.pos, format, args...)
}

func (r *reader) skipWhitespace() {
	for r.canRead() && r.peek() == ' ' {
		r.skip()
	}
}

func (r *reader) expect(c byte) error {
	if !r.canRead() || r.peek() != c {
		return r.errf("Expected '%c'", c)
	}
	r.skip()
	return nil
}

func isNumberChar(c byte) bool { return c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' }

func isUnquotedChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' ||
		c == '_' || c == '-' || c == '.' || c == '+'
}

func isQuote(c byte) bool { return c == '"' || c == '\'' }

func (r *reader) readWhile(ok func(byte) bool) string {
	start := r.pos
	for r.canRead() && ok(r.peek()) {
		r.skip()
	}
	return r.s[start:r.pos]
}

func (r *reader) readUntilSpace() string { return r.readWhile(func(c byte) bool { return c != ' ' }) }

func (r *reader) readNumber(kind string, bits int, float bool) (string, int, error) {
	start := r.pos
	num := r.readWhile(isNumberChar)
	if num == "" {
		return "", start, r.errf("Expected %s", kind)
	}
	var err error
	if float {
		_, err = strconv.ParseFloat(num, bits)
	} else {
		_, err = strconv.ParseInt(num, 10, bits)
	}
	if err != nil {
		r.pos = start
		return "", start, r.errf("Invalid %s '%s'", kind, num)
	}
	return num, start, nil
}

func (r *reader) readInt() (int64, error) {
	s, _, err := r.readNumber("integer", 32, false)
	if err != nil {
		return 0, err
	}
	v, _ := strconv.ParseInt(s, 10, 32)
	return v, nil
}

func (r *reader) readLong() (int64, error) {
	s, _, err := r.readNumber("long", 64, false)
	if err != nil {
		return 0, err
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v, nil
}

func (r *reader) readDouble() (float64, error) {
	s, _, err := r.readNumber("double", 64, true)
	if err != nil {
		return 0, err
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v, nil
}

func (r *reader) readFloat() (float64, error) {
	s, _, err := r.readNumber("float", 32, true)
	if err != nil {
		return 0, err
	}
	v, _ := strconv.ParseFloat(s, 32)
	return v, nil
}

func (r *reader) readUnquoted() string { return r.readWhile(isUnquotedChar) }

func (r *reader) readQuoted() (string, error) {
	if !r.canRead() {
		return "", nil
	}
	q := r.peek()
	if !isQuote(q) {
		return "", r.errf("Expected quote to start a string")
	}
	r.skip()
	return r.readUntil(q)
}

func (r *reader) readUntil(term byte) (string, error) {
	var b strings.Builder
	escaped := false
	for r.canRead() {
		c := r.peek()
		r.skip()
		switch {
		case escaped:
			if c != term && c != '\\' {
				r.pos--
				return "", r.errf("Invalid escape sequence '\\%c' in quoted string", c)
			}
			b.WriteByte(c)
			escaped = false
		case c == '\\':
			escaped = true
		case c == term:
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", r.errf("Unclosed quoted string")
}

func (r *reader) readString() (string, error) {
	if !r.canRead() {
		return "", nil
	}
	if q := r.peek(); isQuote(q) {
		r.skip()
		return r.readUntil(q)
	}
	return r.readUnquoted(), nil
}

func (r *reader) readBool() (bool, error) {
	start := r.pos
	v, err := r.readString()
	if err != nil {
		return false, err
	}
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "":
		return false, r.errf("Expected bool")
	}
	r.pos = start
	return false, r.errf("Invalid bool, expected true or false but found '%s'", v)
}

func isKeyChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '_' || c == ':' || c == '/' || c == '.' || c == '-'
}

// readKey reads a resource location, defaulting the namespace to minecraft.
func (r *reader) readKey() (string, error) {
	start := r.pos
	s := r.readWhile(isKeyChar)
	if s == "" {
		return "", r.errf("Expected resource location")
	}
	ns, path, ok := strings.Cut(s, ":")
	if !ok {
		return "minecraft:" + s, nil
	}
	if strings.ContainsAny(ns, "/") || strings.Contains(path, ":") || ns == "" && path == "" {
		r.pos = start
		return "", r.errf("Invalid ID")
	}
	if ns == "" {
		ns = "minecraft"
	}
	return ns + ":" + path, nil
}
