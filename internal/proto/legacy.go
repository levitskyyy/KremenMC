package proto

import "strings"

// LegacyChar is the section sign used by legacy formatting codes.
const LegacyChar = '§'

var legacyColors = [16]string{
	Black, DarkBlue, DarkGreen, DarkAqua, DarkRed, DarkPurple, Gold, Gray,
	DarkGray, Blue, Green, Aqua, Red, LightPurple, Yellow, White,
}

func hexVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

type legacyStyle struct {
	color                                           string
	bold, italic, underlined, strikethrough, obfusc bool
}

func (s legacyStyle) apply(t *Text) {
	t.Color = s.color
	if s.bold {
		t.Bold = Some(true)
	}
	if s.italic {
		t.Italic = Some(true)
	}
	if s.underlined {
		t.Underlined = Some(true)
	}
	if s.strikethrough {
		t.Strikethrough = Some(true)
	}
	if s.obfusc {
		t.Obfuscated = Some(true)
	}
}

// ParseLegacy converts a string with legacy formatting codes (e.g. "§aHi §lthere")
// introduced by code (usually LegacyChar or '&') into a Text. The Bukkit hex form
// §x§r§r§g§g§b§b is supported. A color code resets formatting, as in vanilla.
func ParseLegacy(s string, code rune) Text {
	var (
		parts []Text
		sb    strings.Builder
		cur   legacyStyle
	)
	flush := func() {
		if sb.Len() == 0 {
			return
		}
		t := Text{Text: sb.String()}
		cur.apply(&t)
		parts = append(parts, t)
		sb.Reset()
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != code || i+1 >= len(rs) {
			sb.WriteRune(rs[i])
			continue
		}
		c := rs[i+1]
		if c == 'x' || c == 'X' {
			if hex, ok := legacyHex(rs[i+2:], code); ok {
				flush()
				cur = legacyStyle{color: hex}
				i += 13
				continue
			}
		}
		lc := c | 0x20
		switch {
		case hexVal(c) >= 0:
			flush()
			cur = legacyStyle{color: legacyColors[hexVal(c)]}
		case lc == 'k':
			flush()
			cur.obfusc = true
		case lc == 'l':
			flush()
			cur.bold = true
		case lc == 'm':
			flush()
			cur.strikethrough = true
		case lc == 'n':
			flush()
			cur.underlined = true
		case lc == 'o':
			flush()
			cur.italic = true
		case lc == 'r':
			flush()
			cur = legacyStyle{}
		default:
			sb.WriteRune(rs[i])
			continue
		}
		i++
	}
	flush()
	switch len(parts) {
	case 0:
		return Text{}
	case 1:
		return parts[0]
	}
	return Text{Extra: parts}
}

func legacyHex(rs []rune, code rune) (string, bool) {
	if len(rs) < 12 {
		return "", false
	}
	var b [7]byte
	b[0] = '#'
	for j := 0; j < 6; j++ {
		if rs[j*2] != code || hexVal(rs[j*2+1]) < 0 {
			return "", false
		}
		b[j+1] = byte(rs[j*2+1] | 0x20)
	}
	return string(b[:]), true
}
