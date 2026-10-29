package command

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestReader(t *testing.T) {
	tests := []struct {
		in     string
		read   func(*reader) (any, error)
		want   any
		err    string
		cursor int
	}{
		{"123 x", func(r *reader) (any, error) { return r.readInt() }, int64(123), "", 3},
		{"-7", func(r *reader) (any, error) { return r.readInt() }, int64(-7), "", 2},
		{"x", func(r *reader) (any, error) { return r.readInt() }, nil, "Expected integer", 0},
		{"1.5", func(r *reader) (any, error) { return r.readInt() }, nil, "Invalid integer '1.5'", 0},
		{"99999999999", func(r *reader) (any, error) { return r.readInt() }, nil, "Invalid integer '99999999999'", 0},
		{"99999999999", func(r *reader) (any, error) { return r.readLong() }, int64(99999999999), "", 11},
		{"1.25", func(r *reader) (any, error) { return r.readDouble() }, 1.25, "", 4},
		{"abc def", func(r *reader) (any, error) { return r.readString() }, "abc", "", 3},
		{`"a b\"c" d`, func(r *reader) (any, error) { return r.readString() }, `a b"c`, "", 8},
		{`'it''`, func(r *reader) (any, error) { return r.readString() }, "it", "", 4},
		{`"open`, func(r *reader) (any, error) { return r.readString() }, nil, "Unclosed quoted string", 5},
		{`"bad\q"`, func(r *reader) (any, error) { return r.readString() }, nil, `Invalid escape sequence '\q' in quoted string`, 5},
		{"true", func(r *reader) (any, error) { return r.readBool() }, true, "", 4},
		{"yes", func(r *reader) (any, error) { return r.readBool() }, nil, "Invalid bool, expected true or false but found 'yes'", 0},
		{"stone", func(r *reader) (any, error) { return r.readKey() }, "minecraft:stone", "", 5},
		{"my:thing/x", func(r *reader) (any, error) { return r.readKey() }, "my:thing/x", "", 10},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r := &reader{s: tt.in}
			got, err := tt.read(r)
			if tt.err != "" {
				var se *SyntaxError
				if !errors.As(err, &se) || se.Message != tt.err || se.Cursor != tt.cursor {
					t.Fatalf("err = %v, want %q at %d", err, tt.err, tt.cursor)
				}
				return
			}
			if err != nil || got != tt.want || r.pos != tt.cursor {
				t.Fatalf("got %v (%v) at %d, want %v at %d", got, err, r.pos, tt.want, tt.cursor)
			}
		})
	}
}

func TestSyntaxErrorContext(t *testing.T) {
	tests := []struct {
		input  string
		cursor int
		want   string
	}{
		{"tp abc", 3, "tp <--[HERE]"},
		{"gamemode creative foo", 18, "... creative <--[HERE]"},
		{"x", 5, "x<--[HERE]"},
	}
	for _, tt := range tests {
		e := &SyntaxError{Message: "m", Input: tt.input, Cursor: tt.cursor}
		if got := e.Context(); got != tt.want {
			t.Errorf("Context(%q,%d) = %q, want %q", tt.input, tt.cursor, got, tt.want)
		}
	}
	e := &SyntaxError{Message: "Expected integer", Input: "num x", Cursor: 4}
	if got := e.Error(); got != "Expected integer at position 4: num <--[HERE]" {
		t.Errorf("Error() = %q", got)
	}
}

