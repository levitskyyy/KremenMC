package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// Text is a chat component. On the wire it is network NBT (1.20.3+); the
// status response uses JSON. Supported content: text, translate, keybind,
// score, selector.
type Text struct {
	Text      string
	Translate string
	Fallback  string
	With      []Text
	Keybind   string
	Score     *Score
	Selector  string
	Separator *Text

	Color         string
	Font          string
	Bold          Opt[bool]
	Italic        Opt[bool]
	Underlined    Opt[bool]
	Strikethrough Opt[bool]
	Obfuscated    Opt[bool]
	ShadowColor   Opt[int32]
	Insertion     string
	ClickEvent    *ClickEvent
	HoverEvent    *HoverEvent

	Extra []Text
}

type Score struct {
	Name      string
	Objective string
}

// ClickEvent Value carries url, command, clipboard value, dialog or custom id
// depending on Action; Page is used by change_page.
type ClickEvent struct {
	Action string
	Value  string
	Page   int32
}

// HoverEvent: show_text uses Contents; show_item uses ID and Count;
// show_entity uses ID, UUID and Name.
type HoverEvent struct {
	Action   string
	Contents *Text
	ID       string
	Count    int32
	UUID     [16]byte
	Name     *Text
}

const (
	Black       = "black"
	DarkBlue    = "dark_blue"
	DarkGreen   = "dark_green"
	DarkAqua    = "dark_aqua"
	DarkRed     = "dark_red"
	DarkPurple  = "dark_purple"
	Gold        = "gold"
	Gray        = "gray"
	DarkGray    = "dark_gray"
	Blue        = "blue"
	Green       = "green"
	Aqua        = "aqua"
	Red         = "red"
	LightPurple = "light_purple"
	Yellow      = "yellow"
	White       = "white"
)

func Plain(s string) Text { return Text{Text: s} }

func Translatable(key string, with ...Text) Text { return Text{Translate: key, With: with} }

// String returns the concatenated literal content, ignoring styling.
func (t *Text) String() string {
	var sb strings.Builder
	t.appendPlain(&sb)
	return sb.String()
}

func (t *Text) appendPlain(sb *strings.Builder) {
	switch {
	case t.Translate != "":
		if t.Fallback != "" {
			sb.WriteString(t.Fallback)
		} else {
			sb.WriteString(t.Translate)
		}
	case t.Keybind != "":
		sb.WriteString(t.Keybind)
	case t.Selector != "":
		sb.WriteString(t.Selector)
	default:
		sb.WriteString(t.Text)
	}
	for i := range t.Extra {
		t.Extra[i].appendPlain(sb)
	}
}

func (t *Text) isPlain() bool {
	return t.Translate == "" && t.Keybind == "" && t.Score == nil && t.Selector == "" &&
		t.Color == "" && t.Font == "" && !t.Bold.Ok && !t.Italic.Ok && !t.Underlined.Ok &&
		!t.Strikethrough.Ok && !t.Obfuscated.Ok && !t.ShadowColor.Ok && t.Insertion == "" &&
		t.ClickEvent == nil && t.HoverEvent == nil && len(t.Extra) == 0
}

func nbtName(w *Writer, tag byte, name string) {
	w.U8(tag)
	nbtStr(w, name)
}

// nbtStr writes an NBT string, truncating at a rune boundary past 65535 bytes.
func nbtStr(w *Writer, s string) {
	if len(s) > math.MaxUint16 {
		n := math.MaxUint16
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n]
	}
	w.U16(uint16(len(s)))
	w.B = append(w.B, s...)
}

func nbtString(w *Writer, name, v string) {
	nbtName(w, nbt.TagString, name)
	nbtStr(w, v)
}

func nbtBool(w *Writer, name string, v Opt[bool]) {
	if !v.Ok {
		return
	}
	nbtName(w, nbt.TagByte, name)
	w.Bool(v.V)
}

func nbtTextList(w *Writer, name string, l []Text) {
	nbtName(w, nbt.TagList, name)
	w.U8(nbt.TagCompound)
	w.I32(int32(len(l)))
	for i := range l {
		l[i].compound(w)
	}
}

// Encode writes t as network NBT.
func (t *Text) Encode(w *Writer) {
	if t.isPlain() {
		w.U8(nbt.TagString)
		nbtStr(w, t.Text)
		return
	}
	w.U8(nbt.TagCompound)
	t.compound(w)
}

