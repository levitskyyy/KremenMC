package kremenmc

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// Event is implemented by the pointer types in this file.
type Event interface {
	Kind() api.EventKind
	decode(r *wire.Reader)
	base() *eventBase
}

type eventBase struct {
	cancel    uint8
	cancelled bool
	muts      wire.Writer
}

func (b *eventBase) base() *eventBase { return b }

// Cancellable events embed this.
type cancellable struct{ eventBase }

func (c *cancellable) Cancelled() bool { return c.cancelled }

func (c *cancellable) SetCancelled(v bool) {
	c.cancelled = v
	if v {
		c.cancel = wire.CancelSet
	} else {
		c.cancel = wire.CancelClear
	}
}

func (b *eventBase) setMessage(c *api.Component) {
	if c == nil {
		b.muts.U8(wire.MutClearMessage)
		return
	}
	b.muts.U8(wire.MutMessage)
	b.muts.Component(*c)
}

func readOptComponent(r *wire.Reader) *api.Component {
	if !r.Bool() {
		return nil
	}
	c := r.Component()
	return &c
}

type PlayerPreLoginEvent struct {
	eventBase
	Name    string
	UUID    api.UUID
	Address string
	Result  api.LoginResult
	Message api.Component
}

func (*PlayerPreLoginEvent) Kind() api.EventKind { return api.KindPlayerPreLogin }
func (e *PlayerPreLoginEvent) decode(r *wire.Reader) {
	e.Name, e.UUID, e.Address, e.Result, e.Message = r.String(), r.UUID(), r.String(), api.LoginResult(r.U8()), r.Component()
}

func (e *PlayerPreLoginEvent) Deny(result api.LoginResult, msg api.Component) {
	e.Result, e.Message = result, msg
	e.muts.U8(wire.MutLoginResult)
	e.muts.U8(uint8(result))
	e.muts.Component(msg)
}

func (e *PlayerPreLoginEvent) Allow() { e.Deny(api.LoginAllowed, api.Component{}) }

type PlayerLoginEvent struct {
	eventBase
	Player   Player
	Hostname string
	Result   api.LoginResult
	Message  api.Component
}

func (*PlayerLoginEvent) Kind() api.EventKind { return api.KindPlayerLogin }
func (e *PlayerLoginEvent) decode(r *wire.Reader) {
	e.Player, e.Hostname, e.Result, e.Message = Player{r.Player()}, r.String(), api.LoginResult(r.U8()), r.Component()
}

func (e *PlayerLoginEvent) Deny(result api.LoginResult, msg api.Component) {
	e.Result, e.Message = result, msg
	e.muts.U8(wire.MutLoginResult)
	e.muts.U8(uint8(result))
	e.muts.Component(msg)
}

type PlayerJoinEvent struct {
	eventBase
	Player  Player
	Message *api.Component
}

func (*PlayerJoinEvent) Kind() api.EventKind { return api.KindPlayerJoin }
func (e *PlayerJoinEvent) decode(r *wire.Reader) {
	e.Player, e.Message = Player{r.Player()}, readOptComponent(r)
}

// SetMessage changes the join broadcast; nil suppresses it.
func (e *PlayerJoinEvent) SetMessage(c *api.Component) { e.Message = c; e.setMessage(c) }

type PlayerQuitEvent struct {
	eventBase
	Player  Player
	Reason  api.QuitReason
	Message *api.Component
}

func (*PlayerQuitEvent) Kind() api.EventKind { return api.KindPlayerQuit }
func (e *PlayerQuitEvent) decode(r *wire.Reader) {
	e.Player, e.Reason, e.Message = Player{r.Player()}, api.QuitReason(r.U8()), readOptComponent(r)
}
func (e *PlayerQuitEvent) SetMessage(c *api.Component) { e.Message = c; e.setMessage(c) }

type PlayerKickEvent struct {
	cancellable
	Player Player
	Reason api.Component
	Leave  *api.Component
}

