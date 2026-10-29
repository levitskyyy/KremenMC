package command

import (
	"slices"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

func usageText(n *api.CommandNode) string {
	if n.Kind == api.NodeLiteral {
		return n.Name
	}
	return "<" + n.Name + ">"
}

// smartUsage follows brigadier's CommandDispatcher.getSmartUsage: one usage
// line per usable child of n. A nil sender sees every node.
func (d *Dispatcher) smartUsage(sender api.CommandSender, n *api.CommandNode) []string {
	optional := n.Executor != nil
	var out []string
	for _, c := range n.Children {
		if u, ok := d.smartUsageOf(sender, c, optional, false); ok {
			out = append(out, u)
		}
	}
	return out
}

func (d *Dispatcher) redirectUsage(n *api.CommandNode) string {
	if n.Redirect == d.tree.Load().root {
		return "..."
	}
	return "-> " + usageText(n.Redirect)
}

func (d *Dispatcher) usable(sender api.CommandSender, n *api.CommandNode) []*api.CommandNode {
	var out []*api.CommandNode
	for _, c := range n.Children {
		if d.canUse(c, sender) {
			out = append(out, c)
		}
	}
	return out
}

func (d *Dispatcher) smartUsageOf(sender api.CommandSender, n *api.CommandNode, optional, deep bool) (string, bool) {
	if !d.canUse(n, sender) {
		return "", false
	}
	self := usageText(n)
	if optional {
		self = "[" + self + "]"
	}
	if deep {
		return self, true
	}
	if n.Redirect != nil {
		return self + " " + d.redirectUsage(n), true
	}
	childOptional := n.Executor != nil
	open, close := "(", ")"
	if childOptional {
		open, close = "[", "]"
	}
	kids := d.usable(sender, n)
	switch {
	case len(kids) == 1:
		if u, ok := d.smartUsageOf(sender, kids[0], childOptional, childOptional); ok {
			return self + " " + u, true
		}
	case len(kids) > 1:
		var uniq []string
		for _, c := range kids {
			if u, ok := d.smartUsageOf(sender, c, childOptional, true); ok && !slices.Contains(uniq, u) {
				uniq = append(uniq, u)
			}
		}
		if len(uniq) == 1 {
			u := uniq[0]
			if childOptional {
				u = "[" + u + "]"
			}
			return self + " " + u, true
		}
		if len(uniq) > 1 {
			parts := make([]string, len(kids))
			for i, c := range kids {
				parts[i] = usageText(c)
			}
			return self + " " + open + strings.Join(parts, "|") + close, true
		}
	}
	return self, true
}
