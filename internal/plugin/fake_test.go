package plugin

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

// fakeServer implements the parts of api.Server the plugin host touches.
// Unimplemented methods panic through the nil embedded interface.
type fakeServer struct {
	api.Server
	mu        sync.Mutex
	players   []*fakePlayer
	console   *fakePlayer
	cmds      *fakeCommands
	perms     *fakePerms
	sched     *fakeScheduler
	broadcast []string
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		console: &fakePlayer{name: "CONSOLE", op: true},
		cmds:    &fakeCommands{roots: map[string]*api.CommandNode{}, owners: map[string]string{}},
		perms:   &fakePerms{all: map[string]api.Permission{}},
		sched:   &fakeScheduler{},
	}
}

func (s *fakeServer) addPlayer(name string) *fakePlayer {
	p := &fakePlayer{name: name, id: api.UUID{byte(len(s.players) + 1), 0xaa}, loc: api.Location{World: "world"}}
	s.mu.Lock()
	s.players = append(s.players, p)
	s.mu.Unlock()
	return p
}

func (s *fakeServer) Players() []api.Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]api.Player, len(s.players))
	for i, p := range s.players {
		out[i] = p
	}
	return out
}

func (s *fakeServer) Player(id api.UUID) (api.Player, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		if p.id == id {
			return p, true
		}
	}
	return nil, false
}

func (s *fakeServer) PlayerByName(name string) (api.Player, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		if strings.EqualFold(p.name, name) {
			return p, true
		}
	}
	return nil, false
}

func (s *fakeServer) Broadcast(msg api.Component) {
	s.mu.Lock()
	s.broadcast = append(s.broadcast, msg.Plain())
	s.mu.Unlock()
}

func (s *fakeServer) Console() api.CommandSender     { return s.console }
func (s *fakeServer) Commands() api.CommandRegistry  { return s.cmds }
func (s *fakeServer) Permissions() api.Permissions   { return s.perms }
func (s *fakeServer) Scheduler() api.Scheduler       { return s.sched }
func (s *fakeServer) Worlds() []api.World            { return nil }
func (s *fakeServer) World(string) (api.World, bool) { return nil, false }
func (s *fakeServer) DefaultWorld() api.World        { return nil }

type fakePlayer struct {
	api.Player
	name string
	id   api.UUID
	op   bool
	loc  api.Location

	mu   sync.Mutex
	msgs []string
}

func (p *fakePlayer) Name() string              { return p.name }
func (p *fakePlayer) UUID() api.UUID            { return p.id }
func (p *fakePlayer) EntityID() int32           { return int32(p.id[0]) }
func (p *fakePlayer) Location() api.Location    { return p.loc }
func (p *fakePlayer) GameMode() api.GameMode    { return api.Survival }
func (p *fakePlayer) Health() float64           { return 20 }
func (p *fakePlayer) IsOp() bool                { return p.op }
func (p *fakePlayer) HasPermission(string) bool { return true }
func (p *fakePlayer) Kick(api.Component)        {}
func (p *fakePlayer) Teleport(to api.Location, _ api.TeleportCause) bool {
	p.loc = to
	return true
}

func (p *fakePlayer) SendMessage(c api.Component) {
	p.mu.Lock()
	p.msgs = append(p.msgs, c.Plain())
	p.mu.Unlock()
}

func (p *fakePlayer) messages() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.msgs...)
}

type fakeCommands struct {
	api.CommandRegistry
	mu     sync.Mutex
	roots  map[string]*api.CommandNode
	owners map[string]string
}

func (c *fakeCommands) Register(owner string, root *api.CommandNode, aliases ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, name := range append([]string{root.Name}, aliases...) {
		c.roots[name] = root
		c.owners[name] = owner
	}
	return nil
}

func (c *fakeCommands) UnregisterAll(owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, o := range c.owners {
		if o == owner {
			delete(c.roots, name)
			delete(c.owners, name)
		}
	}
}

func (c *fakeCommands) has(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.roots[name]
	return ok
}

