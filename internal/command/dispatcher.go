// Package command implements a brigadier-compatible command dispatcher, the
// clientbound command tree encoder and the built-in commands.
package command

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/api"
)

// SelectorPermission gates @-selectors in arguments and messages, mirroring
// the vanilla level-2 requirement.
const SelectorPermission = "minecraft.command.selector"

type command struct {
	owner     string
	namespace string
	name      string
	aliases   []string
	node      *api.CommandNode
}

type tree struct {
	root   *api.CommandNode
	labels map[string]*command
}

// Dispatcher is the server's command registry. Registration is copy-on-write,
// so Dispatch, Complete and EncodeTree never block on registration and are
// safe from any goroutine.
type Dispatcher struct {
	server api.Server

	mu   sync.Mutex
	cmds []*command
	tree atomic.Pointer[tree]

	resend        atomic.Pointer[func(api.Player)]
	profileLookup atomic.Pointer[func(name string) (api.GameProfile, bool)]

	blockNames lazyKeys
	itemNames  lazyKeys
}

func New(server api.Server) *Dispatcher {
	d := &Dispatcher{server: server}
	d.tree.Store(&tree{root: &api.CommandNode{}, labels: map[string]*command{}})
	return d
}

// SetResendHook installs the function that sends a fresh command tree to a
// player. Register, Unregister and Resend call it.
func (d *Dispatcher) SetResendHook(fn func(api.Player)) {
	if fn == nil {
		d.resend.Store(nil)
		return
	}
	d.resend.Store(&fn)
}

// SetProfileLookup installs a resolver for offline player names used by
// game_profile arguments (usercache, Mojang API). Without it only online
// players, whitelist and ban entries resolve.
func (d *Dispatcher) SetProfileLookup(fn func(name string) (api.GameProfile, bool)) {
	if fn == nil {
		d.profileLookup.Store(nil)
		return
	}
	d.profileLookup.Store(&fn)
}

var errBadName = errors.New("command: invalid command name")

func validName(s string) bool {
	return s != "" && !strings.ContainsAny(s, " :/") && strings.ToLower(s) == s
}

func (d *Dispatcher) Register(owner string, root *api.CommandNode, aliases ...string) error {
	ns := owner
	if ns == "" {
		ns = "minecraft"
	}
	return d.register(owner, ns, root, aliases)
}

func (d *Dispatcher) register(owner, ns string, root *api.CommandNode, aliases []string) error {
	if root == nil || root.Kind != api.NodeLiteral {
		return fmt.Errorf("command: root of %q must be a literal", owner)
	}
	name := strings.ToLower(root.Name)
	if !validName(name) {
		return fmt.Errorf("%w: %q", errBadName, root.Name)
	}
	if !validName(strings.ToLower(ns)) {
		return fmt.Errorf("%w: namespace %q", errBadName, ns)
	}
	if name != root.Name {
		cp := *root
		cp.Name = name
		root = &cp
	}
	als := make([]string, 0, len(aliases))
	for _, a := range aliases {
		a = strings.ToLower(a)
		if !validName(a) {
			return fmt.Errorf("%w: alias %q", errBadName, a)
		}
		if a != name && !slices.Contains(als, a) {
			als = append(als, a)
		}
	}
	d.mu.Lock()
	c := &command{owner: owner, namespace: strings.ToLower(ns), name: name, aliases: als, node: root}
	if i := slices.IndexFunc(d.cmds, func(c *command) bool { return c.owner == owner && c.name == name }); i >= 0 {
		d.cmds[i] = c
	} else {
		d.cmds = append(d.cmds, c)
	}
	d.rebuildLocked()
	d.mu.Unlock()
	d.resendAll()
	return nil
}

func (d *Dispatcher) Unregister(owner, name string) bool {
	name = strings.ToLower(strings.TrimPrefix(name, "/"))
	d.mu.Lock()
	n := len(d.cmds)
	d.cmds = slices.DeleteFunc(d.cmds, func(c *command) bool {
		return c.owner == owner && (c.name == name || c.namespace+":"+c.name == name)
	})
	removed := len(d.cmds) != n
	if removed {
		d.rebuildLocked()
	}
	d.mu.Unlock()
	if removed {
		d.resendAll()
	}
	return removed
}

func (d *Dispatcher) UnregisterAll(owner string) {
	d.mu.Lock()
	n := len(d.cmds)
	d.cmds = slices.DeleteFunc(d.cmds, func(c *command) bool { return c.owner == owner })
	removed := len(d.cmds) != n
	if removed {
		d.rebuildLocked()
	}
	d.mu.Unlock()
	if removed {
		d.resendAll()
	}
}

// rebuildLocked assigns labels: namespaced names first, then plain names in
// registration order, then aliases, each only if still free.
func (d *Dispatcher) rebuildLocked() {
	labels := make(map[string]*command, len(d.cmds)*3)
	nodes := make(map[string]*api.CommandNode, len(d.cmds)*3)
	claim := func(label string, c *command) {
		if _, taken := labels[label]; taken {
			return
		}
		labels[label] = c
		if label == c.node.Name {
			nodes[label] = c.node
			return
		}
		cp := *c.node
		cp.Name = label
		nodes[label] = &cp
	}
	for _, c := range d.cmds {
		claim(c.namespace+":"+c.name, c)
	}
	for _, c := range d.cmds {
		claim(c.name, c)
	}
	for _, c := range d.cmds {
		for _, a := range c.aliases {
			claim(a, c)
		}
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	root := &api.CommandNode{Children: make([]*api.CommandNode, 0, len(keys))}
	for _, k := range keys {
		root.Children = append(root.Children, nodes[k])
	}
	d.tree.Store(&tree{root: root, labels: labels})
}

func (d *Dispatcher) resendAll() {
	fn := d.resend.Load()
	if fn == nil || d.server == nil {
		return
	}
	for _, p := range d.server.Players() {
		(*fn)(p)
	}
}

func (d *Dispatcher) Resend(p api.Player) {
	if fn := d.resend.Load(); fn != nil && p != nil {
		(*fn)(p)
	}
}

func (d *Dispatcher) info(c *command, t *tree) api.CommandInfo {
	info := api.CommandInfo{
		Name:        c.name,
		Owner:       c.owner,
		Description: c.node.Description,
		Permission:  c.node.Permission,
	}
	for _, a := range c.aliases {
		if t.labels[a] == c {
			info.Aliases = append(info.Aliases, a)
		}
	}
	if u, ok := d.smartUsageOf(nil, c.node, false, false); ok {
		info.Usage = "/" + u
	}
	return info
}

func (d *Dispatcher) Commands() []api.CommandInfo {
	t := d.tree.Load()
	d.mu.Lock()
	cmds := slices.Clone(d.cmds)
	d.mu.Unlock()
	out := make([]api.CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, d.info(c, t))
	}
	slices.SortFunc(out, func(a, b api.CommandInfo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (d *Dispatcher) Lookup(name string) (api.CommandInfo, bool) {
	t := d.tree.Load()
	c, ok := t.labels[strings.ToLower(strings.TrimPrefix(name, "/"))]
	if !ok {
		return api.CommandInfo{}, false
	}
	return d.info(c, t), true
}

func (d *Dispatcher) canUse(n *api.CommandNode, s api.CommandSender) bool {
	if s == nil {
		return true
	}
	if n.Permission != "" && !s.HasPermission(n.Permission) {
		return false
	}
	return n.Requirement == nil || n.Requirement(s)
}

func (d *Dispatcher) selectorsAllowed(s api.CommandSender) bool {
	return s == nil || s.HasPermission(SelectorPermission)
}
