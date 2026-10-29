package command

import (
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
)

type fakeSender struct {
	api.CommandSender
	name  string
	perms map[string]bool // nil grants everything
	mu    sync.Mutex
	msgs  []string
}

func (s *fakeSender) Name() string { return s.name }
func (s *fakeSender) HasPermission(n string) bool {
	return s.perms == nil || s.perms[n]
}
func (s *fakeSender) SendMessage(c api.Component) {
	s.mu.Lock()
	s.msgs = append(s.msgs, c.Plain())
	s.mu.Unlock()
}
func (s *fakeSender) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.msgs)
}
func (s *fakeSender) last() string {
	m := s.messages()
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

type fakePlayer struct {
	api.Player
	fs      *fakeSender
	id      api.UUID
	loc     api.Location
	world   *fakeWorld
	mode    api.GameMode
	op      bool
	dead    bool
	tags    []string
	level   int
	addr    netip.AddrPort
	inv     *fakeInventory
	kicked  string
	respawn *api.Location
	damaged float64
}

func (p *fakePlayer) Name() string                      { return p.fs.name }
func (p *fakePlayer) HasPermission(n string) bool       { return p.fs.HasPermission(n) }
func (p *fakePlayer) SendMessage(c api.Component)       { p.fs.SendMessage(c) }
func (p *fakePlayer) UUID() api.UUID                    { return p.id }
func (p *fakePlayer) Type() api.Key                     { return "minecraft:player" }
func (p *fakePlayer) Location() api.Location            { return p.loc }
func (p *fakePlayer) EyeLocation() api.Location         { l := p.loc; l.Pos.Y += 1.62; return l }
func (p *fakePlayer) World() api.World                  { return p.world }
func (p *fakePlayer) GameMode() api.GameMode            { return p.mode }
func (p *fakePlayer) SetGameMode(g api.GameMode)        { p.mode = g }
func (p *fakePlayer) IsOp() bool                        { return p.op }
func (p *fakePlayer) SetOp(v bool)                      { p.op = v }
func (p *fakePlayer) IsDead() bool                      { return p.dead }
func (p *fakePlayer) Tags() []string                    { return p.tags }
func (p *fakePlayer) Level() int                        { return p.level }
func (p *fakePlayer) CanSee(api.Player) bool            { return true }
func (p *fakePlayer) Address() netip.AddrPort           { return p.addr }
func (p *fakePlayer) Kick(r api.Component)              { p.kicked = r.Plain() }
func (p *fakePlayer) CustomName() (api.Component, bool) { return api.Component{}, false }
func (p *fakePlayer) BoundingBox() (api.Vec3, api.Vec3) {
	return p.loc.Pos.Sub(api.Vec3{X: 0.3, Z: 0.3}), p.loc.Pos.Add(api.Vec3{X: 0.3, Y: 1.8, Z: 0.3})
}
func (p *fakePlayer) Profile() api.GameProfile           { return api.GameProfile{UUID: p.id, Name: p.fs.name} }
func (p *fakePlayer) Inventory() api.PlayerInventory     { return p.inv }
func (p *fakePlayer) SetRespawnLocation(l *api.Location) { p.respawn = l }
func (p *fakePlayer) Damage(a float64, _ api.DamageSource) {
	p.damaged += a
	p.dead = true
}
func (p *fakePlayer) Teleport(to api.Location, _ api.TeleportCause) bool {
	p.loc = to
	return true
}

type fakeEntity struct {
	api.Entity
	id      api.UUID
	kind    api.Key
	loc     api.Location
	name    string
	tags    []string
	removed bool
}

func (e *fakeEntity) UUID() api.UUID                    { return e.id }
func (e *fakeEntity) Type() api.Key                     { return e.kind }
func (e *fakeEntity) Location() api.Location            { return e.loc }
func (e *fakeEntity) Tags() []string                    { return e.tags }
func (e *fakeEntity) Remove()                           { e.removed = true }
func (e *fakeEntity) BoundingBox() (api.Vec3, api.Vec3) { return e.loc.Pos, e.loc.Pos }
func (e *fakeEntity) CustomName() (api.Component, bool) {
	return api.Text(e.name), e.name != ""
}
func (e *fakeEntity) Teleport(to api.Location, _ api.TeleportCause) bool {
	e.loc = to
	return true
}

type fakeInventory struct {
	api.PlayerInventory
	slots  []api.ItemStack
	cursor api.ItemStack
}

func newInventory() *fakeInventory { return &fakeInventory{slots: make([]api.ItemStack, 36)} }

func (i *fakeInventory) Contents() []api.ItemStack          { return slices.Clone(i.slots) }
func (i *fakeInventory) SetItem(slot int, it api.ItemStack) { i.slots[slot] = it }
func (i *fakeInventory) Cursor() api.ItemStack              { return i.cursor }
func (i *fakeInventory) SetCursor(it api.ItemStack)         { i.cursor = it }
func (i *fakeInventory) AddItem(items ...api.ItemStack) []api.ItemStack {
	var rest []api.ItemStack
	for _, it := range items {
		placed := false
		for s := range i.slots {
			if i.slots[s].IsEmpty() {
				i.slots[s] = it
				placed = true
				break
			}
		}
		if !placed {
			rest = append(rest, it)
		}
	}
	return rest
}

type fakeState struct {
	id    uint32
	typ   api.Key
	props map[string]string
}

func (s *fakeState) ID() uint32                         { return s.id }
func (s *fakeState) Type() api.Key                      { return s.typ }
func (s *fakeState) Property(n string) (string, bool)   { v, ok := s.props[n]; return v, ok }
func (s *fakeState) Properties() map[string]string      { return s.props }
func (s *fakeState) With(string, string) api.BlockState { return s }
func (s *fakeState) IsAir() bool                        { return s.typ == "minecraft:air" }
func (s *fakeState) IsSolid() bool                      { return !s.IsAir() }
func (s *fakeState) IsLiquid() bool                     { return false }
func (s *fakeState) String() string {
	if len(s.props) == 0 {
		return string(s.typ)
	}
	keys := make([]string, 0, len(s.props))
	for k := range s.props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + s.props[k]
	}
	return string(s.typ) + "[" + strings.Join(parts, ",") + "]"
}

