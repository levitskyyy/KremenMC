package wire

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestPrimitivesRoundTrip(t *testing.T) {
	var w Writer
	w.U8(7)
	w.Bool(true)
	w.Uvarint(1 << 40)
	w.Varint(-12345)
	w.F32(1.5)
	w.F64(-2.25)
	w.String("héllo")
	w.Bytes([]byte{1, 2})
	w.Strings([]string{"a", "", "c"})
	w.UUID(api.UUID{1, 2, 3})
	w.Location(api.Location{World: "w", Pos: api.Vec3{X: 1, Y: 2, Z: 3}, Yaw: 4, Pitch: 5})
	w.BlockPos(api.BlockPos{X: -1, Y: -64, Z: 1 << 25})

	r := NewReader(w.B)
	got := []any{r.U8(), r.Bool(), r.Uvarint(), r.Varint(), r.F32(), r.F64(), r.String(), r.Bytes(), r.Strings(), r.UUID(), r.Location(), r.BlockPos()}
	want := []any{uint8(7), true, uint64(1 << 40), int64(-12345), float32(1.5), -2.25, "héllo", []byte{1, 2}, []string{"a", "", "c"},
		api.UUID{1, 2, 3}, api.Location{World: "w", Pos: api.Vec3{X: 1, Y: 2, Z: 3}, Yaw: 4, Pitch: 5}, api.BlockPos{X: -1, Y: -64, Z: 1 << 25}}
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("err=%v leftover=%d", r.Err, r.Len())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestComponentRoundTrip(t *testing.T) {
	yes, no := true, false
	inner := api.Text("hover")
	cases := []api.Component{
		{},
		api.Text("plain"),
		{Text: "styled", Color: "#ff0000", Bold: &yes, Italic: &no, Strike: &yes, Font: "minecraft:uniform", Insertion: "ins"},
		{Translate: "chat.type.text", With: []api.Component{api.Text("a"), api.Colored("b", "red")}},
		{Text: "click", Click: &api.ClickEvent{Action: api.ClickRunCommand, Value: "/help"}, Hover: &api.HoverEvent{Text: &inner}, Extra: []api.Component{api.Text("x"), {Keybind: "key.jump"}}},
	}
	for _, c := range cases {
		var w Writer
		w.Component(c)
		r := NewReader(w.B)
		got := r.Component()
		if r.Err != nil || r.Len() != 0 || !reflect.DeepEqual(got, c) {
			t.Errorf("round trip %+v -> %+v (err %v)", c, got, r.Err)
		}
	}
}

func TestReaderTruncationAndHostileLengths(t *testing.T) {
	inputs := [][]byte{
		{},
		{0x05, 'a'},                    // string longer than input
		{0xff, 0xff, 0xff, 0xff, 0x0f}, // huge count
		{0x80},                         // unterminated varint
	}
	for _, in := range inputs {
		r := NewReader(in)
		_ = r.String()
		_ = r.Strings()
		_ = r.F64()
		if r.Err == nil {
			t.Errorf("%x: expected error", in)
		}
	}
	deep := bytes.Repeat([]byte{cExtra, 1}, 100)
	r := NewReader(deep)
	r.Component()
	if r.Err == nil {
		t.Error("deeply nested component accepted")
	}
}

func TestEmbedManifest(t *testing.T) {
	mod := []byte("\x00asm\x01\x00\x00\x00")
	mod = append(mod, 1, 4, 1, 0x60, 0, 0) // type section: one func type () -> ()
	if _, ok, err := ExtractManifest(mod); ok || err != nil {
		t.Fatalf("unexpected manifest: %v %v", ok, err)
	}
	a, err := EmbedManifest(mod, []byte("id = \"a\""))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EmbedManifest(a, []byte("id = \"b\""))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := ExtractManifest(b)
	if err != nil || !ok || string(got) != `id = "b"` {
		t.Fatalf("extract = %q %v %v", got, ok, err)
	}
	if !bytes.HasPrefix(b, mod) || bytes.Count(b, []byte(ManifestSection)) != 1 {
		t.Error("embedding should keep original sections and replace the old manifest")
	}
	if _, err := EmbedManifest([]byte("nope"), nil); err == nil {
		t.Error("non-wasm accepted")
	}
}
