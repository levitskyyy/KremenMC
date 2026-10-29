package command

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

func reasonArg() *api.CommandNode { return api.Argument("reason", api.MessageArg()) }

func (b *builtins) kick() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		ps, err := b.players(c, "targets")
		if err != nil {
			return 0, err
		}
		reason := api.Text("Kicked by an operator")
		if c.Has("reason") {
			reason = api.Text(c.String("reason"))
		}
		for _, p := range ps {
			p.Kick(reason)
			b.success(c, textf("Kicked %s: %s", p.Name(), reason.Plain()), true)
		}
		return len(ps), nil
	}
	return api.Literal("kick").Describe("Kicks a player off a server").
		Then(api.Argument("targets", api.PlayersArg()).Executes(run).Then(reasonArg().Executes(run)))
}

func banMessage(reason string) api.Component {
	msg := "You are banned from this server."
	if reason != "" {
		msg += "\nReason: " + reason
	}
	return api.Text(msg)
}

func (b *builtins) ban() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		profiles, err := b.profiles(c, "targets")
		if err != nil {
			return 0, err
		}
		reason := "Banned by an operator."
		if c.Has("reason") {
			reason = c.String("reason")
		}
		bans := b.srv.ProfileBans()
		n := 0
		for _, p := range profiles {
			key := p.UUID.String()
			if _, banned := bans.Get(key); banned {
				continue
			}
			bans.Ban(api.BanEntry{Target: key, Name: p.Name, Reason: reason, Source: c.Sender().Name(), Created: time.Now()})
			n++
			b.success(c, textf("Banned %s: %s", p.Name, reason), true)
			if pl, ok := b.srv.Player(p.UUID); ok {
				pl.Kick(banMessage(reason))
			}
		}
		if n == 0 {
			return 0, fail("Nothing changed. The player is already banned")
		}
		return n, nil
	}
	return api.Literal("ban").Describe("Adds player to banlist").
		Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).Executes(run).Then(reasonArg().Executes(run)))
}

func (b *builtins) banIP() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		target := c.String("target")
		addr, err := netip.ParseAddr(target)
		if err != nil {
			p, ok := b.srv.PlayerByName(target)
			if !ok {
				return 0, fail("Invalid IP address or unknown player")
			}
			addr = p.Address().Addr()
		}
		ip := addr.Unmap().String()
		bans := b.srv.IPBans()
		if _, banned := bans.Get(ip); banned {
			return 0, fail("Nothing changed. That IP is already banned")
		}
		reason := "Banned by an operator."
		if c.Has("reason") {
			reason = c.String("reason")
		}
		bans.Ban(api.BanEntry{Target: ip, Name: ip, Reason: reason, Source: c.Sender().Name(), Created: time.Now()})
		var affected []api.Player
		for _, p := range b.srv.Players() {
			if p.Address().Addr().Unmap().String() == ip {
				affected = append(affected, p)
			}
		}
		b.success(c, textf("Banned IP %s: %s", ip, reason), true)
		if len(affected) > 0 {
			b.success(c, textf("This ban affects %d player(s): %s", len(affected), names(affected)), true)
		}
		for _, p := range affected {
			p.Kick(banMessage(reason))
		}
		return len(affected), nil
	}
	return api.Literal("ban-ip").Describe("Adds IP address to banlist").
		Then(api.Argument("target", api.WordArg()).Suggests(b.onlineNames(nil)).Executes(run).Then(reasonArg().Executes(run)))
}

func (b *builtins) onlineNames(keep func(api.Player) bool) api.SuggestionProvider {
	return suggestStrings(func(api.CommandContext) []string {
		var out []string
		for _, p := range b.srv.Players() {
			if keep == nil || keep(p) {
				out = append(out, p.Name())
			}
		}
		return out
	})
}

