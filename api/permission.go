package api

type PermissionDefault uint8

const (
	DefaultOp PermissionDefault = iota
	DefaultTrue
	DefaultFalse
	DefaultNotOp
)

func ParsePermissionDefault(s string) (PermissionDefault, bool) {
	switch s {
	case "op", "isop", "operator", "isoperator", "admin", "isadmin":
		return DefaultOp, true
	case "true":
		return DefaultTrue, true
	case "false":
		return DefaultFalse, true
	case "!op", "notop", "!operator", "notoperator", "!admin", "notadmin":
		return DefaultNotOp, true
	}
	return 0, false
}

func (d PermissionDefault) Value(op bool) bool {
	switch d {
	case DefaultTrue:
		return true
	case DefaultOp:
		return op
	case DefaultNotOp:
		return !op
	}
	return false
}

type Permission struct {
	Name        string
	Description string
	Default     PermissionDefault
	Children    map[string]bool
}

// Permissible is implemented by players and other command senders.
type Permissible interface {
	HasPermission(name string) bool
	// IsPermissionSet reports whether the permission is explicitly set rather
	// than falling back to its default.
	IsPermissionSet(name string) bool
	IsOp() bool
	SetOp(bool)
	AddAttachment(owner string) PermissionAttachment
	EffectivePermissions() map[string]bool
}

type PermissionAttachment interface {
	Owner() string
	Set(name string, value bool)
	Unset(name string)
	Permissions() map[string]bool
	Remove()
}

// Permissions is the registry of declared permissions. A permission backend
// plugin (LuckPerms via the bridge, or a wasm plugin) installs a Provider.
type Permissions interface {
	Register(p Permission) error
	Unregister(name string)
	Get(name string) (Permission, bool)
	All() []Permission
	SetProvider(owner string, p PermissionProvider)
	Provider() PermissionProvider
}

// PermissionProvider answers permission checks. Returning ok=false defers to
// attachments, then the permission default.
type PermissionProvider interface {
	Check(subject UUID, name string) (value, ok bool)
}
