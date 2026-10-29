package server

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

type blockState data.StateID

func stateOf(s data.StateID) api.BlockState { return blockState(s) }

func (b blockState) ID() uint32 { return uint32(b) }

func (b blockState) Type() api.Key {
	if bl := data.BlockOf(data.StateID(b)); bl != nil {
		return api.Key("minecraft:" + bl.Name)
	}
	return "minecraft:air"
}

func (b blockState) Property(name string) (string, bool) {
	bl := data.BlockOf(data.StateID(b))
	if bl == nil {
		return "", false
	}
	i := bl.PropIndex(name)
	if i < 0 {
		return "", false
	}
	return bl.Props[i].Values[bl.ValueIndex(data.StateID(b), i)], true
}

func (b blockState) Properties() map[string]string {
	bl := data.BlockOf(data.StateID(b))
	if bl == nil || len(bl.Props) == 0 {
		return nil
	}
	m := make(map[string]string, len(bl.Props))
	for i := range bl.Props {
		m[bl.Props[i].Name] = bl.Props[i].Values[bl.ValueIndex(data.StateID(b), i)]
	}
	return m
}

func (b blockState) With(name, value string) api.BlockState {
	s, ok := data.With(data.StateID(b), name, value)
	if !ok {
		return b
	}
	return blockState(s)
}

func (b blockState) IsAir() bool    { return data.IsAir(data.StateID(b)) }
func (b blockState) IsSolid() bool  { return data.Flags(data.StateID(b))&data.FlagSolid != 0 }
func (b blockState) IsLiquid() bool { return data.Flags(data.StateID(b))&data.FlagLiquid != 0 }
func (b blockState) String() string { return data.StateString(data.StateID(b)) }

type blockRegistry struct{}

func (blockRegistry) Parse(s string) (api.BlockState, error) {
	id, ok := data.ParseState(s)
	if !ok {
		return nil, fmt.Errorf("unknown block state %q", s)
	}
	return blockState(id), nil
}

func (blockRegistry) ByID(id uint32) (api.BlockState, bool) {
	if !data.Canonical().Valid(data.StateID(id)) {
		return nil, false
	}
	return blockState(id), true
}

func (blockRegistry) Default(block api.Key) (api.BlockState, bool) {
	s, ok := data.DefaultState(string(block))
	if !ok {
		return nil, false
	}
	return blockState(s), true
}

func (blockRegistry) Air() api.BlockState { return blockState(data.Air) }

type itemRegistry struct{}

func (itemRegistry) MaxStackSize(kind api.Key) int32 {
	r := data.Canonical()
	id, ok := r.ItemByName(string(kind))
	if !ok {
		return 0
	}
	return int32(r.Items[id].StackSize)
}

func (itemRegistry) Exists(kind api.Key) bool {
	_, ok := data.Canonical().ItemByName(string(kind))
	return ok
}

func (itemRegistry) ID(kind api.Key) (int32, bool) {
	id, ok := data.Canonical().ItemByName(string(kind))
	return int32(id), ok
}

func (itemRegistry) ByID(id int32) (api.Key, bool) {
	n := data.Canonical().ItemName(data.ItemID(id))
	if n == "" {
		return "", false
	}
	return api.Key("minecraft:" + n), true
}

var clickActions = [...]string{
	api.ClickOpenURL:         "open_url",
	api.ClickRunCommand:      "run_command",
	api.ClickSuggestCommand:  "suggest_command",
	api.ClickChangePage:      "change_page",
	api.ClickCopyToClipboard: "copy_to_clipboard",
}

func optBool(b *bool) proto.Opt[bool] {
	if b == nil {
		return proto.Opt[bool]{}
	}
	return proto.Some(*b)
}

// toText converts an API component to the wire representation.
func toText(c api.Component) proto.Text {
	t := proto.Text{
		Text: c.Text, Translate: c.Translate, Keybind: c.Keybind, Color: c.Color, Font: c.Font,
		Bold: optBool(c.Bold), Italic: optBool(c.Italic), Underlined: optBool(c.Underline),
		Strikethrough: optBool(c.Strike), Obfuscated: optBool(c.Obfuscate), Insertion: c.Insertion,
	}
	for _, w := range c.With {
		t.With = append(t.With, toText(w))
	}
	for _, e := range c.Extra {
		t.Extra = append(t.Extra, toText(e))
	}
	if c.Click != nil && int(c.Click.Action) < len(clickActions) {
		t.ClickEvent = &proto.ClickEvent{Action: clickActions[c.Click.Action], Value: c.Click.Value}
	}
	if c.Hover != nil && c.Hover.Text != nil {
		h := toText(*c.Hover.Text)
		t.HoverEvent = &proto.HoverEvent{Action: "show_text", Contents: &h}
	}
	return t
}

func ptrBool(o proto.Opt[bool]) *bool {
	if !o.Ok {
		return nil
	}
	v := o.V
	return &v
}

func fromText(t proto.Text) api.Component {
	c := api.Component{
		Text: t.Text, Translate: t.Translate, Keybind: t.Keybind, Color: t.Color, Font: t.Font,
		Bold: ptrBool(t.Bold), Italic: ptrBool(t.Italic), Underline: ptrBool(t.Underlined),
		Strike: ptrBool(t.Strikethrough), Obfuscate: ptrBool(t.Obfuscated), Insertion: t.Insertion,
	}
	for _, w := range t.With {
		c.With = append(c.With, fromText(w))
	}
	for _, e := range t.Extra {
		c.Extra = append(c.Extra, fromText(e))
	}
	return c
}

func textJSON(c api.Component) string {
	b, _ := toText(c).MarshalJSON()
	return string(b)
}

func componentFromJSON(s string) api.Component {
	var t proto.Text
	if err := t.UnmarshalJSON([]byte(s)); err != nil {
		return api.Text(s)
	}
	return fromText(t)
}