func (b *builtins) pardon() *api.CommandNode {
	banned := suggestStrings(func(api.CommandContext) []string {
		var out []string
		for _, e := range b.srv.ProfileBans().Entries() {
			out = append(out, e.Name)
		}
		return out
	})
	return api.Literal("pardon").Describe("Removes entries from the banlist").
		Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).Suggests(banned).Executes(func(c api.CommandContext) (int, error) {
			profiles, err := b.profiles(c, "targets")
			if err != nil {
				return 0, err
			}
			n := 0
			for _, p := range profiles {
				if b.srv.ProfileBans().Pardon(p.UUID.String()) {
					n++
					b.success(c, textf("Unbanned %s", p.Name), true)
				}
			}
			if n == 0 {
				return 0, fail("Nothing changed. The player isn't banned")
			}
			return n, nil
		}))
}

func (b *builtins) pardonIP() *api.CommandNode {
	banned := suggestStrings(func(api.CommandContext) []string {
		var out []string
		for _, e := range b.srv.IPBans().Entries() {
			out = append(out, e.Target)
		}
		return out
	})
	return api.Literal("pardon-ip").Describe("Removes entries from the IP banlist").
		Then(api.Argument("target", api.WordArg()).Suggests(banned).Executes(func(c api.CommandContext) (int, error) {
			addr, err := netip.ParseAddr(c.String("target"))
			if err != nil {
				return 0, fail("Invalid IP address")
			}
			ip := addr.Unmap().String()
			if !b.srv.IPBans().Pardon(ip) {
				return 0, fail("Nothing changed. That IP isn't banned")
			}
			b.success(c, textf("Unbanned IP %s", ip), true)
			return 1, nil
		}))
}

func (b *builtins) banlist() *api.CommandNode {
	show := func(players, ips bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			var entries []api.BanEntry
			if players {
				entries = append(entries, b.srv.ProfileBans().Entries()...)
			}
			if ips {
				entries = append(entries, b.srv.IPBans().Entries()...)
			}
			if len(entries) == 0 {
				c.Sender().SendMessage(api.Text("There are no bans"))
				return 0, nil
			}
			slices.SortFunc(entries, func(a, b api.BanEntry) int { return strings.Compare(a.Name, b.Name) })
			c.Sender().SendMessage(textf("There are %d ban(s):", len(entries)))
			for _, e := range entries {
				name := e.Name
				if name == "" {
					name = e.Target
				}
				c.Sender().SendMessage(textf("%s was banned by %s: %s", name, e.Source, e.Reason))
			}
			return len(entries), nil
		}
	}
	return api.Literal("banlist").Describe("Displays the banlist").Executes(show(true, true)).Then(
		api.Literal("ips").Executes(show(false, true)),
		api.Literal("players").Executes(show(true, false)),
	)
}

func (b *builtins) setOp(c api.CommandContext, value bool) (int, error) {
	profiles, err := b.profiles(c, "targets")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range profiles {
		if pl, ok := b.srv.Player(p.UUID); ok {
			if pl.IsOp() == value {
				continue
			}
			pl.SetOp(value)
			b.d.Resend(pl)
		} else if off, ok := b.srv.OfflinePlayer(p.UUID); ok {
			if off.IsOp() == value {
				continue
			}
			off.SetOp(value)
		} else {
			continue
		}
		n++
		if value {
			b.success(c, textf("Made %s a server operator", p.Name), true)
		} else {
			b.success(c, textf("Made %s no longer a server operator", p.Name), true)
		}
	}
	if n == 0 {
		if value {
			return 0, fail("Nothing changed. The player already is an operator")
		}
		return 0, fail("Nothing changed. The player is not an operator")
	}
	return n, nil
}

func (b *builtins) op() *api.CommandNode {
	return api.Literal("op").Describe("Grants operator status to a player").
		Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).
			Suggests(b.onlineNames(func(p api.Player) bool { return !p.IsOp() })).
			Executes(func(c api.CommandContext) (int, error) { return b.setOp(c, true) }))
}

func (b *builtins) deop() *api.CommandNode {
	return api.Literal("deop").Describe("Revokes operator status from a player").
		Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).
			Suggests(b.onlineNames(api.Player.IsOp)).
			Executes(func(c api.CommandContext) (int, error) { return b.setOp(c, false) }))
}

