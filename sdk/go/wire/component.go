package wire

import "github.com/levitskyyy/KremenMC/api"

const (
	cText = 1 << iota
	cTranslate
	cColor
	cStyle
	cExtra
	cClick
	cHover
	cMisc
)

const maxComponentDepth = 32

// Component layout: presence bitmask, then the present fields in bit order.
// Style is two bytes: a set-mask and a value-mask over bold, italic,
// underline, strike, obfuscate.
func (w *Writer) Component(c api.Component) {
	var m uint8
	if c.Text != "" {
		m |= cText
	}
	if c.Translate != "" {
		m |= cTranslate
	}
	if c.Color != "" {
		m |= cColor
	}
	if c.Bold != nil || c.Italic != nil || c.Underline != nil || c.Strike != nil || c.Obfuscate != nil {
		m |= cStyle
	}
	if len(c.Extra) > 0 {
		m |= cExtra
	}
	if c.Click != nil {
		m |= cClick
	}
	if c.Hover != nil && c.Hover.Text != nil {
		m |= cHover
	}
	if c.Keybind != "" || c.Font != "" || c.Insertion != "" {
		m |= cMisc
	}
	w.U8(m)
	if m&cText != 0 {
		w.String(c.Text)
	}
	if m&cTranslate != 0 {
		w.String(c.Translate)
		w.Uvarint(uint64(len(c.With)))
		for _, a := range c.With {
			w.Component(a)
		}
	}
	if m&cColor != 0 {
		w.String(c.Color)
	}
	if m&cStyle != 0 {
		var set, val uint8
		for i, p := range [...]*bool{c.Bold, c.Italic, c.Underline, c.Strike, c.Obfuscate} {
			if p != nil {
				set |= 1 << i
				if *p {
					val |= 1 << i
				}
			}
		}
		w.U8(set)
		w.U8(val)
	}
	if m&cExtra != 0 {
		w.Uvarint(uint64(len(c.Extra)))
		for _, e := range c.Extra {
			w.Component(e)
		}
	}
	if m&cClick != 0 {
		w.U8(uint8(c.Click.Action))
		w.String(c.Click.Value)
	}
	if m&cHover != 0 {
		w.Component(*c.Hover.Text)
	}
	if m&cMisc != 0 {
		w.String(c.Keybind)
		w.String(c.Font)
		w.String(c.Insertion)
	}
}

func (r *Reader) Component() api.Component { return r.component(0) }

func (r *Reader) component(depth int) api.Component {
	var c api.Component
	if depth > maxComponentDepth {
		r.fail()
		return c
	}
	m := r.U8()
	if m&cText != 0 {
		c.Text = r.String()
	}
	if m&cTranslate != 0 {
		c.Translate = r.String()
		n := r.Count()
		for i := 0; i < n && r.Err == nil; i++ {
			c.With = append(c.With, r.component(depth+1))
		}
	}
	if m&cColor != 0 {
		c.Color = r.String()
	}
	if m&cStyle != 0 {
		set, val := r.U8(), r.U8()
		for i, p := range [...]**bool{&c.Bold, &c.Italic, &c.Underline, &c.Strike, &c.Obfuscate} {
			if set&(1<<i) != 0 {
				v := val&(1<<i) != 0
				*p = &v
			}
		}
	}
	if m&cExtra != 0 {
		n := r.Count()
		for i := 0; i < n && r.Err == nil; i++ {
			c.Extra = append(c.Extra, r.component(depth+1))
		}
	}
	if m&cClick != 0 {
		c.Click = &api.ClickEvent{Action: api.ClickAction(r.U8()), Value: r.String()}
	}
	if m&cHover != 0 {
		h := r.component(depth + 1)
		c.Hover = &api.HoverEvent{Text: &h}
	}
	if m&cMisc != 0 {
		c.Keybind, c.Font, c.Insertion = r.String(), r.String(), r.String()
	}
	return c
}

func (w *Writer) Item(s api.ItemStack) {
	w.String(string(s.Type))
	w.Varint(int64(s.Count))
	w.Bool(s.Name != nil)
	if s.Name != nil {
		w.Component(*s.Name)
	}
	w.Uvarint(uint64(len(s.Lore)))
	for _, l := range s.Lore {
		w.Component(l)
	}
	w.Varint(int64(s.Damage))
	w.Bool(s.Unbreakable)
}

func (r *Reader) Item() api.ItemStack {
	s := api.ItemStack{Type: api.Key(r.String()), Count: int32(r.Varint())}
	if r.Bool() {
		n := r.Component()
		s.Name = &n
	}
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		s.Lore = append(s.Lore, r.Component())
	}
	s.Damage = int32(r.Varint())
	s.Unbreakable = r.Bool()
	return s
}
