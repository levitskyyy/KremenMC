package command

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

const (
	msgUnknownCommand  = "Unknown or incomplete command, see below for error"
	msgUnknownArgument = "Incorrect argument for command"
	msgTrailing        = "Expected whitespace to end one argument, but found trailing data"
	msgUnexpected      = "An unexpected error occurred trying to execute that command"
)

type parseResult struct {
	ctx  *cmdContext
	r    reader
	errs []error
}

func (d *Dispatcher) parse(t *tree, sender api.CommandSender, input string) parseResult {
	ctx := &cmdContext{d: d, sender: sender, input: input, root: t.root, top: t.root}
	return d.parseNodes(t.root, true, reader{s: input}, ctx)
}

// relevant mirrors brigadier's CommandNode.getRelevantNodes: an exactly
// matching literal shadows all argument siblings.
func relevant(n *api.CommandNode, isRoot bool, r reader) []*api.CommandNode {
	word := r.s[r.pos:]
	if i := strings.IndexByte(word, ' '); i >= 0 {
		word = word[:i]
	}
	if isRoot {
		word = strings.ToLower(word)
	}
	var args []*api.CommandNode
	for _, c := range n.Children {
		if c.Kind == api.NodeLiteral {
			if c.Name == word {
				return []*api.CommandNode{c}
			}
			continue
		}
		args = append(args, c)
	}
	return args
}

func (d *Dispatcher) parseNodes(n *api.CommandNode, isRoot bool, r reader, ctx *cmdContext) parseResult {
	var potentials []parseResult
	var errs []error
	cursor := r.pos
	for _, child := range relevant(n, isRoot, r) {
		if !d.canUse(child, ctx.sender) {
			continue
		}
		cctx := ctx.copy()
		rr := r
		if err := d.parseNode(child, &rr, cctx); err != nil {
			errs = append(errs, err)
			continue
		}
		if isRoot && cctx.label == "" {
			cctx.label = child.Name
		}
		cctx.exec = child.Executor
		need := 2
		if child.Redirect != nil {
			need = 1
		}
		if !rr.canReadN(need) {
			potentials = append(potentials, parseResult{ctx: cctx, r: rr})
			continue
		}
		rr.skip()
		if child.Redirect != nil {
			sub := &cmdContext{d: d, sender: ctx.sender, input: ctx.input, label: cctx.label, root: child.Redirect, top: ctx.top, start: rr.pos}
			res := d.parseNodes(child.Redirect, child.Redirect == ctx.top, rr, sub)
			cctx.child = res.ctx
			return parseResult{ctx: cctx, r: res.r, errs: res.errs}
		}
		potentials = append(potentials, d.parseNodes(child, false, rr, cctx))
	}
	if len(potentials) == 0 {
		r.pos = cursor
		return parseResult{ctx: ctx, r: r, errs: errs}
	}
	slices.SortStableFunc(potentials, func(a, b parseResult) int {
		if ac, bc := a.r.canRead(), b.r.canRead(); ac != bc {
			if !ac {
				return -1
			}
			return 1
		}
		if ae, be := len(a.errs) == 0, len(b.errs) == 0; ae != be {
			if ae {
				return -1
			}
			return 1
		}
		return 0
	})
	return potentials[0]
}

func (d *Dispatcher) parseNode(n *api.CommandNode, r *reader, ctx *cmdContext) error {
	start := r.pos
	if n.Kind == api.NodeLiteral {
		end := start + len(n.Name)
		if end > len(r.s) || !strings.EqualFold(r.s[start:end], n.Name) || end < len(r.s) && r.s[end] != ' ' {
			return r.errf("Expected literal %s", n.Name)
		}
		r.pos = end
		ctx.nodes = append(ctx.nodes, parsedNode{node: n, start: start, end: end})
		return nil
	}
	v, err := d.parseArgument(n.Type, r, ctx)
	if err != nil {
		r.pos = start
		return err
	}
	if r.canRead() && r.peek() != ' ' {
		err := r.errf(msgTrailing)
		r.pos = start
		return err
	}
	ctx.args = append(ctx.args, argValue{name: n.Name, value: v, start: start, end: r.pos})
	ctx.nodes = append(ctx.nodes, parsedNode{node: n, start: start, end: r.pos})
	return nil
}

// Dispatch parses and runs line for sender. Failures are reported to the
// sender in vanilla style and also returned. A leading '/' is ignored.
func (d *Dispatcher) Dispatch(sender api.CommandSender, line string) (int, error) {
	line = strings.TrimPrefix(line, "/")
	n, err := d.execute(sender, line)
	if err != nil && sender != nil {
		sender.SendMessage(ErrorComponent(err, line))
	}
	return n, err
}

func (d *Dispatcher) execute(sender api.CommandSender, line string) (n int, err error) {
	res := d.parse(d.tree.Load(), sender, line)
	if res.r.canRead() {
		if len(res.errs) == 1 {
			return 0, res.errs[0]
		}
		msg := msgUnknownArgument
		if len(res.ctx.nodes) == 0 {
			msg = msgUnknownCommand
		}
		return 0, res.r.errf("%s", msg)
	}
	leaf := res.ctx
	for leaf.child != nil {
		leaf = leaf.child
	}
	if leaf.exec == nil {
		return 0, res.r.errf(msgUnknownCommand)
	}
	defer func() {
		if p := recover(); p != nil {
			d.logger().Error("command panicked", slog.String("command", line), slog.Any("panic", p))
			n, err = 0, &api.CommandError{Message: api.Text(msgUnexpected)}
		}
	}()
	n, err = leaf.exec(leaf)
	if err != nil {
		var ce *api.CommandError
		var se *SyntaxError
		if !errors.As(err, &ce) && !errors.As(err, &se) {
			d.logger().Error("command failed", slog.String("command", line), slog.Any("err", err))
		}
	}
	return n, err
}

func (d *Dispatcher) logger() *slog.Logger {
	if d.server != nil {
		if l := d.server.Logger(); l != nil {
			return l
		}
	}
	return slog.Default()
}

// ErrorComponent renders a dispatch error the way vanilla shows it: a red
// message and, for syntax errors, a gray context line ending in "<--[HERE]".
func ErrorComponent(err error, line string) api.Component {
	var se *SyntaxError
	if errors.As(err, &se) {
		msg := api.Colored(se.Message, "red")
		if se.Cursor < 0 {
			return msg
		}
		c := min(se.Cursor, len(se.Input))
		ctx := api.Component{Color: "gray", Click: &api.ClickEvent{Action: api.ClickSuggestCommand, Value: "/" + se.Input}}
		if c > 10 {
			ctx = ctx.Append(api.Text("..."))
		}
		ctx = ctx.Append(api.Text(se.Input[max(0, c-10):c]))
		if c < len(se.Input) {
			rest := api.Colored(se.Input[c:], "red")
			u := true
			rest.Underline = &u
			ctx = ctx.Append(rest)
		}
		ctx = ctx.Append(api.Colored("<--[HERE]", "red").WithItalic(true))
		return msg.Append(api.Text("\n"), ctx)
	}
	var ce *api.CommandError
	if errors.As(err, &ce) {
		m := ce.Message
		if m.Color == "" {
			m.Color = "red"
		}
		return m
	}
	return api.Colored(msgUnexpected, "red")
}

func fail(format string, args ...any) error {
	return &api.CommandError{Message: api.Text(fmt.Sprintf(format, args...))}
}