type fakeBlocks struct{ states []*fakeState }

func newBlocks() *fakeBlocks {
	return &fakeBlocks{states: []*fakeState{
		{0, "minecraft:air", nil},
		{1, "minecraft:stone", nil},
		{2, "minecraft:oak_log", map[string]string{"axis": "x"}},
		{3, "minecraft:oak_log", map[string]string{"axis": "y"}},
		{4, "minecraft:oak_log", map[string]string{"axis": "z"}},
		{5, "minecraft:grass_block", nil},
	}}
}

func (b *fakeBlocks) Parse(s string) (api.BlockState, error) {
	id, props, _ := strings.Cut(s, "[")
	key := api.Key(id).Normalize()
	want := map[string]string{}
	if props != "" {
		props = strings.TrimSuffix(props, "]")
		for kv := range strings.SplitSeq(props, ",") {
			k, v, _ := strings.Cut(kv, "=")
			want[k] = v
		}
	}
	var def api.BlockState
	for _, st := range b.states {
		if st.typ != key {
			continue
		}
		if def == nil {
			def = st
		}
		match := true
		for k, v := range want {
			if st.props[k] != v {
				match = false
			}
		}
		if match {
			return st, nil
		}
	}
	if def == nil {
		return nil, fmt.Errorf("unknown block %s", key)
	}
	return nil, fmt.Errorf("Block %s does not accept those properties", key)
}

func (b *fakeBlocks) ByID(id uint32) (api.BlockState, bool) {
	if int(id) < len(b.states) {
		return b.states[id], true
	}
	return nil, false
}

func (b *fakeBlocks) Default(k api.Key) (api.BlockState, bool) {
	for _, st := range b.states {
		if st.typ == k.Normalize() {
			return st, true
		}
	}
	return nil, false
}

func (b *fakeBlocks) Air() api.BlockState { return b.states[0] }

type fakeItems struct{}

var itemIDs = []api.Key{"minecraft:stone", "minecraft:diamond", "minecraft:diamond_sword"}

func (fakeItems) MaxStackSize(k api.Key) int32 {
	if k.Normalize() == "minecraft:diamond_sword" {
		return 1
	}
	return 64
}
func (fakeItems) Exists(k api.Key) bool { return slices.Contains(itemIDs, k.Normalize()) }
func (fakeItems) ID(k api.Key) (int32, bool) {
	i := slices.Index(itemIDs, k.Normalize())
	return int32(i), i >= 0
}
func (fakeItems) ByID(id int32) (api.Key, bool) {
	if int(id) < len(itemIDs) {
		return itemIDs[id], true
	}
	return "", false
}

type fakeWorld struct {
	api.World
	name       string
	server     *fakeServer
	blocks     map[api.BlockPos]api.BlockState
	entities   []api.Entity
	time       int64
	storm      bool
	thunder    bool
	rules      map[string]string
	spawn      api.Location
	difficulty api.Difficulty
	seed       int64
	sets       int
}

