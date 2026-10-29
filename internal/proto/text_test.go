package proto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

func sampleTexts() map[string]Text {
	return map[string]Text{
		"empty": {},
		"plain": Plain("hello"),
		"styled": {Text: "warn", Color: Red, Bold: Some(true), Italic: Some(false), Underlined: Some(true),
			Strikethrough: Some(true), Obfuscated: Some(false), Font: "minecraft:uniform", Insertion: "ins",
			ShadowColor: Some(int32(-16777216))},
		"translate":  Translatable("chat.type.text", Plain("Steve"), Text{Text: "hi", Color: "#ff00aa"}),
		"fallback":   {Translate: "x.y", Fallback: "fb"},
		"keybind":    {Keybind: "key.jump", Color: Gold},
		"score":      {Score: &Score{Name: "@p", Objective: "kills"}},
		"selector":   {Selector: "@a", Separator: &Text{Text: ", "}},
		"extra":      {Text: "a", Extra: []Text{Plain("b"), {Text: "c", Bold: Some(true), Extra: []Text{Plain("d")}}}},
		"click url":  {Text: "site", ClickEvent: &ClickEvent{Action: "open_url", Value: "https://example.com"}},
		"click cmd":  {Text: "run", ClickEvent: &ClickEvent{Action: "run_command", Value: "/help"}},
		"click page": {Text: "p", ClickEvent: &ClickEvent{Action: "change_page", Page: 3}},
		"hover text": {Text: "h", HoverEvent: &HoverEvent{Action: "show_text", Contents: &Text{Text: "tip", Color: Aqua}}},
		"hover item": {Text: "i", HoverEvent: &HoverEvent{Action: "show_item", ID: "minecraft:stone", Count: 3}},
		"hover entity": {Text: "e", HoverEvent: &HoverEvent{Action: "show_entity", ID: "minecraft:pig",
			UUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Name: &Text{Text: "Babe"}}},
	}
}

func TestTextNBTRoundTrip(t *testing.T) {
	for name, tt := range sampleTexts() {
		var w Writer
		tt.Encode(&w)
		n, err := NBTLen(w.B)
		if err != nil || n != len(w.B) {
			t.Fatalf("%s: encoded NBT invalid: len %d/%d, %v", name, n, len(w.B), err)
		}
		var got Text
		r := NewReader(w.B)
		if err := got.Decode(r); err != nil || r.Remaining() != 0 {
			t.Fatalf("%s: Decode: %v (rem %d)", name, err, r.Remaining())
		}
		if !reflect.DeepEqual(got, tt) {
			t.Errorf("%s: roundtrip\n got %+v\nwant %+v", name, got, tt)
		}
	}
}

func TestTextNBTShape(t *testing.T) {
	var w Writer
	p := Plain("hi")
	p.Encode(&w)
	if want := []byte{nbt.TagString, 0, 2, 'h', 'i'}; string(w.B) != string(want) {
		t.Fatalf("plain = % X, want % X", w.B, want)
	}
	styled := Text{Text: "x", Color: Red, Bold: Some(true), Extra: []Text{Plain("y")}}
	_, c, _, err := nbt.Unmarshal(styled.NBT(), true)
	if err != nil {
		t.Fatal(err)
	}
	if c["text"] != "x" || c["color"] != "red" || c["bold"] != int8(1) {
		t.Fatalf("compound = %v", c)
	}
	extra, ok := c["extra"].(nbt.List)
	if !ok || extra.Type != nbt.TagCompound || len(extra.Items) != 1 {
		t.Fatalf("extra = %#v", c["extra"])
	}
}

func TestTextDecodeVariants(t *testing.T) {
	mk := func(c nbt.Compound) []byte {
		b, err := nbt.Marshal(nil, "", c, true)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tests := []struct {
		name string
		in   []byte
		want Text
	}{
		{"end", []byte{nbt.TagEnd}, Text{}},
		{"int arg", mk(nbt.Compound{"translate": "k", "with": nbt.List{Type: nbt.TagInt, Items: []any{int32(7)}}}),
			Text{Translate: "k", With: []Text{{Text: "7"}}}},
		{"string list", mk(nbt.Compound{"text": "", "extra": nbt.List{Type: nbt.TagString, Items: []any{"a", "b"}}}),
			Text{Extra: []Text{{Text: "a"}, {Text: "b"}}}},
		{"wrapped", mk(nbt.Compound{"text": "", "extra": []nbt.Compound{{"": "a"}, {"": int8(1)}}}),
			Text{Extra: []Text{{Text: "a"}, {Text: "1"}}}},
		{"unknown keys", mk(nbt.Compound{"text": "t", "type": "text", "zzz": nbt.Compound{"a": int64(1)}, "bold": "bad"}),
			Text{Text: "t"}},
		{"legacy keys", mk(nbt.Compound{"text": "t", "clickEvent": nbt.Compound{"action": "suggest_command", "value": "/x"}}),
			Text{Text: "t", ClickEvent: &ClickEvent{Action: "suggest_command", Value: "/x"}}},
	}
	for _, tt := range tests {
		var got Text
		if err := got.Decode(NewReader(tt.in)); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v want %+v", tt.name, got, tt.want)
		}
	}
	var got Text
	if err := got.Decode(NewReader([]byte{nbt.TagCompound, nbt.TagString, 0, 4, 't'})); err == nil {
		t.Error("truncated compound accepted")
	}
}

