package pdef_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/proto"

	"github.com/levitskyyy/KremenMC/internal/translate/meta"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
)

func TestCompileEmbedded(t *testing.T) {
	for _, p := range []int32{777, 776, 775} {
		m, err := meta.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		s := pdef.Compile(m.Types)
		for name := range m.Types {
			if _, err := s.Type(name); err != nil {
				t.Errorf("%d: %v", p, err)
			}
		}
		for _, c := range []string{"SlotComponent", "Particle", "entityMetadataEntry"} {
			cases, names, err := s.Cases(c)
			if err != nil {
				t.Errorf("%d: %v", p, err)
				continue
			}
			if len(cases) != len(names) || len(names) < 10 {
				t.Errorf("%d %s: %d cases", p, c, len(cases))
			}
		}
	}
}

func TestSkipAndCopy(t *testing.T) {
	types := map[string]json.RawMessage{
		"Thing": json.RawMessage(`["container",[
			{"name":"flags","type":["bitflags",{"type":"u8","flags":["a","b"]}]},
			{"name":"kind","type":["mapper",{"type":"varint","mappings":{"0":"none","1":"text","2":"num"}}]},
			{"name":"val","type":["switch",{"compareTo":"kind","fields":{"text":"string","num":"i32"},"default":"void"}]},
			{"name":"extra","type":["switch",{"compareTo":"flags/b","fields":{"true":"Inner"},"default":"void"}]},
			{"name":"n","type":"varint"},
			{"name":"list","type":["array",{"count":"n","type":"Inner"}]}]]`),
		"Inner": json.RawMessage(`["container",[{"name":"x","type":"i16"}]]`),
	}
	s := pdef.Compile(types)
	thing, err := s.Type("Thing")
	if err != nil {
		t.Fatal(err)
	}
	var w proto.Writer
	w.U8(2)        // flag b
	w.VarInt(1)    // text
	w.String("hi") // val
	w.I16(7)       // extra
	w.VarInt(2)
	w.I16(1)
	w.I16(2)
	w.U8(0xaa) // trailing
	r := proto.Reader{B: w.B}
	if err := pdef.Skip(thing, &r); err != nil {
		t.Fatal(err)
	}
	if r.Remaining() != 1 {
		t.Fatalf("stopped with %d bytes left", r.Remaining())
	}

	// hooks replace named types
	var out proto.Writer
	wk := pdef.Walker{Hooks: map[string]pdef.Hook{"Inner": func(r *proto.Reader, w *proto.Writer) error {
		v, err := r.I16()
		w.I16(v * 10)
		return err
	}}}
	r = proto.Reader{B: w.B}
	if err := wk.Copy(thing, &r, &out); err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte(nil), w.B[:5]...), 0, 70, 2, 0, 10, 0, 20)
	if !bytes.Equal(out.B, want) {
		t.Errorf("copy % x, want % x", out.B, want)
	}

	// truncated input fails instead of panicking
	r = proto.Reader{B: w.B[:5]}
	if err := pdef.Skip(thing, &r); err == nil {
		t.Error("truncated value accepted")
	}

	other := pdef.Compile(types)
	o, _ := other.Type("Thing")
	if !pdef.Equal(thing, o) {
		t.Error("identical schemas differ")
	}
	in, _ := s.Type("Inner")
	if pdef.Equal(thing, in) {
		t.Error("different schemas equal")
	}
}