// record registers a command whose executors append "path:args" to out.
func record(d *Dispatcher, out *[]string) {
	exec := func(tag string, args ...string) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			parts := []string{tag}
			for _, a := range args {
				v, _ := c.Arg(a)
				parts = append(parts, fmt.Sprint(v))
			}
			*out = append(*out, strings.Join(parts, ":"))
			return 1, nil
		}
	}
	n := api.Literal("t").Then(
		api.Literal("word").Then(api.Argument("w", api.WordArg()).Executes(exec("word", "w"))),
		api.Literal("quoted").Then(api.Argument("q", api.StringArg()).Executes(exec("quoted", "q"))),
		api.Literal("greedy").Then(api.Argument("g", api.GreedyArg()).Executes(exec("greedy", "g"))),
		api.Literal("range").Then(api.Argument("n", api.IntRange(1, 10)).Executes(exec("range", "n"))),
		api.Literal("long").Then(api.Argument("l", api.LongArg()).Executes(exec("long", "l"))),
		api.Literal("float").Then(api.Argument("f", api.ArgumentType{Parser: api.ParserFloat, Max: 2, HasMax: true}).Executes(exec("float", "f"))),
		api.Literal("bool").Then(api.Argument("b", api.BoolArg()).Executes(exec("bool", "b"))),
		api.Literal("time").Then(api.Argument("t", api.ArgumentType{Parser: api.ParserTime}).Executes(exec("time", "t"))),
		api.Literal("uuid").Then(api.Argument("u", api.ArgumentType{Parser: api.ParserUUID}).Executes(exec("uuid", "u"))),
		api.Literal("color").Then(api.Argument("c", api.ArgumentType{Parser: api.ParserColor}).Executes(exec("color", "c"))),
		api.Literal("res").Then(api.Argument("r", api.ResourceArg()).Executes(exec("res", "r"))),
		api.Literal("dim").Then(api.Argument("d", api.ArgumentType{Parser: api.ParserDimension}).Executes(exec("dim", "d"))),
		api.Literal("block").Then(api.Argument("b", api.BlockStateArg()).Executes(exec("block", "b"))),
		api.Literal("item").Then(api.Argument("i", api.ItemArg()).Executes(func(c api.CommandContext) (int, error) {
			*out = append(*out, "item:"+string(c.Item("i").Type))
			return 1, nil
		})),
		api.Literal("comp").Then(api.Argument("c", api.ArgumentType{Parser: api.ParserComponent}).Executes(func(c api.CommandContext) (int, error) {
			comp := c.Component("c")
			*out = append(*out, fmt.Sprintf("comp:%s:%s", comp.Plain(), comp.Color))
			return 1, nil
		})),
		api.Literal("mode").Then(api.Argument("m", api.GameModeArg()).Executes(exec("mode", "m"))),
		// Backtracking: an int and a word compete for the same position.
		api.Literal("pick").Then(
			api.Argument("num", api.IntArg()).Then(api.Literal("end").Executes(exec("pick-num", "num"))),
			api.Argument("name", api.WordArg()).Then(api.Literal("stop").Executes(exec("pick-name", "name"))),
		),
		api.Literal("opt").Executes(exec("opt")).Then(api.Argument("x", api.IntArg()).Executes(exec("opt", "x"))),
	)
	if err := d.Register("test", n); err != nil {
		panic(err)
	}
	target := api.Literal("target").Then(api.Argument("v", api.IntArg()).Executes(exec("target", "v")))
	if err := d.Register("test", api.Literal("real").Then(target)); err != nil {
		panic(err)
	}
	if err := d.Register("test", api.Literal("jump").RedirectTo(target)); err != nil {
		panic(err)
	}
	if err := d.Register("test", api.Literal("secret").RequiresPermission("test.secret").Executes(exec("secret"))); err != nil {
		panic(err)
	}
}