func (*PlayerKickEvent) Kind() api.EventKind { return api.KindPlayerKick }
func (e *PlayerKickEvent) decode(r *wire.Reader) {
	e.Player, e.Reason, e.Leave, e.cancelled = Player{r.Player()}, r.Component(), readOptComponent(r), r.Bool()
}
func (e *PlayerKickEvent) SetLeaveMessage(c *api.Component) { e.Leave = c; e.setMessage(c) }

type PlayerChatEvent struct {
	cancellable
	Player     Player
	Message    string
	Recipients []api.UUID
}

func (*PlayerChatEvent) Kind() api.EventKind { return api.KindPlayerChat }
func (e *PlayerChatEvent) decode(r *wire.Reader) {
	e.Player, e.Message = Player{r.Player()}, r.String()
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		e.Recipients = append(e.Recipients, r.UUID())
	}
	e.cancelled = r.Bool()
}

func (e *PlayerChatEvent) SetMessage(msg string) {
	e.Message = msg
	e.muts.U8(wire.MutText)
	e.muts.String(msg)
}

func (e *PlayerChatEvent) SetRecipients(ids []api.UUID) {
	e.Recipients = ids
	e.muts.U8(wire.MutRecipients)
	e.muts.Uvarint(uint64(len(ids)))
	for _, id := range ids {
		e.muts.UUID(id)
	}
}

type PlayerCommandEvent struct {
	cancellable
	Player Player
	Line   string
}

func (*PlayerCommandEvent) Kind() api.EventKind { return api.KindPlayerCommand }
func (e *PlayerCommandEvent) decode(r *wire.Reader) {
	e.Player, e.Line, e.cancelled = Player{r.Player()}, r.String(), r.Bool()
}

func (e *PlayerCommandEvent) SetLine(line string) {
	e.Line = line
	e.muts.U8(wire.MutText)
	e.muts.String(line)
}

type ServerCommandEvent struct {
	cancellable
	Sender string
	Line   string
}

func (*ServerCommandEvent) Kind() api.EventKind { return api.KindServerCommand }
func (e *ServerCommandEvent) decode(r *wire.Reader) {
	e.Sender, e.Line, e.cancelled = r.String(), r.String(), r.Bool()
}

func (e *ServerCommandEvent) SetLine(line string) {
	e.Line = line
	e.muts.U8(wire.MutText)
	e.muts.String(line)
}

type PlayerMoveEvent struct {
	cancellable
	Player   Player
	From, To api.Location
}

func (*PlayerMoveEvent) Kind() api.EventKind { return api.KindPlayerMove }
func (e *PlayerMoveEvent) decode(r *wire.Reader) {
	e.Player, e.From, e.To, e.cancelled = Player{r.Player()}, r.Location(), r.Location(), r.Bool()
}

func (e *PlayerMoveEvent) SetTo(l api.Location) {
	e.To = l
	e.muts.U8(wire.MutTo)
	e.muts.Location(l)
}

type PlayerTeleportEvent struct {
	cancellable
	Player   Player
	From, To api.Location
	Cause    api.TeleportCause
}

func (*PlayerTeleportEvent) Kind() api.EventKind { return api.KindPlayerTeleport }
func (e *PlayerTeleportEvent) decode(r *wire.Reader) {
	e.Player, e.From, e.To, e.Cause, e.cancelled = Player{r.Player()}, r.Location(), r.Location(), api.TeleportCause(r.U8()), r.Bool()
}

func (e *PlayerTeleportEvent) SetTo(l api.Location) {
	e.To = l
	e.muts.U8(wire.MutTo)
	e.muts.Location(l)
}

type PlayerChangeWorldEvent struct {
	eventBase
	Player Player
	From   string
}

func (*PlayerChangeWorldEvent) Kind() api.EventKind { return api.KindPlayerChangeWorld }
func (e *PlayerChangeWorldEvent) decode(r *wire.Reader) {
	e.Player, e.From = Player{r.Player()}, r.String()
}

type PlayerRespawnEvent struct {
	eventBase
	Player   Player
	Location api.Location
	BedSpawn bool
}

func (*PlayerRespawnEvent) Kind() api.EventKind { return api.KindPlayerRespawn }
func (e *PlayerRespawnEvent) decode(r *wire.Reader) {
	e.Player, e.Location, e.BedSpawn = Player{r.Player()}, r.Location(), r.Bool()
}

