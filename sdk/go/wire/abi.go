package wire

import "github.com/levitskyyy/KremenMC/api"

// ABIVersion is bumped on incompatible changes to exports, imports or layouts.
const ABIVersion = 1

// HostModule is the import module name for host functions.
const HostModule = "kremenmc"

// Guest exports.
const (
	ExportAlloc      = "kremenmc_alloc"       // (size u32) -> ptr u32
	ExportEnable     = "kremenmc_enable"      // () -> i32 status (0 ok)
	ExportDisable    = "kremenmc_disable"     // ()
	ExportOnEvent    = "kremenmc_on_event"    // (kind u32, ptr u32, len u32) -> u64 (ptr<<32 | len) of response, 0 for none
	ExportOnCommand  = "kremenmc_on_command"  // (handler u32, ptr u32, len u32) -> i32 result
	ExportOnTask     = "kremenmc_on_task"     // (task u32)
	ExportABIVersion = "kremenmc_abi_version" // () -> u32, optional
)

// Host imports (module "kremenmc"). Functions that return variable-length data
// return its length (or -1 when absent) and stage it; the guest then calls
// result(ptr) to copy it into a buffer of that length.
const (
	ImportLog             = "log"              // (level i32, ptr, len)
	ImportResult          = "result"           // (ptr)
	ImportSendMessage     = "send_message"     // (uuidPtr or 0 for console, msgPtr, msgLen) -> i32
	ImportBroadcast       = "broadcast"        // (msgPtr, msgLen)
	ImportPlayers         = "players"          // () -> len: count, PlayerRef...
	ImportPlayer          = "player"           // (uuidPtr) -> len: PlayerRef
	ImportPlayerByName    = "player_by_name"   // (ptr, len) -> len: PlayerRef
	ImportTeleport        = "teleport"         // (uuidPtr, locPtr, locLen) -> i32 (1 ok)
	ImportKick            = "kick"             // (uuidPtr, msgPtr, msgLen) -> i32
	ImportHasPermission   = "has_permission"   // (uuidPtr, ptr, len) -> i32
	ImportGetBlock        = "get_block"        // (worldPtr, worldLen, x, y, z i32) -> len: state string
	ImportSetBlock        = "set_block"        // (worldPtr, worldLen, x, y, z i32, statePtr, stateLen) -> i32
	ImportRegisterCommand = "register_command" // (ptr, len) -> i32
	// The low 16 bits of kind are the event kind; the high 16 bits are a guest
	// tag echoed back in kremenmc_on_event so one kind can have several listeners.
	ImportSubscribe       = "subscribe"        // (kind|tag u32, priority u32, ignoreCancelled u32) -> i32
	ImportSchedule        = "schedule"         // (task u32, delay i64, period i64, async u32) -> i32
	ImportCancelTask      = "cancel_task"      // (task u32)
	ImportDispatchCommand = "dispatch_command" // (uuidPtr or 0 for console, ptr, len) -> i32
	ImportKVGet           = "kv_get"           // (kPtr, kLen) -> len or -1
	ImportKVSet           = "kv_set"           // (kPtr, kLen, vPtr, vLen) -> i32
	ImportKVDelete        = "kv_delete"        // (kPtr, kLen) -> i32
	ImportWorlds          = "worlds"           // () -> len: strings
)

// Status codes returned by host functions.
const (
	OK          = 1
	Fail        = 0
	NotFound    = -1
	BadArgument = -2
)

type LogLevel int32

const (
	LogDebug LogLevel = -4
	LogInfo  LogLevel = 0
	LogWarn  LogLevel = 4
	LogError LogLevel = 8
)

// PlayerRef is the snapshot of a player passed across the boundary.
type PlayerRef struct {
	UUID     api.UUID
	Name     string
	EntityID int32
	Location api.Location
	GameMode api.GameMode
	Health   float64
	Op       bool
}

func (w *Writer) Player(p PlayerRef) {
	w.UUID(p.UUID)
	w.String(p.Name)
	w.Varint(int64(p.EntityID))
	w.Location(p.Location)
	w.U8(uint8(p.GameMode))
	w.F64(p.Health)
	w.Bool(p.Op)
}

func (r *Reader) Player() PlayerRef {
	return PlayerRef{
		UUID:     r.UUID(),
		Name:     r.String(),
		EntityID: int32(r.Varint()),
		Location: r.Location(),
		GameMode: api.GameMode(r.U8()),
		Health:   r.F64(),
		Op:       r.Bool(),
	}
}

type EntityRef struct {
	UUID     api.UUID
	EntityID int32
	Type     api.Key
	Location api.Location
}

