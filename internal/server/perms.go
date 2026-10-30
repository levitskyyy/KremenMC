package server

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
)

type permissions struct {
	mu       sync.RWMutex
	perms    map[string]api.Permission
	provider api.PermissionProvider
	owner    string
}

func newPermissions() *permissions { return &permissions{perms: map[string]api.Permission{}} }

func (p *permissions) Register(perm api.Permission) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.perms[perm.Name]; ok {
		return fmt.Errorf("permission %s already registered", perm.Name)
	}
	p.perms[perm.Name] = perm
	return nil
}

func (p *permissions) Unregister(name string) {
	p.mu.Lock()
	delete(p.perms, name)
	p.mu.Unlock()
}

func (p *permissions) Get(name string) (api.Permission, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.perms[name]
	return v, ok
}

func (p *permissions) All() []api.Permission {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.Collect(maps.Values(p.perms))
}

func (p *permissions) SetProvider(owner string, pr api.PermissionProvider) {
	p.mu.Lock()
	p.owner, p.provider = owner, pr
	p.mu.Unlock()
}

func (p *permissions) Provider() api.PermissionProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.provider
}

type attachment struct {
	owner  string
	parent *permissible
	mu     sync.Mutex
	perms  map[string]bool
}

func (a *attachment) Owner() string { return a.owner }

func (a *attachment) Set(name string, v bool) {
	a.mu.Lock()
	a.perms[name] = v
	a.mu.Unlock()
}

func (a *attachment) Unset(name string) {
	a.mu.Lock()
	delete(a.perms, name)
	a.mu.Unlock()
}

func (a *attachment) Permissions() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return maps.Clone(a.perms)
}

func (a *attachment) Remove() { a.parent.remove(a) }

// permissible resolves permissions for one subject: provider, then
// attachments (latest wins, children expanded), then the declared default.
type permissible struct {
	perms   *permissions
	subject api.UUID
	op      func() bool
	setOp   func(bool)
	mu      sync.Mutex
	atts    []*attachment
}

func (p *permissible) remove(a *attachment) {
	p.mu.Lock()
	p.atts = slices.DeleteFunc(p.atts, func(x *attachment) bool { return x == a })
	p.mu.Unlock()
}

func (p *permissible) IsOp() bool   { return p.op() }
func (p *permissible) SetOp(v bool) { p.setOp(v) }
func (p *permissible) AddAttachment(owner string) api.PermissionAttachment {
	a := &attachment{owner: owner, parent: p, perms: map[string]bool{}}
	p.mu.Lock()
	p.atts = append(p.atts, a)
	p.mu.Unlock()
	return a
}

func (p *permissible) attached(name string) (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.atts) - 1; i >= 0; i-- {
		a := p.atts[i]
		a.mu.Lock()
		v, ok := a.perms[name]
		if !ok {
			for parent, pv := range a.perms {
				if perm, found := p.perms.Get(parent); found {
					if cv, has := perm.Children[name]; has {
						v, ok = cv == pv, true
						break
					}
				}
			}
		}
		a.mu.Unlock()
		if ok {
			return v, true
		}
	}
	return false, false
}

func (p *permissible) IsPermissionSet(name string) bool {
	if pr := p.perms.Provider(); pr != nil {
		if _, ok := pr.Check(p.subject, name); ok {
			return true
		}
	}
	_, ok := p.attached(name)
	return ok
}

func (p *permissible) HasPermission(name string) bool {
	return p.hasPermissionOr(name, api.DefaultOp)
}

// hasPermissionOr uses def for permissions the server does not know.
func (p *permissible) hasPermissionOr(name string, def api.PermissionDefault) bool {
	if name == "" {
		return true
	}
	if pr := p.perms.Provider(); pr != nil && !p.subject.IsZero() {
		if v, ok := pr.Check(p.subject, name); ok {
			return v
		}
	}
	if v, ok := p.attached(name); ok {
		return v
	}
	if perm, ok := p.perms.Get(name); ok {
		return perm.Default.Value(p.op())
	}
	return def.Value(p.op())
}

func (p *permissible) EffectivePermissions() map[string]bool {
	out := map[string]bool{}
	for _, perm := range p.perms.All() {
		out[perm.Name] = perm.Default.Value(p.op())
	}
	p.mu.Lock()
	for _, a := range p.atts {
		maps.Copy(out, a.Permissions())
	}
	p.mu.Unlock()
	return out
}
