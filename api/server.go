package api

import (
	"log/slog"
	"time"
)

// Server is the root handle given to plugins. All methods are safe from any
// goroutine.
type Server interface {
	Name() string
	Version() string // engine version
	GameVersion() string
	Protocol() int32

	Players() []Player
	Player(id UUID) (Player, bool)
	PlayerByName(name string) (Player, bool)
	MaxPlayers() int

	Worlds() []World
	World(name string) (World, bool)
	DefaultWorld() World
	CreateWorld(opts WorldOptions) (World, error)
	UnloadWorld(name string, save bool) bool

	Broadcast(msg Component)
	// BroadcastPermission sends to every player holding perm and the console.
	BroadcastPermission(msg Component, perm string)
	Console() CommandSender

	Scheduler() Scheduler
	Commands() CommandRegistry
	Permissions() Permissions
	Blocks() BlockRegistry
	Items() ItemRegistry
	Plugins() PluginRegistry

	// CreateInventory builds a custom inventory not backed by a block.
	CreateInventory(kind InventoryType, title Component, size int) Inventory
	OfflinePlayer(id UUID) (OfflinePlayer, bool)
	ProfileBans() BanList
	IPBans() BanList
	Whitelist() Whitelist

	Logger() *slog.Logger
	CurrentTick() int64
	TPS() float64
	StartTime() time.Time
	Shutdown(reason Component)
}

type OfflinePlayer interface {
	UUID() UUID
	Name() string
	IsOnline() bool
	IsOp() bool
	SetOp(bool)
	IsBanned() bool
	IsWhitelisted() bool
	SetWhitelisted(bool)
	LastSeen() time.Time
	PersistentData() DataContainer
}

// PluginInfo describes a loaded plugin regardless of its runtime (wasm or java).
type PluginInfo struct {
	ID          string
	Name        string
	Version     string
	Description string
	Authors     []string
	Runtime     string // "wasm", "java", "go"
	Depends     []string
	SoftDepends []string
}

type PluginRegistry interface {
	Plugins() []PluginInfo
	Plugin(id string) (PluginInfo, bool)
	IsEnabled(id string) bool
	// DataDir is the per-plugin writable directory (plugins/<id>/).
	DataDir(id string) string
}

// WorldOptions mirrors Bukkit's WorldCreator.
type WorldOptions struct {
	Name               string
	Dimension          Key // dimension type, e.g. "minecraft:overworld"
	Generator          string
	GeneratorSettings  string
	Seed               int64
	RandomSeed         bool
	GenerateStructures bool
	Hardcore           bool
}

type BanEntry struct {
	Target  string // UUID string for profile bans, address for IP bans
	Name    string
	Reason  string
	Source  string
	Created time.Time
	Expires time.Time // zero means permanent
}

type BanList interface {
	Ban(entry BanEntry)
	Pardon(target string) bool
	Get(target string) (BanEntry, bool)
	Entries() []BanEntry
}

type Whitelist interface {
	Enabled() bool
	SetEnabled(bool)
	Add(profile GameProfile)
	Remove(id UUID) bool
	Contains(id UUID) bool
	Entries() []GameProfile
}
