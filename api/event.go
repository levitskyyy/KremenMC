package api

// Priority orders handlers. Lower runs first; Monitor runs last and must not
// modify the event. Values match Bukkit's EventPriority ordinal.
type Priority uint8

const (
	Lowest Priority = iota
	Low
	Normal
	High
	Highest
	Monitor
)

func (p Priority) String() string {
	switch p {
	case Lowest:
		return "LOWEST"
	case Low:
		return "LOW"
	case Normal:
		return "NORMAL"
	case High:
		return "HIGH"
	case Highest:
		return "HIGHEST"
	case Monitor:
		return "MONITOR"
	}
	return "UNKNOWN"
}

func ParsePriority(s string) (Priority, bool) {
	for p := Lowest; p <= Monitor; p++ {
		if p.String() == s {
			return p, true
		}
	}
	return 0, false
}

// EventKind identifies an event type. Built-in kinds are stable and shared
// with the WASM ABI; custom kinds are allocated at runtime above KindCustom.
type EventKind uint16

const (
	KindInvalid EventKind = iota
	KindPlayerPreLogin
	KindPlayerLogin
	KindPlayerJoin
	KindPlayerQuit
	KindPlayerChat
	KindPlayerCommand
	KindPlayerMove
	KindPlayerTeleport
	KindBlockBreak
	KindBlockPlace
	KindPlayerInteract
	KindPlayerInteractEntity
	KindEntityDamage
	KindEntityDeath
	KindEntitySpawn
	KindWorldLoad
	KindWorldUnload
	KindChunkLoad
	KindChunkUnload
	KindServerTickStart
	KindServerTickEnd
	KindPluginEnable
	KindPluginDisable
	KindPlayerRespawn
	KindInventoryClick
	KindInventoryClose
	KindServerCommand
	KindPlayerKick
	KindPlayerChangeWorld
	KindPluginMessage

	KindCustom EventKind = 1024
)

var kindNames = [...]string{
	KindInvalid:              "invalid",
	KindPlayerPreLogin:       "player_pre_login",
	KindPlayerLogin:          "player_login",
	KindPlayerJoin:           "player_join",
	KindPlayerQuit:           "player_quit",
	KindPlayerChat:           "player_chat",
	KindPlayerCommand:        "player_command",
	KindPlayerMove:           "player_move",
	KindPlayerTeleport:       "player_teleport",
	KindBlockBreak:           "block_break",
	KindBlockPlace:           "block_place",
	KindPlayerInteract:       "player_interact",
	KindPlayerInteractEntity: "player_interact_entity",
	KindEntityDamage:         "entity_damage",
	KindEntityDeath:          "entity_death",
	KindEntitySpawn:          "entity_spawn",
	KindWorldLoad:            "world_load",
	KindWorldUnload:          "world_unload",
	KindChunkLoad:            "chunk_load",
	KindChunkUnload:          "chunk_unload",
	KindServerTickStart:      "server_tick_start",
	KindServerTickEnd:        "server_tick_end",
	KindPluginEnable:         "plugin_enable",
	KindPluginDisable:        "plugin_disable",
	KindPlayerRespawn:        "player_respawn",
	KindInventoryClick:       "inventory_click",
	KindInventoryClose:       "inventory_close",
	KindServerCommand:        "server_command",
	KindPlayerKick:           "player_kick",
	KindPlayerChangeWorld:    "player_change_world",
	KindPluginMessage:        "plugin_message",
}

// BuiltinKinds is one past the last built-in kind.
const BuiltinKinds = KindPluginMessage + 1

func (k EventKind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "custom"
}

// Event is implemented by every event. Async events are fired off the tick
// goroutines and their handlers may block (e.g. database lookups during
// pre-login); sync events are fired on the owning region or global tick and
// handlers must not block.
type Event interface {
	Kind() EventKind
	Async() bool
}

type Cancellable interface {
	Event
	Cancelled() bool
	SetCancelled(bool)
}

// Cancel is embedded by cancellable events.
type Cancel struct{ cancelled bool }

func (c *Cancel) Cancelled() bool     { return c.cancelled }
func (c *Cancel) SetCancelled(v bool) { c.cancelled = v }

type syncEvent struct{}

func (syncEvent) Async() bool { return false }

type asyncEvent struct{}

func (asyncEvent) Async() bool { return true }

type LoginResult uint8

const (
	LoginAllowed LoginResult = iota
	LoginKickFull
	LoginKickBanned
	LoginKickWhitelist
	LoginKickOther
)

// PlayerPreLoginEvent fires asynchronously after authentication, before the
// player entity exists. Setting Result other than LoginAllowed denies login.
type PlayerPreLoginEvent struct {
	asyncEvent
	Name    string
	UUID    UUID
	Address string
	Result  LoginResult
	Message Component
}

