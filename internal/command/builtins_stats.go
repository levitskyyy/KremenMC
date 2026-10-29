package command

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/metrics"
)

// AdminBarServer shows live server stats to a player in the sidebar and a
// boss bar and remembers the choice per player.
type AdminBarServer interface {
	AdminBar(p api.Player) (sidebar, bossbar bool)
	SetAdminBar(p api.Player, sidebar, bossbar bool) bool
}

func (b *builtins) adminbar() *api.CommandNode {
	run := func(arg string) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			srv, ok := b.srv.(AdminBarServer)
			if !ok {
				return 0, fail("Not supported by this server")
			}
			p, ok := c.Sender().(api.Player)
			if !ok {
				return 0, fail("A player is required to run this command here")
			}
			side, boss := srv.AdminBar(p)
			switch arg {
			case "":
				on := !side && !boss
				side, boss = on, on
			case "on":
				side, boss = true, true
			case "off":
				side, boss = false, false
			case "sidebar":
				side = !side
			case "bossbar":
				boss = !boss
			}
			if !srv.SetAdminBar(p, side, boss) {
				return 0, fail("A player is required to run this command here")
			}
			c.Sender().SendMessage(api.Text("Admin bar: ").Append(onOff("sidebar", side), api.Text(", "), onOff("boss bar", boss)))
			return 1, nil
		}
	}
	n := api.Literal("adminbar").Describe("Toggles live server stats in the sidebar and boss bar").Executes(run(""))
	for _, arg := range []string{"on", "off", "sidebar", "bossbar"} {
		n.Then(api.Literal(arg).Executes(run(arg)))
	}
	return n
}

func onOff(what string, on bool) api.Component {
	if on {
		return api.Colored(what+" on", "green")
	}
	return api.Colored(what+" off", "gray")
}

// kremenmc groups engine administration; "/kremenmc stats" prints the same snapshot
// the admin bar shows.
func (b *builtins) kremenmc() *api.CommandNode {
	return api.Literal("kremenmc").Describe("kremenmc engine administration").Then(
		api.Literal("stats").Describe("Shows live server statistics").Executes(func(c api.CommandContext) (int, error) {
			sp, ok := b.srv.(api.StatsProvider)
			if !ok {
				return 0, fail("Not supported by this server")
			}
			st := sp.Stats()
			c.Sender().SendMessage(metrics.Title())
			for _, l := range metrics.Lines(&st) {
				c.Sender().SendMessage(l)
			}
			return int(st.TPS), nil
		}),
	)
}
