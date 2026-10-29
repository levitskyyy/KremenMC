package kremenmc

import (
	"errors"
	"runtime"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// Node is a command tree node. Build trees with Literal and Argument and
// register the root with RegisterCommand.
type Node struct {
	kind        api.NodeKind
	name        string
	typ         api.ArgumentType
	permission  string
	description string
	suggestions []string
	exec        func(*CommandContext) int
	children    []*Node
}

func Literal(name string) *Node { return &Node{kind: api.NodeLiteral, name: name} }

func Argument(name string, t api.ArgumentType) *Node {
	return &Node{kind: api.NodeArgument, name: name, typ: t}
}

func (n *Node) Then(children ...*Node) *Node { n.children = append(n.children, children...); return n }

// Executes sets the handler; its return value is the command result.
func (n *Node) Executes(fn func(*CommandContext) int) *Node { n.exec = fn; return n }

func (n *Node) Requires(permission string) *Node { n.permission = permission; return n }

func (n *Node) Describe(desc string) *Node { n.description = desc; return n }

// Suggest sets static suggestions for an argument node.
func (n *Node) Suggest(values ...string) *Node { n.suggestions = values; return n }

type commandReg struct {
	node    *Node
	aliases []string
}

var (
	handlers = []func(*CommandContext) int{nil}
	commands []commandReg
)

// RegisterCommand registers a root literal. Before the plugin is enabled the
// registration is queued.
func RegisterCommand(root *Node, aliases ...string) error {
	if root.kind != api.NodeLiteral {
		return errors.New("kremenmc: command root must be a literal")
	}
	commands = append(commands, commandReg{root, aliases})
	if !enabled {
		return nil
	}
	return registerCommand(root, aliases)
}

// Command is shorthand for a single-literal command without arguments.
func Command(name string, fn func(*CommandContext) int, aliases ...string) error {
	return RegisterCommand(Literal(name).Executes(fn), aliases...)
}

func registerCommand(root *Node, aliases []string) error {
	var w wire.Writer
	w.Strings(aliases)
	encodeNode(&w, root)
	r := hostRegisterCommand(addr(w.B), uint32(len(w.B)))
	runtime.KeepAlive(w.B)
	if r != wire.OK {
		return errors.New("kremenmc: register command " + root.name + " failed")
	}
	return nil
}

func encodeNode(w *wire.Writer, n *Node) {
	w.U8(uint8(n.kind))
	w.String(n.name)
	if n.kind == api.NodeArgument {
		w.String(string(n.typ.Parser))
		var flags uint8
		if n.typ.HasMin {
			flags |= wire.ArgHasMin
		}
		if n.typ.HasMax {
			flags |= wire.ArgHasMax
		}
		if n.typ.Single {
			flags |= wire.ArgSingle
		}
		if n.typ.PlayersOnly {
			flags |= wire.ArgPlayersOnly
		}
		w.U8(flags)
		w.F64(n.typ.Min)
		w.F64(n.typ.Max)
		w.U8(uint8(n.typ.StringMode))
		w.String(string(n.typ.Registry))
	}
	w.String(n.permission)
	if n.exec != nil {
		handlers = append(handlers, n.exec)
		w.Uvarint(uint64(len(handlers) - 1))
	} else {
		w.Uvarint(0)
	}
	w.Strings(n.suggestions)
	w.String(n.description)
	w.Uvarint(uint64(len(n.children)))
	for _, c := range n.children {
		encodeNode(w, c)
	}
}

type argValue struct {
	tag   uint8
	s     string
	i     int64
	f     float64
	b     bool
	uuids []api.UUID
	pos   api.BlockPos
	vec   api.Vec3
}

// CommandContext carries the sender and parsed arguments of an invocation.
type CommandContext struct {
	// Player is set when a player ran the command.
	Player     *Player
	SenderName string
	Input      string
	Label      string
	args       map[string]argValue
}

func (c *CommandContext) IsPlayer() bool { return c.Player != nil }

// Reply sends a message to whoever ran the command.
func (c *CommandContext) Reply(msg api.Component) {
	if c.Player != nil {
		c.Player.SendMessage(msg)
		return
	}
	Console(msg)
}

func (c *CommandContext) Replyf(text string) { c.Reply(api.Text(text)) }

func (c *CommandContext) Has(name string) bool { _, ok := c.args[name]; return ok }

func (c *CommandContext) String(name string) string { return c.args[name].s }
func (c *CommandContext) Int(name string) int64     { return c.args[name].i }
func (c *CommandContext) Float(name string) float64 { return c.args[name].f }
func (c *CommandContext) Bool(name string) bool     { return c.args[name].b }

// Targets returns the UUIDs selected by an entity or player argument.
func (c *CommandContext) Targets(name string) []api.UUID    { return c.args[name].uuids }
func (c *CommandContext) BlockPos(name string) api.BlockPos { return c.args[name].pos }
func (c *CommandContext) Vec3(name string) api.Vec3         { return c.args[name].vec }

func decodeCommand(data []byte) (*CommandContext, error) {
	r := wire.NewReader(data)
	c := &CommandContext{args: map[string]argValue{}}
	kind := r.U8()
	c.SenderName = r.String()
	if kind == wire.SenderPlayer {
		id := r.UUID()
		p := Player{wire.PlayerRef{UUID: id, Name: c.SenderName}}
		if full, ok := PlayerByUUID(id); ok {
			p = full
		}
		c.Player = &p
	}
	c.Input, c.Label = r.String(), r.String()
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		name := r.String()
		v := argValue{tag: r.U8()}
		switch v.tag {
		case wire.ValString:
			v.s = r.String()
		case wire.ValInt:
			v.i = r.Varint()
			v.f = float64(v.i)
		case wire.ValFloat:
			v.f = r.F64()
		case wire.ValBool:
			v.b = r.Bool()
		case wire.ValUUIDs:
			m := r.Count()
			for j := 0; j < m && r.Err == nil; j++ {
				v.uuids = append(v.uuids, r.UUID())
			}
		case wire.ValBlockPos:
			v.pos = r.BlockPos()
		case wire.ValVec3:
			v.vec = r.Vec3()
		default:
			return nil, errors.New("kremenmc: unknown argument tag")
		}
		c.args[name] = v
	}
	return c, r.Err
}

func onCommand(handler uint32, data []byte) int32 {
	if handler == 0 || int(handler) >= len(handlers) || handlers[handler] == nil {
		return 0
	}
	ctx, err := decodeCommand(data)
	if err != nil {
		Errorf("decode command: %v", err)
		return 0
	}
	var res int
	if !guard("command "+ctx.Label, func() { res = handlers[handler](ctx) }) {
		ctx.Reply(api.Colored("An internal error occurred while running this command.", "red"))
		return 0
	}
	return int32(res)
}
