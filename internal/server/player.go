package server

import (
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Player is one connected player. Fields under "tick-owned" are touched only
// by the tick goroutine's player and streaming phases; everything else is
// guarded by mu or atomic.
type Player struct {
	s    *Server
	c    *mcnet.Conn
	prof api.GameProfile
	permissible

	world atomic.Pointer[World]

	inMu   sync.Mutex
	inbox  []packets.Packet
	spare  []packets.Packet
	tasks  tickQueue
	chatCh chan string

	mu          sync.Mutex
	e           *entity.Entity
	gm          api.GameMode
	flying      bool
	allowFlight bool
	sneaking    bool
	sprinting   bool
	health      float64
	food        int
	saturation  float32
	expLevel    int
	expProgress float32
	displayName *api.Component
	listName    *api.Component
	respawn     *api.Location
	tags        []string
	ticks       int64
	extra       [][]byte
	metaDirty   bool
	tpID        int32
	pendingTP   int32
	hidden      map[api.UUID]map[string]bool

	inv  *invStore
	held atomic.Int32
	use  useState
	liv  living // combat state (damage.go)

	laterMu  sync.Mutex
	deferred []func()

	joined atomic.Bool
	quit   atomic.Bool
	pipe   *bridge.Pipe // set before play when the Java bridge routes this player's packets
	pdata  *dataContainer

	// tick-owned
	trk        entity.Tracked
	sentMu     sync.RWMutex
	sent       map[uint64]struct{}
	ticket     world.TicketID
	center     world.ChunkPos
	hasCenter  bool
	vd         int
	complete   bool
	quota      float32
	cpt        float32
	unacked    int
	maxUnacked int
	ackSeq     int32
	dg         digMachine
	dataLoaded bool        // playerdata applied at join (playerdata.go)
	saveReq    atomic.Bool // autosave requested (playerdata.go)
	tracking   map[int32]struct{}
	loaded     bool
	portal     portalState // tick-goroutine owned (portal_travel.go)
}

func newPlayer(s *Server, c *mcnet.Conn) *Player {
	prof := c.Profile()
	pt, _ := data.Canonical().EntityByName("player")
	e := entity.New(pt)
	e.UUID = prof.UUID
	p := &Player{
		s: s, c: c,
		prof:       api.GameProfile{UUID: api.UUID(prof.UUID), Name: prof.Name},
		e:          e,
		gm:         s.DefaultGameMode(),
		health:     20,
		food:       20,
		saturation: 5,
		sent:       map[uint64]struct{}{},
		tracking:   map[int32]struct{}{},
		cpt:        9,
		maxUnacked: 1,
		ackSeq:     -1,
		chatCh:     make(chan string, 32),
		pdata:      newDataContainer(),
		hidden:     map[api.UUID]map[string]bool{},
	}
	p.liv.init("player")
	for _, pr := range prof.Properties {
		p.prof.Properties = append(p.prof.Properties, api.ProfileProperty{Name: pr.Name, Value: pr.Value, Signature: pr.Signature.V})
	}
	p.permissible = permissible{
		perms: s.perms, subject: p.prof.UUID,
		op:    func() bool { _, ok := s.ops.get(p.prof.UUID); return ok },
		setOp: func(v bool) { s.setOp(p.prof.UUID, p.prof.Name, v) },
	}
	p.allowFlight = p.gm == api.Creative || p.gm == api.Spectator
	p.flying = p.gm == api.Spectator
	p.inv = newInvStore(p)
	p.trk = entity.Tracked{ID: e.ID, UUID: e.UUID, Type: int32(pt.ID), Range: 512}
	p.metaDirty = true
	return p
}

// Handle queues a play packet from the connection goroutine.
func (p *Player) Handle(pk packets.Packet) {
	p.inMu.Lock()
	p.inbox = append(p.inbox, pk)
	p.inMu.Unlock()
}

func (p *Player) Closed(err error) {
	if p.pipe != nil {
		p.pipe.Close()
	}
	p.quit.Store(true)
	select {
	case p.s.quits <- p:
	default:
		go func() { p.s.quits <- p }()
	}
}

func (p *Player) drain() []packets.Packet {
	p.inMu.Lock()
	in := p.inbox
	p.inbox = p.spare[:0]
	p.inMu.Unlock()
	return in
}

func (p *Player) recycle(in []packets.Packet) {
	clear(in)
	p.inMu.Lock()
	p.spare = in[:0]
	p.inMu.Unlock()
}

func (p *Player) send(pk packets.Packet) { p.c.Send(pk) }

func (p *Player) hasChunk(k uint64) bool {
	p.sentMu.RLock()
	_, ok := p.sent[k]
	p.sentMu.RUnlock()
	return ok
}

func (p *Player) sentChunks() []uint64 {
	p.sentMu.RLock()
	defer p.sentMu.RUnlock()
	out := make([]uint64, 0, len(p.sent))
	for k := range p.sent {
		out = append(out, k)
	}
	return out
}

func (p *Player) EntityID() int32 { return p.e.ID }
func (p *Player) UUID() api.UUID  { return p.prof.UUID }
func (p *Player) Type() api.Key   { return "minecraft:player" }
func (p *Player) Name() string    { return p.prof.Name }
func (p *Player) World() api.World {
	if w := p.world.Load(); w != nil {
		return w
	}
	return nil
}

func (p *Player) Location() api.Location {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.locationLocked()
}

func (p *Player) locationLocked() api.Location {
	name := ""
	if w := p.world.Load(); w != nil {
		name = w.name
	}
	return api.Location{World: name, Pos: p.e.Pos, Yaw: p.e.Yaw, Pitch: p.e.Pitch}
}

func (p *Player) Velocity() api.Vec3 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.e.Vel
}

