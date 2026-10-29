package command

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestParseSelector(t *testing.T) {
	tests := []struct {
		in     string
		check  func(*selector) bool
		err    string
		cursor int
	}{
		{in: "@a", check: func(s *selector) bool { return s.kind == 'a' && s.playersOnly && s.limit == 0 }},
		{in: "@p", check: func(s *selector) bool { return s.limit == 1 && s.sort == sortNearest }},
		{in: "@r", check: func(s *selector) bool { return s.limit == 1 && s.sort == sortRandom }},
		{in: "@n", check: func(s *selector) bool { return s.limit == 1 && !s.playersOnly }},
		{in: "@s", check: func(s *selector) bool { return s.kind == 's' && s.single() }},
		{in: "@e[type=minecraft:pig,limit=3,sort=furthest]", check: func(s *selector) bool {
			return s.limit == 3 && s.sort == sortFurthest && s.types[0] == negKey{"minecraft:pig", false}
		}},
		{in: "@e[type=!cow, type=!pig]", check: func(s *selector) bool { return len(s.types) == 2 && s.types[1].neg }},
		{in: "@a[name=Steve]", check: func(s *selector) bool { return s.names[0].v == "Steve" }},
		{in: `@a[name="With Space"]`, check: func(s *selector) bool { return s.names[0].v == "With Space" }},
		{in: "@e[distance=..5]", check: func(s *selector) bool { return !s.distance.hasMin && s.distance.max == 5 }},
		{in: "@e[distance=2..]", check: func(s *selector) bool { return s.distance.min == 2 && !s.distance.hasMax }},
		{in: "@e[distance=3]", check: func(s *selector) bool { return s.distance.min == 3 && s.distance.max == 3 }},
		{in: "@e[x=1,y=-2.5,z=3,dx=4,dy=5,dz=6]", check: func(s *selector) bool {
			return *s.pos[1] == -2.5 && *s.delta[2] == 6 && s.worldLimited()
		}},
		{in: "@e[gamemode=!creative]", check: func(s *selector) bool { return s.modes[0].neg && s.playersOnly }},
		{in: "@e[tag=a,tag=!b]", check: func(s *selector) bool { return len(s.tags) == 2 }},
		{in: "@a[level=1..3]", check: func(s *selector) bool { return s.level.min == 1 && s.level.max == 3 }},
		{in: "@e[]", check: func(s *selector) bool { return s.kind == 'e' }},
		{in: "Notch", check: func(s *selector) bool { return s.name == "Notch" && s.single() }},
		{in: "069a79f4-44e9-4726-a5be-fca90e38aaf5", check: func(s *selector) bool { return s.hasUUID }},
		{in: "@x", err: "Unknown selector type '@x'", cursor: 0},
		{in: "@", err: "Missing selector type", cursor: 1},
		{in: "@e[foo=1]", err: "Unknown option 'foo'", cursor: 3},
		{in: "@e[limit=0]", err: "Limit must be at least 1", cursor: 9},
		{in: "@e[sort=up]", err: "Invalid or unknown sort type 'up'", cursor: 8},
		{in: "@e[distance=-1]", err: "Distance cannot be negative", cursor: 12},
		{in: "@e[distance=5..1]", err: "Min cannot be bigger than max", cursor: 12},
		{in: "@e[gamemode=god]", err: "Invalid or unknown game mode 'god'", cursor: 12},
		{in: "@e[limit=1", err: "Expected end of options", cursor: 10},
		{in: "@e[limit]", err: "Expected value for option 'limit'", cursor: 3},
		{in: "@s[limit=2]", err: "Option 'limit' isn't applicable here", cursor: 3},
		{in: "ThisNameIsTooLong", err: "Invalid name or UUID", cursor: 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r := &reader{s: tt.in}
			s, err := parseSelector(r, true)
			if tt.err != "" {
				var se *SyntaxError
				if !errors.As(err, &se) || se.Message != tt.err || se.Cursor != tt.cursor {
					t.Fatalf("err = %v, want %q at %d", err, tt.err, tt.cursor)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.canRead() {
				t.Fatalf("unconsumed %q", r.remaining())
			}
			if !tt.check(s) {
				t.Fatalf("unexpected selector %+v", s)
			}
		})
	}
	if _, err := parseSelector(&reader{s: "@a"}, false); err == nil || err.(*SyntaxError).Message != msgSelectorNotAllowed {
		t.Fatalf("selectors without permission: %v", err)
	}
}

func TestEntityArgumentConstraints(t *testing.T) {
	srv := newServer()
	d := srv.cmds
	noop := func(api.CommandContext) (int, error) { return 1, nil }
	d.Register("", api.Literal("one").Then(api.Argument("p", api.PlayerArg()).Executes(noop)))
	d.Register("", api.Literal("ents").Then(api.Argument("e", api.ArgumentType{Parser: api.ParserEntity, Single: true}).Executes(noop)))
	d.Register("", api.Literal("pl").Then(api.Argument("p", api.PlayersArg()).Executes(noop)))
	tests := []struct {
		line, err string
	}{
		{"one @a", msgOnlyOnePlayer},
		{"one @p", ""},
		{"one @a[limit=1]", ""},
		{"ents @e", msgOnlyOneEntity},
		{"ents @n", ""},
		{"pl @e", msgOnlyPlayers},
		{"pl @e[type=player]", ""},
		{"pl @s", ""},
	}
	for _, tt := range tests {
		_, err := d.execute(srv.console, tt.line)
		var se *SyntaxError
		switch {
		case tt.err == "" && err != nil:
			t.Errorf("%s: unexpected %v", tt.line, err)
		case tt.err != "" && (!errors.As(err, &se) || se.Message != tt.err):
			t.Errorf("%s: err = %v, want %q", tt.line, err, tt.err)
		}
	}
}

func TestResolveSelector(t *testing.T) {
	srv := newServer()
	alice := srv.addPlayer("Alice", api.Vec3{X: 0, Y: 64, Z: 0}, nil)
	bob := srv.addPlayer("Bob", api.Vec3{X: 10, Y: 64, Z: 0}, nil)
	carol := srv.addPlayer("Carol", api.Vec3{X: 3, Y: 64, Z: 0}, nil)
	carol.mode = api.Creative
	bob.tags = []string{"red"}
	bob.level = 30
	pig := &fakeEntity{id: api.UUID{9}, kind: "minecraft:pig", loc: api.Location{World: "overworld", Pos: api.Vec3{X: 1, Y: 64}}}
	cow := &fakeEntity{id: api.UUID{8}, kind: "minecraft:cow", name: "Bessie", loc: api.Location{World: "overworld", Pos: api.Vec3{X: 50, Y: 64}}}
	srv.worlds[0].entities = []api.Entity{pig, cow}

	tests := []struct {
		sel  string
		want []string
	}{
		{"@a", []string{"Alice", "Bob", "Carol"}},
		{"@p", []string{"Alice"}},
		{"@s", []string{"Alice"}},
		{"@a[sort=nearest]", []string{"Alice", "Carol", "Bob"}},
		{"@a[sort=furthest,limit=1]", []string{"Bob"}},
		{"@a[distance=..5]", []string{"Alice", "Carol"}},
		{"@a[distance=4..]", []string{"Bob"}},
		{"@a[gamemode=creative]", []string{"Carol"}},
		{"@a[gamemode=!creative]", []string{"Alice", "Bob"}},
		{"@a[name=!Alice]", []string{"Bob", "Carol"}},
		{"@a[tag=red]", []string{"Bob"}},
		{"@a[tag=]", []string{"Alice", "Carol"}},
		{"@a[level=10..]", []string{"Bob"}},
		{"@e[type=pig]", []string{"Pig"}},
		{"@e[type=!player,sort=nearest]", []string{"Pig", "Bessie"}},
		{"@n[type=!player]", []string{"Pig"}},
		{"@e[x=9,y=63,z=-1,dx=2,dy=2,dz=2]", []string{"Bob"}},
		{"@e[name=Bessie]", []string{"Bessie"}},
		{"Bob", []string{"Bob"}},
		{"Nobody", nil},
		{"09000000-0000-0000-0000-000000000000", []string{"Pig"}},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			s, err := parseSelector(&reader{s: tt.sel}, true)
			if err != nil {
				t.Fatal(err)
			}
			c := &cmdContext{d: srv.cmds, sender: alice}
			es, _ := c.resolveEntities(s)
			var got []string
			for _, e := range es {
				got = append(got, entityName(e))
			}
			if s.sort == sortArbitrary {
				slices.Sort(got)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoords(t *testing.T) {
	origin := api.Location{World: "overworld", Pos: api.Vec3{X: 10, Y: 64, Z: -5}}
	tests := []struct {
		in    string
		kind  coordKind
		yaw   float32
		pitch float32
		want  api.Vec3
		err   string
	}{
		{in: "1 2 3", kind: coordVec3, want: api.Vec3{X: 1.5, Y: 2, Z: 3.5}},
		{in: "1.0 2 3.25", kind: coordVec3, want: api.Vec3{X: 1, Y: 2, Z: 3.25}},
		{in: "~ ~1 ~-1", kind: coordVec3, want: api.Vec3{X: 10, Y: 65, Z: -6}},
		{in: "^ ^ ^1", kind: coordVec3, want: api.Vec3{X: 10, Y: 64, Z: -4}},
		{in: "^ ^ ^1", kind: coordVec3, yaw: 90, want: api.Vec3{X: 9, Y: 64, Z: -5}},
		{in: "^1 ^ ^", kind: coordVec3, want: api.Vec3{X: 11, Y: 64, Z: -5}},
		{in: "^ ^ ^2", kind: coordVec3, pitch: -90, want: api.Vec3{X: 10, Y: 66, Z: -5}},
		{in: "~0.5 ~ 7", kind: coordBlock, want: api.Vec3{X: 10, Y: 64, Z: 7}},
		{in: "3 4", kind: coordColumn, want: api.Vec3{X: 3, Z: 4}},
		{in: "~1 2", kind: coordVec2, want: api.Vec3{X: 11, Z: 2.5}},
		{in: "~ ~", kind: coordRotation, yaw: 45, pitch: 10, want: api.Vec3{X: 45, Y: 10}},
		{in: "~10 -20", kind: coordRotation, yaw: 45, want: api.Vec3{X: 55, Y: -20}},
		{in: "~ ^ ~", kind: coordVec3, err: msgPosMixed},
		{in: "^ ~ ^", kind: coordVec3, err: msgPosMixed},
		{in: "1 2", kind: coordVec3, err: msgPosIncomplete},
		{in: "1", kind: coordVec2, err: msgPos2Incomplete},
		{in: "1.5 2 3", kind: coordBlock, err: "Invalid integer '1.5'"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			co, err := parseCoords(&reader{s: tt.in}, tt.kind)
			if tt.err != "" {
				if err == nil || err.(*SyntaxError).Message != tt.err {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			o := origin
			o.Yaw, o.Pitch = tt.yaw, tt.pitch
			var got api.Vec3
			switch tt.kind {
			case coordBlock:
				p := co.blockPos(o)
				got = api.Vec3{X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)}
			case coordVec3:
				got = co.vec3(o)
			default:
				got = co.raw(o)
			}
			if math.Abs(got.X-tt.want.X) > 1e-9 || math.Abs(got.Y-tt.want.Y) > 1e-9 || math.Abs(got.Z-tt.want.Z) > 1e-9 {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMessageSelectors(t *testing.T) {
	srv := newServer()
	alice := srv.addPlayer("Alice", api.Vec3{}, nil)
	srv.addPlayer("Bob", api.Vec3{X: 3}, map[string]bool{})
	m, err := parseMessage(&reader{s: "hi @a and @p, bye @x"}, true)
	if err != nil {
		t.Fatal(err)
	}
	c := &cmdContext{d: srv.cmds, sender: alice}
	if got := c.expandMessage(m); got != "hi Alice, Bob and Alice, bye @x" {
		t.Fatalf("expanded %q", got)
	}
	plain, _ := parseMessage(&reader{s: "hi @a"}, false)
	if got := c.expandMessage(plain); got != "hi @a" {
		t.Fatalf("unprivileged expanded %q", got)
	}
}
