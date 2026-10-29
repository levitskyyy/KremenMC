package plugin

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

func playerRef(p api.Player) wire.PlayerRef {
	if p == nil {
		return wire.PlayerRef{}
	}
	return wire.PlayerRef{
		UUID:     p.UUID(),
		Name:     p.Name(),
		EntityID: p.EntityID(),
		Location: p.Location(),
		GameMode: p.GameMode(),
		Health:   p.Health(),
		Op:       p.IsOp(),
	}
}

func entityRef(e api.Entity) wire.EntityRef {
	if e == nil {
		return wire.EntityRef{}
	}
	return wire.EntityRef{UUID: e.UUID(), EntityID: e.EntityID(), Type: e.Type(), Location: e.Location()}
}

func blockRef(b api.Block, state api.BlockState) wire.BlockRef {
	if b == nil {
		return wire.BlockRef{}
	}
	ref := wire.BlockRef{Pos: b.Pos()}
	if w := b.World(); w != nil {
		ref.World = w.Name()
	}
	if state == nil {
		state = b.State()
	}
	if state != nil {
		ref.State = state.String()
	}
	return ref
}

func stateString(s api.BlockState) string {
	if s == nil {
		return ""
	}
	return s.String()
}

func worldName(w api.World) string {
	if w == nil {
		return ""
	}
	return w.Name()
}

func optComponent(w *wire.Writer, c *api.Component) {
	w.Bool(c != nil)
	if c != nil {
		w.Component(*c)
	}
}

func writePlugin(w *wire.Writer, p api.PluginInfo) {
	w.String(p.ID)
	w.String(p.Name)
	w.String(p.Version)
}