func (p *Player) SetVelocity(v api.Vec3) {
	p.mu.Lock()
	p.e.Vel = v
	p.mu.Unlock()
	p.send(&packets.ClientboundSetEntityMotion{EntityID: p.e.ID, Velocity: proto.LpVec3{X: v.X, Y: v.Y, Z: v.Z}})
}

func (p *Player) OnGround() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.e.OnGround
}

func (p *Player) BoundingBox() (api.Vec3, api.Vec3) {
	p.mu.Lock()
	b := p.e.Box()
	p.mu.Unlock()
	return api.Vec3{X: b.MinX, Y: b.MinY, Z: b.MinZ}, api.Vec3{X: b.MaxX, Y: b.MaxY, Z: b.MaxZ}
}

// Teleport moves the player within its world; cross-world moves are not supported yet.
func (p *Player) Teleport(to api.Location, cause api.TeleportCause) bool {
	if w := p.world.Load(); w == nil || to.World != "" && to.World != w.name {
		return false
	}
	if p.s.bus.HasHandlers(api.KindPlayerTeleport) {
		ev := &api.PlayerTeleportEvent{Player: p, From: p.Location(), To: to, Cause: cause}
		if p.s.bus.Dispatch(ev).(*api.PlayerTeleportEvent).Cancelled() {
			return false
		}
		to = ev.To
	}
	if !p.bridgeTeleport(&to, cause) {
		return false
	}
	p.teleport(to)
	return true
}

func (p *Player) teleport(to api.Location) {
	p.mu.Lock()
	p.tpID++
	id := p.tpID
	p.pendingTP = id
	p.e.Pos, p.e.Prev = to.Pos, to.Pos
	p.e.Yaw, p.e.Pitch, p.e.HeadYaw = to.Yaw, to.Pitch, to.Yaw
	p.e.Vel = api.Vec3{}
	p.e.FallDistance = 0
	p.mu.Unlock()
	p.send(&packets.ClientboundPlayerPosition{TeleportID: id, X: to.Pos.X, Y: to.Pos.Y, Z: to.Pos.Z, Yaw: to.Yaw, Pitch: to.Pitch})
}

func (p *Player) CustomName() (api.Component, bool) { return api.Component{}, false }
func (p *Player) SetCustomName(*api.Component)      {}
func (p *Player) Silent() bool                      { return false }
func (p *Player) SetSilent(bool)                    {}
func (p *Player) Glowing() bool                     { return false }
func (p *Player) SetGlowing(bool)                   {}
func (p *Player) Gravity() bool                     { return true }
func (p *Player) SetGravity(bool)                   {}

