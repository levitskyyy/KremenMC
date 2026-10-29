package command

import (
	"bytes"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

func TestParserTable(t *testing.T) {
	if len(Parsers777) != 62 {
		t.Fatalf("Parsers777 has %d entries, want 62", len(Parsers777))
	}
	want := map[api.ParserID]int32{
		api.ParserBool:              0,
		api.ParserString:            5,
		api.ParserEntity:            6,
		api.ParserBlockState:        12,
		api.ParserColor:             16,
		ParserTeamColor:             16,
		api.ParserMessage:           20,
		"minecraft:nbt":             21,
		api.ParserRotation:          29,
		api.ParserResource:          36,
		api.ParserDimension:         41,
		api.ParserGamemode:          42,
		api.ParserTime:              43,
		api.ParserResourceKey:       47,
		"minecraft:dialog":          58,
		"minecraft:swing_animation": 60,
		api.ParserUUID:              61,
	}
	for p, id := range want {
		if got, ok := Table777.Lookup(p); !ok || got != id {
			t.Errorf("%s = %d (%v), want %d", p, got, ok, id)
		}
	}
	old := NewParserTable([]api.ParserID{"brigadier:bool", "minecraft:color"})
	if id, ok := old.Lookup(ParserTeamColor); !ok || id != 1 {
		t.Errorf("reverse alias: %d %v", id, ok)
	}
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func str(s string) []byte { return append([]byte{byte(len(s))}, s...) }

func TestEncodeTree(t *testing.T) {
	srv := newServer()
	d := srv.cmds
	noop := func(api.CommandContext) (int, error) { return 1, nil }
	n := api.Argument("n", api.IntRange(0, 10)).Executes(noop).
		Suggests(func(api.CommandContext, string) []api.Suggestion { return nil })
	a := api.Literal("a").Executes(noop).Then(n)
	b := api.Literal("b").RedirectTo(a).RequiresPermission("x.b")
	d.tree.Store(&tree{root: &api.CommandNode{Children: []*api.CommandNode{a, b}}, labels: map[string]*command{}})

	full := cat(
		[]byte{4},
		[]byte{0x00, 2, 1, 2},
		[]byte{0x05, 1, 3}, str("a"),
		[]byte{0x29, 0, 1}, str("b"),
		[]byte{0x16, 0}, str("n"), []byte{3, 0x03, 0, 0, 0, 0, 0, 0, 0, 10}, str("minecraft:ask_server"),
		[]byte{0},
	)
	filtered := cat(
		[]byte{3},
		[]byte{0x00, 1, 1},
		[]byte{0x05, 1, 2}, str("a"),
		[]byte{0x16, 0}, str("n"), []byte{3, 0x03, 0, 0, 0, 0, 0, 0, 0, 10}, str("minecraft:ask_server"),
		[]byte{0},
	)
	tests := []struct {
		name   string
		sender api.CommandSender
		want   []byte
	}{
		{"op", &fakeSender{name: "op"}, full},
		{"guest", &fakeSender{name: "guest", perms: map[string]bool{}}, filtered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w proto.Writer
			d.EncodeTree(&w, tt.sender)
			if !bytes.Equal(w.Bytes(), tt.want) {
				t.Fatalf("got  % x\nwant % x", w.Bytes(), tt.want)
			}
		})
	}
}

func TestWriteParser(t *testing.T) {
	tests := []struct {
		name string
		t    api.ArgumentType
		want []byte
	}{
		{"word", api.WordArg(), []byte{5, 0}},
		{"greedy", api.GreedyArg(), []byte{5, 2}},
		{"player", api.PlayerArg(), []byte{6, 0x03}},
		{"entities", api.EntitiesArg(), []byte{6, 0x00}},
		{"double min", api.ArgumentType{Parser: api.ParserDouble, Min: 1, HasMin: true}, []byte{2, 0x01, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0}},
		{"float max", api.ArgumentType{Parser: api.ParserFloat, Max: 2, HasMax: true}, []byte{1, 0x02, 0x40, 0, 0, 0}},
		{"long", api.LongArg(), []byte{4, 0}},
		{"time", api.ArgumentType{Parser: api.ParserTime, Min: 1, HasMin: true}, []byte{43, 0, 0, 0, 1}},
		{"resource_key", api.ArgumentType{Parser: api.ParserResourceKey, Registry: "worldgen/biome"}, cat([]byte{47}, str("minecraft:worldgen/biome"))},
		{"color alias", api.ArgumentType{Parser: api.ParserColor}, []byte{16}},
		{"unknown", api.ArgumentType{Parser: "plugin:custom"}, []byte{5, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w proto.Writer
			writeParser(&w, tt.t, Table777)
			if !bytes.Equal(w.Bytes(), tt.want) {
				t.Fatalf("got % x, want % x", w.Bytes(), tt.want)
			}
		})
	}
}

func TestEncodeBuiltinsDecodes(t *testing.T) {
	srv := newServer()
	RegisterBuiltins(srv.cmds, srv)
	var w proto.Writer
	srv.cmds.EncodeTree(&w, srv.console)
	r := proto.NewReader(w.Bytes())
	count, err := r.VarInt()
	if err != nil || count < 50 {
		t.Fatalf("count %d, %v", count, err)
	}
	for i := range count {
		flags, err := r.U8()
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		kids, _ := r.VarInt()
		for range kids {
			if c, _ := r.VarInt(); c >= count {
				t.Fatalf("node %d child %d out of range", i, c)
			}
		}
		if flags&flagRedirect != 0 {
			r.VarInt()
		}
		if flags&3 == nodeRoot {
			continue
		}
		if _, err := r.String(256); err != nil {
			t.Fatalf("node %d name: %v", i, err)
		}
		if flags&3 != nodeArgument {
			continue
		}
		id, _ := r.VarInt()
		skipProps(t, r, Parsers777[id])
		if flags&flagSuggestions != 0 {
			r.String(256)
		}
	}
	if root, err := r.VarInt(); err != nil || root != 0 || r.Remaining() != 0 {
		t.Fatalf("root %d, %v, %d trailing", root, err, r.Remaining())
	}
}

func skipProps(t *testing.T, r *proto.Reader, p api.ParserID) {
	t.Helper()
	switch p {
	case api.ParserInteger, api.ParserFloat, api.ParserDouble, api.ParserLong:
		size := map[api.ParserID]int{api.ParserInteger: 4, api.ParserFloat: 4, api.ParserDouble: 8, api.ParserLong: 8}[p]
		f, _ := r.U8()
		n := 0
		if f&1 != 0 {
			n += size
		}
		if f&2 != 0 {
			n += size
		}
		r.Raw(n)
	case api.ParserString:
		r.VarInt()
	case api.ParserEntity, ParserScoreHolder:
		r.U8()
	case api.ParserTime:
		r.I32()
	case ParserResourceOrTag, ParserResourceOrKey, ParserResourceFull, api.ParserResourceKey, ParserResourceSelect:
		r.String(256)
	}
}
