package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

// AdminCommand builds "/plugins [list|reload|enable|disable <id>]" guarded by
// perm. The server registers it under its own owner.
func (m *Manager) AdminCommand(perm string) *api.CommandNode {
	ids := func(_ api.CommandContext, remaining string) []api.Suggestion {
		var out []api.Suggestion
		for _, id := range m.Order() {
			if strings.HasPrefix(id, remaining) {
				out = append(out, api.Suggestion{Text: id})
			}
		}
		return out
	}
	action := func(name string, fn func(ctx context.Context, id string) error, done string) *api.CommandNode {
		return api.Literal(name).Then(api.Argument("plugin", api.WordArg()).Suggests(ids).Executes(func(c api.CommandContext) (int, error) {
			id := c.String("plugin")
			if err := fn(context.Background(), id); err != nil {
				return 0, &api.CommandError{Message: api.Colored(fmt.Sprintf("%s %s failed: %v", name, id, err), "red")}
			}
			c.Sender().SendMessage(api.Colored(fmt.Sprintf("%s %s.", done, id), "green"))
			return 1, nil
		}))
	}
	list := func(c api.CommandContext) (int, error) {
		c.Sender().SendMessage(m.listComponent())
		return len(m.Order()), nil
	}
	return api.Literal("plugins").
		RequiresPermission(perm).
		Describe("Manage plugins").
		Executes(list).
		Then(
			api.Literal("list").Executes(list),
			action("reload", m.Reload, "Reloaded"),
			action("enable", m.Enable, "Enabled"),
			action("disable", m.Disable, "Disabled"),
		)
}

func (m *Manager) listComponent() api.Component {
	order := m.Order()
	failed := m.Failed()
	msg := api.Text(fmt.Sprintf("Plugins (%d): ", len(order)))
	first := true
	add := func(name, color string) {
		if !first {
			msg = msg.Append(api.Text(", "))
		}
		first = false
		msg = msg.Append(api.Colored(name, color))
	}
	for _, id := range order {
		color := "red"
		if st, _ := m.State(id); st == StateEnabled {
			color = "green"
		} else if st == StateLoaded || st == StateDisabled {
			color = "gray"
		}
		add(id, color)
	}
	for id := range failed {
		if _, loaded := m.State(id); !loaded {
			add(id, "dark_red")
		}
	}
	return msg
}