func (p *Player) Invulnerable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gm == api.Creative || p.gm == api.Spectator
}
func (p *Player) SetInvulnerable(bool) {}

func (p *Player) Passengers() []api.Entity        { return nil }
func (p *Player) AddPassenger(api.Entity) bool    { return false }
func (p *Player) RemovePassenger(api.Entity) bool { return false }
func (p *Player) Vehicle() (api.Entity, bool)     { return nil, false }

func (p *Player) Tags() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.tags)
}

func (p *Player) AddTag(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.tags, t) {
		return false
	}
	p.tags = append(p.tags, t)
	return true
}

func (p *Player) RemoveTag(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.tags)
	p.tags = slices.DeleteFunc(p.tags, func(x string) bool { return x == t })
	return n != len(p.tags)
}

func (p *Player) TicksLived() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ticks
}

func (p *Player) IsValid() bool  { return p.joined.Load() && !p.quit.Load() }
func (p *Player) IsOnline() bool { return p.IsValid() }
func (p *Player) Remove()        { p.Kick(api.Text("Removed")) }

func (p *Player) PersistentData() api.DataContainer { return p.pdata }

func (p *Player) Scheduler() api.EntityScheduler {
	return entitySched{queueSched: queueSched{s: p.s.sched, q: &p.tasks}, valid: p.IsValid}
}

func (p *Player) Health() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.health
}

func (p *Player) SetHealth(h float64) {
	p.liv.mu.Lock()
	mh := p.maxHealthAttr()
	p.liv.mu.Unlock()
	p.mu.Lock()
	p.health = min(max(h, 0), mh)
	p.mu.Unlock()
	p.sendHealth()
}

func (p *Player) sendHealth() {
	p.mu.Lock()
	pk := &packets.ClientboundSetHealth{Health: float32(p.health), Food: int32(p.food), FoodSaturation: p.saturation}
	p.mu.Unlock()
	p.send(pk)
}

func (p *Player) MaxHealth() float64 {
	p.liv.mu.Lock()
	defer p.liv.mu.Unlock()
	return p.maxHealthAttr()
}

// Damage applies src through the vanilla damage pipeline (damage.go).
func (p *Player) Damage(amount float64, src api.DamageSource) {
	if amount <= 0 {
		return
	}
	hurt(p, fromAPI(src), float32(amount))
}

func (p *Player) IsDead() bool { return p.Health() <= 0 }

func (p *Player) EyeLocation() api.Location {
	l := p.Location()
	l.Pos.Y += 1.62
	return l
}

func (p *Player) Equipment() api.Equipment        { return playerEquipment{p} }
func (p *Player) AddEffect(api.PotionEffect) bool { return false }
func (p *Player) RemoveEffect(api.Key)            {}
func (p *Player) Effects() []api.PotionEffect     { return nil }

type playerEquipment struct{ p *Player }

func (e playerEquipment) slot(s api.EquipmentSlot) int {
	switch s {
	case api.SlotMainHand:
		return int(e.p.held.Load())
	case api.SlotOffHand:
		return 40
	case api.SlotFeet:
		return 36
	case api.SlotLegs:
		return 37
	case api.SlotChest:
		return 38
	case api.SlotHead:
		return 39
	}
	return -1
}

func (e playerEquipment) Get(s api.EquipmentSlot) api.ItemStack {
	return e.p.Inventory().Item(e.slot(s))
}
func (e playerEquipment) Set(s api.EquipmentSlot, it api.ItemStack) {
	e.p.Inventory().SetItem(e.slot(s), it)
}

func (p *Player) SendMessage(msg api.Component) { p.c.SendRaw(systemChat(msg, false)) }
func (p *Player) SendActionBar(msg api.Component) {
	p.send(&packets.ClientboundSetActionBarText{Text: toText(msg)})
}

func (p *Player) ShowTitle(t api.Title) {
	p.send(&packets.ClientboundSetTitlesAnimation{FadeIn: int32(t.FadeIn), Stay: int32(t.Stay), FadeOut: int32(t.FadeOut)})
	if !t.Subtitle.IsEmpty() {
		p.send(&packets.ClientboundSetSubtitleText{Text: toText(t.Subtitle)})
	}
	p.send(&packets.ClientboundSetTitleText{Text: toText(t.Title)})
}