func (*PlayerPreLoginEvent) Kind() EventKind { return KindPlayerPreLogin }

func (e *PlayerPreLoginEvent) Deny(r LoginResult, msg Component) { e.Result, e.Message = r, msg }

// PlayerLoginEvent fires synchronously when the player entity is created but
// before it is added to a world.
type PlayerLoginEvent struct {
	syncEvent
	Player   Player
	Hostname string
	Result   LoginResult
	Message  Component
}

func (*PlayerLoginEvent) Kind() EventKind { return KindPlayerLogin }

func (e *PlayerLoginEvent) Deny(r LoginResult, msg Component) { e.Result, e.Message = r, msg }

type PlayerJoinEvent struct {
	syncEvent
	Player Player
	// Message is broadcast after dispatch; nil suppresses it.
	Message *Component
}

func (*PlayerJoinEvent) Kind() EventKind { return KindPlayerJoin }

type QuitReason uint8

const (
	QuitDisconnected QuitReason = iota
	QuitKicked
	QuitTimedOut
	QuitErroneous
)

type PlayerQuitEvent struct {
	syncEvent
	Player  Player
	Reason  QuitReason
	Message *Component
}

func (*PlayerQuitEvent) Kind() EventKind { return KindPlayerQuit }

type PlayerKickEvent struct {
	Cancel
	syncEvent
	Player Player
	Reason Component
	Leave  *Component
}

func (*PlayerKickEvent) Kind() EventKind { return KindPlayerKick }

// PlayerChatEvent fires asynchronously for chat messages. Handlers may edit
// Message and Recipients, or set Renderer to control per-viewer formatting.
type PlayerChatEvent struct {
	Cancel
	asyncEvent
	Player     Player
	Message    string
	Recipients []Player
	// Renderer formats the message for one viewer; nil uses the vanilla format.
	Renderer func(source Player, displayName, message Component, viewer Audience) Component
}

func (*PlayerChatEvent) Kind() EventKind { return KindPlayerChat }

// PlayerCommandEvent fires before a player command is parsed. Line has no
// leading slash and may be rewritten.
type PlayerCommandEvent struct {
	Cancel
	syncEvent
	Player Player
	Line   string
}

func (*PlayerCommandEvent) Kind() EventKind { return KindPlayerCommand }

type ServerCommandEvent struct {
	Cancel
	syncEvent
	Sender CommandSender
	Line   string
}

func (*ServerCommandEvent) Kind() EventKind { return KindServerCommand }

// PlayerMoveEvent fires on the owning region when a player's position or
// rotation changes. Changing To redirects the player; cancelling snaps back
// to From.
type PlayerMoveEvent struct {
	Cancel
	syncEvent
	Player Player
	From   Location
	To     Location
}

func (*PlayerMoveEvent) Kind() EventKind { return KindPlayerMove }

type PlayerTeleportEvent struct {
	Cancel
	syncEvent
	Player Player
	From   Location
	To     Location
	Cause  TeleportCause
}

func (*PlayerTeleportEvent) Kind() EventKind { return KindPlayerTeleport }

type PlayerChangeWorldEvent struct {
	syncEvent
	Player Player
	From   World
}

func (*PlayerChangeWorldEvent) Kind() EventKind { return KindPlayerChangeWorld }

type PlayerRespawnEvent struct {
	syncEvent
	Player   Player
	Location Location
	BedSpawn bool
}

func (*PlayerRespawnEvent) Kind() EventKind { return KindPlayerRespawn }

type BlockBreakEvent struct {
	Cancel
	syncEvent
	Player    Player
	Block     Block
	State     BlockState // state before breaking
	DropItems bool
	Exp       int
}

func (*BlockBreakEvent) Kind() EventKind { return KindBlockBreak }

type BlockPlaceEvent struct {
	Cancel
	syncEvent
	Player   Player
	Block    Block
	Placed   BlockState
	Replaced BlockState
	Against  BlockPos
	Hand     Hand
	Item     ItemStack
	CanBuild bool
}

func (*BlockPlaceEvent) Kind() EventKind { return KindBlockPlace }

type InteractAction uint8

const (
	LeftClickAir InteractAction = iota
	LeftClickBlock
	RightClickAir
	RightClickBlock
	Physical
)

type PlayerInteractEvent struct {
	Cancel
	syncEvent
	Player Player
	Action InteractAction
	Block  Block // nil for air actions
	Face   BlockFace
	Hand   Hand
	Item   ItemStack
	Cursor Vec3 // hit position within the block
}

func (*PlayerInteractEvent) Kind() EventKind { return KindPlayerInteract }

