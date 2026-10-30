package bridge

// Wire protocol between kremenmc and the sidecar JVM. Mirrored in
// bridge/java/src/kremenmc/bridge/ipc/Wire.java; keep both in sync.
//
// Transport: unix domain socket (TCP loopback on Windows). The JVM connects
// and must send OpHello with the token from KREMENMC_BRIDGE_TOKEN as its first
// frame.
//
// Frame:
//
//	u32  length of the rest of the frame (big endian, <= MaxFrame)
//	u8   kind: KindCall | KindReply | KindError | KindNotify
//	u32  id   correlation id; a reply carries the id of its call; 0 for notify
//	u16  op   operation code; replies echo the op of the call
//	...  zero or more tagged values (the arguments or results)
//
// Tagged value: u8 tag followed by
//
//	TagNil, TagFalse, TagTrue  nothing
//	TagInt                     zigzag uvarint (int64)
//	TagFloat                   8 bytes IEEE-754 big endian (float64)
//	TagString                  uvarint length + UTF-8 bytes
//	TagBytes                   uvarint length + bytes
//	TagList                    uvarint count + values
//	TagMap                     uvarint count + (uvarint-length key bytes, value) pairs
//	TagUUID                    16 bytes, most significant first
//
// A KindError reply carries one string value: the error message.
//
// Maps used for structured data:
//
//	location: {world string, x, y, z, yaw, pitch float}
//	player:   {uuid UUID, entityId int, name string, displayName string(json), world string,
//	           x, y, z, yaw, pitch float, op bool, gameMode string, address string,
//	           ping int, properties [{name, value, signature string}]}
//	world:    {name string, uuid UUID, environment string, minY, maxY int,
//	           seed int, spawn location}
//	block position fields inside events: world string, x, y, z int.
const (
	ProtocolVersion = 1
	MaxFrame        = 16 << 20
)

const (
	KindCall   byte = 1
	KindReply  byte = 2
	KindError  byte = 3
	KindNotify byte = 4
)

const (
	TagNil    byte = 0
	TagFalse  byte = 1
	TagTrue   byte = 2
	TagInt    byte = 3
	TagFloat  byte = 4
	TagString byte = 5
	TagBytes  byte = 6
	TagList   byte = 7
	TagMap    byte = 8
	TagUUID   byte = 9
)

// Operations initiated by the JVM (handled by Go).
const (
	OpHello           uint16 = 1  // call   [token, protocolVersion, javaVersion] -> [serverInfo map]
	OpListeners       uint16 = 2  // notify [list of event names with at least one listener]
	OpPluginState     uint16 = 3  // notify [plugin name, state string, version]
	OpPlayers         uint16 = 10 // call   [] -> [list of player]
	OpPlayer          uint16 = 11 // call   [uuid | name] -> [player | nil]
	OpSendMessage     uint16 = 12 // notify [uuid | nil(console), component json]
	OpBroadcast       uint16 = 13 // call   [component json, permission ("" = all)] -> [recipients int]
	OpKick            uint16 = 14 // notify [uuid, reason json]
	OpTeleport        uint16 = 15 // call   [uuid, location] -> [ok bool]
	OpHasPermission   uint16 = 16 // call   [uuid, permission, java default ("" | true | false | op | notop)] -> [bool]
	OpSendPacket      uint16 = 17 // notify [uuid, packet kind, fields map]; see Packet* below
	OpWorlds          uint16 = 20 // call   [] -> [list of world]
	OpGetBlock        uint16 = 21 // call   [world, x, y, z] -> [block state string]
	OpSetBlock        uint16 = 22 // notify [world, x, y, z, block state string, physics bool]
	OpRegisterCommand uint16 = 30 // notify [plugin, name, aliases list, description, usage, permission, tree node | nil]
	OpDispatchCommand uint16 = 31 // call   [uuid | nil(console), command line without slash] -> [handled bool]
	OpQuery           uint16 = 50 // call   [kind, fields map] -> [result]; see Query* below
	OpAction          uint16 = 51 // notify [kind, fields map]; see Action* below
	OpPipeline        uint16 = 40 // notify [active bool]: whether a plugin hooked the listening channel
	OpPipeSend        uint16 = 41 // notify [uuid, body bytes | nil, marker int]: clientbound body, or a returned marker
	OpPipeRecv        uint16 = 42 // notify [uuid, body bytes]: serverbound body
)