func (p *Player) ClearTitle() { p.send(&packets.ClientboundClearTitles{Reset: true}) }

var soundCategories = []string{"master", "music", "record", "weather", "block", "hostile", "neutral", "player", "ambient", "voice", "ui"}

func (p *Player) PlaySound(s api.Sound) {
	at := p.Location().Pos
	if s.At != nil {
		at = *s.At
	}
	cat := max(slices.Index(soundCategories, s.Category), 0)
	p.send(&packets.ClientboundSound{
		Sound:         proto.Holder[packets.ItemSoundEvent]{Value: &packets.ItemSoundEvent{SoundName: string(s.Name.Normalize())}},
		SoundCategory: int32(cat),
		X:             int32(at.X * 8), Y: int32(at.Y * 8), Z: int32(at.Z * 8),
		Volume: s.Volume, Pitch: s.Pitch, Seed: s.Seed,
	})
}

func (p *Player) Profile() api.GameProfile { return p.prof }

func (p *Player) DisplayName() api.Component {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.displayName != nil {
		return *p.displayName
	}
	return api.Text(p.prof.Name)
}

func (p *Player) SetDisplayName(c api.Component) {
	p.mu.Lock()
	p.displayName = &c
	p.mu.Unlock()
}

func (p *Player) PlayerListName() api.Component {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listName != nil {
		return *p.listName
	}
	return api.Text(p.prof.Name)
}

func (p *Player) SetPlayerListName(c api.Component) {
	p.mu.Lock()
	p.listName = &c
	p.mu.Unlock()
	raw := encode(&packets.ClientboundPlayerInfoUpdate{
		Action: packets.ClientboundPlayerInfoUpdateActionUpdateDisplayName,
		Data:   []packets.ClientboundPlayerInfoUpdateDataEntry{{UUID: p.prof.UUID, DisplayName: proto.Some(toText(c))}},
	})
	for _, o := range p.s.onlinePlayers() {
		o.c.SendRaw(raw)
	}
}

func (p *Player) Locale() string          { return p.c.ClientInfo().Locale }
func (p *Player) Ping() time.Duration     { return p.c.Ping() }
func (p *Player) Address() netip.AddrPort { return p.c.Addr() }
func (p *Player) ProtocolVersion() int32  { return p.c.Protocol() }
func (p *Player) ClientBrand() string     { return p.c.Brand() }

func (p *Player) GameMode() api.GameMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gm
}

func (p *Player) SetGameMode(g api.GameMode) {
	p.mu.Lock()
	if p.gm == g {
		p.mu.Unlock()
		return
	}
	p.gm = g
	p.allowFlight = g == api.Creative || g == api.Spectator
	if !p.allowFlight {
		p.flying = false
	}
	if g == api.Spectator {
		p.flying = true
	}
	p.mu.Unlock()
	p.send(&packets.ClientboundGameEvent{Reason: 3, GameMode: float32(g)})
	p.sendAbilities()
	raw := encode(&packets.ClientboundPlayerInfoUpdate{
		Action: packets.ClientboundPlayerInfoUpdateActionUpdateGameMode,
		Data:   []packets.ClientboundPlayerInfoUpdateDataEntry{{UUID: p.prof.UUID, Gamemode: int32(g)}},
	})
	for _, o := range p.s.onlinePlayers() {
		o.c.SendRaw(raw)
	}
}

func (p *Player) abilities() *packets.ClientboundPlayerAbilities {
	p.mu.Lock()
	defer p.mu.Unlock()
	var f int8
	if p.gm == api.Creative || p.gm == api.Spectator {
		f |= 0x01
	}
	if p.flying {
		f |= 0x02
	}
	if p.allowFlight {
		f |= 0x04
	}
	if p.gm == api.Creative {
		f |= 0x08
	}
	return &packets.ClientboundPlayerAbilities{Flags: f, FlyingSpeed: 0.05, WalkingSpeed: 0.1}
}

