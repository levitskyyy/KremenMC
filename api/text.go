package api

import "strings"

// Component is the API-level chat component. It mirrors the vanilla JSON/NBT
// text model closely enough to convert losslessly for the common cases.
type Component struct {
	Text      string
	Translate string
	With      []Component
	Keybind   string
	Color     string // named color ("red") or "#rrggbb"; empty inherits
	Font      string
	Bold      *bool
	Italic    *bool
	Underline *bool
	Strike    *bool
	Obfuscate *bool
	Insertion string
	Click     *ClickEvent
	Hover     *HoverEvent
	Extra     []Component
}

type ClickAction uint8

const (
	ClickOpenURL ClickAction = iota
	ClickRunCommand
	ClickSuggestCommand
	ClickChangePage
	ClickCopyToClipboard
)

type ClickEvent struct {
	Action ClickAction
	Value  string
}

type HoverEvent struct {
	Text *Component
}

func Text(s string) Component           { return Component{Text: s} }
func Colored(s, color string) Component { return Component{Text: s, Color: color} }
func Translatable(key string, with ...Component) Component {
	return Component{Translate: key, With: with}
}

func (c Component) Append(children ...Component) Component {
	c.Extra = append(append([]Component(nil), c.Extra...), children...)
	return c
}

func (c Component) WithColor(color string) Component { c.Color = color; return c }

func (c Component) WithBold(b bool) Component { c.Bold = &b; return c }

func (c Component) WithItalic(b bool) Component { c.Italic = &b; return c }

func (c Component) IsEmpty() bool {
	return c.Text == "" && c.Translate == "" && c.Keybind == "" && len(c.Extra) == 0
}

// Plain flattens the component to unstyled text. Translatable components are
// rendered as their key followed by their arguments.
func (c Component) Plain() string {
	var b strings.Builder
	c.writePlain(&b)
	return b.String()
}

func (c Component) writePlain(b *strings.Builder) {
	b.WriteString(c.Text)
	if c.Translate != "" {
		b.WriteString(c.Translate)
		for _, w := range c.With {
			b.WriteByte(' ')
			w.writePlain(b)
		}
	}
	for _, e := range c.Extra {
		e.writePlain(b)
	}
}