// Operations initiated by Go (handled by the JVM).
const (
	OpLoadPlugins uint16 = 100 // call   [list of jar paths in load order, plugins dir] -> [list of {name, version, state}]
	OpEvent       uint16 = 101 // call   [event name, fields map, async bool] -> [cancelled bool, mutated fields map]
	OpTick        uint16 = 102 // notify [tick int]
	OpCommand     uint16 = 103 // call   [uuid | nil(console), command line without slash] -> [handled bool]
	OpTabComplete uint16 = 104 // call   [uuid | nil(console), partial command line] -> [list of strings]
	OpShutdown    uint16 = 105 // call   [] -> [] ; plugins are disabled, then the JVM exits
	OpPipeOpen    uint16 = 106 // call   [uuid, name, address, history [[clientbound bool, body bytes]]] -> [routed bool]
	OpPipeOut     uint16 = 107 // notify [uuid, body bytes | nil, marker int]
	OpPipeIn      uint16 = 108 // notify [uuid, body bytes]
	OpPipeClose   uint16 = 109 // notify [uuid]
	OpSuggest     uint16 = 110 // call   [uuid | nil(console), command line without slash] -> [start int, length int, [{text, tooltip json}]]
	OpCanUse      uint16 = 111 // call   [uuid | nil(console), command label] -> [bool]
)

// Command tree nodes sent with OpRegisterCommand for brigadier commands:
//
//	{k "lit" | "arg", n name, x executable bool, c [children], r redirect path [names] | nil,
//	 s ask-server suggestions bool, q has a requirement bool,
//	 p parser id, min, max float (absent = unbounded), mode int (string), single, players bool (entity),
//	 registry string}

// Routed pipelines: when a plugin hooks the listening channel, each player's
// packet bodies (VarInt ID + payload, protocol 777, decrypted and
// decompressed) go through their Java Netty channel: Go sends OpPipeOut and
// OpPipeIn, the pipeline's ends answer with OpPipeSend and OpPipeRecv. A
// marker (body nil) comes back after the packets queued before it.

// Event names used with OpEvent and OpListeners.
const (
	EventAsyncPreLogin     = "AsyncPlayerPreLogin" // name, uuid, address; mutated: result, kickMessage
	EventJoin              = "PlayerJoin"          // player uuid, joinMessage json; mutated: joinMessage
	EventQuit              = "PlayerQuit"          // player uuid, quitMessage json, reason; mutated: quitMessage
	EventChat              = "Chat"                // player uuid, message string; mutated: message json, format
	EventCommandPreprocess = "PlayerCommandPreprocess"
	EventMove              = "PlayerMove"     // player, from location, to location; mutated: to
	EventBlockBreak        = "BlockBreak"     // player, world, x, y, z, block; mutated: dropItems, expToDrop
	EventBlockPlace        = "BlockPlace"     // player, world, x, y, z, block, against {x,y,z}, hand
	EventInteract          = "PlayerInteract" // player, action, hand, face, block {world,x,y,z,block} | nil; mutated: useInteractedBlock, useItemInHand
)

// Query kinds for OpQuery: read-only lookups answered by Host.Query.
const (
	QuerySelect      = "select"       // sender uuid | nil, input string -> [uuid]
	QueryPlayerState = "player_state" // player -> {health, maxHealth, food, saturation, exp, level, flying, allowFlight}
	QueryTPS         = "tps"          // -> {tps, mspt float}
)

// Action kinds for OpAction: fire-and-forget mutations applied by Host.Action.
const (
	ActionPlayerSet = "player_set" // player, field (health|food|saturation|exp|gameMode|flying|allowFlight), value
	ActionTitle     = "title"      // player, title, subtitle json, fadeIn, stay, fadeOut ticks; clear bool
	ActionActionBar = "action_bar" // player, text json
	ActionSound     = "sound"      // player, name, category, volume, pitch, seed, x, y, z (absent = at listener)
)

// Packet kinds sent with OpSendPacket by the JVM's NMS compatibility layer.
// Components are JSON strings; nil means absent. Enum fields carry wire ids.
const (
	PacketTabList             = "tab_list"              // header, footer
	PacketPlayerInfoUpdate    = "player_info_update"    // actions bitmask, entries [{uuid, name, properties, listed, latency, gameMode, displayName, listOrder, showHat}]
	PacketPlayerInfoRemove    = "player_info_remove"    // uuids
	PacketSetObjective        = "set_objective"         // name, method, displayName, renderType, numberFormat {type, value} | nil
	PacketSetDisplayObjective = "set_display_objective" // slot, name
	PacketSetScore            = "set_score"             // owner, objective, score, displayName, numberFormat
	PacketResetScore          = "reset_score"           // owner, objective | nil (all objectives)
	PacketSetPlayerTeam       = "set_player_team"       // name, method, players, params {displayName, prefix, suffix, visibility, collision, color (-1 none), options} | nil
)