func (w *Writer) Entity(e EntityRef) {
	w.UUID(e.UUID)
	w.Varint(int64(e.EntityID))
	w.String(string(e.Type))
	w.Location(e.Location)
}

func (r *Reader) Entity() EntityRef {
	return EntityRef{UUID: r.UUID(), EntityID: int32(r.Varint()), Type: api.Key(r.String()), Location: r.Location()}
}

type BlockRef struct {
	World string
	Pos   api.BlockPos
	State string
}

func (w *Writer) Block(b BlockRef) {
	w.String(b.World)
	w.BlockPos(b.Pos)
	w.String(b.State)
}

func (r *Reader) Block() BlockRef {
	return BlockRef{World: r.String(), Pos: r.BlockPos(), State: r.String()}
}

// Event responses start with a cancel byte followed by tagged mutations.
const (
	CancelUnchanged = 0
	CancelSet       = 1
	CancelClear     = 2
)

// Mutation tags in event responses.
const (
	MutMessage      = 1  // component: join/quit/kick-leave/death message, login deny message
	MutClearMessage = 2  // suppress the message
	MutText         = 3  // string: chat message, command line
	MutTo           = 4  // location: move/teleport destination, respawn location
	MutDamage       = 5  // f64
	MutLoginResult  = 6  // u8 result, component message
	MutDropItems    = 7  // bool
	MutExp          = 8  // varint
	MutKeepInv      = 9  // bool
	MutRecipients   = 10 // count, uuids
)

// Command tree node layout used by register_command:
//
//	kind u8, name str, [argument: parser str, flags u8, min f64, max f64,
//	stringMode u8, registry str], permission str, handler uvarint (0 = not
//	executable), suggestions strings, description str, children count, nodes...
//
// The root is prefixed by aliases (strings).
const (
	ArgHasMin      = 1 << 0
	ArgHasMax      = 1 << 1
	ArgSingle      = 1 << 2
	ArgPlayersOnly = 1 << 3
)

// Command invocation payload passed to kremenmc_on_command:
//
//	senderKind u8, sender name str, [player: uuid], input str, label str,
//	argument count, (name str, tag u8, value)...
const (
	SenderConsole = 0 // any non-player sender; the name says which
	SenderPlayer  = 1
)

const (
	ValString   = 1
	ValInt      = 2
	ValFloat    = 3
	ValBool     = 4
	ValUUIDs    = 5 // count, uuids
	ValBlockPos = 6
	ValVec3     = 7
)

// ManifestSection is the custom section name holding an embedded plugin.toml.
const ManifestSection = "kremenmc.plugin"

// EmbedManifest appends a custom section with the manifest to a wasm binary,
// replacing any existing one.
func EmbedManifest(bin, manifest []byte) ([]byte, error) {
	sections, err := wasmSections(bin)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), bin[:8]...)
	for _, s := range sections {
		if s.id == 0 && s.name == ManifestSection {
			continue
		}
		out = append(out, bin[s.start:s.end]...)
	}
	var body Writer
	body.String(ManifestSection)
	body.Raw(manifest)
	out = append(out, 0)
	out = binaryAppendUvarint(out, uint64(len(body.B)))
	return append(out, body.B...), nil
}

// ExtractManifest returns the embedded manifest, if any.
func ExtractManifest(bin []byte) ([]byte, bool, error) {
	sections, err := wasmSections(bin)
	if err != nil {
		return nil, false, err
	}
	for _, s := range sections {
		if s.id == 0 && s.name == ManifestSection {
			return bin[s.payload:s.end], true, nil
		}
	}
	return nil, false, nil
}

type section struct {
	id                  byte
	name                string
	start, payload, end int
}

var errNotWasm = errorString("wire: not a wasm binary")

type errorString string

func (e errorString) Error() string { return string(e) }

func wasmSections(bin []byte) ([]section, error) {
	if len(bin) < 8 || string(bin[:4]) != "\x00asm" {
		return nil, errNotWasm
	}
	var out []section
	off := 8
	for off < len(bin) {
		start := off
		id := bin[off]
		r := NewReader(bin[off+1:])
		size := r.Uvarint()
		if r.Err != nil || size > uint64(len(r.B)) {
			return nil, errNotWasm
		}
		body := len(bin) - len(r.B)
		end := body + int(size)
		s := section{id: id, start: start, payload: body, end: end}
		if id == 0 {
			nr := NewReader(bin[body:end])
			s.name = nr.String()
			if nr.Err != nil {
				return nil, errNotWasm
			}
			s.payload = end - len(nr.B)
		}
		out = append(out, s)
		off = end
	}
	return out, nil
}