func (e *PlayerRespawnEvent) SetLocation(l api.Location) {
	e.Location = l
	e.muts.U8(wire.MutTo)
	e.muts.Location(l)
}

type BlockBreakEvent struct {
	cancellable
	Player    Player
	Block     wire.BlockRef
	DropItems bool
	Exp       int
}

func (*BlockBreakEvent) Kind() api.EventKind { return api.KindBlockBreak }
func (e *BlockBreakEvent) decode(r *wire.Reader) {
	e.Player, e.Block, e.DropItems, e.Exp, e.cancelled = Player{r.Player()}, r.Block(), r.Bool(), int(r.Varint()), r.Bool()
}

func (e *BlockBreakEvent) SetDropItems(v bool) {
	e.DropItems = v
	e.muts.U8(wire.MutDropItems)
	e.muts.Bool(v)
}

func (e *BlockBreakEvent) SetExp(v int) {
	e.Exp = v
	e.muts.U8(wire.MutExp)
	e.muts.Varint(int64(v))
}

type BlockPlaceEvent struct {
	cancellable
	Player   Player
	Block    wire.BlockRef // State is the placed state
	Replaced string
	Against  api.BlockPos
	Hand     api.Hand
	Item     api.ItemStack
	CanBuild bool
}

func (*BlockPlaceEvent) Kind() api.EventKind { return api.KindBlockPlace }
func (e *BlockPlaceEvent) decode(r *wire.Reader) {
	e.Player, e.Block, e.Replaced, e.Against = Player{r.Player()}, r.Block(), r.String(), r.BlockPos()
	e.Hand, e.Item, e.CanBuild, e.cancelled = api.Hand(r.U8()), r.Item(), r.Bool(), r.Bool()
}

type PlayerInteractEvent struct {
	cancellable
	Player Player
	Action api.InteractAction
	Block  *wire.BlockRef
	Face   api.BlockFace
	Hand   api.Hand
	Item   api.ItemStack
	Cursor api.Vec3
}

func (*PlayerInteractEvent) Kind() api.EventKind { return api.KindPlayerInteract }
func (e *PlayerInteractEvent) decode(r *wire.Reader) {
	e.Player, e.Action = Player{r.Player()}, api.InteractAction(r.U8())
	if r.Bool() {
		b := r.Block()
		e.Block = &b
	}
	e.Face, e.Hand, e.Item, e.Cursor, e.cancelled = api.BlockFace(r.U8()), api.Hand(r.U8()), r.Item(), r.Vec3(), r.Bool()
}

type PlayerInteractEntityEvent struct {
	cancellable
	Player Player
	Target wire.EntityRef
	Hand   api.Hand
	Attack bool
	At     api.Vec3
}

func (*PlayerInteractEntityEvent) Kind() api.EventKind { return api.KindPlayerInteractEntity }
func (e *PlayerInteractEntityEvent) decode(r *wire.Reader) {
	e.Player, e.Target, e.Hand, e.Attack, e.At, e.cancelled = Player{r.Player()}, r.Entity(), api.Hand(r.U8()), r.Bool(), r.Vec3(), r.Bool()
}

type EntityDamageEvent struct {
	cancellable
	Entity     wire.EntityRef
	Cause      api.DamageCause
	DamageType api.Key
	Damager    *wire.EntityRef
	Damage     float64
}

func (*EntityDamageEvent) Kind() api.EventKind { return api.KindEntityDamage }
func (e *EntityDamageEvent) decode(r *wire.Reader) {
	e.Entity, e.Cause, e.DamageType = r.Entity(), api.DamageCause(r.U8()), api.Key(r.String())
	if r.Bool() {
		d := r.Entity()
		e.Damager = &d
	}
	e.Damage, e.cancelled = r.F64(), r.Bool()
}

func (e *EntityDamageEvent) SetDamage(v float64) {
	e.Damage = v
	e.muts.U8(wire.MutDamage)
	e.muts.F64(v)
}