func (b *builtins) whitelist() *api.CommandNode {
	toggle := func(on bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			wl := b.srv.Whitelist()
			state := map[bool]string{true: "on", false: "off"}[on]
			if wl.Enabled() == on {
				return 0, fail("Whitelist is already turned %s", state)
			}
			wl.SetEnabled(on)
			b.success(c, textf("Whitelist is now turned %s", state), true)
			return 1, nil
		}
	}
	list := func(c api.CommandContext) (int, error) {
		entries := b.srv.Whitelist().Entries()
		if len(entries) == 0 {
			c.Sender().SendMessage(api.Text("There are no whitelisted players"))
			return 0, nil
		}
		c.Sender().SendMessage(textf("There are %d whitelisted player(s): %s", len(entries), profileNames(entries)))
		return len(entries), nil
	}
	add := func(c api.CommandContext) (int, error) {
		profiles, err := b.profiles(c, "targets")
		if err != nil {
			return 0, err
		}
		wl := b.srv.Whitelist()
		n := 0
		for _, p := range profiles {
			if wl.Contains(p.UUID) {
				continue
			}
			wl.Add(p)
			n++
			b.success(c, textf("Added %s to the whitelist", p.Name), true)
		}
		if n == 0 {
			return 0, fail("Player is already whitelisted")
		}
		return n, nil
	}
	remove := func(c api.CommandContext) (int, error) {
		profiles, err := b.profiles(c, "targets")
		if err != nil {
			return 0, err
		}
		n := 0
		for _, p := range profiles {
			if b.srv.Whitelist().Remove(p.UUID) {
				n++
				b.success(c, textf("Removed %s from the whitelist", p.Name), true)
			}
		}
		if n == 0 {
			return 0, fail("Player is not whitelisted")
		}
		return n, nil
	}
	reload := func(c api.CommandContext) (int, error) {
		r, ok := b.srv.(WhitelistReloader)
		if !ok {
			return 0, fail("Not supported by this server")
		}
		if err := r.ReloadWhitelist(); err != nil {
			return 0, fail("Could not reload the whitelist: %v", err)
		}
		b.success(c, api.Text("Reloaded the whitelist"), true)
		return 1, nil
	}
	listed := suggestStrings(func(api.CommandContext) []string {
		return strings.Split(profileNames(b.srv.Whitelist().Entries()), ", ")
	})
	return api.Literal("whitelist").Describe("Manages server whitelist").Then(
		api.Literal("on").Executes(toggle(true)),
		api.Literal("off").Executes(toggle(false)),
		api.Literal("list").Executes(list),
		api.Literal("add").Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).
			Suggests(b.onlineNames(func(p api.Player) bool { return !b.srv.Whitelist().Contains(p.UUID()) })).Executes(add)),
		api.Literal("remove").Then(api.Argument("targets", api.ArgumentType{Parser: api.ParserGameProfile}).Suggests(listed).Executes(remove)),
		api.Literal("reload").Executes(reload),
	)
}

func (b *builtins) stop() *api.CommandNode {
	return api.Literal("stop").Describe("Stops the server").Executes(func(c api.CommandContext) (int, error) {
		b.success(c, api.Text("Stopping the server"), true)
		go b.srv.Shutdown(api.Text("Server closed"))
		return 1, nil
	})
}

func (b *builtins) saveAll() *api.CommandNode {
	run := func(flush bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			b.success(c, api.Text("Saving the game (this may take a moment!)"), true)
			save := func() {
				var err error
				if s, ok := b.srv.(SaveAllServer); ok {
					err = s.SaveAll(flush)
				} else {
					for _, w := range b.srv.Worlds() {
						if e := w.Save(); e != nil && err == nil {
							err = e
						}
					}
				}
				if err != nil {
					b.d.logger().Error("save-all failed", "err", err)
					c.Sender().SendMessage(ErrorComponent(fail("Unable to save the game (is there enough disk space?)"), ""))
					return
				}
				b.success(c, api.Text("Saved the game"), true)
			}
			if sch := b.srv.Scheduler(); sch != nil && sch.Async() != nil {
				sch.Async().RunNow(taskOwner, func(api.Task) { save() })
			} else {
				save()
			}
			return 1, nil
		}
	}
	return api.Literal("save-all").Describe("Saves the server to disk").Executes(run(false)).
		Then(api.Literal("flush").Executes(run(true)))
}

