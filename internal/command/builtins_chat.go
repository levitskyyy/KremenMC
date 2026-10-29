package command

import (
	"slices"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

func (b *builtins) help() *api.CommandNode {
	all := func(c api.CommandContext) (int, error) {
		d := b.d
		root := d.tree.Load().root
		var lines []string
		for _, n := range root.Children {
			if strings.Contains(n.Name, ":") {
				continue
			}
			if u, ok := d.smartUsageOf(c.Sender(), n, false, false); ok {
				lines = append(lines, "/"+u)
			}
		}
		for _, l := range lines {
			c.Sender().SendMessage(api.Text(l))
		}
		return len(lines), nil
	}
	one := func(c api.CommandContext) (int, error) {
		d := b.d
		line := c.String("command")
		res := d.parse(d.tree.Load(), c.Sender(), line)
		if len(res.ctx.nodes) == 0 {
			return 0, fail("Unknown command or insufficient permissions")
		}
		leaf := res.ctx
		for leaf.child != nil && len(leaf.child.nodes) > 0 {
			leaf = leaf.child
		}
		last := leaf.nodes[len(leaf.nodes)-1].node
		parsed := strings.TrimRight(res.r.s[:res.r.pos], " ")
		usages := d.smartUsage(c.Sender(), last)
		if last.Redirect != nil {
			usages = append(usages, d.redirectUsage(last))
		}
		if last.Executor != nil {
			c.Sender().SendMessage(api.Text("/" + parsed))
		}
		for _, u := range usages {
			c.Sender().SendMessage(api.Text("/" + parsed + " " + u))
		}
		return max(1, len(usages)), nil
	}
	complete := func(c api.CommandContext, remaining string) []api.Suggestion {
		comp := b.d.Suggest(c.Sender(), remaining)
		out := make([]api.Suggestion, len(comp.Suggestions))
		for i, s := range comp.Suggestions {
			out[i] = api.Suggestion{Text: remaining[:comp.Start] + s.Text, Tooltip: s.Tooltip}
		}
		return out
	}
	return api.Literal("help").Describe("Shows command usage").Executes(all).
		Then(api.Argument("command", api.GreedyArg()).Suggests(complete).Executes(one))
}

func (b *builtins) list() *api.CommandNode {
	run := func(uuids bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			ps := b.srv.Players()
			slices.SortFunc(ps, func(a, b api.Player) int { return strings.Compare(a.Name(), b.Name()) })
			parts := make([]string, len(ps))
			for i, p := range ps {
				parts[i] = p.Name()
				if uuids {
					parts[i] += " (" + p.UUID().String() + ")"
				}
			}
			c.Sender().SendMessage(textf("There are %d of a max of %d players online: %s", len(ps), b.srv.MaxPlayers(), strings.Join(parts, ", ")))
			return len(ps), nil
		}
	}
	return api.Literal("list").Describe("Lists players on the server").Executes(run(false)).
		Then(api.Literal("uuids").Executes(run(true)))
}

func (b *builtins) say() *api.CommandNode {
	return api.Literal("say").Describe("Broadcasts a message").
		Then(api.Argument("message", api.MessageArg()).Executes(func(c api.CommandContext) (int, error) {
			b.srv.Broadcast(textf("[%s] %s", c.Sender().Name(), c.String("message")))
			return 1, nil
		}))
}

func (b *builtins) me() *api.CommandNode {
	return api.Literal("me").Describe("Displays a message about yourself").
		Then(api.Argument("action", api.MessageArg()).Executes(func(c api.CommandContext) (int, error) {
			b.srv.Broadcast(textf("* %s %s", c.Sender().Name(), c.String("action")))
			return 1, nil
		}))
}

func (b *builtins) msg() *api.CommandNode {
	return api.Literal("msg").Describe("Sends a private message").
		Then(api.Argument("targets", api.PlayersArg()).
			Then(api.Argument("message", api.MessageArg()).Executes(func(c api.CommandContext) (int, error) {
				ps, err := b.players(c, "targets")
				if err != nil {
					return 0, err
				}
				text := c.String("message")
				whisper := func(s string) api.Component {
					return api.Colored(s, "gray").WithItalic(true)
				}
				for _, p := range ps {
					p.SendMessage(whisper(c.Sender().Name() + " whispers to you: " + text))
					c.Sender().SendMessage(whisper("You whisper to " + p.Name() + ": " + text))
				}
				return len(ps), nil
			})))
}
