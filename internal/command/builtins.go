package command

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/api"
)

// Optional capabilities a server may implement for commands the api does
// not cover. Commands report "not supported" when they are missing.
type (
	DefaultGameModeServer interface {
		DefaultGameMode() api.GameMode
		SetDefaultGameMode(api.GameMode)
	}
	AutoSaveServer interface {
		AutoSave() bool
		SetAutoSave(bool)
	}
	SaveAllServer interface {
		SaveAll(flush bool) error
	}
	MSPTServer interface {
		MSPT() float64
	}
	WhitelistReloader interface {
		ReloadWhitelist() error
	}
)

// FeedbackPermission receives "[Sender: message]" echoes of admin commands.
const FeedbackPermission = "minecraft.admin.command_feedback"

const taskOwner = "minecraft"

type builtins struct {
	d   *Dispatcher
	srv api.Server
}

type builtin struct {
	ns    string
	level int
	perm  string
	node  *api.CommandNode
	alias []string
}

// RegisterBuiltins registers the vanilla commands under "minecraft:" and
// kremenmc's own under "kremenmc:", and declares their permissions. Vanilla op
// levels map to permission defaults: level 0 is granted to everyone, levels
// 1-4 to operators; minecraft.oplevel.<n> groups each level.
func RegisterBuiltins(d *Dispatcher, server api.Server) {
	b := &builtins{d: d, srv: server}
	list := []builtin{
		{level: 0, node: b.help()},
		{level: 0, node: b.list()},
		{level: 0, node: b.me()},
		{level: 0, node: b.msg(), alias: []string{"tell", "w"}},
		{level: 2, node: b.say()},
		{level: 2, node: b.teleport(), alias: []string{"tp"}},
		{level: 2, node: b.gamemode()},
		{level: 2, node: b.defaultGamemode()},
		{level: 2, node: b.time()},
		{level: 2, node: b.weather()},
		{level: 2, node: b.difficulty()},
		{level: 2, node: b.give()},
		{level: 2, node: b.clear()},
		{level: 2, node: b.kill()},
		{level: 2, node: b.setblock()},
		{level: 2, node: b.fill()},
		{level: 2, node: b.setWorldSpawn()},
		{level: 2, node: b.spawnpoint()},
		{level: 2, node: b.gamerule()},
		{level: 2, node: b.seed()},
		{level: 3, node: b.kick()},
		{level: 3, node: b.ban()},
		{level: 3, node: b.banIP()},
		{level: 3, node: b.pardon()},
		{level: 3, node: b.pardonIP()},
		{level: 3, node: b.banlist()},
		{level: 3, node: b.op()},
		{level: 3, node: b.deop()},
		{level: 3, node: b.whitelist()},
		{level: 4, node: b.stop()},
		{level: 4, node: b.saveAll()},
		{level: 4, node: b.saveToggle("save-on", true)},
		{level: 4, node: b.saveToggle("save-off", false)},
		{ns: "kremenmc", level: 2, node: b.tps()},
		{ns: "kremenmc", level: 2, node: b.mspt()},
		{ns: "kremenmc", level: 0, node: b.version()},
		{ns: "kremenmc", level: 4, node: b.plugins(), alias: []string{"pl"}},
		{ns: "kremenmc", level: 2, node: b.adminbar()},
		{ns: "kremenmc", level: 2, node: b.kremenmc()},
	}
	perms := map[int][]string{}
	for _, c := range list {
		ns := c.ns
		if ns == "" {
			ns = "minecraft"
		}
		perm := ns + ".command." + c.node.Name
		if c.node.Permission == "" {
			c.node.Permission = perm
		}
		perm = c.node.Permission
		perms[c.level] = append(perms[c.level], perm)
		b.declare(perm, fmt.Sprintf("/%s (vanilla op level %d)", c.node.Name, c.level), c.level)
		_ = d.register("", ns, c.node, c.alias)
	}
	perms[2] = append(perms[2], SelectorPermission, FeedbackPermission)
	b.declare(SelectorPermission, "Use @-selectors in commands", 2)
	b.declare(FeedbackPermission, "Receive feedback of other operators' commands", 2)
	for lvl := 1; lvl <= 4; lvl++ {
		children := map[string]bool{}
		if lvl > 1 {
			children[fmt.Sprintf("minecraft.oplevel.%d", lvl-1)] = true
		}
		for _, p := range perms[lvl] {
			children[p] = true
		}
		b.declarePerm(api.Permission{
			Name:        fmt.Sprintf("minecraft.oplevel.%d", lvl),
			Description: fmt.Sprintf("Everything vanilla allows at op level %d", lvl),
			Default:     api.DefaultOp,
			Children:    children,
		})
	}
}

func (b *builtins) declare(name, desc string, level int) {
	def := api.DefaultOp
	if level == 0 {
		def = api.DefaultTrue
	}
	b.declarePerm(api.Permission{Name: name, Description: desc, Default: def})
}

func (b *builtins) declarePerm(p api.Permission) {
	if b.srv == nil || b.srv.Permissions() == nil {
		return
	}
	if _, ok := b.srv.Permissions().Get(p.Name); ok {
		return
	}
	_ = b.srv.Permissions().Register(p)
}

func cctx(c api.CommandContext) *cmdContext { return c.(*cmdContext) }