// EncodeEvent writes the guest-visible payload of e. It reports false for
// events that have no wire form (custom kinds).
func EncodeEvent(w *wire.Writer, e api.Event) bool {
	switch e := e.(type) {
	case *api.PlayerPreLoginEvent:
		w.String(e.Name)
		w.UUID(e.UUID)
		w.String(e.Address)
		w.U8(uint8(e.Result))
		w.Component(e.Message)
	case *api.PlayerLoginEvent:
		w.Player(playerRef(e.Player))
		w.String(e.Hostname)
		w.U8(uint8(e.Result))
		w.Component(e.Message)
	case *api.PlayerJoinEvent:
		w.Player(playerRef(e.Player))
		optComponent(w, e.Message)
	case *api.PlayerQuitEvent:
		w.Player(playerRef(e.Player))
		w.U8(uint8(e.Reason))
		optComponent(w, e.Message)
	case *api.PlayerKickEvent:
		w.Player(playerRef(e.Player))
		w.Component(e.Reason)
		optComponent(w, e.Leave)
		w.Bool(e.Cancelled())
	case *api.PlayerChatEvent:
		w.Player(playerRef(e.Player))
		w.String(e.Message)
		w.Uvarint(uint64(len(e.Recipients)))
		for _, r := range e.Recipients {
			w.UUID(r.UUID())
		}
		w.Bool(e.Cancelled())
	case *api.PlayerCommandEvent:
		w.Player(playerRef(e.Player))
		w.String(e.Line)
		w.Bool(e.Cancelled())
	case *api.ServerCommandEvent:
		name := ""
		if e.Sender != nil {
			name = e.Sender.Name()
		}
		w.String(name)
		w.String(e.Line)
		w.Bool(e.Cancelled())
	case *api.PlayerMoveEvent:
		w.Player(playerRef(e.Player))
		w.Location(e.From)
		w.Location(e.To)
		w.Bool(e.Cancelled())
	case *api.PlayerTeleportEvent:
		w.Player(playerRef(e.Player))
		w.Location(e.From)
		w.Location(e.To)
		w.U8(uint8(e.Cause))
		w.Bool(e.Cancelled())
	case *api.PlayerChangeWorldEvent:
		w.Player(playerRef(e.Player))
		w.String(worldName(e.From))
	case *api.PlayerRespawnEvent:
		w.Player(playerRef(e.Player))
		w.Location(e.Location)
		w.Bool(e.BedSpawn)
	case *api.BlockBreakEvent:
		w.Player(playerRef(e.Player))
		w.Block(blockRef(e.Block, e.State))
		w.Bool(e.DropItems)
		w.Varint(int64(e.Exp))
		w.Bool(e.Cancelled())
	case *api.BlockPlaceEvent:
		w.Player(playerRef(e.Player))
		w.Block(blockRef(e.Block, e.Placed))
		w.String(stateString(e.Replaced))
		w.BlockPos(e.Against)
		w.U8(uint8(e.Hand))
		w.Item(e.Item)
		w.Bool(e.CanBuild)
		w.Bool(e.Cancelled())
	case *api.PlayerInteractEvent:
		w.Player(playerRef(e.Player))
		w.U8(uint8(e.Action))
		w.Bool(e.Block != nil)
		if e.Block != nil {
			w.Block(blockRef(e.Block, nil))
		}
		w.U8(uint8(e.Face))
		w.U8(uint8(e.Hand))
		w.Item(e.Item)
		w.Vec3(e.Cursor)
		w.Bool(e.Cancelled())
	case *api.PlayerInteractEntityEvent:
		w.Player(playerRef(e.Player))
		w.Entity(entityRef(e.Target))
		w.U8(uint8(e.Hand))
		w.Bool(e.Attack)
		w.Vec3(e.At)
		w.Bool(e.Cancelled())
	case *api.EntityDamageEvent:
		w.Entity(entityRef(e.Entity))
		w.U8(uint8(e.Source.Cause))
		w.String(string(e.Source.Type))
		damager := e.Source.Causing
		if damager == nil {
			damager = e.Source.Direct
		}
		w.Bool(damager != nil)
		if damager != nil {
			w.Entity(entityRef(damager))
		}
		w.F64(e.Damage)
		w.Bool(e.Cancelled())
	case *api.EntityDeathEvent:
		w.Entity(entityRef(e.Entity))
		w.U8(uint8(e.Source.Cause))
		w.Varint(int64(e.Exp))
		optComponent(w, e.DeathMessage)
		w.Bool(e.KeepInventory)
		w.Uvarint(uint64(len(e.Drops)))
		for _, it := range e.Drops {
			w.Item(it)
		}
	case *api.EntitySpawnEvent:
		w.Entity(entityRef(e.Entity))
		w.Location(e.Location)
		w.U8(uint8(e.Reason))
		w.Bool(e.Cancelled())
	case *api.WorldLoadEvent:
		w.String(worldName(e.World))
	case *api.WorldUnloadEvent:
		w.String(worldName(e.World))
		w.Bool(e.Cancelled())
	case *api.ChunkLoadEvent:
		w.String(worldName(e.World))
		w.Varint(int64(e.X))
		w.Varint(int64(e.Z))
		w.Bool(e.New)
	case *api.ChunkUnloadEvent:
		w.String(worldName(e.World))
		w.Varint(int64(e.X))
		w.Varint(int64(e.Z))
		w.Bool(e.Save)
	case *api.ServerTickStartEvent:
		w.Varint(e.Tick)
	case *api.ServerTickEndEvent:
		w.Varint(e.Tick)
		w.Varint(e.DurationNanos)
		w.Varint(e.RemainingNanos)
	case *api.PluginEnableEvent:
		writePlugin(w, e.Plugin)
	case *api.PluginDisableEvent:
		writePlugin(w, e.Plugin)
	case *api.InventoryClickEvent:
		w.Player(playerRef(e.Player))
		w.Varint(int64(e.Slot))
		w.U8(uint8(e.Click))
		w.Varint(int64(e.Button))
		w.Item(e.Current)
		w.Item(e.Cursor)
		w.Bool(e.Cancelled())
	case *api.InventoryCloseEvent:
		w.Player(playerRef(e.Player))
	case *api.PluginMessageEvent:
		w.Player(playerRef(e.Player))
		w.String(string(e.Channel))
		w.Bytes(e.Data)
	default:
		return false
	}
	return true
}

