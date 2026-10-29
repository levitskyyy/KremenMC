package command

import (
	"fmt"
	"strconv"

	"github.com/levitskyyy/KremenMC/api"
)

// parseComponentArg reads a text component in JSON or SNBT form (vanilla
// accepts both since 1.21.5) and converts it to api.Component.
func parseComponentArg(r *reader) (api.Component, error) {
	start := r.pos
	v, err := readValue(r)
	if err != nil {
		return api.Component{}, err
	}
	c, err := toComponent(v)
	if err != nil {
		r.pos = start
		return api.Component{}, r.errf("Invalid chat component: %s", err.Error())
	}
	return c, nil
}

func readValue(r *reader) (any, error) {
	r.skipWhitespace()
	if !r.canRead() {
		return nil, r.errf("Expected value")
	}
	switch c := r.peek(); {
	case c == '{':
		r.skip()
		m := map[string]any{}
		r.skipWhitespace()
		if r.canRead() && r.peek() == '}' {
			r.skip()
			return m, nil
		}
		for {
			r.skipWhitespace()
			key, err := r.readString()
			if err != nil {
				return nil, err
			}
			if key == "" {
				return nil, r.errf("Expected key")
			}
			r.skipWhitespace()
			if err := r.expect(':'); err != nil {
				return nil, err
			}
			v, err := readValue(r)
			if err != nil {
				return nil, err
			}
			m[key] = v
			r.skipWhitespace()
			if r.canRead() && r.peek() == ',' {
				r.skip()
				continue
			}
			if err := r.expect('}'); err != nil {
				return nil, err
			}
			return m, nil
		}
	case c == '[':
		r.skip()
		var l []any
		r.skipWhitespace()
		if r.canRead() && r.peek() == ']' {
			r.skip()
			return l, nil
		}
		for {
			v, err := readValue(r)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
			r.skipWhitespace()
			if r.canRead() && r.peek() == ',' {
				r.skip()
				continue
			}
			if err := r.expect(']'); err != nil {
				return nil, err
			}
			return l, nil
		}
	case isQuote(c):
		return r.readString()
	default:
		s := r.readUnquoted()
		if s == "" {
			return nil, r.errf("Expected value")
		}
		switch s {
		case "true", "1b":
			return true, nil
		case "false", "0b":
			return false, nil
		}
		return s, nil
	}
}

func toComponent(v any) (api.Component, error) {
	switch v := v.(type) {
	case string:
		return api.Text(v), nil
	case bool:
		return api.Text(strconv.FormatBool(v)), nil
	case []any:
		if len(v) == 0 {
			return api.Component{}, fmt.Errorf("empty list")
		}
		head, err := toComponent(v[0])
		if err != nil {
			return head, err
		}
		for _, e := range v[1:] {
			c, err := toComponent(e)
			if err != nil {
				return head, err
			}
			head.Extra = append(head.Extra, c)
		}
		return head, nil
	case map[string]any:
		return mapComponent(v)
	}
	return api.Component{}, fmt.Errorf("unsupported value")
}

func mapComponent(m map[string]any) (api.Component, error) {
	var c api.Component
	str := func(k string) string {
		s, _ := m[k].(string)
		return s
	}
	flag := func(k string) *bool {
		switch b := m[k].(type) {
		case bool:
			return &b
		case string:
			if v, err := strconv.ParseBool(b); err == nil {
				return &v
			}
		}
		return nil
	}
	c.Text = str("text")
	c.Translate = str("translate")
	c.Keybind = str("keybind")
	c.Color = str("color")
	c.Font = str("font")
	c.Insertion = str("insertion")
	c.Bold, c.Italic, c.Underline, c.Strike, c.Obfuscate = flag("bold"), flag("italic"), flag("underlined"), flag("strikethrough"), flag("obfuscated")
	if _, ok := m["text"]; !ok && c.Translate == "" && c.Keybind == "" {
		if _, ok := m["extra"]; !ok {
			return c, fmt.Errorf("missing text, translate or keybind")
		}
	}
	for _, key := range []string{"with", "extra"} {
		l, ok := m[key]
		if !ok {
			continue
		}
		list, ok := l.([]any)
		if !ok {
			return c, fmt.Errorf("%s must be a list", key)
		}
		for _, e := range list {
			sub, err := toComponent(e)
			if err != nil {
				return c, err
			}
			if key == "with" {
				c.With = append(c.With, sub)
			} else {
				c.Extra = append(c.Extra, sub)
			}
		}
	}
	for _, key := range []string{"click_event", "clickEvent"} {
		ev, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		action, _ := ev["action"].(string)
		val := ""
		for _, f := range []string{"value", "url", "command", "page"} {
			if s, ok := ev[f].(string); ok {
				val = s
				break
			}
		}
		if a, ok := clickActions[action]; ok {
			c.Click = &api.ClickEvent{Action: a, Value: val}
		}
	}
	for _, key := range []string{"hover_event", "hoverEvent"} {
		ev, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		for _, f := range []string{"value", "text", "contents"} {
			if raw, ok := ev[f]; ok {
				if t, err := toComponent(raw); err == nil {
					c.Hover = &api.HoverEvent{Text: &t}
				}
				break
			}
		}
	}
	return c, nil
}

var clickActions = map[string]api.ClickAction{
	"open_url":          api.ClickOpenURL,
	"run_command":       api.ClickRunCommand,
	"suggest_command":   api.ClickSuggestCommand,
	"change_page":       api.ClickChangePage,
	"copy_to_clipboard": api.ClickCopyToClipboard,
}