func TestDispatchParsing(t *testing.T) {
	srv := newServer()
	var got []string
	record(srv.cmds, &got)
	tests := []struct {
		line   string
		want   string
		err    string
		cursor int
	}{
		{line: "t word hello", want: "word:hello"},
		{line: "/t word hello", want: "word:hello"},
		{line: "T word hello", want: "word:hello"},
		{line: "test:t word hello", want: "word:hello"},
		{line: `t quoted "two words"`, want: "quoted:two words"},
		{line: "t quoted plain", want: "quoted:plain"},
		{line: "t greedy all of  this", want: "greedy:all of  this"},
		{line: "t range 10", want: "range:10"},
		{line: "t range 11", err: "Integer must not be more than 10, found 11", cursor: 8},
		{line: "t range 0", err: "Integer must not be less than 1, found 0", cursor: 8},
		{line: "t range x", err: "Expected integer", cursor: 8},
		{line: "t long 9000000000", want: "long:9000000000"},
		{line: "t float 2.5", err: "Float must not be more than 2, found 2.5", cursor: 8},
		{line: "t bool false", want: "bool:false"},
		{line: "t time 1d", want: "time:24000"},
		{line: "t time 1.5s", want: "time:30"},
		{line: "t time 7", want: "time:7"},
		{line: "t time 3x", err: "Invalid unit", cursor: 8},
		{line: "t uuid 069a79f4-44e9-4726-a5be-fca90e38aaf5", want: "uuid:069a79f4-44e9-4726-a5be-fca90e38aaf5"},
		{line: "t uuid nope", err: "Invalid UUID", cursor: 7},
		{line: "t color dark_red", want: "color:dark_red"},
		{line: "t color pink", err: "Unknown color 'pink'", cursor: 8},
		{line: "t res foo", want: "res:minecraft:foo"},
		{line: "t dim my:nether", want: "dim:my:nether"},
		{line: "t block oak_log[axis=z]", want: "block:minecraft:oak_log[axis=z]"},
		{line: "t block minecraft:stone", want: "block:minecraft:stone"},
		{line: "t block dirt", err: "Unknown block type 'dirt'", cursor: 8},
		{line: "t item diamond_sword[damage=3]", want: "item:minecraft:diamond_sword"},
		{line: "t item bread", err: "Unknown item 'minecraft:bread'", cursor: 7},
		{line: `t comp {"text":"hi","color":"red"}`, want: "comp:hi:red"},
		{line: `t comp {text:'hi',extra:[" there"]}`, want: "comp:hi there:"},
		{line: `t comp "plain"`, want: "comp:plain:"},
		{line: "t mode creative", want: "mode:creative"},
		{line: "t mode god", err: "Unknown game mode: god", cursor: 7},
		{line: "t pick 5 end", want: "pick-num:5"},
		{line: "t pick five stop", want: "pick-name:five"},
		{line: "t opt", want: "opt"},
		{line: "t opt 3", want: "opt:3"},
		{line: "t opt 3x", err: msgTrailing, cursor: 7},
		{line: "real target 4", want: "target:4"},
		{line: "jump 4", want: "target:4"},
		{line: "jump", err: msgUnknownCommand, cursor: 4},
		{line: "t", err: msgUnknownCommand, cursor: 1},
		{line: "t word", err: msgUnknownCommand, cursor: 6},
		{line: "nope", err: msgUnknownCommand, cursor: 0},
		{line: "t nope", err: msgUnknownArgument, cursor: 2},
		{line: "secret", err: msgUnknownCommand, cursor: 0},
	}
	sender := &fakeSender{name: "tester", perms: map[string]bool{}}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got = got[:0]
			_, err := srv.cmds.Dispatch(sender, tt.line)
			if tt.err != "" {
				var se *SyntaxError
				if !errors.As(err, &se) || se.Message != tt.err || se.Cursor != tt.cursor {
					t.Fatalf("err = %v, want %q at %d", err, tt.err, tt.cursor)
				}
				if !strings.Contains(sender.last(), "<--[HERE]") {
					t.Errorf("sender message %q lacks context", sender.last())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("executed %v, want %q", got, tt.want)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	srv := newServer()
	d := srv.cmds
	noop := func(api.CommandContext) (int, error) { return 7, nil }
	if err := d.Register("alpha", api.Literal("home").Executes(noop), "h"); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("beta", api.Literal("home").Describe("beta home").Executes(noop)); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("x", api.Literal("bad name")); err == nil {
		t.Fatal("expected error for name with space")
	}
	info, ok := d.Lookup("home")
	if !ok || info.Owner != "alpha" || info.Aliases[0] != "h" || info.Usage != "/home" {
		t.Fatalf("home = %+v", info)
	}
	if info, ok := d.Lookup("beta:home"); !ok || info.Description != "beta home" {
		t.Fatalf("beta:home = %+v", info)
	}
	if n, err := d.Dispatch(srv.console, "h"); n != 7 || err != nil {
		t.Fatalf("alias dispatch = %d, %v", n, err)
	}
	if !d.Unregister("alpha", "home") {
		t.Fatal("unregister failed")
	}
	if info, ok := d.Lookup("home"); !ok || info.Owner != "beta" {
		t.Fatalf("after unregister home = %+v, %v", info, ok)
	}
	if _, ok := d.Lookup("h"); ok {
		t.Fatal("alias survived unregister")
	}
	d.UnregisterAll("beta")
	if len(d.Commands()) != 0 {
		t.Fatalf("commands left: %v", d.Commands())
	}
}

func TestUsage(t *testing.T) {
	srv := newServer()
	d := srv.cmds
	noop := func(api.CommandContext) (int, error) { return 1, nil }
	d.Register("", api.Literal("gm").Then(
		api.Argument("mode", api.GameModeArg()).Executes(noop).Then(api.Argument("target", api.PlayersArg()).Executes(noop))))
	d.Register("", api.Literal("w").Then(api.Literal("on").Executes(noop), api.Literal("off").Executes(noop)))
	d.Register("", api.Literal("o").Executes(noop).Then(api.Argument("x", api.IntArg()).Executes(noop)))
	tests := map[string]string{"gm": "/gm <mode> [<target>]", "w": "/w (on|off)", "o": "/o [<x>]"}
	for name, want := range tests {
		info, _ := d.Lookup(name)
		if info.Usage != want {
			t.Errorf("usage(%s) = %q, want %q", name, info.Usage, want)
		}
	}
}

func TestExecutorErrors(t *testing.T) {
	srv := newServer()
	d := srv.cmds
	d.Register("", api.Literal("boom").Executes(func(api.CommandContext) (int, error) { panic("x") }))
	d.Register("", api.Literal("nope").Executes(func(api.CommandContext) (int, error) { return 0, api.Errorf("no way") }))
	s := &fakeSender{name: "s"}
	if _, err := d.Dispatch(s, "boom"); err == nil || s.last() != msgUnexpected {
		t.Fatalf("panic: err=%v msg=%q", err, s.last())
	}
	if _, err := d.Dispatch(s, "nope"); err == nil || s.last() != "no way" {
		t.Fatalf("command error: err=%v msg=%q", err, s.last())
	}
}
