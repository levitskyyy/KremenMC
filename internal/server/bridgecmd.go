package server

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/command"
)

func senderUUID(s api.CommandSender) bridge.UUID {
	if p, ok := s.(*Player); ok {
		return bridge.UUID(p.prof.UUID)
	}
	return bridge.UUID{}
}

// RegisterCommand registers a Java plugin command. The Java side parses,
// runs and completes it; Go only advertises its tree to clients.
func (h BridgeHost) RegisterCommand(spec bridge.CommandSpec) {
	s := h.s
	name := strings.ToLower(spec.Name)
	var root *api.CommandNode
	if spec.Tree != nil {
		root = bridgeNode(spec.Tree, nil)
		root.Name = name
		root.Kind = api.NodeLiteral
		if spec.Tree.Bool("q") {
			root.Requirement = func(snd api.CommandSender) bool {
				b := s.javaBridge()
				return b != nil && b.CanUse(context.Background(), senderUUID(snd), name)
			}
		}
	} else {
		root = api.Literal(name).Executes(noopExec).RequiresPermission(spec.Permission).
			Then(api.Argument("args", api.GreedyArg()).Executes(noopExec).Suggests(noopSuggest))
	}
	root.Describe(spec.Description)
	f := &command.Foreign{
		Run: func(snd api.CommandSender, line string) (int, error) {
			b := s.javaBridge()
			if b == nil {
				return 0, api.Errorf("Java plugins are not running")
			}
			ok, err := b.Command(context.Background(), senderUUID(snd), line)
			if err != nil || !ok {
				return 0, err
			}
			return 1, nil
		},
		Suggest: func(snd api.CommandSender, line string) command.Completion {
			b := s.javaBridge()
			if b == nil {
				return command.Completion{Start: len(line)}
			}
			start, length, ms, err := b.Suggest(context.Background(), senderUUID(snd), line)
			if err != nil {
				return command.Completion{Start: len(line)}
			}
			bs := utf16Offset(line, start)
			c := command.Completion{Start: bs, Length: utf16Offset(line[bs:], length)}
			for _, m := range ms {
				sg := api.Suggestion{Text: m.Text}
				if m.TooltipJSON != "" {
					t := componentFromJSON(m.TooltipJSON)
					sg.Tooltip = &t
				}
				c.Suggestions = append(c.Suggestions, sg)
			}
			return c
		},
	}
	owner := strings.ToLower(spec.Plugin)
	s.cmds.Unregister(owner, name)
	if err := s.cmds.RegisterForeign(owner, owner, root, spec.Aliases, f); err != nil {
		s.log.Warn("bridge command not registered", "command", spec.Name, "err", err)
	}
}

func noopExec(api.CommandContext) (int, error)                { return 0, nil }
func noopSuggest(api.CommandContext, string) []api.Suggestion { return nil }

// utf16Offset converts n UTF-16 code units at the start of s to a byte offset.
func utf16Offset(s string, n int) int {
	i := 0
	for i < len(s) && n > 0 {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n -= 2
		} else {
			n--
		}
		i += size
	}
	return i
}

// bridgeNode builds the advertised form of a brigadier node; redirects are
// resolved against the command's own root once the tree is complete.
func bridgeNode(f bridge.Fields, top *api.CommandNode) *api.CommandNode {
	n := &api.CommandNode{Name: f.String("n")}
	if top == nil {
		top = n
	}
	if f.String("k") == "arg" {
		n.Kind = api.NodeArgument
		n.Type = bridgeArgType(f)
		if f.Bool("s") {
			n.Suggest = noopSuggest
		}
	}
	if f.Bool("x") {
		n.Executor = noopExec
	}
	for _, v := range f.List("c") {
		n.Children = append(n.Children, bridgeNode(bridge.Fields(bridge.ArgMap([]any{v}, 0)), top))
	}
	if path := f.List("r"); len(path) > 0 && n != top {
		t := top
		if first, _ := path[0].(string); !strings.EqualFold(first, top.Name) {
			path = nil
			t = nil
		}
		for _, p := range path[min(1, len(path)):] {
			name, _ := p.(string)
			var next *api.CommandNode
			for _, c := range t.Children {
				if c.Name == name {
					next = c
					break
				}
			}
			if next == nil {
				t = nil
				break
			}
			t = next
		}
		n.Redirect = t
	}
	return n
}

func bridgeArgType(f bridge.Fields) api.ArgumentType {
	t := api.ArgumentType{Parser: api.ParserID(f.String("p"))}
	if t.Parser == "" {
		t.Parser = api.ParserString
	}
	if f.Has("min") {
		t.Min, t.HasMin = f.Float("min"), true
	}
	if f.Has("max") {
		t.Max, t.HasMax = f.Float("max"), true
	}
	t.StringMode = api.StringMode(f.Int("mode"))
	t.Single = f.Bool("single")
	t.PlayersOnly = f.Bool("players")
	t.Registry = api.Key(f.String("registry"))
	return t
}