// Dispatch walks the tree with whitespace tokenization; arguments are taken
// as single words.
func (c *fakeCommands) Dispatch(sender api.CommandSender, line string) (int, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return 0, errors.New("empty")
	}
	c.mu.Lock()
	node, ok := c.roots[parts[0]]
	c.mu.Unlock()
	if !ok {
		return 0, errors.New("unknown command")
	}
	ctx := &fakeCtx{sender: sender, input: line, label: parts[0], args: map[string]string{}}
	for _, tok := range parts[1:] {
		var next *api.CommandNode
		for _, ch := range node.Children {
			if ch.Kind == api.NodeLiteral && ch.Name == tok || ch.Kind == api.NodeArgument {
				next = ch
				break
			}
		}
		if next == nil {
			return 0, errors.New("bad syntax")
		}
		if next.Kind == api.NodeArgument {
			ctx.args[next.Name] = tok
		}
		node = next
	}
	if node.Executor == nil {
		return 0, errors.New("incomplete command")
	}
	return node.Executor(ctx)
}

type fakeCtx struct {
	api.CommandContext
	sender api.CommandSender
	input  string
	label  string
	args   map[string]string
}

func (c *fakeCtx) Sender() api.CommandSender { return c.sender }
func (c *fakeCtx) Input() string             { return c.input }
func (c *fakeCtx) Label() string             { return c.label }
func (c *fakeCtx) Has(n string) bool         { _, ok := c.args[n]; return ok }
func (c *fakeCtx) String(n string) string    { return c.args[n] }

type fakePerms struct {
	api.Permissions
	mu  sync.Mutex
	all map[string]api.Permission
}

func (p *fakePerms) Register(perm api.Permission) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.all[perm.Name] = perm
	return nil
}

func (p *fakePerms) Unregister(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.all, name)
}

func (p *fakePerms) has(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.all[name]
	return ok
}

type fakeTask struct {
	id        uint64
	owner     string
	fn        func(api.Task)
	cancelled atomic.Bool
	repeating bool
}

func (t *fakeTask) ID() uint64      { return t.id }
func (t *fakeTask) Owner() string   { return t.owner }
func (t *fakeTask) Cancel()         { t.cancelled.Store(true) }
func (t *fakeTask) Cancelled() bool { return t.cancelled.Load() }
func (t *fakeTask) Repeating() bool { return t.repeating }

type fakeScheduler struct {
	mu    sync.Mutex
	tasks []*fakeTask
}

func (s *fakeScheduler) add(owner string, repeating bool, fn func(api.Task)) api.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &fakeTask{id: uint64(len(s.tasks) + 1), owner: owner, fn: fn, repeating: repeating}
	s.tasks = append(s.tasks, t)
	return t
}

// tick runs every pending task once.
func (s *fakeScheduler) tick() {
	s.mu.Lock()
	tasks := append([]*fakeTask(nil), s.tasks...)
	s.mu.Unlock()
	for _, t := range tasks {
		if !t.Cancelled() {
			t.fn(t)
			if !t.repeating {
				t.Cancel()
			}
		}
	}
}

func (s *fakeScheduler) Global() api.TaskScheduler                       { return fakeTS{s} }
func (s *fakeScheduler) Async() api.AsyncScheduler                       { return fakeAsync{s} }
func (s *fakeScheduler) Region(api.World, int, int) api.TaskScheduler    { return fakeTS{s} }
func (s *fakeScheduler) Entity(api.Entity) api.EntityScheduler           { return nil }
func (s *fakeScheduler) IsOwnedByCurrentRegion(api.World, int, int) bool { return true }
func (s *fakeScheduler) IsGlobalThread() bool                            { return true }
func (s *fakeScheduler) CancelAll(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.owner == owner {
			t.Cancel()
		}
	}
}

type fakeTS struct{ s *fakeScheduler }

func (f fakeTS) Run(o string, fn func(api.Task)) api.Task { return f.s.add(o, false, fn) }
func (f fakeTS) RunDelayed(o string, _ api.Ticks, fn func(api.Task)) api.Task {
	return f.s.add(o, false, fn)
}
func (f fakeTS) RunRepeating(o string, _, _ api.Ticks, fn func(api.Task)) api.Task {
	return f.s.add(o, true, fn)
}

type fakeAsync struct{ s *fakeScheduler }

func (f fakeAsync) RunNow(o string, fn func(api.Task)) api.Task { return f.s.add(o, false, fn) }
func (f fakeAsync) RunDelayed(o string, _ time.Duration, fn func(api.Task)) api.Task {
	return f.s.add(o, false, fn)
}
func (f fakeAsync) RunAtFixedRate(o string, _, _ time.Duration, fn func(api.Task)) api.Task {
	return f.s.add(o, true, fn)
}