func (w *fakeWorld) Name() string                   { return w.name }
func (w *fakeWorld) Key() api.Key                   { return api.Key("minecraft:" + w.name) }
func (w *fakeWorld) MinY() int                      { return -64 }
func (w *fakeWorld) MaxY() int                      { return 320 }
func (w *fakeWorld) Seed() int64                    { return w.seed }
func (w *fakeWorld) Time() int64                    { return w.time }
func (w *fakeWorld) FullTime() int64                { return w.time }
func (w *fakeWorld) SetTime(t int64)                { w.time = t }
func (w *fakeWorld) Storming() bool                 { return w.storm }
func (w *fakeWorld) SetStorming(v bool)             { w.storm = v }
func (w *fakeWorld) Thundering() bool               { return w.thunder }
func (w *fakeWorld) SetThundering(v bool)           { w.thunder = v }
func (w *fakeWorld) Spawn() api.Location            { return w.spawn }
func (w *fakeWorld) SetSpawn(l api.Location)        { w.spawn = l }
func (w *fakeWorld) Difficulty() api.Difficulty     { return w.difficulty }
func (w *fakeWorld) SetDifficulty(d api.Difficulty) { w.difficulty = d }
func (w *fakeWorld) IsChunkLoaded(int, int) bool    { return true }
func (w *fakeWorld) Save() error                    { return nil }
func (w *fakeWorld) Block(api.BlockPos) api.Block   { return nil }
func (w *fakeWorld) GameRule(n string) (string, bool) {
	v, ok := w.rules[n]
	return v, ok
}
func (w *fakeWorld) SetGameRule(n, v string) bool {
	old, ok := w.rules[n]
	if !ok {
		return false
	}
	if (old == "true" || old == "false") && v != "true" && v != "false" {
		return false
	}
	w.rules[n] = v
	return true
}
func (w *fakeWorld) BlockState(p api.BlockPos) api.BlockState {
	if s, ok := w.blocks[p]; ok {
		return s
	}
	return w.server.blocks.Air()
}
func (w *fakeWorld) SetBlockState(p api.BlockPos, s api.BlockState, _ api.BlockUpdateFlags) bool {
	w.blocks[p] = s
	w.sets++
	return true
}
func (w *fakeWorld) Players() []api.Player {
	var out []api.Player
	for _, p := range w.server.players {
		if p.world == w {
			out = append(out, p)
		}
	}
	return out
}
func (w *fakeWorld) Entities() []api.Entity { return w.entities }
func (w *fakeWorld) Entity(id api.UUID) (api.Entity, bool) {
	for _, e := range w.entities {
		if e.UUID() == id {
			return e, true
		}
	}
	return nil, false
}
func (w *fakeWorld) DropItem(api.Vec3, api.ItemStack) (api.Entity, error) { return nil, nil }

type fakePerms struct {
	mu    sync.Mutex
	perms map[string]api.Permission
}

func (p *fakePerms) Register(perm api.Permission) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.perms[perm.Name] = perm
	return nil
}
func (p *fakePerms) Unregister(n string) {}
func (p *fakePerms) Get(n string) (api.Permission, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.perms[n]
	return v, ok
}
func (p *fakePerms) All() []api.Permission                      { return nil }
func (p *fakePerms) SetProvider(string, api.PermissionProvider) {}
func (p *fakePerms) Provider() api.PermissionProvider           { return nil }

type fakeBans struct{ m map[string]api.BanEntry }

func (b *fakeBans) Ban(e api.BanEntry) { b.m[e.Target] = e }
func (b *fakeBans) Pardon(t string) bool {
	_, ok := b.m[t]
	delete(b.m, t)
	return ok
}
func (b *fakeBans) Get(t string) (api.BanEntry, bool) { e, ok := b.m[t]; return e, ok }
func (b *fakeBans) Entries() []api.BanEntry {
	var out []api.BanEntry
	for _, e := range b.m {
		out = append(out, e)
	}
	return out
}

type fakeWhitelist struct {
	on      bool
	entries []api.GameProfile
}

func (w *fakeWhitelist) Enabled() bool         { return w.on }
func (w *fakeWhitelist) SetEnabled(v bool)     { w.on = v }
func (w *fakeWhitelist) Add(p api.GameProfile) { w.entries = append(w.entries, p) }
func (w *fakeWhitelist) Remove(id api.UUID) bool {
	n := len(w.entries)
	w.entries = slices.DeleteFunc(w.entries, func(p api.GameProfile) bool { return p.UUID == id })
	return n != len(w.entries)
}
func (w *fakeWhitelist) Contains(id api.UUID) bool {
	return slices.ContainsFunc(w.entries, func(p api.GameProfile) bool { return p.UUID == id })
}
func (w *fakeWhitelist) Entries() []api.GameProfile { return w.entries }