type EntityDeathEvent struct {
	eventBase
	Entity        wire.EntityRef
	Cause         api.DamageCause
	Exp           int
	DeathMessage  *api.Component
	KeepInventory bool
	Drops         []api.ItemStack
}

func (*EntityDeathEvent) Kind() api.EventKind { return api.KindEntityDeath }
func (e *EntityDeathEvent) decode(r *wire.Reader) {
	e.Entity, e.Cause, e.Exp = r.Entity(), api.DamageCause(r.U8()), int(r.Varint())
	e.DeathMessage, e.KeepInventory = readOptComponent(r), r.Bool()
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		e.Drops = append(e.Drops, r.Item())
	}
}

func (e *EntityDeathEvent) SetDeathMessage(c *api.Component) { e.DeathMessage = c; e.setMessage(c) }

func (e *EntityDeathEvent) SetExp(v int) {
	e.Exp = v
	e.muts.U8(wire.MutExp)
	e.muts.Varint(int64(v))
}

func (e *EntityDeathEvent) SetKeepInventory(v bool) {
	e.KeepInventory = v
	e.muts.U8(wire.MutKeepInv)
	e.muts.Bool(v)
}

type EntitySpawnEvent struct {
	cancellable
	Entity   wire.EntityRef
	Location api.Location
	Reason   api.SpawnReason
}

func (*EntitySpawnEvent) Kind() api.EventKind { return api.KindEntitySpawn }
func (e *EntitySpawnEvent) decode(r *wire.Reader) {
	e.Entity, e.Location, e.Reason, e.cancelled = r.Entity(), r.Location(), api.SpawnReason(r.U8()), r.Bool()
}

type WorldLoadEvent struct {
	eventBase
	World string
}

func (*WorldLoadEvent) Kind() api.EventKind     { return api.KindWorldLoad }
func (e *WorldLoadEvent) decode(r *wire.Reader) { e.World = r.String() }

type WorldUnloadEvent struct {
	cancellable
	World string
}

func (*WorldUnloadEvent) Kind() api.EventKind     { return api.KindWorldUnload }
func (e *WorldUnloadEvent) decode(r *wire.Reader) { e.World, e.cancelled = r.String(), r.Bool() }

type ChunkLoadEvent struct {
	eventBase
	World string
	X, Z  int
	New   bool
}

func (*ChunkLoadEvent) Kind() api.EventKind { return api.KindChunkLoad }
func (e *ChunkLoadEvent) decode(r *wire.Reader) {
	e.World, e.X, e.Z, e.New = r.String(), int(r.Varint()), int(r.Varint()), r.Bool()
}

type ChunkUnloadEvent struct {
	eventBase
	World string
	X, Z  int
	Save  bool
}

func (*ChunkUnloadEvent) Kind() api.EventKind { return api.KindChunkUnload }
func (e *ChunkUnloadEvent) decode(r *wire.Reader) {
	e.World, e.X, e.Z, e.Save = r.String(), int(r.Varint()), int(r.Varint()), r.Bool()
}

type ServerTickStartEvent struct {
	eventBase
	Tick int64
}

func (*ServerTickStartEvent) Kind() api.EventKind     { return api.KindServerTickStart }
func (e *ServerTickStartEvent) decode(r *wire.Reader) { e.Tick = r.Varint() }

type ServerTickEndEvent struct {
	eventBase
	Tick                          int64
	DurationNanos, RemainingNanos int64
}

func (*ServerTickEndEvent) Kind() api.EventKind { return api.KindServerTickEnd }
func (e *ServerTickEndEvent) decode(r *wire.Reader) {
	e.Tick, e.DurationNanos, e.RemainingNanos = r.Varint(), r.Varint(), r.Varint()
}

type PluginInfo struct{ ID, Name, Version string }

type PluginEnableEvent struct {
	eventBase
	Plugin PluginInfo
}

func (*PluginEnableEvent) Kind() api.EventKind { return api.KindPluginEnable }
func (e *PluginEnableEvent) decode(r *wire.Reader) {
	e.Plugin = PluginInfo{r.String(), r.String(), r.String()}
}

type PluginDisableEvent struct {
	eventBase
	Plugin PluginInfo
}