func (t *Text) compound(w *Writer) {
	switch {
	case t.Translate != "":
		nbtString(w, "translate", t.Translate)
		if t.Fallback != "" {
			nbtString(w, "fallback", t.Fallback)
		}
		if len(t.With) > 0 {
			nbtTextList(w, "with", t.With)
		}
	case t.Keybind != "":
		nbtString(w, "keybind", t.Keybind)
	case t.Score != nil:
		nbtName(w, nbt.TagCompound, "score")
		nbtString(w, "name", t.Score.Name)
		nbtString(w, "objective", t.Score.Objective)
		w.U8(nbt.TagEnd)
	case t.Selector != "":
		nbtString(w, "selector", t.Selector)
		if t.Separator != nil {
			nbtName(w, nbt.TagCompound, "separator")
			t.Separator.compound(w)
		}
	default:
		nbtString(w, "text", t.Text)
	}
	if t.Color != "" {
		nbtString(w, "color", t.Color)
	}
	if t.Font != "" {
		nbtString(w, "font", t.Font)
	}
	nbtBool(w, "bold", t.Bold)
	nbtBool(w, "italic", t.Italic)
	nbtBool(w, "underlined", t.Underlined)
	nbtBool(w, "strikethrough", t.Strikethrough)
	nbtBool(w, "obfuscated", t.Obfuscated)
	if t.ShadowColor.Ok {
		nbtName(w, nbt.TagInt, "shadow_color")
		w.I32(t.ShadowColor.V)
	}
	if t.Insertion != "" {
		nbtString(w, "insertion", t.Insertion)
	}
	if c := t.ClickEvent; c != nil {
		nbtName(w, nbt.TagCompound, "click_event")
		nbtString(w, "action", c.Action)
		if c.Action == "change_page" {
			nbtName(w, nbt.TagInt, "page")
			w.I32(c.Page)
		} else {
			nbtString(w, clickKey(c.Action), c.Value)
		}
		w.U8(nbt.TagEnd)
	}
	if h := t.HoverEvent; h != nil {
		nbtName(w, nbt.TagCompound, "hover_event")
		nbtString(w, "action", h.Action)
		switch h.Action {
		case "show_text":
			nbtName(w, nbt.TagCompound, "value")
			if h.Contents != nil {
				h.Contents.compound(w)
			} else {
				nbtString(w, "text", "")
				w.U8(nbt.TagEnd)
			}
		case "show_item":
			nbtString(w, "id", h.ID)
			if h.Count != 0 {
				nbtName(w, nbt.TagInt, "count")
				w.I32(h.Count)
			}
		case "show_entity":
			nbtString(w, "id", h.ID)
			nbtName(w, nbt.TagIntArray, "uuid")
			w.I32(4)
			w.Raw(h.UUID[:])
			if h.Name != nil {
				nbtName(w, nbt.TagCompound, "name")
				h.Name.compound(w)
			}
		}
		w.U8(nbt.TagEnd)
	}
	if len(t.Extra) > 0 {
		nbtTextList(w, "extra", t.Extra)
	}
	w.U8(nbt.TagEnd)
}

func clickKey(action string) string {
	switch action {
	case "open_url":
		return "url"
	case "open_file":
		return "path"
	case "run_command", "suggest_command":
		return "command"
	case "show_dialog":
		return "dialog"
	case "custom":
		return "id"
	}
	return "value"
}

// NBT returns the network NBT encoding of t.
func (t *Text) NBT() RawNBT {
	w := Writer{}
	t.Encode(&w)
	return RawNBT(w.B)
}

// Decode reads a network NBT text component.
func (t *Text) Decode(r *Reader) error {
	tag, err := r.U8()
	if err != nil {
		return err
	}
	*t = Text{}
	if tag == nbt.TagEnd {
		return nil
	}
	return t.decodeTag(r, tag, 0)
}

func readNBTString(r *Reader) (string, error) {
	n, err := r.U16()
	if err != nil {
		return "", err
	}
	p, err := r.take(int(n))
	return string(p), err
}

func skipNBT(r *Reader, tag byte) error {
	end, err := nbtPayloadLen(r.B, r.Off, tag, 0)
	if err != nil {
		return err
	}
	r.Off = end
	return nil
}

func nbtScalarString(r *Reader, tag byte) (string, bool, error) {
	switch tag {
	case nbt.TagString:
		s, err := readNBTString(r)
		return s, true, err
	case nbt.TagByte:
		v, err := r.I8()
		return strconv.Itoa(int(v)), true, err
	case nbt.TagShort:
		v, err := r.I16()
		return strconv.Itoa(int(v)), true, err
	case nbt.TagInt:
		v, err := r.I32()
		return strconv.Itoa(int(v)), true, err
	case nbt.TagLong:
		v, err := r.I64()
		return strconv.FormatInt(v, 10), true, err
	case nbt.TagFloat:
		v, err := r.F32()
		return strconv.FormatFloat(float64(v), 'g', -1, 32), true, err
	case nbt.TagDouble:
		v, err := r.F64()
		return strconv.FormatFloat(v, 'g', -1, 64), true, err
	}
	return "", false, nil
}

