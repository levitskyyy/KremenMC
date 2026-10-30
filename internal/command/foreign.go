package command

import (
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

// Foreign runs and completes a command whose syntax another dispatcher owns,
// such as a Java plugin's brigadier tree. The registered node tree is only
// advertised to clients; input after the label is handed over unparsed.
type Foreign struct {
	// Run executes line (no leading slash) and reports its own errors.
	Run func(sender api.CommandSender, line string) (int, error)
	// Suggest completes line (no leading slash); Start indexes into line.
	Suggest func(sender api.CommandSender, line string) Completion
}

// RegisterForeign is Register for a command handled by f.
func (d *Dispatcher) RegisterForeign(owner, ns string, root *api.CommandNode, aliases []string, f *Foreign) error {
	if ns == "" {
		ns = owner
	}
	return d.registerWith(owner, ns, root, aliases, f)
}

func (d *Dispatcher) foreignFor(line string) (*command, bool) {
	label := line
	if i := strings.IndexByte(label, ' '); i >= 0 {
		label = label[:i]
	}
	c, ok := d.tree.Load().labels[strings.ToLower(label)]
	if !ok || c.foreign == nil {
		return nil, false
	}
	return c, true
}

// Select resolves an entity selector, player name or UUID as sender would
// in a command argument.
func (d *Dispatcher) Select(sender api.CommandSender, input string) ([]api.Entity, error) {
	r := reader{s: input}
	s, err := parseSelector(&r, d.selectorsAllowed(sender))
	if err != nil {
		return nil, err
	}
	ctx := &cmdContext{d: d, sender: sender, input: input}
	return ctx.resolveEntities(s)
}
