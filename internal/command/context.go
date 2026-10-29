package command

import (
	"slices"

	"github.com/levitskyyy/KremenMC/api"
)

type parsedNode struct {
	node       *api.CommandNode
	start, end int
}

type argValue struct {
	name       string
	value      any
	start, end int
}

// cmdContext is the brigadier CommandContextBuilder equivalent and also the
// api.CommandContext handed to executors and suggestion providers.
type cmdContext struct {
	d      *Dispatcher
	sender api.CommandSender
	input  string
	label  string
	root   *api.CommandNode
	top    *api.CommandNode
	start  int
	nodes  []parsedNode
	args   []argValue
	exec   api.CommandExecutor
	child  *cmdContext
}

func (c *cmdContext) copy() *cmdContext {
	cp := *c
	cp.nodes = slices.Clone(c.nodes)
	cp.args = slices.Clone(c.args)
	return &cp
}

func (c *cmdContext) rangeEnd() int {
	if len(c.nodes) == 0 {
		return c.start
	}
	return c.nodes[len(c.nodes)-1].end
}

func (c *cmdContext) lookup(name string) (any, bool) {
	for i := len(c.args) - 1; i >= 0; i-- {
		if c.args[i].name == name {
			return c.args[i].value, true
		}
	}
	return nil, false
}

func (c *cmdContext) Sender() api.CommandSender { return c.sender }
func (c *cmdContext) Input() string             { return c.input }
func (c *cmdContext) Label() string             { return c.label }

func (c *cmdContext) Has(name string) bool {
	_, ok := c.lookup(name)
	return ok
}

// Arg returns the argument converted to a public type: selectors resolve to
// []api.Entity, profiles to []api.GameProfile, coordinates to api.Vec3 or
// api.BlockPos, messages to their expanded string.
func (c *cmdContext) Arg(name string) (any, bool) {
	v, ok := c.lookup(name)
	if !ok {
		return nil, false
	}
	switch v := v.(type) {
	case *selector:
		es, _ := c.resolveEntities(v)
		return es, true
	case *profileArg:
		ps, _ := c.resolveProfiles(v)
		return ps, true
	case *coords:
		switch v.kind {
		case coordBlock:
			return v.blockPos(c.origin()), true
		case coordColumn:
			return v.blockPos(c.origin()), true
		case coordVec2, coordRotation, coordAngle:
			return v.raw(c.origin()), true
		}
		return v.vec3(c.origin()), true
	case *message:
		return c.expandMessage(v), true
	}
	return v, true
}

func (c *cmdContext) String(name string) string {
	v, _ := c.lookup(name)
	switch v := v.(type) {
	case string:
		return v
	case api.Key:
		return string(v)
	case *message:
		return c.expandMessage(v)
	case api.UUID:
		return v.String()
	case api.GameMode:
		return v.String()
	case api.BlockState:
		return v.String()
	}
	return ""
}

func (c *cmdContext) Int(name string) int64 {
	v, _ := c.lookup(name)
	switch v := v.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func (c *cmdContext) Float(name string) float64 {
	v, _ := c.lookup(name)
	switch v := v.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case *coords:
		if v.kind == coordAngle {
			return v.raw(c.origin()).X
		}
	}
	return 0
}

func (c *cmdContext) Bool(name string) bool {
	v, _ := c.lookup(name)
	b, _ := v.(bool)
	return b
}

func (c *cmdContext) Players(name string) []api.Player {
	v, _ := c.lookup(name)
	switch v := v.(type) {
	case *selector:
		es, _ := c.resolveEntities(v)
		out := make([]api.Player, 0, len(es))
		for _, e := range es {
			if p, ok := e.(api.Player); ok {
				out = append(out, p)
			}
		}
		return out
	case *profileArg:
		ps, _ := c.resolveProfiles(v)
		var out []api.Player
		for _, p := range ps {
			if pl, ok := c.d.server.Player(p.UUID); ok {
				out = append(out, pl)
			}
		}
		return out
	}
	return nil
}

func (c *cmdContext) Entities(name string) []api.Entity {
	v, _ := c.lookup(name)
	if s, ok := v.(*selector); ok {
		es, _ := c.resolveEntities(s)
		return es
	}
	return nil
}

func (c *cmdContext) BlockPos(name string) api.BlockPos {
	v, _ := c.lookup(name)
	if co, ok := v.(*coords); ok {
		return co.blockPos(c.origin())
	}
	return api.BlockPos{}
}

func (c *cmdContext) Vec3(name string) api.Vec3 {
	v, _ := c.lookup(name)
	if co, ok := v.(*coords); ok {
		switch co.kind {
		case coordVec3:
			return co.vec3(c.origin())
		case coordBlock, coordColumn:
			p := co.blockPos(c.origin())
			return api.Vec3{X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)}
		}
		return co.raw(c.origin())
	}
	return api.Vec3{}
}

func (c *cmdContext) BlockState(name string) api.BlockState {
	v, _ := c.lookup(name)
	s, _ := v.(api.BlockState)
	return s
}

func (c *cmdContext) Item(name string) api.ItemStack {
	v, _ := c.lookup(name)
	s, _ := v.(api.ItemStack)
	return s
}

func (c *cmdContext) Component(name string) api.Component {
	v, _ := c.lookup(name)
	switch v := v.(type) {
	case api.Component:
		return v
	case *message:
		return api.Text(c.expandMessage(v))
	case string:
		return api.Text(v)
	}
	return api.Component{}
}

// origin is the execution position: the sender's location if it has one,
// otherwise the default world spawn.
func (c *cmdContext) origin() api.Location {
	return senderOrigin(c.d.server, c.sender)
}

type locator interface{ Location() api.Location }

func senderOrigin(srv api.Server, s api.CommandSender) api.Location {
	if l, ok := s.(locator); ok {
		return l.Location()
	}
	if srv != nil {
		if w := srv.DefaultWorld(); w != nil {
			l := w.Spawn()
			if l.World == "" {
				l.World = w.Name()
			}
			return l
		}
	}
	return api.Location{}
}

func (c *cmdContext) world() api.World {
	return senderWorld(c.d.server, c.sender)
}

func senderWorld(srv api.Server, s api.CommandSender) api.World {
	if e, ok := s.(api.Entity); ok {
		if w := e.World(); w != nil {
			return w
		}
	}
	if srv == nil {
		return nil
	}
	if l, ok := s.(locator); ok {
		if w, ok := srv.World(l.Location().World); ok {
			return w
		}
	}
	return srv.DefaultWorld()
}