type fakeServer struct {
	api.Server
	players   []*fakePlayer
	worlds    []*fakeWorld
	console   *fakeSender
	blocks    *fakeBlocks
	perms     *fakePerms
	bans      *fakeBans
	ipBans    *fakeBans
	wl        *fakeWhitelist
	broadcast []string
	cmds      *Dispatcher
}

func newServer() *fakeServer {
	s := &fakeServer{
		console: &fakeSender{name: "Server"},
		blocks:  newBlocks(),
		perms:   &fakePerms{perms: map[string]api.Permission{}},
		bans:    &fakeBans{m: map[string]api.BanEntry{}},
		ipBans:  &fakeBans{m: map[string]api.BanEntry{}},
		wl:      &fakeWhitelist{},
	}
	w := &fakeWorld{name: "overworld", server: s, blocks: map[api.BlockPos]api.BlockState{}, seed: 42,
		rules: map[string]string{"keep_inventory": "false", "random_tick_speed": "3", "max_block_modifications": "32768"}}
	s.worlds = []*fakeWorld{w}
	s.cmds = New(s)
	return s
}

func (s *fakeServer) addPlayer(name string, pos api.Vec3, perms map[string]bool) *fakePlayer {
	var id api.UUID
	copy(id[:], fmt.Sprintf("%-16s", name))
	p := &fakePlayer{
		fs: &fakeSender{name: name, perms: perms}, id: id, world: s.worlds[0],
		loc:  api.Location{World: "overworld", Pos: pos},
		addr: netip.MustParseAddrPort(fmt.Sprintf("10.0.0.%d:25565", len(s.players)+1)),
		inv:  newInventory(),
	}
	s.players = append(s.players, p)
	return p
}

func (s *fakeServer) Name() string        { return "kremenmc" }
func (s *fakeServer) Version() string     { return "dev" }
func (s *fakeServer) GameVersion() string { return "26.3" }
func (s *fakeServer) Protocol() int32     { return 777 }
func (s *fakeServer) MaxPlayers() int     { return 20 }
func (s *fakeServer) TPS() float64        { return 20 }
func (s *fakeServer) Players() []api.Player {
	out := make([]api.Player, len(s.players))
	for i, p := range s.players {
		out[i] = p
	}
	return out
}
func (s *fakeServer) Player(id api.UUID) (api.Player, bool) {
	for _, p := range s.players {
		if p.id == id {
			return p, true
		}
	}
	return nil, false
}
func (s *fakeServer) PlayerByName(n string) (api.Player, bool) {
	for _, p := range s.players {
		if strings.EqualFold(p.fs.name, n) {
			return p, true
		}
	}
	return nil, false
}
func (s *fakeServer) Worlds() []api.World {
	out := make([]api.World, len(s.worlds))
	for i, w := range s.worlds {
		out[i] = w
	}
	return out
}
func (s *fakeServer) World(n string) (api.World, bool) {
	for _, w := range s.worlds {
		if w.name == n {
			return w, true
		}
	}
	return nil, false
}
func (s *fakeServer) DefaultWorld() api.World                          { return s.worlds[0] }
func (s *fakeServer) Broadcast(c api.Component)                        { s.broadcast = append(s.broadcast, c.Plain()) }
func (s *fakeServer) Console() api.CommandSender                       { return s.console }
func (s *fakeServer) Scheduler() api.Scheduler                         { return nil }
func (s *fakeServer) Commands() api.CommandRegistry                    { return s.cmds }
func (s *fakeServer) Permissions() api.Permissions                     { return s.perms }
func (s *fakeServer) Blocks() api.BlockRegistry                        { return s.blocks }
func (s *fakeServer) Items() api.ItemRegistry                          { return fakeItems{} }
func (s *fakeServer) Plugins() api.PluginRegistry                      { return nil }
func (s *fakeServer) ProfileBans() api.BanList                         { return s.bans }
func (s *fakeServer) IPBans() api.BanList                              { return s.ipBans }
func (s *fakeServer) Whitelist() api.Whitelist                         { return s.wl }
func (s *fakeServer) Logger() *slog.Logger                             { return slog.New(slog.DiscardHandler) }
func (s *fakeServer) OfflinePlayer(api.UUID) (api.OfflinePlayer, bool) { return nil, false }