func (b *builtins) saveToggle(name string, on bool) *api.CommandNode {
	desc := map[bool]string{true: "Enables automatic server saves", false: "Disables automatic server saves"}[on]
	return api.Literal(name).Describe(desc).Executes(func(c api.CommandContext) (int, error) {
		s, ok := b.srv.(AutoSaveServer)
		if !ok {
			return 0, fail("Not supported by this server")
		}
		if s.AutoSave() == on {
			if on {
				return 0, fail("Saving is already turned on")
			}
			return 0, fail("Saving is already turned off")
		}
		s.SetAutoSave(on)
		if on {
			b.success(c, api.Text("Automatic saving is now enabled"), true)
		} else {
			b.success(c, api.Text("Automatic saving is now disabled"), true)
		}
		return 1, nil
	})
}

func tpsColor(tps float64) string {
	switch {
	case tps >= 18:
		return "green"
	case tps >= 15:
		return "yellow"
	}
	return "red"
}

func (b *builtins) tps() *api.CommandNode {
	return api.Literal("tps").Describe("Shows the server tick rate").Executes(func(c api.CommandContext) (int, error) {
		tps := b.srv.TPS()
		c.Sender().SendMessage(api.Text("TPS: ").Append(api.Colored(fmt.Sprintf("%.2f", tps), tpsColor(tps))))
		return int(tps), nil
	})
}

func (b *builtins) mspt() *api.CommandNode {
	return api.Literal("mspt").Describe("Shows the average tick duration").Executes(func(c api.CommandContext) (int, error) {
		s, ok := b.srv.(MSPTServer)
		if !ok {
			return 0, fail("Not supported by this server")
		}
		ms := s.MSPT()
		color := "green"
		if ms > 50 {
			color = "red"
		} else if ms > 40 {
			color = "yellow"
		}
		c.Sender().SendMessage(api.Text("MSPT: ").Append(api.Colored(fmt.Sprintf("%.2f ms", ms), color)))
		return int(ms), nil
	})
}

func (b *builtins) version() *api.CommandNode {
	return api.Literal("version").Describe("Shows the server version").Executes(func(c api.CommandContext) (int, error) {
		c.Sender().SendMessage(textf("This server is running %s version %s (Minecraft %s, protocol %d)",
			b.srv.Name(), b.srv.Version(), b.srv.GameVersion(), b.srv.Protocol()))
		return 1, nil
	})
}

type adminCommander interface {
	AdminCommand(perm string) *api.CommandNode
}

// plugins delegates to internal/plugin's AdminCommand when the server's
// plugin registry provides it and falls back to a read-only list.
func (b *builtins) plugins() *api.CommandNode {
	const perm = "kremenmc.command.plugins"
	if reg := b.srv.Plugins(); reg != nil {
		if a, ok := reg.(adminCommander); ok {
			if n := a.AdminCommand(perm); n != nil && n.Kind == api.NodeLiteral {
				return n
			}
		}
	}
	return api.Literal("plugins").Describe("Lists plugins").Executes(func(c api.CommandContext) (int, error) {
		reg := b.srv.Plugins()
		if reg == nil {
			c.Sender().SendMessage(api.Text("Plugins (0): "))
			return 0, nil
		}
		ps := reg.Plugins()
		msg := textf("Plugins (%d): ", len(ps))
		for i, p := range ps {
			if i > 0 {
				msg = msg.Append(api.Text(", "))
			}
			color := "red"
			if reg.IsEnabled(p.ID) {
				color = "green"
			}
			msg = msg.Append(api.Colored(p.Name, color))
		}
		c.Sender().SendMessage(msg)
		return len(ps), nil
	})
}