type PlayerInteractEntityEvent struct {
	Cancel
	syncEvent
	Player Player
	Target Entity
	Hand   Hand
	Attack bool
	At     Vec3 // interaction point relative to the target
}

func (*PlayerInteractEntityEvent) Kind() EventKind { return KindPlayerInteractEntity }

type DamageCause uint8

const (
	DamageCustom DamageCause = iota
	DamageEntityAttack
	DamageProjectile
	DamageFall
	DamageFire
	DamageFireTick
	DamageLava
	DamageDrowning
	DamageSuffocation
	DamageVoid
	DamageStarvation
	DamagePoison
	DamageMagic
	DamageWither
	DamageExplosion
	DamageContact
	DamageLightning
	DamageFreeze
	DamageKill
	DamageThorns
	DamageCramming
	DamageFlyIntoWall
	DamageHotFloor
	DamageSonicBoom
	DamageWorldBorder
)

type EntityDamageEvent struct {
	Cancel
	syncEvent
	Entity Entity
	Source DamageSource
	Damage float64
}

func (*EntityDamageEvent) Kind() EventKind { return KindEntityDamage }

type EntityDeathEvent struct {
	syncEvent
	Entity Entity
	Source DamageSource
	Drops  []ItemStack
	Exp    int
	// DeathMessage is set for players; nil suppresses the broadcast.
	DeathMessage  *Component
	KeepInventory bool
}

func (*EntityDeathEvent) Kind() EventKind { return KindEntityDeath }

type SpawnReason uint8

const (
	SpawnNatural SpawnReason = iota
	SpawnChunkGen
	SpawnSpawner
	SpawnEgg
	SpawnCommand
	SpawnPlugin
	SpawnBreeding
	SpawnDispense
	SpawnItemDrop
	SpawnProjectile
	SpawnDefault
)

type EntitySpawnEvent struct {
	Cancel
	syncEvent
	Entity   Entity
	Location Location
	Reason   SpawnReason
}

func (*EntitySpawnEvent) Kind() EventKind { return KindEntitySpawn }

type WorldLoadEvent struct {
	syncEvent
	World World
}

func (*WorldLoadEvent) Kind() EventKind { return KindWorldLoad }

type WorldUnloadEvent struct {
	Cancel
	syncEvent
	World World
}

func (*WorldUnloadEvent) Kind() EventKind { return KindWorldUnload }

type ChunkLoadEvent struct {
	syncEvent
	World World
	X, Z  int
	New   bool // freshly generated
}

func (*ChunkLoadEvent) Kind() EventKind { return KindChunkLoad }

type ChunkUnloadEvent struct {
	syncEvent
	World World
	X, Z  int
	Save  bool
}

func (*ChunkUnloadEvent) Kind() EventKind { return KindChunkUnload }

type ServerTickStartEvent struct {
	syncEvent
	Tick int64
}

func (*ServerTickStartEvent) Kind() EventKind { return KindServerTickStart }

type ServerTickEndEvent struct {
	syncEvent
	Tick int64
	// DurationNanos is the time spent ticking; RemainingNanos is the slack
	// before the next tick (negative when overloaded).
	DurationNanos  int64
	RemainingNanos int64
}

func (*ServerTickEndEvent) Kind() EventKind { return KindServerTickEnd }

type PluginEnableEvent struct {
	syncEvent
	Plugin PluginInfo
}

func (*PluginEnableEvent) Kind() EventKind { return KindPluginEnable }

type PluginDisableEvent struct {
	syncEvent
	Plugin PluginInfo
}

func (*PluginDisableEvent) Kind() EventKind { return KindPluginDisable }

type ClickType uint8

const (
	ClickLeft ClickType = iota
	ClickRight
	ClickShiftLeft
	ClickShiftRight
	ClickMiddle
	ClickNumberKey
	ClickDouble
	ClickDrop
	ClickControlDrop
	ClickSwapOffhand
	ClickUnknown
)

type InventoryClickEvent struct {
	Cancel
	syncEvent
	Player    Player
	Inventory Inventory
	Slot      int // raw slot in the open view; -999 outside the window
	Click     ClickType
	Button    int
	Current   ItemStack
	Cursor    ItemStack
}

func (*InventoryClickEvent) Kind() EventKind { return KindInventoryClick }

type InventoryCloseEvent struct {
	syncEvent
	Player    Player
	Inventory Inventory
}

func (*InventoryCloseEvent) Kind() EventKind { return KindInventoryClose }

// PluginMessageEvent carries a custom payload packet from a client or proxy
// (e.g. "bungeecord:main", "velocity:player_info").
type PluginMessageEvent struct {
	syncEvent
	Player  Player
	Channel Key
	Data    []byte
}

func (*PluginMessageEvent) Kind() EventKind { return KindPluginMessage }
