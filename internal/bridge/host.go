package bridge

// Host is what the server provides to the bridge. Methods are called from
// IPC goroutines and must be safe for concurrent use. Text arguments named
// *JSON are chat components in JSON form.
//
// Plugins call back into the Host while Go waits in Bridge.FireEvent, so Host
// methods must not wait on the goroutine that fired the event (for example the
// tick goroutine); queue world mutations instead of blocking on them.
type Host interface {
	ServerInfo() ServerInfo
	Players() []PlayerInfo
	Player(id UUID) (PlayerInfo, bool)
	PlayerByName(name string) (PlayerInfo, bool)
	// SendMessage targets the console when target is the zero UUID.
	SendMessage(target UUID, componentJSON string)
	// Broadcast sends to every player holding permission ("" = everyone) and returns the count.
	Broadcast(componentJSON, permission string) int
	Kick(id UUID, reasonJSON string)
	Teleport(id UUID, to Location) bool
	HasPermission(id UUID, permission string) bool
	Worlds() []WorldInfo
	// Block returns a block state such as "minecraft:oak_log[axis=y]".
	Block(world string, x, y, z int) (state string, ok bool)
	SetBlock(world string, x, y, z int, state string, physics bool)
	// RegisterCommand makes a Java plugin command known to the server's
	// command tree; executing it should call Bridge.Command. Must be idempotent
	// because commands are re-registered after a JVM restart.
	RegisterCommand(cmd CommandSpec)
	// DispatchCommand runs a command line (no leading slash) as sender (zero UUID = console).
	DispatchCommand(sender UUID, line string) bool
}

type ServerInfo struct {
	Name             string
	Version          string
	MinecraftVersion string
	Protocol         int
	DataVersion      int
	Motd             string // component JSON
	MaxPlayers       int
	Port             int
	OnlineMode       bool
}

type Location struct {
	World      string
	X, Y, Z    float64
	Yaw, Pitch float32
}

func (l Location) Map() map[string]any {
	return map[string]any{"world": l.World, "x": l.X, "y": l.Y, "z": l.Z, "yaw": float64(l.Yaw), "pitch": float64(l.Pitch)}
}

func LocationFromMap(m map[string]any) Location {
	a := []any{m["world"], m["x"], m["y"], m["z"], m["yaw"], m["pitch"]}
	return Location{
		World: ArgString(a, 0),
		X:     ArgFloat(a, 1), Y: ArgFloat(a, 2), Z: ArgFloat(a, 3),
		Yaw: float32(ArgFloat(a, 4)), Pitch: float32(ArgFloat(a, 5)),
	}
}

type PlayerInfo struct {
	UUID        UUID
	Name        string
	DisplayName string // component JSON; empty means the plain name
	Location    Location
	Op          bool
	GameMode    string // SURVIVAL, CREATIVE, ADVENTURE, SPECTATOR
	Address     string // host:port
}

func (p PlayerInfo) Map() map[string]any {
	l := p.Location
	return map[string]any{
		"uuid": p.UUID, "name": p.Name, "displayName": p.DisplayName,
		"world": l.World, "x": l.X, "y": l.Y, "z": l.Z, "yaw": float64(l.Yaw), "pitch": float64(l.Pitch),
		"op": p.Op, "gameMode": p.GameMode, "address": p.Address,
	}
}

type WorldInfo struct {
	Name        string
	UUID        UUID
	Environment string // NORMAL, NETHER, THE_END
	MinY, MaxY  int
	Seed        int64
	Spawn       Location
}

func (w WorldInfo) Map() map[string]any {
	return map[string]any{
		"name": w.Name, "uuid": w.UUID, "environment": w.Environment,
		"minY": w.MinY, "maxY": w.MaxY, "seed": w.Seed, "spawn": w.Spawn.Map(),
	}
}

type CommandSpec struct {
	Plugin      string
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Permission  string
}

// Event is a server event forwarded to Java plugins. Fields follow the
// per-event layout documented next to the Event* constants in protocol.go.
type Event struct {
	Name   string
	Fields map[string]any
	// Async events (AsyncPlayerPreLogin, Chat) run on a Java worker thread and
	// are not subject to the sync latency budget.
	Async bool
}