func TestTextJSON(t *testing.T) {
	for name, tt := range sampleTexts() {
		b, err := json.Marshal(tt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got Text
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: unmarshal %s: %v", name, b, err)
		}
		if !reflect.DeepEqual(got, tt) {
			t.Errorf("%s: JSON roundtrip %s\n got %+v\nwant %+v", name, b, got, tt)
		}
	}
	b, _ := json.Marshal(Plain("hi"))
	if string(b) != `"hi"` {
		t.Errorf("plain JSON = %s", b)
	}
	b, _ = json.Marshal(Text{Text: "a", Color: Red})
	if string(b) != `{"color":"red","text":"a"}` {
		t.Errorf("styled JSON = %s", b)
	}
	var arr Text
	if err := json.Unmarshal([]byte(`["a", {"text":"b","bold":true}, 3]`), &arr); err != nil {
		t.Fatal(err)
	}
	want := Text{Text: "a", Extra: []Text{{Text: "b", Bold: Some(true)}, {Text: "3"}}}
	if !reflect.DeepEqual(arr, want) {
		t.Errorf("array JSON = %+v", arr)
	}
}

func TestParseLegacy(t *testing.T) {
	tests := []struct {
		in   string
		code rune
		want Text
	}{
		{"", LegacyChar, Text{}},
		{"plain", LegacyChar, Plain("plain")},
		{"§cRed", LegacyChar, Text{Text: "Red", Color: Red}},
		{"a§lb§rc", LegacyChar, Text{Extra: []Text{Plain("a"), {Text: "b", Bold: Some(true)}, Plain("c")}}},
		{"§l§obold§aGreen", LegacyChar, Text{Extra: []Text{
			{Text: "bold", Bold: Some(true), Italic: Some(true)}, {Text: "Green", Color: Green}}}},
		{"§aG§nU", LegacyChar, Text{Extra: []Text{{Text: "G", Color: Green}, {Text: "U", Color: Green, Underlined: Some(true)}}}},
		{"&6Gold &kX", '&', Text{Extra: []Text{{Text: "Gold ", Color: Gold}, {Text: "X", Color: Gold, Obfuscated: Some(true)}}}},
		{"§x§F§F§0§0§a§aHex", LegacyChar, Text{Text: "Hex", Color: "#ff00aa"}},
		{"§x§Fbad", LegacyChar, Text{Extra: []Text{Plain("§x"), {Text: "bad", Color: White}}}},
		{"§zkeep§", LegacyChar, Plain("§zkeep§")},
		{"§Mstrike", LegacyChar, Text{Text: "strike", Strikethrough: Some(true)}},
	}
	for _, tt := range tests {
		got := ParseLegacy(tt.in, tt.code)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseLegacy(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	if s := ParseLegacy("§aHi §lthere", LegacyChar); s.String() != "Hi there" {
		t.Errorf("String() = %q", s.String())
	}
}

func BenchmarkTextEncode(b *testing.B) {
	t := Text{Text: "<Steve> ", Color: Gray, Extra: []Text{Plain("hello world")}}
	var w Writer
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		t.Encode(&w)
	}
}

func TestTextLongString(t *testing.T) {
	long := strings.Repeat("é", 40000)
	var w Writer
	p := Plain(long)
	p.Encode(&w)
	var got Text
	if err := got.Decode(NewReader(w.B)); err != nil {
		t.Fatal(err)
	}
	if len(got.Text) > 65535 || !utf8.ValidString(got.Text) || !strings.HasPrefix(long, got.Text) {
		t.Fatalf("truncated text invalid: len %d", len(got.Text))
	}
}
