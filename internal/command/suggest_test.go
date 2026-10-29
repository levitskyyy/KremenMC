package command

import (
	"slices"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestSuggest(t *testing.T) {
	srv := newServer()
	RegisterBuiltins(srv.cmds, srv)
	srv.cmds.Register("test", api.Literal("pick").Then(
		api.Argument("fruit", api.WordArg()).Suggests(suggestStrings(func(api.CommandContext) []string {
			return []string{"apple", "apricot", "banana"}
		})).Executes(func(api.CommandContext) (int, error) { return 1, nil })))
	alice := srv.addPlayer("Alice", api.Vec3{}, nil)
	srv.addPlayer("Albert", api.Vec3{}, nil)
	guest := srv.addPlayer("Guest", api.Vec3{}, map[string]bool{"minecraft.command.msg": true, "minecraft.command.help": true})

	tests := []struct {
		text   string
		sender api.CommandSender
		start  int
		length int
		want   []string
	}{
		{text: "gam", sender: alice, start: 0, length: 3, want: []string{"gamemode", "gamerule"}},
		{text: "/gam", sender: alice, start: 1, length: 3, want: []string{"gamemode", "gamerule"}},
		{text: "", sender: guest, start: 0, want: []string{"help", "minecraft:help", "minecraft:msg", "msg", "pick", "tell", "test:pick", "w"}},
		{text: "gamemode c", sender: alice, start: 9, length: 1, want: []string{"creative"}},
		{text: "gamemode ", sender: alice, start: 9, want: []string{"adventure", "creative", "spectator", "survival"}},
		{text: "gamemode creative Al", sender: alice, start: 18, length: 2, want: []string{"Albert", "Alice"}},
		{text: "msg A", sender: guest, start: 4, length: 1, want: []string{"Albert", "Alice"}},
		{text: "msg @", sender: guest, start: 4, length: 1, want: nil},
		{text: "msg @", sender: alice, start: 4, length: 1, want: []string{"@a", "@p", "@r", "@s"}},
		{text: "kill @e[ty", sender: alice, start: 8, length: 2, want: []string{"type="}},
		{text: "setblock ", sender: alice, start: 9, want: []string{"~", "~ ~", "~ ~ ~"}},
		{text: "setblock ~ ", sender: alice, start: 9, length: 2, want: []string{"~ ~", "~ ~ ~"}},
		{text: "setblock 1 2 3 oak", sender: alice, start: 15, length: 3, want: []string{"minecraft:oak_log"}},
		{text: "give Alice minecraft:dia", sender: alice, start: 11, length: 13, want: []string{"minecraft:diamond", "minecraft:diamond_sword"}},
		{text: "time set 5", sender: alice, start: 9, length: 1, want: []string{"5d", "5s", "5t"}},
		{text: "pick ap", sender: alice, start: 5, length: 2, want: []string{"apple", "apricot"}},
		{text: "test:pick b", sender: alice, start: 10, length: 1, want: []string{"banana"}},
		{text: "tp", sender: alice, start: 0, length: 2, want: []string{"tps"}},
		{text: "help gamem", sender: alice, start: 5, length: 5, want: []string{"gamemode"}},
		{text: "weather rain ", sender: alice, start: 13, want: nil},
		{text: "difficulty h", sender: guest, start: 12, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := srv.cmds.Suggest(tt.sender, tt.text)
			var texts []string
			for _, s := range got.Suggestions {
				texts = append(texts, s.Text)
			}
			if !slices.Equal(texts, tt.want) {
				t.Fatalf("suggestions %v, want %v", texts, tt.want)
			}
			if len(tt.want) > 0 && (got.Start != tt.start || got.Length != tt.length) {
				t.Fatalf("range %d+%d, want %d+%d", got.Start, got.Length, tt.start, tt.length)
			}
		})
	}
}

func TestFindSuggestionContextInsideArgument(t *testing.T) {
	srv := newServer()
	srv.cmds.Register("", api.Literal("a").Then(api.Argument("x", api.IntArg()).Then(api.Argument("y", api.IntArg()))))
	res := srv.cmds.parse(srv.cmds.tree.Load(), nil, "a 12 3")
	_, parent, start := findSuggestionContext(res.ctx, 3)
	if parent.Name != "a" || start != 2 {
		t.Fatalf("parent %q start %d", parent.Name, start)
	}
	_, parent, start = findSuggestionContext(res.ctx, 6)
	if parent.Name != "x" || start != 5 {
		t.Fatalf("parent %q start %d", parent.Name, start)
	}
}