func (t *Text) decodeTag(r *Reader, tag byte, depth int) error {
	if depth > maxNBTDepth {
		return ErrNBT
	}
	switch tag {
	case nbt.TagCompound:
		return t.decodeCompound(r, depth)
	case nbt.TagList:
		l, err := decodeTextList(r, depth)
		if err != nil {
			return err
		}
		if len(l) > 0 {
			*t = l[0]
			t.Extra = append(t.Extra, l[1:]...)
		}
		return nil
	}
	s, ok, err := nbtScalarString(r, tag)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNBT
	}
	t.Text = s
	return nil
}

func decodeTextList(r *Reader, depth int) ([]Text, error) {
	et, err := r.U8()
	if err != nil {
		return nil, err
	}
	n, err := r.I32()
	if err != nil {
		return nil, err
	}
	if n < 0 || int(n) > r.Remaining() {
		return nil, ErrNBT
	}
	if n == 0 {
		return nil, nil
	}
	l := make([]Text, n)
	for i := range l {
		if err := l[i].decodeTag(r, et, depth+1); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func readNBTBool(r *Reader, tag byte) (Opt[bool], error) {
	if tag != nbt.TagByte {
		return Opt[bool]{}, skipNBT(r, tag)
	}
	v, err := r.U8()
	return Some(v != 0), err
}

func (t *Text) decodeCompound(r *Reader, depth int) error {
	for {
		tag, err := r.U8()
		if err != nil {
			return err
		}
		if tag == nbt.TagEnd {
			return nil
		}
		key, err := readNBTString(r)
		if err != nil {
			return err
		}
		if err := t.decodeKey(r, key, tag, depth); err != nil {
			return fmt.Errorf("text %q: %w", key, err)
		}
	}
}

func (t *Text) decodeKey(r *Reader, key string, tag byte, depth int) error {
	str := func(dst *string) error {
		if tag != nbt.TagString {
			return skipNBT(r, tag)
		}
		s, err := readNBTString(r)
		*dst = s
		return err
	}
	var err error
	switch key {
	case "", "text":
		if key == "" && tag != nbt.TagString {
			return t.decodeTag(r, tag, depth+1)
		}
		s, ok, e := nbtScalarString(r, tag)
		if !ok {
			return skipNBT(r, tag)
		}
		t.Text, err = s, e
	case "translate":
		err = str(&t.Translate)
	case "fallback":
		err = str(&t.Fallback)
	case "keybind":
		err = str(&t.Keybind)
	case "selector":
		err = str(&t.Selector)
	case "color":
		err = str(&t.Color)
	case "font":
		err = str(&t.Font)
	case "insertion":
		err = str(&t.Insertion)
	case "bold":
		t.Bold, err = readNBTBool(r, tag)
	case "italic":
		t.Italic, err = readNBTBool(r, tag)
	case "underlined":
		t.Underlined, err = readNBTBool(r, tag)
	case "strikethrough":
		t.Strikethrough, err = readNBTBool(r, tag)
	case "obfuscated":
		t.Obfuscated, err = readNBTBool(r, tag)
	case "shadow_color":
		if tag != nbt.TagInt {
			return skipNBT(r, tag)
		}
		var v int32
		v, err = r.I32()
		t.ShadowColor = Some(v)
	case "with", "extra":
		if tag != nbt.TagList {
			return skipNBT(r, tag)
		}
		var l []Text
		if l, err = decodeTextList(r, depth); err != nil {
			return err
		}
		if key == "with" {
			t.With = l
		} else {
			t.Extra = l
		}
	case "separator":
		t.Separator = &Text{}
		err = t.Separator.decodeTag(r, tag, depth+1)
	case "score":
		if tag != nbt.TagCompound {
			return skipNBT(r, tag)
		}
		t.Score = &Score{}
		err = decodeFields(r, func(k string, tag byte) error {
			switch k {
			case "name":
				return strField(r, tag, &t.Score.Name)
			case "objective":
				return strField(r, tag, &t.Score.Objective)
			}
			return skipNBT(r, tag)
		})
	case "click_event", "clickEvent":
		if tag != nbt.TagCompound {
			return skipNBT(r, tag)
		}
		c := &ClickEvent{}
		t.ClickEvent = c
		err = decodeFields(r, func(k string, tag byte) error {
			switch k {
			case "action":
				return strField(r, tag, &c.Action)
			case "page":
				if tag == nbt.TagInt {
					v, err := r.I32()
					c.Page = v
					return err
				}
			case "url", "path", "command", "value", "dialog", "id":
				if tag == nbt.TagString {
					return strField(r, tag, &c.Value)
				}
			}
			return skipNBT(r, tag)
		})
	case "hover_event", "hoverEvent":
		if tag != nbt.TagCompound {
			return skipNBT(r, tag)
		}
		h := &HoverEvent{}
		t.HoverEvent = h
		err = decodeFields(r, func(k string, tag byte) error {
			switch k {
			case "action":
				return strField(r, tag, &h.Action)
			case "value", "contents":
				h.Contents = &Text{}
				return h.Contents.decodeTag(r, tag, depth+1)
			case "id":
				return strField(r, tag, &h.ID)
			case "count":
				if tag == nbt.TagInt {
					v, err := r.I32()
					h.Count = v
					return err
				}
			case "uuid":
				if tag == nbt.TagIntArray {
					n, err := r.I32()
					if err != nil {
						return err
					}
					if n != 4 {
						return ErrNBT
					}
					p, err := r.take(16)
					copy(h.UUID[:], p)
					return err
				}
			case "name":
				h.Name = &Text{}
				return h.Name.decodeTag(r, tag, depth+1)
			}
			return skipNBT(r, tag)
		})
	default:
		err = skipNBT(r, tag)
	}
	return err
}

func strField(r *Reader, tag byte, dst *string) error {
	if tag != nbt.TagString {
		return skipNBT(r, tag)
	}
	s, err := readNBTString(r)
	*dst = s
	return err
}

func decodeFields(r *Reader, f func(key string, tag byte) error) error {
	for {
		tag, err := r.U8()
		if err != nil {
			return err
		}
		if tag == nbt.TagEnd {
			return nil
		}
		key, err := readNBTString(r)
		if err != nil {
			return err
		}
		if err := f(key, tag); err != nil {
			return err
		}
	}
}

// MarshalJSON encodes t in the JSON component format.
func (t Text) MarshalJSON() ([]byte, error) {
	if t.isPlain() {
		return json.Marshal(t.Text)
	}
	return json.Marshal(t.jsonMap())
}

func (t *Text) jsonMap() map[string]any {
	m := map[string]any{}
	switch {
	case t.Translate != "":
		m["translate"] = t.Translate
		if t.Fallback != "" {
			m["fallback"] = t.Fallback
		}
		if len(t.With) > 0 {
			m["with"] = jsonList(t.With)
		}
	case t.Keybind != "":
		m["keybind"] = t.Keybind
	case t.Score != nil:
		m["score"] = map[string]any{"name": t.Score.Name, "objective": t.Score.Objective}
	case t.Selector != "":
		m["selector"] = t.Selector
		if t.Separator != nil {
			m["separator"] = t.Separator.jsonMap()
		}
	default:
		m["text"] = t.Text
	}
	if t.Color != "" {
		m["color"] = t.Color
	}
	if t.Font != "" {
		m["font"] = t.Font
	}
	for _, f := range [...]struct {
		k string
		v Opt[bool]
	}{{"bold", t.Bold}, {"italic", t.Italic}, {"underlined", t.Underlined}, {"strikethrough", t.Strikethrough}, {"obfuscated", t.Obfuscated}} {
		if f.v.Ok {
			m[f.k] = f.v.V
		}
	}
	if t.ShadowColor.Ok {
		m["shadow_color"] = t.ShadowColor.V
	}
	if t.Insertion != "" {
		m["insertion"] = t.Insertion
	}
	if c := t.ClickEvent; c != nil {
		cm := map[string]any{"action": c.Action}
		if c.Action == "change_page" {
			cm["page"] = c.Page
		} else {
			cm[clickKey(c.Action)] = c.Value
		}
		m["click_event"] = cm
	}
	if h := t.HoverEvent; h != nil {
		hm := map[string]any{"action": h.Action}
		switch h.Action {
		case "show_text":
			if h.Contents != nil {
				hm["value"] = h.Contents.jsonMap()
			} else {
				hm["value"] = ""
			}
		case "show_item":
			hm["id"] = h.ID
			if h.Count != 0 {
				hm["count"] = h.Count
			}
		case "show_entity":
			hm["id"] = h.ID
			u := h.UUID
			hm["uuid"] = []int32{int32(binary.BigEndian.Uint32(u[0:])), int32(binary.BigEndian.Uint32(u[4:])), int32(binary.BigEndian.Uint32(u[8:])), int32(binary.BigEndian.Uint32(u[12:]))}
			if h.Name != nil {
				hm["name"] = h.Name.jsonMap()
			}
		}
		m["hover_event"] = hm
	}
	if len(t.Extra) > 0 {
		m["extra"] = jsonList(t.Extra)
	}
	return m
}

func jsonList(l []Text) []any {
	out := make([]any, len(l))
	for i := range l {
		out[i] = l[i].jsonMap()
	}
	return out
}

func (t *Text) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*t = textFromJSON(v, 0)
	return nil
}

func textFromJSON(v any, depth int) Text {
	if depth > maxNBTDepth {
		return Text{}
	}
	switch x := v.(type) {
	case string:
		return Text{Text: x}
	case float64:
		return Text{Text: strconv.FormatFloat(x, 'g', -1, 64)}
	case bool:
		return Text{Text: strconv.FormatBool(x)}
	case []any:
		if len(x) == 0 {
			return Text{}
		}
		t := textFromJSON(x[0], depth+1)
		for _, e := range x[1:] {
			t.Extra = append(t.Extra, textFromJSON(e, depth+1))
		}
		return t
	case map[string]any:
		return textFromMap(x, depth)
	}
	return Text{}
}

func jsonStr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func jsonBool(m map[string]any, k string) Opt[bool] {
	if b, ok := m[k].(bool); ok {
		return Some(b)
	}
	return Opt[bool]{}
}

func jsonTexts(v any, depth int) []Text {
	l, _ := v.([]any)
	if len(l) == 0 {
		return nil
	}
	out := make([]Text, len(l))
	for i, e := range l {
		out[i] = textFromJSON(e, depth+1)
	}
	return out
}

func textFromMap(m map[string]any, depth int) Text {
	t := Text{
		Translate:     jsonStr(m, "translate"),
		Fallback:      jsonStr(m, "fallback"),
		Keybind:       jsonStr(m, "keybind"),
		Selector:      jsonStr(m, "selector"),
		Color:         jsonStr(m, "color"),
		Font:          jsonStr(m, "font"),
		Insertion:     jsonStr(m, "insertion"),
		Bold:          jsonBool(m, "bold"),
		Italic:        jsonBool(m, "italic"),
		Underlined:    jsonBool(m, "underlined"),
		Strikethrough: jsonBool(m, "strikethrough"),
		Obfuscated:    jsonBool(m, "obfuscated"),
		With:          jsonTexts(m["with"], depth),
		Extra:         jsonTexts(m["extra"], depth),
	}
	switch x := m["text"].(type) {
	case string:
		t.Text = x
	case float64:
		t.Text = strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		t.Text = strconv.FormatBool(x)
	}
	if sc, ok := m["shadow_color"].(float64); ok && sc == math.Trunc(sc) {
		t.ShadowColor = Some(int32(int64(sc)))
	}
	if s, ok := m["score"].(map[string]any); ok {
		t.Score = &Score{Name: jsonStr(s, "name"), Objective: jsonStr(s, "objective")}
	}
	if s, ok := m["separator"]; ok {
		sep := textFromJSON(s, depth+1)
		t.Separator = &sep
	}
	c, ok := m["click_event"].(map[string]any)
	if !ok {
		c, ok = m["clickEvent"].(map[string]any)
	}
	if ok {
		ce := &ClickEvent{Action: jsonStr(c, "action")}
		if p, ok := c["page"].(float64); ok {
			ce.Page = int32(p)
		}
		for _, k := range [...]string{"url", "path", "command", "value", "dialog", "id"} {
			if s, ok := c[k].(string); ok {
				ce.Value = s
				break
			}
		}
		t.ClickEvent = ce
	}
	h, ok := m["hover_event"].(map[string]any)
	if !ok {
		h, ok = m["hoverEvent"].(map[string]any)
	}
	if ok {
		he := &HoverEvent{Action: jsonStr(h, "action"), ID: jsonStr(h, "id")}
		for _, k := range [...]string{"value", "contents"} {
			if v, ok := h[k]; ok && he.Action == "show_text" {
				ct := textFromJSON(v, depth+1)
				he.Contents = &ct
				break
			}
		}
		if n, ok := h["count"].(float64); ok {
			he.Count = int32(n)
		}
		if u, ok := h["uuid"].([]any); ok && len(u) == 4 {
			for i, e := range u {
				if f, ok := e.(float64); ok {
					binary.BigEndian.PutUint32(he.UUID[i*4:], uint32(int32(f)))
				}
			}
		}
		if n, ok := h["name"]; ok {
			nt := textFromJSON(n, depth+1)
			he.Name = &nt
		}
		t.HoverEvent = he
	}
	return t
}