func (*PluginDisableEvent) Kind() api.EventKind { return api.KindPluginDisable }
func (e *PluginDisableEvent) decode(r *wire.Reader) {
	e.Plugin = PluginInfo{r.String(), r.String(), r.String()}
}

type InventoryClickEvent struct {
	cancellable
	Player          Player
	Slot            int
	Click           api.ClickType
	Button          int
	Current, Cursor api.ItemStack
}

func (*InventoryClickEvent) Kind() api.EventKind { return api.KindInventoryClick }
func (e *InventoryClickEvent) decode(r *wire.Reader) {
	e.Player, e.Slot, e.Click, e.Button = Player{r.Player()}, int(r.Varint()), api.ClickType(r.U8()), int(r.Varint())
	e.Current, e.Cursor, e.cancelled = r.Item(), r.Item(), r.Bool()
}

type InventoryCloseEvent struct {
	eventBase
	Player Player
}

func (*InventoryCloseEvent) Kind() api.EventKind     { return api.KindInventoryClose }
func (e *InventoryCloseEvent) decode(r *wire.Reader) { e.Player = Player{r.Player()} }

type PluginMessageEvent struct {
	eventBase
	Player  Player
	Channel api.Key
	Data    []byte
}

func (*PluginMessageEvent) Kind() api.EventKind { return api.KindPluginMessage }
func (e *PluginMessageEvent) decode(r *wire.Reader) {
	e.Player, e.Channel, e.Data = Player{r.Player()}, api.Key(r.String()), r.Bytes()
}

type listener struct {
	kind            api.EventKind
	priority        api.Priority
	ignoreCancelled bool
	handle          func(r *wire.Reader) *eventBase
}

var listeners []listener

func (l *listener) subscribe(i int) {
	tag := uint32(i+1) << 16
	ign := uint32(0)
	if l.ignoreCancelled {
		ign = 1
	}
	if hostSubscribe(uint32(l.kind)|tag, uint32(l.priority), ign) != wire.OK {
		Errorf("subscribe %s failed", l.kind)
	}
}

// Listen registers fn for event type E at the given priority. Call it from
// init, OnEnable or later.
//
//	kremenmc.Listen(api.Normal, func(e *kremenmc.PlayerJoinEvent) { ... })
func Listen[E any, PE interface {
	*E
	Event
}](priority api.Priority, fn func(PE)) {
	listen[E, PE](priority, false, fn)
}

// ListenIgnoreCancelled is Listen with Bukkit's ignoreCancelled=true.
func ListenIgnoreCancelled[E any, PE interface {
	*E
	Event
}](priority api.Priority, fn func(PE)) {
	listen[E, PE](priority, true, fn)
}

func listen[E any, PE interface {
	*E
	Event
}](priority api.Priority, ignoreCancelled bool, fn func(PE)) {
	var zero PE
	if len(listeners) >= 0xffff {
		Errorf("too many listeners")
		return
	}
	listeners = append(listeners, listener{
		kind:            zero.Kind(),
		priority:        priority,
		ignoreCancelled: ignoreCancelled,
		handle: func(r *wire.Reader) *eventBase {
			e := PE(new(E))
			e.decode(r)
			if r.Err != nil {
				Errorf("decode %s: %v", e.Kind(), r.Err)
				return nil
			}
			guard(e.Kind().String()+" listener", func() { fn(e) })
			return e.base()
		},
	})
	if enabled {
		listeners[len(listeners)-1].subscribe(len(listeners) - 1)
	}
}

func onEvent(kindTag uint32, data []byte) uint64 {
	i := int(kindTag>>16) - 1
	if i < 0 || i >= len(listeners) || uint32(listeners[i].kind) != kindTag&0xffff {
		return 0
	}
	b := listeners[i].handle(wire.NewReader(data))
	if b == nil || (b.cancel == wire.CancelUnchanged && len(b.muts.B) == 0) {
		return 0
	}
	respBuf.Reset()
	respBuf.U8(b.cancel)
	respBuf.Raw(b.muts.B)
	return uint64(addr(respBuf.B))<<32 | uint64(len(respBuf.B))
}