func (p *Player) sendAbilities() { p.send(p.abilities()) }

func (p *Player) Flying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.flying
}

func (p *Player) SetFlying(v bool) {
	p.mu.Lock()
	p.flying = v && p.allowFlight
	p.mu.Unlock()
	p.sendAbilities()
}

func (p *Player) AllowFlight() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allowFlight
}

func (p *Player) SetAllowFlight(v bool) {
	p.mu.Lock()
	p.allowFlight = v
	if !v {
		p.flying = false
	}
	p.mu.Unlock()
	p.sendAbilities()
}

func (p *Player) Sneaking() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sneaking
}

func (p *Player) Sprinting() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sprinting
}

func (p *Player) Food() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.food
}

func (p *Player) SetFood(v int) {
	p.mu.Lock()
	p.food = min(max(v, 0), 20)
	p.mu.Unlock()
	p.sendHealth()
}

func (p *Player) Saturation() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saturation
}

func (p *Player) SetSaturation(v float32) {
	p.mu.Lock()
	p.saturation = v
	p.mu.Unlock()
	p.sendHealth()
}

func (p *Player) Exp() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.expProgress
}

func (p *Player) Level() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.expLevel
}

func (p *Player) SetExp(level int, progress float32) {
	p.mu.Lock()
	p.expLevel, p.expProgress = level, progress
	p.mu.Unlock()
	p.send(&packets.ClientboundSetExperience{ExperienceBar: progress, Level: int32(level)})
}

func (p *Player) RespawnLocation() (api.Location, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.respawn == nil {
		return api.Location{}, false
	}
	return *p.respawn, true
}

func (p *Player) SetRespawnLocation(l *api.Location) {
	p.mu.Lock()
	p.respawn = l
	p.mu.Unlock()
}

func (p *Player) Inventory() api.PlayerInventory { return playerInv{p} }

func (p *Player) OpenInventory(api.Inventory)            {}
func (p *Player) OpenedInventory() (api.Inventory, bool) { return nil, false }
func (p *Player) CloseInventory()                        {}

func (p *Player) Kick(reason api.Component) {
	if p.s.bus.HasHandlers(api.KindPlayerKick) {
		ev := &api.PlayerKickEvent{Player: p, Reason: reason}
		if p.s.bus.Dispatch(ev).(*api.PlayerKickEvent).Cancelled() {
			return
		}
		reason = ev.Reason
	}
	p.c.Disconnect(toText(reason))
}

func (p *Player) Chat(msg string) {
	if strings.HasPrefix(msg, "/") {
		p.PerformCommand(msg[1:])
		return
	}
	select {
	case p.chatCh <- msg:
	default:
	}
}

func (p *Player) PerformCommand(line string) bool {
	_, err := p.s.cmds.Dispatch(p, line)
	return err == nil
}

func (p *Player) SendBlockChange(pos api.BlockPos, st api.BlockState) {
	if st == nil {
		return
	}
	p.send(&packets.ClientboundBlockUpdate{Location: proto.BlockPos{X: int32(pos.X), Y: int32(pos.Y), Z: int32(pos.Z)}, Type: int32(st.ID())})
}

func (p *Player) SendPluginMessage(ch api.Key, b []byte) {
	p.send(&packets.ClientboundCustomPayload{Channel: string(ch.Normalize()), Data: b})
}

func (p *Player) Transfer(host string, port int) {
	p.send(&packets.ClientboundTransfer{Host: host, Port: int32(port)})
}

func (p *Player) CanSee(o api.Player) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.hidden[o.UUID()]) == 0
}

func (p *Player) HidePlayer(owner string, o api.Player) {
	p.mu.Lock()
	m := p.hidden[o.UUID()]
	if m == nil {
		m = map[string]bool{}
		p.hidden[o.UUID()] = m
	}
	m[owner] = true
	p.mu.Unlock()
}

func (p *Player) ShowPlayer(owner string, o api.Player) {
	p.mu.Lock()
	delete(p.hidden[o.UUID()], owner)
	p.mu.Unlock()
}

func (p *Player) canSeeID(id api.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.hidden[id]) == 0
}

var _ api.Player = (*Player)(nil)