// ApplyResponse applies guest mutations to e. Unknown or inapplicable tags
// are ignored so newer guests work with older hosts.
func ApplyResponse(e api.Event, data []byte, srv api.Server) error {
	r := wire.NewReader(data)
	switch r.U8() {
	case wire.CancelSet:
		if c, ok := e.(api.Cancellable); ok {
			c.SetCancelled(true)
		}
	case wire.CancelClear:
		if c, ok := e.(api.Cancellable); ok {
			c.SetCancelled(false)
		}
	}
	for r.Err == nil && r.Len() > 0 {
		switch tag := r.U8(); tag {
		case wire.MutMessage:
			c := r.Component()
			if r.Err != nil {
				break
			}
			setMessage(e, &c)
		case wire.MutClearMessage:
			setMessage(e, nil)
		case wire.MutText:
			s := r.String()
			switch e := e.(type) {
			case *api.PlayerChatEvent:
				e.Message = s
			case *api.PlayerCommandEvent:
				e.Line = s
			case *api.ServerCommandEvent:
				e.Line = s
			}
		case wire.MutTo:
			l := r.Location()
			if r.Err != nil {
				break
			}
			switch e := e.(type) {
			case *api.PlayerMoveEvent:
				e.To = l
			case *api.PlayerTeleportEvent:
				e.To = l
			case *api.PlayerRespawnEvent:
				e.Location = l
			}
		case wire.MutDamage:
			if d, ok := e.(*api.EntityDamageEvent); ok {
				d.Damage = r.F64()
			} else {
				r.F64()
			}
		case wire.MutLoginResult:
			res, msg := api.LoginResult(r.U8()), r.Component()
			switch e := e.(type) {
			case *api.PlayerPreLoginEvent:
				e.Result, e.Message = res, msg
			case *api.PlayerLoginEvent:
				e.Result, e.Message = res, msg
			}
		case wire.MutDropItems:
			v := r.Bool()
			if b, ok := e.(*api.BlockBreakEvent); ok {
				b.DropItems = v
			}
		case wire.MutExp:
			v := int(r.Varint())
			switch e := e.(type) {
			case *api.BlockBreakEvent:
				e.Exp = v
			case *api.EntityDeathEvent:
				e.Exp = v
			}
		case wire.MutKeepInv:
			v := r.Bool()
			if d, ok := e.(*api.EntityDeathEvent); ok {
				d.KeepInventory = v
			}
		case wire.MutRecipients:
			n := r.Count()
			ids := make([]api.UUID, 0, n)
			for i := 0; i < n && r.Err == nil; i++ {
				ids = append(ids, r.UUID())
			}
			if c, ok := e.(*api.PlayerChatEvent); ok && r.Err == nil && srv != nil {
				out := c.Recipients[:0:0]
				for _, id := range ids {
					if p, ok := srv.Player(id); ok {
						out = append(out, p)
					}
				}
				c.Recipients = out
			}
		default:
			return nil
		}
	}
	return r.Err
}

func setMessage(e api.Event, c *api.Component) {
	switch e := e.(type) {
	case *api.PlayerJoinEvent:
		e.Message = c
	case *api.PlayerQuitEvent:
		e.Message = c
	case *api.PlayerKickEvent:
		e.Leave = c
	case *api.EntityDeathEvent:
		e.DeathMessage = c
	case *api.PlayerPreLoginEvent:
		if c != nil {
			e.Message = *c
		}
	case *api.PlayerLoginEvent:
		if c != nil {
			e.Message = *c
		}
	}
}