func (b *builtins) entities(c api.CommandContext, name string) ([]api.Entity, error) {
	x := cctx(c)
	v, ok := x.lookup(name)
	if !ok {
		e, isEntity := c.Sender().(api.Entity)
		if !isEntity {
			return nil, fail("An entity is required to run this command here")
		}
		return []api.Entity{e}, nil
	}
	es, _ := x.resolveEntities(v.(*selector))
	if len(es) == 0 {
		if v.(*selector).playersOnly {
			return nil, fail(msgNoPlayer)
		}
		return nil, fail(msgNoEntity)
	}
	return es, nil
}

func (b *builtins) players(c api.CommandContext, name string) ([]api.Player, error) {
	if !c.Has(name) {
		p, ok := c.Sender().(api.Player)
		if !ok {
			return nil, fail("A player is required to run this command here")
		}
		return []api.Player{p}, nil
	}
	ps := c.Players(name)
	if len(ps) == 0 {
		return nil, fail(msgNoPlayer)
	}
	return ps, nil
}

func (b *builtins) profiles(c api.CommandContext, name string) ([]api.GameProfile, error) {
	x := cctx(c)
	v, _ := x.lookup(name)
	pa, ok := v.(*profileArg)
	if !ok {
		return nil, fail("That player does not exist")
	}
	return x.resolveProfiles(pa)
}

func sameSender(a, b api.CommandSender) bool {
	if a == nil || b == nil {
		return false
	}
	pa, ok1 := a.(api.Player)
	pb, ok2 := b.(api.Player)
	if ok1 && ok2 {
		return pa.UUID() == pb.UUID()
	}
	return ok1 == ok2 && a.Name() == b.Name()
}

// success sends msg to the sender and, when broadcast is set, echoes it to
// other operators the way vanilla's sendSuccess(..., true) does.
func (b *builtins) success(c api.CommandContext, msg api.Component, broadcast bool) {
	sender := c.Sender()
	sender.SendMessage(msg)
	if !broadcast || b.srv == nil {
		return
	}
	if w := b.srv.DefaultWorld(); w != nil {
		if v, ok := w.GameRule("log_admin_commands"); ok && v == "false" {
			return
		}
	}
	echo := api.Component{Color: "gray", Italic: ptr(true)}.Append(api.Text("["+sender.Name()+": "), msg, api.Text("]"))
	for _, p := range b.srv.Players() {
		if !sameSender(p, sender) && p.HasPermission(FeedbackPermission) {
			p.SendMessage(echo)
		}
	}
	if con := b.srv.Console(); con != nil && !sameSender(con, sender) {
		con.SendMessage(echo)
	}
}

func ptr[T any](v T) *T { return &v }

func textf(format string, args ...any) api.Component { return api.Text(fmt.Sprintf(format, args...)) }

// forEntities runs fn for each entity on that entity's scheduler and calls
// done once all have run or been retired.
func (b *builtins) forEntities(es []api.Entity, fn func(api.Entity), done func()) {
	left := atomic.Int32{}
	left.Store(int32(len(es)))
	finish := func() {
		if left.Add(-1) == 0 && done != nil {
			done()
		}
	}
	if len(es) == 0 {
		if done != nil {
			done()
		}
		return
	}
	for _, e := range es {
		var sch api.EntityScheduler
		if b.srv != nil && b.srv.Scheduler() != nil {
			sch = b.srv.Scheduler().Entity(e)
		}
		if sch == nil {
			fn(e)
			finish()
			continue
		}
		sch.RunWithRetired(taskOwner, 0, func(api.Task) { fn(e); finish() }, finish)
	}
}

type chunkKey struct{ x, z int }

// forChunks runs fn for each chunk on its owning region, inline when the
// caller already owns it, then calls done.
func (b *builtins) forChunks(w api.World, chunks []chunkKey, fn func(chunkKey), done func()) {
	if len(chunks) == 0 {
		done()
		return
	}
	left := atomic.Int32{}
	left.Store(int32(len(chunks)))
	for _, ck := range chunks {
		run := func() {
			fn(ck)
			if left.Add(-1) == 0 {
				done()
			}
		}
		sch := b.srv.Scheduler()
		if sch == nil || sch.IsOwnedByCurrentRegion(w, ck.x, ck.z) {
			run()
			continue
		}
		sch.Region(w, ck.x, ck.z).Run(taskOwner, func(api.Task) { run() })
	}
}

// onGlobal runs fn on the server-wide tick, which owns time and weather.
func (b *builtins) onGlobal(fn func()) {
	sch := b.srv.Scheduler()
	if sch == nil || sch.IsGlobalThread() {
		fn()
		return
	}
	sch.Global().Run(taskOwner, func(api.Task) { fn() })
}

type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) add(n int) {
	c.mu.Lock()
	c.n += n
	c.mu.Unlock()
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func titleCase(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func modeName(g api.GameMode) string { return titleCase(g.String()) + " Mode" }

func names(ps []api.Player) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return strings.Join(out, ", ")
}

func profileNames(ps []api.GameProfile) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return strings.Join(out, ", ")
}

func (b *builtins) senderWorld(c api.CommandContext) (api.World, error) {
	w := senderWorld(b.srv, c.Sender())
	if w == nil {
		return nil, fail("No world is loaded")
	}
	return w, nil
}

// suggestStrings builds a provider over a dynamic candidate list.
func suggestStrings(list func(api.CommandContext) []string) api.SuggestionProvider {
	return func(c api.CommandContext, remaining string) []api.Suggestion {
		var out []api.Suggestion
		rem := strings.ToLower(remaining)
		for _, s := range list(c) {
			if strings.HasPrefix(strings.ToLower(s), rem) {
				out = append(out, api.Suggestion{Text: s})
			}
		}
		return out
	}
}
