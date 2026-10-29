package kremenmc

import (
	"runtime"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// Player is a snapshot of a player taken when it was fetched or when the
// event fired. Methods act on the live player by UUID.
type Player struct{ wire.PlayerRef }

func uuidAddr(u *api.UUID) uint32 { return addr(u[:]) }

func (p Player) SendMessage(msg api.Component) bool {
	var w wire.Writer
	w.Component(msg)
	id := p.UUID
	ok := hostSendMessage(uuidAddr(&id), addr(w.B), uint32(len(w.B))) == wire.OK
	runtime.KeepAlive(w.B)
	return ok
}

func (p Player) Send(text string) bool { return p.SendMessage(api.Text(text)) }

func (p Player) Teleport(to api.Location) bool {
	var w wire.Writer
	w.Location(to)
	id := p.UUID
	return hostTeleport(uuidAddr(&id), addr(w.B), uint32(len(w.B))) == wire.OK
}

func (p Player) Kick(reason api.Component) bool {
	var w wire.Writer
	w.Component(reason)
	id := p.UUID
	return hostKick(uuidAddr(&id), addr(w.B), uint32(len(w.B))) == wire.OK
}

func (p Player) HasPermission(perm string) bool {
	id := p.UUID
	ptr, n := strAddr(perm)
	r := hostHasPermission(uuidAddr(&id), ptr, n) == 1
	runtime.KeepAlive(perm)
	return r
}

// Refresh fetches the current state; ok is false if the player left.
func (p Player) Refresh() (Player, bool) { return PlayerByUUID(p.UUID) }

// PerformCommand runs a command as this player.
func (p Player) PerformCommand(line string) int {
	id := p.UUID
	ptr, n := strAddr(line)
	r := hostDispatchCommand(uuidAddr(&id), ptr, n)
	runtime.KeepAlive(line)
	return int(r)
}

func Players() []Player {
	data, ok := fetch(hostPlayers())
	if !ok {
		return nil
	}
	r := wire.NewReader(data)
	n := r.Count()
	out := make([]Player, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		out = append(out, Player{r.Player()})
	}
	return out
}

func PlayerByUUID(id api.UUID) (Player, bool) {
	return decodePlayer(fetch(hostPlayer(uuidAddr(&id))))
}

func PlayerByName(name string) (Player, bool) {
	ptr, n := strAddr(name)
	res := hostPlayerByName(ptr, n)
	runtime.KeepAlive(name)
	return decodePlayer(fetch(res))
}

func decodePlayer(data []byte, ok bool) (Player, bool) {
	if !ok {
		return Player{}, false
	}
	r := wire.NewReader(data)
	p := Player{r.Player()}
	return p, r.Err == nil
}

func Broadcast(msg api.Component) {
	var w wire.Writer
	w.Component(msg)
	hostBroadcast(addr(w.B), uint32(len(w.B)))
}

// Console sends a message to the server console.
func Console(msg api.Component) {
	var w wire.Writer
	w.Component(msg)
	hostSendMessage(0, addr(w.B), uint32(len(w.B)))
}

// ConsoleCommand runs a command as the console.
func ConsoleCommand(line string) int {
	ptr, n := strAddr(line)
	r := hostDispatchCommand(0, ptr, n)
	runtime.KeepAlive(line)
	return int(r)
}

func Worlds() []string {
	data, ok := fetch(hostWorlds())
	if !ok {
		return nil
	}
	return wire.NewReader(data).Strings()
}

// BlockAt returns the block state string ("minecraft:stone") at pos. An empty
// world name means the default world.
func BlockAt(world string, pos api.BlockPos) (string, bool) {
	wp, wn := strAddr(world)
	res := hostGetBlock(wp, wn, int32(pos.X), int32(pos.Y), int32(pos.Z))
	runtime.KeepAlive(world)
	data, ok := fetch(res)
	return string(data), ok
}

// SetBlock places state (e.g. "minecraft:oak_log[axis=x]") at pos.
func SetBlock(world string, pos api.BlockPos, state string) bool {
	wp, wn := strAddr(world)
	sp, sn := strAddr(state)
	r := hostSetBlock(wp, wn, int32(pos.X), int32(pos.Y), int32(pos.Z), sp, sn) == wire.OK
	runtime.KeepAlive(world)
	runtime.KeepAlive(state)
	return r
}

// KV is the plugin's persistent key-value store.
var KV kvStore

type kvStore struct{}

func (kvStore) Get(key string) ([]byte, bool) {
	kp, kn := strAddr(key)
	res := hostKVGet(kp, kn)
	runtime.KeepAlive(key)
	return fetch(res)
}

func (kvStore) Set(key string, value []byte) bool {
	kp, kn := strAddr(key)
	r := hostKVSet(kp, kn, addr(value), uint32(len(value))) == wire.OK
	runtime.KeepAlive(key)
	runtime.KeepAlive(value)
	return r
}

func (kvStore) Delete(key string) bool {
	kp, kn := strAddr(key)
	r := hostKVDelete(kp, kn) == wire.OK
	runtime.KeepAlive(key)
	return r
}

// Task scheduling. Ticks are 50ms; async tasks run off the tick threads and
// must not touch world state directly.

type TaskID uint32

var (
	tasks    = map[TaskID]taskEntry{}
	nextTask TaskID
)

type taskEntry struct {
	fn        func()
	repeating bool
}

func schedule(delay, period api.Ticks, async bool, fn func()) TaskID {
	nextTask++
	id := nextTask
	tasks[id] = taskEntry{fn: fn, repeating: period > 0}
	a := uint32(0)
	if async {
		a = 1
	}
	if hostSchedule(uint32(id), int64(delay), int64(period), a) != wire.OK {
		delete(tasks, id)
		return 0
	}
	return id
}

func RunLater(delay api.Ticks, fn func()) TaskID { return schedule(delay, 0, false, fn) }
func RunTimer(delay, period api.Ticks, fn func()) TaskID {
	return schedule(delay, max(period, 1), false, fn)
}
func RunAsyncLater(delay api.Ticks, fn func()) TaskID { return schedule(delay, 0, true, fn) }
func RunAsyncTimer(delay, period api.Ticks, fn func()) TaskID {
	return schedule(delay, max(period, 1), true, fn)
}

func CancelTask(id TaskID) {
	delete(tasks, id)
	hostCancelTask(uint32(id))
}

func onTask(id uint32) {
	t, ok := tasks[TaskID(id)]
	if !ok {
		return
	}
	if !t.repeating {
		delete(tasks, TaskID(id))
	}
	guard("task", t.fn)
}
